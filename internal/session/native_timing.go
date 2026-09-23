package session

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	timing "github.com/36Dge/yijie-agent-host/internal/contracts/nativetiming"
	"github.com/google/uuid"
)

// NativeTimingError contains only a closed transport diagnostic, never raw history.
type NativeTimingError struct{ Code timing.TimingErrorCode }

func (e NativeTimingError) Error() string { return string(e.Code) }

func timingFailure(code timing.TimingErrorCode) error { return NativeTimingError{Code: code} }

func timingNumber(raw json.RawMessage, minimum, maximum int64) (timing.TimeFieldState, *int64) {
	if len(raw) == 0 || string(raw) == "null" {
		return timing.Unknown, nil
	}
	var value int64
	if json.Unmarshal(raw, &value) != nil || value < minimum || value > maximum {
		return timing.Invalid, nil
	}
	return timing.Known, &value
}

// ReadNativeTurnTiming uses the same managed stable history reader. It does not
// resume a thread, modify the Store, or infer lifecycle from wall-clock values.
func (s *Service) ReadNativeTurnTiming(ctx context.Context, sessionID, turnID string) (timing.NativeTurnTiming, error) {
	var empty timing.NativeTurnTiming
	canonical := func(value string) bool {
		id, err := uuid.Parse(value)
		return err == nil && id != uuid.Nil && id.String() == value
	}
	if !canonical(sessionID) || !canonical(turnID) {
		return empty, timingFailure(timing.InvalidRequest)
	}
	record, err := s.store.Get(sessionID)
	if errors.Is(err, ErrNotFound) {
		return empty, timingFailure(timing.SessionNotFound)
	}
	if err != nil || !canonical(record.CodexThreadID) {
		return empty, timingFailure(timing.NativeTimingUnavailable)
	}
	runtime, ok := s.runtime.(interface {
		ReadThread(context.Context, string) (json.RawMessage, error)
	})
	if !ok {
		return empty, timingFailure(timing.NativeTimingUnavailable)
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	raw, err := runtime.ReadThread(ctx, record.CodexThreadID)
	if errors.Is(ctx.Err(), context.DeadlineExceeded) || errors.Is(err, context.DeadlineExceeded) {
		return empty, timingFailure(timing.NativeTimingTimeout)
	}
	if err != nil || ctx.Err() != nil {
		return empty, timingFailure(timing.NativeTimingUnavailable)
	}
	if len(raw) > 8<<20 {
		return empty, timingFailure(timing.NativeTimingLimitExceeded)
	}
	var wire struct {
		ID    string `json:"id"`
		Turns []struct {
			ID          string          `json:"id"`
			StartedAt   json.RawMessage `json:"startedAt"`
			CompletedAt json.RawMessage `json:"completedAt"`
			DurationMs  json.RawMessage `json:"durationMs"`
		} `json:"turns"`
	}
	if json.Unmarshal(raw, &wire) != nil {
		return empty, timingFailure(timing.NativeTimingUnavailable)
	}
	if wire.ID != record.CodexThreadID {
		return empty, timingFailure(timing.NativeTimingIdentityMismatch)
	}
	if len(wire.Turns) > 1024 {
		return empty, timingFailure(timing.NativeTimingLimitExceeded)
	}
	out := timing.NativeTurnTiming{SchemaVersion: timing.N1, Source: timing.RuntimeRead,
		AgentSessionId: uuid.MustParse(sessionID), ThreadId: uuid.MustParse(record.CodexThreadID), TurnId: uuid.MustParse(turnID)}
	found := false
	for _, turn := range wire.Turns {
		if turn.ID != turnID {
			continue
		}
		if found {
			return empty, timingFailure(timing.NativeTimingIdentityMismatch)
		}
		found = true
		out.StartedAt.State, out.StartedAt.Value = timingNumber(turn.StartedAt, -8640000000000, 8640000000000)
		out.CompletedAt.State, out.CompletedAt.Value = timingNumber(turn.CompletedAt, -8640000000000, 8640000000000)
		out.DurationMs.State, out.DurationMs.Value = timingNumber(turn.DurationMs, 0, 9007199254740991)
	}
	if !found {
		return empty, timingFailure(timing.TurnNotFound)
	}
	return out, nil
}
