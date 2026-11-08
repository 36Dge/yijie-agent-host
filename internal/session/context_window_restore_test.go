package session

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/36Dge/yijie-agent-host/internal/codex"
	wire "github.com/36Dge/yijie-agent-host/internal/contracts/contextwindow"
)

type contextRestoreRuntime struct {
	fakeRuntime
	value      *codex.NativeContextUsage
	beforeRead func()
	reads      int
}

func (r *contextRestoreRuntime) ReadContextUsage(context.Context, string) (*codex.NativeContextUsage, error) {
	r.reads++
	if r.beforeRead != nil {
		r.beforeRead()
	}
	return r.value, nil
}
func restoredUsage() *codex.NativeContextUsage {
	capacity := int64(950000)
	return &codex.NativeContextUsage{ThreadID: testThreadID, TurnID: testTurnID, Model: "MiniMax-M3", WindowKey: testThreadID, ObservedAt: time.Date(2026, 11, 8, 8, 54, 33, 0, time.UTC), Usage: wire.ThreadTokenUsage{Last: wire.TokenUsageBreakdown{TotalTokens: 19638}, Total: wire.TokenUsageBreakdown{TotalTokens: 76542}, ModelContextWindow: &capacity}}
}
func coldContextService(t *testing.T, r *contextRestoreRuntime) *Service {
	t.Helper()
	store := openBoundFEAT134Store(t)
	_, err := store.update(testSessionID, func(v *Record) error {
		v.ModelProfile = "minimax-m3-high-v1"
		v.ModelRevision = 1
		v.LastTurnID = testTurnID
		v.LastTurnStatus = "completed"
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return NewService(r, store, NewEventHub(32, 8), nil)
}
func TestContextWindowColdReadRestoresExistingInitialModelWithoutInference(t *testing.T) {
	r := &contextRestoreRuntime{value: restoredUsage()}
	s := coldContextService(t, r)
	v := readContextTest(t, s)
	if v.Sample == nil || v.Sample.UsedTokens != 19638 || *v.Sample.CapacityTokens != 950000 || !v.Sample.ObservedAt.Equal(r.value.ObservedAt) {
		t.Fatal(v)
	}
	_ = readContextTest(t, s)
	if r.reads != 1 {
		t.Fatal("re-read native history on every poll")
	}
	// Normal Host object recreation, same durable Store and native history.
	reopened := NewService(r, s.store, NewEventHub(32, 8), nil)
	again := readContextTest(t, reopened)
	if again.Sample == nil || again.Sample.WindowId != v.Sample.WindowId || again.Sample.UsedTokens != v.Sample.UsedTokens {
		t.Fatal("normal reopen lost native statistics")
	}
}
func TestContextWindowRestoreCannotReviveOldModelRevision(t *testing.T) {
	for _, revision := range []int64{1, 3} {
		r := &contextRestoreRuntime{value: restoredUsage()}
		s := coldContextService(t, r)
		_, err := s.store.update(testSessionID, func(v *Record) error {
			v.ModelRevision = revision
			v.ModelSelectionOperation = testTurnOperationID
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		if readContextTest(t, s).Sample != nil {
			t.Fatal("same model name proved a later revision")
		}
	}
}
func TestContextWindowRestoreRejectsLateReadAfterLiveCompaction(t *testing.T) {
	r := &contextRestoreRuntime{value: restoredUsage()}
	s := coldContextService(t, r)
	r.beforeRead = func() {
		s.observeContextNotification(RuntimeNotificationItemStarted, rawJSON(t, map[string]any{"threadId": testThreadID, "turnId": testTurnID, "item": map[string]any{"type": "contextCompaction", "id": "ordinary-live-window"}}))
	}
	if readContextTest(t, s).Sample != nil {
		t.Fatal("late cold read replaced a live window invalidation")
	}
}
func TestContextWindowTurnRevisionProvenanceSurvivesStoreReopen(t *testing.T) {
	r := &contextRestoreRuntime{value: restoredUsage()}
	s := coldContextService(t, r)
	_, err := s.store.update(testSessionID, func(v *Record) error { v.ModelRevision = 3; return nil })
	if err != nil {
		t.Fatal(err)
	}
	digest := strings.Repeat("a", 64)
	op, _, err := s.store.prepareModelTurnOperation(testSessionID, testTurnOperationID, digest, TraceContext{}, "minimax-m3-high-v1")
	if err != nil {
		t.Fatal(err)
	}
	if op.ContextModelRevision != 3 {
		t.Fatal("missing private display provenance")
	}
	if _, err = s.store.AcceptTurnOperation(testSessionID, testTurnOperationID, digest, testTurnID); err != nil {
		t.Fatal(err)
	}
	if _, err = s.store.CompleteTurn(testSessionID, testTurnID, "completed"); err != nil {
		t.Fatal(err)
	}
	if readContextTest(t, s).Sample == nil {
		t.Fatal("measured current revision was not restored")
	}
	path := s.store.db.Path()
	if err = s.store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err := OpenStore(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	reopened := NewService(r, store, NewEventHub(32, 8), nil)
	if readContextTest(t, reopened).Sample == nil {
		t.Fatal("durable revision proof was lost")
	}
}
