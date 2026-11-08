package session

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/36Dge/yijie-agent-host/internal/codex"
	wire "github.com/36Dge/yijie-agent-host/internal/contracts/contextwindow"
)

func contextWindowTestService(t *testing.T) *Service {
	t.Helper()
	s := NewService(&fakeRuntime{}, openBoundFEAT134Store(t), NewEventHub(32, 8), nil)
	_, err := s.store.update(testSessionID, func(r *Record) error { r.ModelProfile = "minimax-m3-high-v1"; r.ModelRevision = 1; return nil })
	if err != nil {
		t.Fatal(err)
	}
	s.observeContextNotification(RuntimeNotificationTurnStarted, rawJSON(t, map[string]any{"threadId": testThreadID, "turn": map[string]any{"id": testTurnID}}))
	return s
}

func contextUsageJSON(t *testing.T, turn string, used, cumulative int64, capacity any) json.RawMessage {
	t.Helper()
	counts := func(n int64) map[string]any {
		return map[string]any{"totalTokens": n, "inputTokens": n, "outputTokens": 0, "cachedInputTokens": 0, "reasoningOutputTokens": 0}
	}
	return rawJSON(t, map[string]any{"threadId": testThreadID, "turnId": turn, "tokenUsage": map[string]any{"last": counts(used), "total": counts(cumulative), "modelContextWindow": capacity}})
}

func readContextTest(t *testing.T, s *Service) wire.ContextWindowSnapshot {
	t.Helper()
	value, err := s.ReadContextWindow(context.Background(), testSessionID, false)
	if err != nil {
		t.Fatal(err)
	}
	if err = wire.Validate(value); err != nil {
		t.Fatal(err)
	}
	return value
}

func TestContextWindowUsesLastNativeSampleAndEffectiveCapacity(t *testing.T) {
	s := contextWindowTestService(t)
	if readContextTest(t, s).Sample != nil {
		t.Fatal("invented initial zero")
	}
	// Exercise the actual notification dispatcher, without a provider request.
	s.HandleNotification(tokenUsageNotification, contextUsageJSON(t, testTurnID, 143000, 3000000, 730000))
	value := readContextTest(t, s)
	if value.Sample == nil || value.Sample.UsedTokens != 143000 || *value.Sample.CapacityTokens != 730000 {
		t.Fatal("did not retain the same native sample", value)
	}
	_, events, _, cancel, err := s.SubscribeNativeEvents(testSessionID, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	defer cancel()
	if len(events) != 0 {
		t.Fatal("new usage leaked into the old closed event contract")
	}
}

func TestContextWindowUnknownCapacityAndRealZeroRemainDistinct(t *testing.T) {
	s := contextWindowTestService(t)
	s.HandleNotification(tokenUsageNotification, contextUsageJSON(t, testTurnID, 0, 0, nil))
	value := readContextTest(t, s)
	if value.Sample == nil || value.Sample.UsedTokens != 0 || value.Sample.CapacityTokens != nil {
		t.Fatal("unknown capacity or known zero changed")
	}
	s.HandleNotification(tokenUsageNotification, rawJSON(t, map[string]any{"threadId": testThreadID, "turnId": testTurnID, "tokenUsage": map[string]any{}}))
	if readContextTest(t, s).Sample != nil {
		t.Fatal("missing native counts became zero")
	}
}

func TestContextWindowModelRevisionAndLateReplay(t *testing.T) {
	s := contextWindowTestService(t)
	old := contextUsageJSON(t, testTurnID, 143000, 143000, 730000)
	s.HandleNotification(tokenUsageNotification, old)
	_, err := s.store.update(testSessionID, func(r *Record) error { r.ModelRevision = 2; r.ModelProfile = "kimi-k3-1-pro-max-v1"; return nil })
	if err != nil {
		t.Fatal(err)
	}
	if readContextTest(t, s).Sample != nil {
		t.Fatal("old model sample survived")
	}
	s.HandleNotification(tokenUsageNotification, old)
	if readContextTest(t, s).Sample != nil {
		t.Fatal("resume replay repopulated old model sample")
	}
	newTurn := "ordinary-next-turn"
	s.observeContextNotification(RuntimeNotificationTurnStarted, rawJSON(t, map[string]any{"threadId": testThreadID, "turn": map[string]any{"id": newTurn}}))
	s.HandleNotification(tokenUsageNotification, contextUsageJSON(t, newTurn, 143000, 143000, 730000))
	if readContextTest(t, s).Sample != nil {
		t.Fatal("rate-limit replay repopulated old sample")
	}
	s.HandleNotification(tokenUsageNotification, contextUsageJSON(t, newTurn, 8000, 151000, 996147))
	value := readContextTest(t, s)
	if value.Sample == nil || value.Sample.UsedTokens != 8000 || *value.Sample.CapacityTokens != 996147 {
		t.Fatal("fresh new model sample lost")
	}
}

func TestContextWindowCompactionAndNewContextLifecycle(t *testing.T) {
	s := contextWindowTestService(t)
	s.HandleNotification(tokenUsageNotification, contextUsageJSON(t, testTurnID, 700000, 900000, 730000))
	epoch := readContextTest(t, s).Sample.WindowId
	item := rawJSON(t, map[string]any{"threadId": testThreadID, "turnId": testTurnID, "item": map[string]any{"id": "native-window-item", "type": "contextCompaction"}})
	s.observeContextNotification(RuntimeNotificationItemStarted, item)
	if v := readContextTest(t, s); v.Sample != nil || v.Compactions[0].State != wire.InProgress {
		t.Fatal("old window did not clear")
	}
	// Native rate-limit events can repeat stale pre-compaction statistics.
	s.HandleNotification(tokenUsageNotification, contextUsageJSON(t, testTurnID, 700000, 900000, 730000))
	if readContextTest(t, s).Sample != nil {
		t.Fatal("old sample returned during compaction")
	}
	var recomputed map[string]any
	if err := json.Unmarshal(contextUsageJSON(t, testTurnID, 12000, 900000, 730000), &recomputed); err != nil {
		t.Fatal(err)
	}
	recomputed["tokenUsage"].(map[string]any)["last"].(map[string]any)["inputTokens"] = 0
	s.HandleNotification(tokenUsageNotification, rawJSON(t, recomputed))
	s.observeContextNotification(RuntimeNotificationItemCompleted, item)
	s.observeContextNotification(RuntimeNotificationItemStarted, item) // duplicate must not reopen
	v := readContextTest(t, s)
	if v.Sample == nil || v.Sample.UsedTokens != 12000 || v.Sample.WindowId == epoch || len(v.Compactions) != 1 || v.Compactions[0].State != wire.Completed {
		t.Fatal("native recomputation/completion lost", v)
	}
}

func TestContextWindowIncompleteCompactionDoesNotClaimSuccess(t *testing.T) {
	s := contextWindowTestService(t)
	s.observeContextNotification(RuntimeNotificationItemStarted, rawJSON(t, map[string]any{"threadId": testThreadID, "turnId": testTurnID, "item": map[string]any{"id": "interrupted-window", "type": "contextCompaction"}}))
	s.observeContextNotification(RuntimeNotificationTurnCompleted, rawJSON(t, map[string]any{"threadId": testThreadID, "turn": map[string]any{"id": testTurnID, "status": "interrupted"}}))
	v := readContextTest(t, s)
	if v.Compactions[0].State != wire.Interrupted || v.Sample != nil {
		t.Fatal("invented completion")
	}
}

func TestContextWindowHistoryUsesExactNativeIdentities(t *testing.T) {
	s := contextWindowTestService(t)
	r, _ := s.store.Get(testSessionID)
	s.observeContextHistory(r, rawJSON(t, map[string]any{"id": testThreadID, "turns": []any{map[string]any{"id": testTurnID, "items": []any{map[string]any{"id": "native-history-window", "type": "contextCompaction"}}}}}))
	v := readContextTest(t, s)
	if len(v.Compactions) != 1 || v.Compactions[0].ItemId != "native-history-window" || v.Compactions[0].State != wire.Completed || v.Sample != nil {
		t.Fatal("history changed identity or fabricated usage")
	}
}

type contextWindowRuntime struct {
	fakeRuntime
	ready bool
}

func (r *contextWindowRuntime) Snapshot() codex.Status { return codex.Status{Ready: r.ready} }

func TestContextWindowUnavailableRuntimeDoesNotServeCachedUsage(t *testing.T) {
	s := contextWindowTestService(t)
	s.HandleNotification(tokenUsageNotification, contextUsageJSON(t, testTurnID, 143000, 143000, 730000))
	s.runtime = &contextWindowRuntime{ready: false}
	if _, err := s.ReadContextWindow(context.Background(), testSessionID, false); err != ErrRuntimeRequest {
		t.Fatal("served cache without native readiness", err)
	}
}
