package session

import (
	"context"
	"encoding/json"
	native "github.com/36Dge/yijie-agent-host/internal/contracts/nativeconversationv2"
	"github.com/getkin/kin-openapi/openapi3"
	"os"
	"strings"
	"testing"
)

func TestFEAT132NativeFinalItemIsIndependentOfLegacyReconciliation(t *testing.T) {
	s := NewService(&fakeRuntime{}, openBoundFEAT134Store(t), NewEventHub(32, 8), nil, WithV4Events(NewEventHubVersion(4, 32, 8)))
	for _, input := range []struct {
		method string
		item   map[string]any
		delta  string
	}{
		{RuntimeNotificationItemStarted, map[string]any{"id": "item-live", "type": "agentMessage", "text": "", "phase": "commentary"}, ""},
		{RuntimeNotificationItemAgentMessageDelta, nil, "draft"},
		{RuntimeNotificationItemCompleted, map[string]any{"id": "item-live", "type": "agentMessage", "text": "revised final", "phase": "final_answer"}, ""},
	} {
		s.HandleNotification(input.method, rawJSON(t, map[string]any{"threadId": testThreadID, "turnId": testTurnID, "itemId": "item-live", "item": input.item, "delta": input.delta}))
	}
	_, events, _, cancel, err := s.SubscribeNativeEvents(testSessionID, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	defer cancel()
	if len(events) != 3 {
		t.Fatalf("expected three native facts, got %d", len(events))
	}
	last := events[2].Payload.Native
	if last.Source != native.RuntimeNotification || last.Item == nil || last.Item.Text == nil || *last.Item.Text != "revised final" || last.Item.Phase == nil || *last.Item.Phase != native.FinalAnswer || events[2].Terminal {
		t.Fatal("native final item was reinterpreted")
	}
}

func TestFEAT132NativeProjectionMetadataCannotInventExecutionFailure(t *testing.T) {
	for _, status := range []string{"completed", "future_status"} {
		raw := json.RawMessage(`{"id":"command-1","type":"commandExecution","command":"echo safe","cwd":"/workspace","status":"` + status + `","durationMs":-1,"exitCode":0}`)
		item, err := projectNativeItem(raw, "/workspace", false)
		if err != nil {
			t.Fatal(err)
		}
		if item.Status != nil && string(*item.Status) != status {
			t.Fatal("projection changed execution status")
		}
		if item.Availability != native.NativeItemAvailabilityPartial || item.DurationMs != nil {
			t.Fatal("invalid metadata was not isolated")
		}
	}
}

type nativeReadRuntime struct {
	fakeRuntime
	called string
	raw    json.RawMessage
}

func (r *nativeReadRuntime) ReadThread(_ context.Context, id string) (json.RawMessage, error) {
	r.called = id
	return r.raw, nil
}

func TestFEAT132NativeReadUsesRuntimeHistoryAndPreservesNativeIDs(t *testing.T) {
	r := &nativeReadRuntime{raw: rawJSON(t, map[string]any{"id": testThreadID, "cwd": "/private/not-exposed", "turns": []any{map[string]any{"id": testTurnID, "status": "completed", "items": []any{map[string]any{"id": "item-17", "type": "agentMessage", "text": "native history", "phase": nil}}}}})}
	s := NewService(r, openBoundFEAT134Store(t), NewEventHub(32, 8), nil)
	view, err := s.ReadNativeThreadV2(context.Background(), testSessionID)
	if err != nil {
		t.Fatal(err)
	}
	if r.called != testThreadID || view.Source != native.RuntimeRead || len(view.Turns) != 1 || view.Turns[0].Items[0].Id != "item-17" || view.Turns[0].ItemsComplete {
		t.Fatal("native history was reconstructed or misclassified")
	}
	encoded, _ := json.Marshal(view)
	if strings.Contains(string(encoded), "/private/not-exposed") {
		t.Fatal("raw metadata escaped projection")
	}
}

func TestFEAT132NativeTurnTerminalDoesNotSynthesizeItems(t *testing.T) {
	s := NewService(&fakeRuntime{}, openBoundFEAT134Store(t), NewEventHub(32, 8), nil)
	s.HandleNotification(RuntimeNotificationTurnCompleted, rawJSON(t, map[string]any{"threadId": testThreadID, "turn": map[string]any{"id": testTurnID, "status": "failed", "items": []any{}, "error": map[string]any{"message": "safe failure"}}}))
	_, events, _, cancel, err := s.SubscribeNativeEvents(testSessionID, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	defer cancel()
	if len(events) != 1 || !events[0].Terminal {
		t.Fatal("native terminal absent")
	}
	turn := events[0].Payload.Native.Turn
	if turn == nil || turn.Status == nil || *turn.Status != "failed" || len(turn.Items) != 0 || turn.ItemsComplete {
		t.Fatal("native terminal was inferred")
	}
}

func TestFEAT132NativeProjectionNoticeIsNeverATerminal(t *testing.T) {
	s := NewService(&fakeRuntime{}, openBoundFEAT134Store(t), NewEventHub(32, 8), nil)
	s.publishNativeNotification(RuntimeNotificationItemCompleted, rawJSON(t, map[string]any{"threadId": testThreadID, "turnId": testTurnID, "item": map[string]any{"type": "agentMessage"}}))
	_, events, _, cancel, err := s.SubscribeNativeEvents(testSessionID, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	defer cancel()
	if len(events) != 1 || events[0].Terminal || events[0].Payload.Native.Source != native.ProjectionNotice || events[0].Payload.Native.Turn != nil {
		t.Fatal("projection failure became execution terminal")
	}
}

func TestFEAT132NativeReasoningRespectsProjectionAuthority(t *testing.T) {
	raw := json.RawMessage(`{"id":"reason-1","type":"reasoning","content":["private reasoning"],"summary":["public summary"]}`)
	for _, allowed := range []bool{false, true} {
		item, err := projectNativeItem(raw, "/workspace", allowed)
		if err != nil {
			t.Fatal(err)
		}
		if item.Content == nil || (len(*item.Content) > 0) != allowed {
			t.Fatal("raw reasoning authority was not enforced")
		}
	}
}

func TestFEAT132NativeResumePreservesObservedFailureAndRejectsStaleRead(t *testing.T) {
	store := openBoundFEAT134Store(t)
	started, err := store.BindTurn(testSessionID, testTurnID)
	if err != nil {
		t.Fatal(err)
	}
	final, err := store.CompleteTurn(testSessionID, testTurnID, "failed")
	if err != nil {
		t.Fatal(err)
	}
	restored, err := store.Resume(testSessionID, TraceContext{}, "runtime-session", "", testTurnID, "completed", final.NativeRevision)
	if err != nil {
		t.Fatal(err)
	}
	if restored.LastTurnStatus != "failed" || restored.NativeTerminalTurnID != testTurnID {
		t.Fatal("cold history replaced a native failure")
	}
	stale, err := store.Resume(testSessionID, TraceContext{}, "runtime-session", testTurnID, testTurnID, "in_progress", started.NativeRevision)
	if err != nil {
		t.Fatal(err)
	}
	if stale.ActiveTurnID != "" || stale.LastTurnStatus != "failed" {
		t.Fatal("stale read replaced the newer terminal")
	}
}

func TestFEAT132NativeUnknownTerminalIsAProjectionNoticeOnly(t *testing.T) {
	store := openBoundFEAT134Store(t)
	if _, err := store.BindTurn(testSessionID, testTurnID); err != nil {
		t.Fatal(err)
	}
	s := NewService(&fakeRuntime{}, store, NewEventHub(32, 8), nil)
	s.HandleNotification(RuntimeNotificationTurnCompleted, rawJSON(t, map[string]any{"threadId": testThreadID, "turn": map[string]any{"id": testTurnID, "status": "futureStatus"}}))
	current, _ := store.Get(testSessionID)
	if current.ActiveTurnID != testTurnID || current.LastTurnStatus == "failed" {
		t.Fatal("unknown status changed native execution")
	}
	_, events, _, cancel, err := s.SubscribeNativeEvents(testSessionID, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	defer cancel()
	if len(events) != 1 || events[0].Terminal || events[0].Payload.Native.Source != native.ProjectionNotice {
		t.Fatal("projection notice absent or promoted to terminal")
	}
}

func TestFEAT132NativeSummaryIndexesAndSafeErrorAreNative(t *testing.T) {
	s := NewService(&fakeRuntime{}, openBoundFEAT134Store(t), NewEventHub(32, 8), nil)
	s.HandleNotification("item/reasoning/summaryTextDelta", rawJSON(t, map[string]any{"threadId": testThreadID, "turnId": testTurnID, "itemId": "r", "summaryIndex": 2, "delta": "summary"}))
	s.HandleNotification(RuntimeNotificationTurnCompleted, rawJSON(t, map[string]any{"threadId": testThreadID, "turn": map[string]any{"id": testTurnID, "status": "failed", "error": map[string]any{"message": "Budget reached", "codexErrorInfo": "usageLimitExceeded"}}}))
	_, events, _, cancel, err := s.SubscribeNativeEvents(testSessionID, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	defer cancel()
	if len(events) != 2 || events[0].Payload.Native.Index == nil || *events[0].Payload.Native.Index != 2 {
		t.Fatal("native summary index was lost")
	}
	last := events[1].Payload.Native.Turn
	if last.Error == nil || last.Error.CodexErrorInfo == nil || string(*last.Error.CodexErrorInfo) != "usageLimitExceeded" {
		t.Fatal("native error discriminator was discarded")
	}
}

func TestFEAT132NativeProducerConformsToSourceContract(t *testing.T) {
	document, err := openapi3.NewLoader().LoadFromFile("../../api/openapi/native-conversation-v2.yaml")
	if err != nil {
		t.Fatal(err)
	}
	s := NewService(&fakeRuntime{}, openBoundFEAT134Store(t), NewEventHub(32, 8), nil)
	s.HandleNotification(RuntimeNotificationItemStarted, rawJSON(t, map[string]any{"threadId": testThreadID, "turnId": testTurnID, "item": map[string]any{"id": "native-message", "type": "agentMessage", "text": "", "phase": nil}}))
	s.HandleNotification(RuntimeNotificationItemCompleted, rawJSON(t, map[string]any{"threadId": testThreadID, "turnId": testTurnID, "item": map[string]any{"id": "native-message", "type": "agentMessage", "text": "final native value", "phase": "final_answer"}}))
	s.HandleNotification(RuntimeNotificationTurnCompleted, rawJSON(t, map[string]any{"threadId": testThreadID, "turn": map[string]any{"id": testTurnID, "status": "failed", "items": []any{}, "error": map[string]any{"message": "Native budget exhausted", "codexErrorInfo": "usageLimitExceeded"}}}))
	_, events, _, cancel, err := s.SubscribeNativeEvents(testSessionID, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	defer cancel()
	if len(events) != 3 {
		t.Fatal("native events absent")
	}
	for _, event := range events {
		encoded, err := json.Marshal(event)
		if err != nil {
			t.Fatal(err)
		}
		var value any
		if err = json.Unmarshal(encoded, &value); err != nil {
			t.Fatal(err)
		}
		if err = document.Components.Schemas["NativeEvent"].Value.VisitJSON(value); err != nil {
			t.Fatal(err)
		}
	}
}

// Test-only exchange of real Host projection bytes. It never starts Runtime/model.
func TestFEAT132NativeCrossRepoExport(t *testing.T) {
	output := os.Getenv("YIJIE_FEAT132_TEST_EXCHANGE")
	if output == "" {
		t.Skip("run Desktop check:native-integration for cross-repository exchange")
	}
	r := &nativeReadRuntime{raw: rawJSON(t, map[string]any{"id": testThreadID, "turns": []any{map[string]any{"id": testTurnID, "status": "completed", "items": []any{map[string]any{"id": "item-0", "type": "agentMessage", "text": "cold history", "phase": "final_answer"}}}}})}
	s := NewService(r, openBoundFEAT134Store(t), NewEventHub(32, 8), nil)
	for _, entry := range []struct {
		method  string
		payload map[string]any
	}{
		{RuntimeNotificationTurnStarted, map[string]any{"turn": map[string]any{"id": testTurnID, "status": "inProgress", "items": []any{}}}},
		{RuntimeNotificationItemStarted, map[string]any{"item": map[string]any{"id": "host-agent", "type": "agentMessage", "text": "", "phase": "commentary"}}},
		{RuntimeNotificationItemAgentMessageDelta, map[string]any{"itemId": "host-agent", "delta": "临时草稿"}},
		{RuntimeNotificationItemStarted, map[string]any{"item": map[string]any{"id": "host-reason", "type": "reasoning", "content": []any{}, "summary": []any{}}}},
		{"item/reasoning/summaryTextDelta", map[string]any{"itemId": "host-reason", "summaryIndex": 1, "delta": "公开摘要"}},
		{RuntimeNotificationItemCompleted, map[string]any{"item": map[string]any{"id": "host-agent", "type": "agentMessage", "text": "来自 Host 的原生最终值", "phase": "final_answer"}}},
		{RuntimeNotificationTurnCompleted, map[string]any{"turn": map[string]any{"id": testTurnID, "status": "failed", "items": []any{}, "error": map[string]any{"message": "Normal quota result", "codexErrorInfo": "usageLimitExceeded"}}}},
	} {
		entry.payload["threadId"] = testThreadID
		entry.payload["turnId"] = testTurnID
		s.HandleNotification(entry.method, rawJSON(t, entry.payload))
	}
	_, events, _, cancel, err := s.SubscribeNativeEvents(testSessionID, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	defer cancel()
	history, err := s.ReadNativeThreadV2(context.Background(), testSessionID)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 7 {
		t.Fatalf("expected 7 native events, got %d", len(events))
	}
	bytes, err := json.Marshal(map[string]any{"events": events, "history": history})
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(output, bytes, 0600); err != nil {
		t.Fatal(err)
	}
}
