package session

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"

	"github.com/36Dge/yijie-agent-host/internal/artifact"
	"github.com/36Dge/yijie-agent-host/internal/codex"
	"github.com/36Dge/yijie-agent-host/internal/imagegen"
)

const maxImageToolArguments = 8 << 10

type imageReference struct {
	mediaType string
	dataURL   string
}

type imageTurn struct {
	sessionID  string
	turnID     string
	references []imageReference
	context    context.Context
	cancel     context.CancelFunc
	called     bool
}

type imageToolArguments struct {
	Prompt      string `json:"prompt"`
	Mode        string `json:"mode"`
	AspectRatio string `json:"aspect_ratio,omitempty"`
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

func (s *Service) beginImageTurn(record Record, references []imageReference) error {
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
		references: append([]imageReference(nil), references...),
		context:    turnContext,
		cancel:     cancel,
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
	turn.turnID = turnID
	return nil
}

func (s *Service) clearImageTurn(threadID string) {
	s.imageMu.Lock()
	turn := s.imageTurns[threadID]
	delete(s.imageTurns, threadID)
	s.imageMu.Unlock()
	if turn != nil {
		turn.cancel()
		for index := range turn.references {
			turn.references[index].dataURL = ""
		}
	}
}

func (s *Service) clearImageSession(sessionID string) {
	s.imageMu.Lock()
	turns := make([]*imageTurn, 0, 1)
	for threadID, turn := range s.imageTurns {
		if turn.sessionID == sessionID {
			turns = append(turns, turn)
			delete(s.imageTurns, threadID)
		}
	}
	s.imageMu.Unlock()
	for _, turn := range turns {
		turn.cancel()
		for index := range turn.references {
			turn.references[index].dataURL = ""
		}
	}
}

func (s *Service) cancelImageTurn(threadID, turnID string) {
	s.imageMu.Lock()
	turn := s.imageTurns[threadID]
	if turn != nil && (turn.turnID == "" || turn.turnID == turnID) {
		turn.cancel()
	}
	s.imageMu.Unlock()
}

// HandleDynamicToolCall is the closed Host-owned implementation of the
// Runtime item/tool/call reverse request. Provider content never returns to
// Runtime; only a short, stable outcome string does.
func (s *Service) HandleDynamicToolCall(callContext context.Context, call codex.DynamicToolCall) codex.DynamicToolResult {
	if s.imageGenerator == nil || call.Tool != codex.DynamicToolGenerateImage ||
		requireUUID("codex_thread_id", call.ThreadID) != nil || requireUUID("turn_id", call.TurnID) != nil ||
		call.CallID == "" || len(call.CallID) > 256 {
		return imageToolFailure("protocol_error")
	}

	s.imageMu.Lock()
	turn := s.imageTurns[call.ThreadID]
	if turn == nil || turn.called || (turn.turnID != "" && turn.turnID != call.TurnID) {
		s.imageMu.Unlock()
		return imageToolFailure("protocol_error")
	}
	turn.turnID = call.TurnID
	turn.called = true
	sessionID := turn.sessionID
	references := append([]imageReference(nil), turn.references...)
	turnContext := turn.context
	s.imageMu.Unlock()

	record, err := s.store.GetByThread(call.ThreadID)
	if err != nil || record.AgentSessionID != sessionID || record.ActiveTurnID != call.TurnID {
		return imageToolFailure("protocol_error")
	}
	arguments, err := decodeImageToolArguments(call.Arguments)
	if err != nil {
		return imageToolFailure("generation_failed")
	}
	request := imagegen.Request{
		Prompt: arguments.Prompt, Mode: arguments.Mode, AspectRatio: arguments.AspectRatio,
	}
	if arguments.Mode == imagegen.ModeSubject {
		if len(references) != 1 || (references[0].mediaType != "image/png" && references[0].mediaType != "image/jpeg") {
			return imageToolFailure("generation_failed")
		}
		request.ReferenceDataURL = references[0].dataURL
	}

	artifactID, err := newUUID()
	if err != nil {
		return imageToolFailure("resource_unavailable")
	}
	itemID := "provider-image-" + artifactID
	ordinal := 0
	displayName := imagegen.DisplayName("image/png")
	if err := s.publishV3(record, Event{
		TurnID: call.TurnID, ItemID: itemID, EventType: EventItemArtifactStarted,
		Payload: EventPayload{
			ArtifactID: artifactID, Kind: "image", Provenance: "provider", Status: "in_progress",
			Ordinal: &ordinal, DisplayName: displayName,
		},
	}); err != nil {
		return imageToolFailure("resource_unavailable")
	}
	progress := 10
	if err := s.publishV3(record, Event{
		TurnID: call.TurnID, ItemID: itemID, EventType: EventItemArtifactProgress,
		Payload: EventPayload{
			ArtifactID: artifactID, Kind: "image", Provenance: "provider", Status: "in_progress",
			Ordinal: &ordinal, Stage: "generating", Progress: &progress,
		},
	}); err != nil {
		return imageToolFailure("resource_unavailable")
	}

	providerContext, cancel := context.WithCancel(turnContext)
	stop := context.AfterFunc(callContext, cancel)
	result, generateErr := s.imageGenerator.Generate(providerContext, request)
	stop()
	cancel()
	if generateErr != nil {
		failureCode := providerFailureCode(generateErr)
		retryable := imagegen.Retryable(generateErr)
		_ = s.publishProviderImageFailure(record, call.TurnID, itemID, artifactID, ordinal, failureCode, retryable)
		return imageToolFailure(failureCode)
	}
	displayName = imagegen.DisplayName(result.MediaType)
	if err := s.artifacts.Put(artifact.Manifest{
		SessionID: sessionID, ArtifactID: artifactID, Kind: "image", DisplayName: displayName,
		MediaType: result.MediaType, Content: result.Bytes,
	}); err != nil {
		failureCode := artifactFailureCode(err)
		retryable := failureCode == "resource_unavailable"
		_ = s.publishProviderImageFailure(record, call.TurnID, itemID, artifactID, ordinal, failureCode, retryable)
		return imageToolFailure(failureCode)
	}

	digest := sha256.Sum256(result.Bytes)
	size := int64(len(result.Bytes))
	baseHref := fmt.Sprintf("/v3/agent-sessions/%s/artifacts/%s", sessionID, artifactID)
	if err := s.publishV3(record, Event{
		TurnID: call.TurnID, ItemID: itemID, EventType: EventItemArtifactCompleted,
		Payload: EventPayload{
			ArtifactID: artifactID, Kind: "image", Provenance: "provider", Status: "ready",
			Ordinal: &ordinal, DisplayName: displayName, MediaType: result.MediaType,
			SizeBytes: &size, SHA256: fmt.Sprintf("%x", digest), ContentHref: baseHref + "/content",
		},
	}); err != nil {
		return imageToolFailure("resource_unavailable")
	}
	return codex.DynamicToolResult{Success: true, Text: "Image generation succeeded and the image was published in the conversation."}
}

func decodeImageToolArguments(raw json.RawMessage) (imageToolArguments, error) {
	if len(raw) == 0 || len(raw) > maxImageToolArguments {
		return imageToolArguments{}, errors.New("invalid image tool arguments")
	}
	var arguments imageToolArguments
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&arguments); err != nil || decoder.Decode(&struct{}{}) != io.EOF {
		return imageToolArguments{}, errors.New("invalid image tool arguments")
	}
	if !utf8.ValidString(arguments.Prompt) || strings.TrimSpace(arguments.Prompt) == "" ||
		utf8.RuneCountInString(arguments.Prompt) > 1500 || strings.ContainsRune(arguments.Prompt, '\x00') {
		return imageToolArguments{}, errors.New("invalid image prompt")
	}
	if arguments.Mode != imagegen.ModeTextToImage && arguments.Mode != imagegen.ModeSubject {
		return imageToolArguments{}, errors.New("invalid image mode")
	}
	if arguments.AspectRatio == "" {
		arguments.AspectRatio = "1:1"
	}
	switch arguments.AspectRatio {
	case "1:1", "16:9", "4:3", "3:2", "2:3", "3:4", "9:16", "21:9":
	default:
		return imageToolArguments{}, errors.New("invalid image aspect ratio")
	}
	return arguments, nil
}

func (s *Service) publishProviderImageFailure(record Record, turnID, itemID, artifactID string, ordinal int, code string, retryable bool) error {
	message := "Image generation failed."
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
