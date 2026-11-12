package session

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/36Dge/yijie-agent-host/internal/artifact"
	media "github.com/36Dge/yijie-agent-host/internal/contracts/mediageneration"
	"github.com/36Dge/yijie-agent-host/internal/imagegen"
)

func imageResult(code media.GenerateCode) media.GenerateResult {
	result := media.GenerateResult{Source: "builtin", Status: media.GenerateStatusBlocked, Code: code, NextAction: media.NextActionReportFailure}
	switch code {
	case media.GenerateCodeExpertRequired:
		result.NextAction = media.NextActionUseExpert
	case media.GenerateCodeOutcomeUnknown:
		result.Status = media.GenerateStatusUnknown
	case media.GenerateCodeGenerationFailed, media.GenerateCodeContentRejected:
		result.Status = media.GenerateStatusFailed
	}
	return result
}

// GenerateImage is the only active builtin image producer. It accepts the
// generated MCP bridge contract, never legacy DynamicToolCall/HostImage RPCs.
func (s *Service) GenerateImage(ctx context.Context, req media.GenerateRequest) media.GenerateResult {
	if req.Validate() != nil || requireUUID("thread_id", req.ThreadId) != nil || requireUUID("turn_id", req.TurnId) != nil {
		return imageResult(media.GenerateCodeInputUnsupported)
	}
	if s.imageGenerator == nil || s.artifacts == nil {
		return imageResult(media.GenerateCodeServiceUnavailable)
	}
	if ctx.Err() != nil {
		return imageResult(media.GenerateCodeCancelled)
	}
	record, err := s.store.GetByThread(req.ThreadId)
	if err != nil || record.Purpose == purposeDraft || record.CleanupOperationID != "" {
		return imageResult(media.GenerateCodeCancelled)
	}
	// The persisted expert selection is authoritative even if a stale tool
	// definition remains in native conversation history.
	binding, bindingErr := s.store.expertBinding(record.AgentSessionID)
	if bindingErr != nil && !errors.Is(bindingErr, ErrNotFound) {
		return imageResult(media.GenerateCodeServiceUnavailable)
	}
	if binding.Expert != nil || binding.PendingOperation != "" {
		return imageResult(media.GenerateCodeExpertRequired)
	}
	if code := s.awaitImageTurnBinding(ctx, record.AgentSessionID, req.ThreadId, req.TurnId); code != "" {
		return imageResult(code)
	}
	record, err = s.store.GetByThread(req.ThreadId)
	if err != nil || record.ActiveTurnID != req.TurnId || record.CleanupOperationID != "" {
		return imageResult(media.GenerateCodeCancelled)
	}
	encoded, _ := json.Marshal(req.Input)
	digest := fmt.Sprintf("%x", sha256.Sum256(encoded))
	s.imageMu.Lock()
	turn := s.imageTurns[req.ThreadId]
	if turn == nil || turn.sessionID != record.AgentSessionID || turn.turnID != req.TurnId || turn.stopped || turn.context.Err() != nil {
		s.imageMu.Unlock()
		return imageResult(media.GenerateCodeCancelled)
	}
	if turn.route != "builtin" {
		s.imageMu.Unlock()
		return imageResult(media.GenerateCodeExpertRequired)
	}
	if turn.called {
		result := imageResult(media.GenerateCodeStatePending)
		result.NextAction = media.NextActionReportFailure
		if digest != turn.inputDigest {
			result = imageResult(media.GenerateCodeOperationConflict)
		} else if turn.result != nil {
			result = *turn.result
		}
		s.imageMu.Unlock()
		return s.refreshImageReceipt(record.AgentSessionID, result)
	}
	if len(turn.references) > 1 || (req.Input.UseCurrentReference && (len(turn.references) != 1 || (turn.references[0].mediaType != "image/png" && turn.references[0].mediaType != "image/jpeg"))) {
		s.imageMu.Unlock()
		return imageResult(media.GenerateCodeInputUnsupported)
	}
	request := imagegen.Request{Prompt: req.Input.Prompt, Mode: imagegen.ModeTextToImage}
	if req.Input.UseCurrentReference {
		request.Mode = imagegen.ModeSubject
		request.ReferenceDataURL = turn.references[0].dataURL
	}
	turn.called, turn.inputDigest = true, digest
	turnContext := turn.context
	s.imageMu.Unlock()

	result := s.executeBuiltinImage(ctx, turnContext, record, req.TurnId, request, turn)
	return s.completeImageCall(req.ThreadId, turn, result)
}

func (s *Service) completeImageCall(threadID string, turn *imageTurn, result media.GenerateResult) media.GenerateResult {
	s.imageMu.Lock()
	if s.imageTurns[threadID] == turn {
		// executeBuiltinImage arbitrates cancellation against publication under
		// imageMu. A later stop cannot rewrite an already delivered receipt.
		turn.result = &result
	}
	s.imageMu.Unlock()
	return s.refreshImageReceipt(turn.sessionID, result)
}

// The MCP HTTP connection is independent of native notifications/RPC replies.
// Wait only for an operation already prepared by Host, never bind from model
// metadata. Native binding, cancellation, and the bounded timeout settle it.
func (s *Service) awaitImageTurnBinding(ctx context.Context, sessionID, threadID, turnID string) media.GenerateCode {
	s.imageMu.Lock()
	turn := s.imageTurns[threadID]
	if turn == nil || turn.sessionID != sessionID || turn.stopped || turn.context.Err() != nil {
		s.imageMu.Unlock()
		return media.GenerateCodeCancelled
	}
	bound, turnContext, id := turn.bound, turn.context, turn.turnID
	s.imageMu.Unlock()
	if id != "" {
		if id != turnID {
			return media.GenerateCodeCancelled
		}
		return ""
	}
	timer := time.NewTimer(media.BindingWaitMilliseconds * time.Millisecond)
	defer timer.Stop()
	select {
	case <-bound:
		return "" // caller revalidates the persisted/native identity under lock
	case <-ctx.Done():
		return media.GenerateCodeCancelled
	case <-turnContext.Done():
		return media.GenerateCodeCancelled
	case <-timer.C:
		return media.GenerateCodeStatePending
	}
}

func (s *Service) executeBuiltinImage(ctx, turnContext context.Context, record Record, turnID string, request imagegen.Request, turn *imageTurn) media.GenerateResult {
	artifactID, err := newUUID()
	if err != nil {
		return imageResult(media.GenerateCodeServiceUnavailable)
	}
	itemID, ordinal := "provider-image-"+artifactID, 0
	s.imageMu.Lock()
	if s.imageTurns[record.CodexThreadID] != turn || turn.stopped || turnContext.Err() != nil || ctx.Err() != nil {
		s.imageMu.Unlock()
		return imageResult(media.GenerateCodeCancelled)
	}
	if s.publishV3(record, Event{TurnID: turnID, ItemID: itemID, EventType: EventItemArtifactStarted, Payload: EventPayload{ArtifactID: artifactID, Kind: "image", Provenance: "provider", Status: "in_progress", Ordinal: &ordinal, DisplayName: imagegen.DisplayName("image/png")}}) != nil {
		s.imageMu.Unlock()
		return imageResult(media.GenerateCodeServiceUnavailable)
	}
	turn.artifactID, turn.artifactItemID = artifactID, itemID
	s.imageMu.Unlock()
	providerContext, cancel := context.WithCancel(turnContext)
	stop := context.AfterFunc(ctx, cancel)
	content, generateErr := s.imageGenerator.Generate(providerContext, request)
	stop()
	interrupted := providerContext.Err() != nil || ctx.Err() != nil
	cancel()
	if interrupted {
		generateErr = &imagegen.ProviderError{Code: imagegen.ErrorRequestCanceled}
	}
	if generateErr != nil {
		s.imageMu.Lock()
		s.finishImageFailureLocked(record, turn, providerFailureCode(generateErr), imagegen.Diagnostic(generateErr))
		s.imageMu.Unlock()
		switch imagegen.ErrorCode(generateErr) {
		case imagegen.ErrorAuthentication, imagegen.ErrorBalance:
			return imageResult(media.GenerateCodeServiceUnavailable)
		case imagegen.ErrorRequestCanceled:
			return imageResult(media.GenerateCodeCancelled)
		case imagegen.ErrorContentRejected:
			return imageResult(media.GenerateCodeContentRejected)
		case imagegen.ErrorTimeout, imagegen.ErrorUnavailable, imagegen.ErrorInvalidResponse:
			return imageResult(media.GenerateCodeOutcomeUnknown)
		default:
			return imageResult(media.GenerateCodeGenerationFailed)
		}
	}
	s.imageMu.Lock()
	defer s.imageMu.Unlock()
	if s.imageTurns[record.CodexThreadID] != turn || turn.stopped || turnContext.Err() != nil || ctx.Err() != nil {
		s.finishImageFailureLocked(record, turn, "turn_interrupted", "Image delivery was interrupted.")
		return imageResult(media.GenerateCodeCancelled)
	}
	name := imagegen.DisplayName(content.MediaType)
	if err = s.artifacts.Put(artifact.Manifest{SessionID: record.AgentSessionID, ArtifactID: artifactID, Kind: "image", DisplayName: name, MediaType: content.MediaType, Content: content.Bytes}); err != nil {
		s.finishImageFailureLocked(record, turn, artifactFailureCode(err), "Generated image delivery is unavailable.")
		return imageResult(media.GenerateCodeOutcomeUnknown)
	}
	hash, size := sha256.Sum256(content.Bytes), int64(len(content.Bytes))
	turn.artifactFinished = true
	err = s.publishV3(record, Event{TurnID: turnID, ItemID: itemID, EventType: EventItemArtifactCompleted, Payload: EventPayload{
		ArtifactID: artifactID, Kind: "image", Provenance: "provider", Status: "ready", Ordinal: &ordinal,
		DisplayName: name, MediaType: content.MediaType, SizeBytes: &size, SHA256: fmt.Sprintf("%x", hash),
		ContentHref: fmt.Sprintf("/v3/agent-sessions/%s/artifacts/%s/content", record.AgentSessionID, artifactID),
	}})
	if err != nil {
		return imageResult(media.GenerateCodeOutcomeUnknown)
	}
	return media.GenerateResult{Status: media.GenerateStatusStaged, Code: media.GenerateCodeOk, Source: "builtin", ArtifactId: &artifactID, NextAction: media.NextActionCheckArtifact}
}

func (s *Service) refreshImageReceipt(session string, result media.GenerateResult) media.GenerateResult {
	if result.Code == media.GenerateCodeOk && result.ArtifactId != nil && s.artifacts.Status(session, *result.ArtifactId) == "committed" {
		result.Status = media.GenerateStatusCommitted
	}
	return result
}
