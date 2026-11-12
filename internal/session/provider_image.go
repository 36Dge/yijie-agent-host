package session

import (
	"context"
	"errors"

	"github.com/36Dge/yijie-agent-host/internal/codex"
	media "github.com/36Dge/yijie-agent-host/internal/contracts/mediageneration"
	hostimage "github.com/36Dge/yijie-agent-host/internal/contracts/runtimehostimages"
	"github.com/36Dge/yijie-agent-host/internal/imagegen"
)

type imageReference struct {
	mediaType string
	dataURL   string
}

type imageTurn struct {
	route            string
	sessionID        string
	turnID           string
	bound            chan struct{}
	references       []imageReference
	context          context.Context
	cancel           context.CancelFunc
	called           bool
	inputDigest      string
	result           *media.GenerateResult
	stopped          bool
	artifactID       string
	artifactItemID   string
	artifactFinished bool
}

func imageReferences(blocks []TurnContentBlock) []imageReference {
	references := make([]imageReference, 0, len(blocks))
	for _, block := range blocks {
		if block.Type == ContentBlockImage && block.Image != nil {
			references = append(references, imageReference{
				mediaType: block.Image.MediaType,
				dataURL:   block.Image.DataURL,
			})
		}
	}
	return references
}

func (s *Service) beginImageTurn(ctx context.Context, record Record, references []imageReference) error {
	if s.imageGenerator == nil {
		return nil
	}
	if s.eventsV3 == nil || s.artifacts == nil || record.CodexThreadID == "" {
		return ErrSessionNotUsable
	}
	turnContext, cancel := context.WithCancel(context.Background())
	s.imageMu.Lock()
	defer s.imageMu.Unlock()
	if _, exists := s.imageTurns[record.CodexThreadID]; exists {
		cancel()
		return ErrTurnActive
	}
	s.imageTurns[record.CodexThreadID] = &imageTurn{
		sessionID:  record.AgentSessionID,
		route:      codex.ImageRoute(ctx),
		references: append([]imageReference(nil), references...),
		context:    turnContext,
		cancel:     cancel,
		bound:      make(chan struct{}),
	}
	return nil
}

func (s *Service) bindImageTurn(threadID, turnID string) error {
	if s.imageGenerator == nil {
		return nil
	}
	s.imageMu.Lock()
	defer s.imageMu.Unlock()
	turn := s.imageTurns[threadID]
	if turn == nil {
		return nil
	}
	if turn.turnID != "" && turn.turnID != turnID {
		return errors.New("image turn identity conflict")
	}
	// The native terminal can beat the turn/start response. Store.BindTurn
	// already preserves that terminal; mirror it instead of retaining a turn
	// which can never dispatch or release its reference image.
	record, err := s.store.Get(turn.sessionID)
	if err != nil {
		return err
	}
	if record.ActiveTurnID == "" && record.LastTurnID == turnID && record.LastTurnStatus != "" {
		s.stopImageTurnLocked(turn)
		delete(s.imageTurns, threadID)
		return nil
	}
	if turn.turnID == "" {
		close(turn.bound)
	}
	turn.turnID = turnID
	return nil
}

func (s *Service) clearImageTurnForID(threadID, turnID string) {
	s.imageMu.Lock()
	defer s.imageMu.Unlock()
	if turn := s.imageTurns[threadID]; turn != nil && turn.turnID == turnID {
		s.stopImageTurnLocked(turn)
		delete(s.imageTurns, threadID)
	}
}

func (s *Service) clearImageTurn(threadID string) {
	s.imageMu.Lock()
	defer s.imageMu.Unlock()
	if turn := s.imageTurns[threadID]; turn != nil {
		s.stopImageTurnLocked(turn)
	}
	delete(s.imageTurns, threadID)
}

func (s *Service) clearImageSession(sessionID string) {
	s.imageMu.Lock()
	defer s.imageMu.Unlock()
	for threadID, turn := range s.imageTurns {
		if turn.sessionID == sessionID {
			s.stopImageTurnLocked(turn)
			delete(s.imageTurns, threadID)
		}
	}
}

func (s *Service) cancelImageTurn(threadID, turnID string) {
	s.imageMu.Lock()
	defer s.imageMu.Unlock()
	turn := s.imageTurns[threadID]
	if turn == nil {
		return
	}
	if turn.turnID == "" {
		// InterruptTurn may have read the old active turn before a terminal
		// and a new submission. An unbound image turn is not a wildcard.
		record, err := s.store.Get(turn.sessionID)
		if err != nil || record.ActiveTurnID != turnID {
			return
		}
	} else if turn.turnID != turnID {
		return
	}
	s.stopImageTurnLocked(turn)
}

// Close the published Artifact immediately when its owning turn ends. A slow
// credential/provider callback must not keep the UI in progress or publish a
// second terminal after cancellation. This reuses the existing failure event.
func (s *Service) stopImageTurnLocked(turn *imageTurn) {
	turn.stopped = true
	turn.cancel()
	for index := range turn.references {
		turn.references[index].dataURL = ""
	}
	if turn.artifactID != "" && !turn.artifactFinished {
		if record, err := s.store.Get(turn.sessionID); err == nil {
			s.finishImageFailureLocked(record, turn, "turn_interrupted", "Image delivery ended with the turn.")
		} else {
			turn.artifactFinished = true
		}
	}
}

func (s *Service) finishImageFailureLocked(record Record, turn *imageTurn, code, message string) {
	if turn.artifactID == "" || turn.artifactFinished {
		return
	}
	turn.artifactFinished = true
	_ = s.publishProviderImageFailure(record, turn.turnID, turn.artifactItemID, turn.artifactID, 0, code, false, message)
}

// Retired entry points cannot dispatch a provider, including direct internal callers.
func (s *Service) HandleDynamicToolCall(context.Context, codex.DynamicToolCall) codex.DynamicToolResult {
	return imageToolFailure("retired_image_entry")
}
func (s *Service) HandleHostImage(context.Context, hostimage.HostImageGenerateParams) bool {
	return false
}

func (s *Service) publishProviderImageFailure(record Record, turnID, itemID, artifactID string, ordinal int, code string, retryable bool, message string) error {
	return s.publishV3(record, Event{
		TurnID: turnID, ItemID: itemID, EventType: EventItemArtifactFailed,
		Payload: EventPayload{
			ArtifactID: artifactID, Kind: "image", Provenance: "provider", Status: "failed",
			Ordinal: &ordinal, ErrorCode: code, Retryable: &retryable, Message: &message,
		},
	})
}

func providerFailureCode(err error) string {
	switch imagegen.ErrorCode(err) {
	case imagegen.ErrorAuthentication, imagegen.ErrorBalance:
		return "unsupported_provider"
	case imagegen.ErrorInvalidResponse:
		return "integrity_failed"
	case imagegen.ErrorRateLimited:
		return "limit_exceeded"
	case imagegen.ErrorTimeout, imagegen.ErrorUnavailable:
		return "resource_unavailable"
	case imagegen.ErrorRequestCanceled:
		return "turn_interrupted"
	default:
		return "generation_failed"
	}
}

func imageToolFailure(code string) codex.DynamicToolResult {
	return codex.DynamicToolResult{Success: false, Text: "Image generation failed (" + code + ")."}
}
