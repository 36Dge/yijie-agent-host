package session

import (
	"context"
	"encoding/json"

	"github.com/36Dge/yijie-agent-host/internal/codex"
)

// Called while holding modelOperationMu. Cleanup is purpose-independent: it
// never resumes a thread, re-applies an expert, or starts a provider request.
func (s *Service) beginVerifiedCleanup(ctx context.Context, operationID, sessionID string) (CleanupOperation, error) {
	record, err := s.store.Get(sessionID)
	if err != nil {
		return CleanupOperation{}, err
	}
	if record.CleanupOperationID != "" {
		// A durable delete may already have removed the Runtime thread. Continue
		// that operation; do not require a new read of deleted history.
		return s.store.BeginCleanup(operationID, sessionID)
	}
	runtime, ok := s.runtime.(interface {
		ReadThreadStatus(context.Context, string) (string, error)
	})
	if !ok || !validUUID(record.CodexThreadID) {
		return s.store.BeginCleanup(operationID, sessionID)
	}
	status, err := runtime.ReadThreadStatus(ctx, record.CodexThreadID)
	if err != nil && !codex.IsThreadNotFound(err, record.CodexThreadID) {
		return CleanupOperation{}, ErrRuntimeRequest
	}
	if err == nil && status == "active" {
		threadID := record.CodexThreadID
		if err := s.interruptCleanupTurn(ctx, threadID); err != nil {
			return CleanupOperation{}, err
		}
		// The ordinary interrupt may have emitted a terminal notification. Take
		// a new record before the confirming status read and atomic comparison.
		record, err = s.store.Get(sessionID)
		if err != nil {
			return CleanupOperation{}, err
		}
		if record.CodexThreadID != threadID {
			return CleanupOperation{}, ErrTurnActive
		}
		status, err = runtime.ReadThreadStatus(ctx, record.CodexThreadID)
		if err != nil && !codex.IsThreadNotFound(err, record.CodexThreadID) {
			return CleanupOperation{}, ErrRuntimeRequest
		}
	}
	if ctx.Err() != nil {
		return CleanupOperation{}, ErrRuntimeRequest
	}
	if err == nil {
		switch status {
		case "idle", "notLoaded":
		case "active":
			return CleanupOperation{}, ErrTurnActive
		default:
			return CleanupOperation{}, ErrRuntimeRequest
		}
	}
	return s.store.beginCleanup(operationID, sessionID, &record)
}

func (s *Service) interruptCleanupTurn(ctx context.Context, threadID string) error {
	runtime, ok := s.runtime.(interface {
		ReadThread(context.Context, string) (json.RawMessage, error)
	})
	if !ok {
		return ErrTurnActive
	}
	raw, err := runtime.ReadThread(ctx, threadID)
	if err != nil {
		return ErrRuntimeRequest
	}
	var thread struct {
		ID    string           `json:"id"`
		Turns []nativeWireTurn `json:"turns"`
	}
	if len(raw) > 8<<20 || json.Unmarshal(raw, &thread) != nil || thread.ID != threadID || len(thread.Turns) == 0 {
		return ErrRuntimeRequest
	}
	turn := thread.Turns[len(thread.Turns)-1]
	if !validUUID(turn.ID) || normalizeTurnStatus(turn.Status) != "in_progress" {
		// The turn may have completed during the read. The next cleanup attempt
		// will observe that fact; do not choose a turn from cached Host state.
		return ErrTurnActive
	}
	for _, previous := range thread.Turns[:len(thread.Turns)-1] {
		if normalizeTurnStatus(previous.Status) == "in_progress" {
			return ErrRuntimeRequest
		}
	}
	s.cancelImageTurn(threadID, turn.ID)
	if err := s.runtime.InterruptTurn(ctx, threadID, turn.ID); err != nil {
		return ErrRuntimeRequest
	}
	return nil
}
