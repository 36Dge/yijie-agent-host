package session

import "context"

// Called only inside the existing owned-submission/model-operation lock, after
// scope, model, expert, pending-operation and approval admission checks. A cold
// thread status alone cannot clear an active turn: its exact native terminal
// fact is required. Reads never synthesize events or send/resume a turn.
func (s *Service) reconcileMarketIdle(ctx context.Context, record Record) (Record, error) {
	if record.State != StateActive || record.ActiveTurnID == "" || record.CleanupOperationID != "" {
		return record, ErrSessionNotUsable
	}
	history, err := s.ReadNativeThreadV2(ctx, record.AgentSessionID)
	if err != nil || history.ThreadId != record.CodexThreadID || len(history.Turns) == 0 || len(history.Turns) >= 1024 {
		return record, ErrSessionNotUsable
	}
	for _, turn := range history.Turns {
		if turn.Status == nil || normalizeTurnStatus(string(*turn.Status)) == "" || normalizeTurnStatus(string(*turn.Status)) == "in_progress" {
			return record, ErrSessionNotUsable
		}
	}
	last := history.Turns[len(history.Turns)-1]
	if last.Id != record.ActiveTurnID {
		return record, ErrSessionNotUsable
	}
	current, err := s.ReadNativeThreadStatusV2(ctx, record.AgentSessionID)
	if err != nil || current.ThreadId != record.CodexThreadID || (current.Status != "idle" && current.Status != "notLoaded" && current.Status != "systemError") {
		return record, ErrSessionNotUsable
	}
	s.notificationMu.Lock()
	defer s.notificationMu.Unlock()
	if ctx.Err() != nil || s.requireNoRuntimeApprovalForThread(record.CodexThreadID) != nil {
		return record, ErrSessionNotUsable
	}
	// Reuse normal resume persistence. Its revision guard preserves a notification
	// that arrived during the reads; the caller rechecks the returned idle state.
	return s.store.Resume(record.AgentSessionID, record.Trace, "", "", last.Id,
		normalizeTurnStatus(string(*last.Status)), record.NativeRevision)
}
