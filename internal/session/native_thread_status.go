package session

import (
	"context"

	native "github.com/36Dge/yijie-agent-host/internal/contracts/nativeconversationv2"
)

// ReadNativeThreadStatusV2 is a transient native status observation, independent
// of history projection and local delivery state. It never persists the read.
func (s *Service) ReadNativeThreadStatusV2(ctx context.Context, sessionID string) (native.NativeThreadStatusSnapshot, error) {
	if err := requireUUID("agent_session_id", sessionID); err != nil {
		return native.NativeThreadStatusSnapshot{}, err
	}
	record, err := s.store.Get(sessionID)
	if err != nil {
		return native.NativeThreadStatusSnapshot{}, err
	}
	if record.CodexThreadID == "" || len(record.CodexThreadID) > 512 {
		return native.NativeThreadStatusSnapshot{}, ErrSessionNotUsable
	}
	runtime, ok := s.runtime.(interface {
		ReadThreadStatus(context.Context, string) (string, error)
	})
	if !ok {
		return native.NativeThreadStatusSnapshot{}, ErrSessionNotUsable
	}
	value, err := runtime.ReadThreadStatus(ctx, record.CodexThreadID)
	if err != nil {
		return native.NativeThreadStatusSnapshot{}, ErrRuntimeRequest
	}
	status := native.NativeThreadStatusSnapshotStatus(value)
	if !status.Valid() {
		return native.NativeThreadStatusSnapshot{}, ErrRuntimeRequest
	}
	return native.NativeThreadStatusSnapshot{
		SchemaVersion: native.NativeThreadStatusSnapshotSchemaVersionN2,
		Source:        native.NativeThreadStatusSnapshotSourceRuntimeRead,
		ThreadId:      record.CodexThreadID,
		Status:        status,
	}, nil
}
