package session

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	wire "github.com/36Dge/yijie-agent-host/internal/contracts/markethost"
)

func TestMarketP2OwnedGenericHashIsPublicToolIdentity(t *testing.T) {
	digest := fmt.Sprintf("%x", sha256.Sum256([]byte("sorftime\npublic_lookup")))
	name := "m_" + digest[:60]
	if !v5SensitiveCommandLine(name) || !safeMarketToolName(name) {
		t.Fatal("ordinary generic identifier not recognized")
	}
	s, _, scope, home := marketService(t)
	accepted, err := s.SubmitMarket(context.Background(), registerMarket(t, s, scope, marketSubmission(t, home)))
	if err != nil {
		t.Fatal(err)
	}
	record, err := s.marketRecord(string(*accepted.AgentSessionId))
	if err != nil {
		t.Fatal(err)
	}
	status := "completed"
	item := nativeWireItem{ID: "ordinary-native-item", Type: "mcpToolCall", Server: "yijie_market", Tool: name, Status: &status, Result: json.RawMessage(`{"content":[],"structuredContent":{"rows":[1]}}`)}
	view := s.projectMarketTool(record, string(*accepted.NativeTurnId), item)
	if view == nil || view.ToolName != name || view.ResultText == nil {
		t.Fatal("generic history item disappeared")
	}
	raw, _ := json.Marshal(item)
	native, err := projectNativeItem(raw, "/workspace", false)
	if err != nil || native.Mcp == nil || native.Mcp.Tool == nil || *native.Mcp.Tool != name {
		t.Fatal("generic notification identity disappeared")
	}
	item.Server = "ordinary-provider"
	raw, _ = json.Marshal(item)
	native, err = projectNativeItem(raw, "/workspace", false)
	if err != nil || native.Mcp.Tool != nil {
		t.Fatal("owned-name exception escaped its server")
	}
}

func TestMarketP2StructuredProjectionAndNativeDisplayAgree(t *testing.T) {
	for _, tc := range []struct {
		name, result, source string
		separate, failed     bool
	}{
		{"structured", `{"content":[],"structuredContent":{"rows":[{"value":1}],"id":9007199254740993}}`, "structured", false, false},
		{"same", `{"content":[{"type":"text","text":"{\"rows\":[]}"}],"structuredContent":{"rows":[]}}`, "content", false, false},
		{"different", `{"content":[{"type":"text","text":"one ordinary row"}],"structuredContent":{"rows":[1]}}`, "content", true, false},
		{"business-error", `{"content":[],"structuredContent":{"message":"ordinary failure"},"isError":true}`, "structured", false, true},
		{"empty", `{"content":[]}`, "", false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw := json.RawMessage(tc.result)
			result := projectMarketResult(raw)
			if result.truncated || result.unsupported || result.isError != tc.failed || (result.structured != nil) != tc.separate {
				t.Fatal(result)
			}
			if tc.source != "" && (result.source == nil || string(*result.source) != tc.source || result.text == nil) {
				t.Fatal("lost source/text")
			}
			encoded, _ := json.Marshal(map[string]any{"id": "ordinary-item", "type": "mcpToolCall", "server": "yijie_market", "tool": "public_lookup", "status": "completed", "durationMs": 123, "arguments": map[string]string{"query": "ordinary public lookup"}, "result": raw})
			item, err := projectNativeItem(encoded, "/workspace", false)
			if err != nil || item.Mcp == nil {
				t.Fatal(item, err)
			}
			if item.DurationMs == nil || *item.DurationMs != 123 {
				t.Fatal("native duration fact lost")
			}
			if item.ArgumentsSummary == nil || *item.ArgumentsSummary != "参数不可展示" {
				t.Fatal("native argument confidentiality label lost")
			}
			if tc.source == "content" && result.text != nil && (len(item.Mcp.Texts) == 0 || item.Mcp.Texts[0].Text != *result.text) {
				t.Fatal("event/history projections differ")
			}
			if tc.source == "structured" && (len(item.Mcp.Texts) != 0 || item.Mcp.ResultKind != "unsupported") {
				t.Fatal("structured JSON became a fabricated native content block")
			}
			if tc.failed && (item.Status == nil || *item.Status != "failed") {
				t.Fatal("error promoted to success")
			}
			if tc.name == "structured" && !strings.Contains(*result.text, "9007199254740993") {
				t.Fatal("numeric identifier rounded")
			}
		})
	}
}

func TestMarketP2NativeContentKeepsOriginalIndexes(t *testing.T) {
	raw := json.RawMessage(`{"id":"ordinary-item","type":"mcpToolCall","server":"yijie_market","tool":"lookup","status":"completed","result":{"content":[{"type":"image","data":"ordinary-placeholder"},{"type":"text","text":"first actual text"},{"type":"text","text":"second actual text"}]}}`)
	item, err := projectNativeItem(raw, "/workspace", false)
	if err != nil || item.Mcp == nil || len(item.Mcp.Texts) != 2 || item.Mcp.Texts[0].Index != 1 || item.Mcp.Texts[1].Index != 2 {
		t.Fatal("native content indexes changed", err)
	}
}

func TestMarketP2ProjectionBoundsAndRedactsOrdinaryJSON(t *testing.T) {
	raw := rawJSON(t, map[string]any{"content": []any{}, "structuredContent": map[string]any{"password": "ordinary-placeholder", "rows": []any{map[string]any{"note": "ordinary public result"}}}})
	result := projectMarketResult(raw)
	if !result.redacted || result.text == nil || strings.Contains(*result.text, "ordinary-placeholder") || !strings.Contains(*result.text, "ordinary public result") {
		t.Fatal("structured redaction failed")
	}
	raw = rawJSON(t, map[string]any{"content": []any{map[string]any{"type": "text", "text": strings.Repeat("普通资料\n", 12000)}}, "structuredContent": map[string]any{"rows": []any{1}}})
	result = projectMarketResult(raw)
	if !result.truncated || result.text == nil || len(*result.text) > wire.MaxResultTextBytes {
		t.Fatal("display budget lost")
	}
}

func TestMarketP2NativeSnapshotsAvoidRepeatedThreadReads(t *testing.T) {
	s, runtime, scope, home := marketService(t)
	accepted, err := s.SubmitMarket(context.Background(), registerMarket(t, s, scope, marketSubmission(t, home)))
	if err != nil {
		t.Fatal(err)
	}
	turn, thread, agent := string(*accepted.NativeTurnId), string(*accepted.NativeThreadId), string(*accepted.AgentSessionId)
	runtime.toolTurnID = turn
	for range 4 {
		if _, err := s.ReadMarketToolEvents(context.Background(), agent, turn); err != nil {
			t.Fatal(err)
		}
	}
	if runtime.toolReads != 1 {
		t.Fatal("warm snapshot reread entire history", runtime.toolReads)
	}
	completed := func(text string) {
		s.publishNativeNotification(RuntimeNotificationItemCompleted, rawJSON(t, map[string]any{"threadId": thread, "turnId": turn, "item": map[string]any{"id": "native-tool-item-1", "type": "mcpToolCall", "server": "yijie_market", "tool": "public_lookup", "status": "completed", "result": map[string]any{"content": []any{}, "structuredContent": map[string]any{"text": text}}}}))
	}
	completed("notification result")
	result, err := s.ReadMarketToolEvents(context.Background(), agent, turn)
	if err != nil || runtime.toolReads != 1 || result.Items[0].ResultText == nil || !strings.Contains(*result.Items[0].ResultText, "notification result") {
		t.Fatal(result, err, runtime.toolReads)
	}
	// Normal ordering race: completion notification arrives during a requested read.
	runtime.toolReadHook = func() { completed("later notification") }
	if _, err := s.ReadMarketTools(context.Background(), agent, turn); err != nil {
		t.Fatal(err)
	}
	result, err = s.ReadMarketToolEvents(context.Background(), agent, turn)
	if err != nil || runtime.toolReads != 2 || !strings.Contains(*result.Items[0].ResultText, "later notification") {
		t.Fatal("history overwrote a newer native fact")
	}
	s.publishNativeNotification(RuntimeNotificationItemStarted, rawJSON(t, map[string]any{"threadId": thread, "turnId": turn, "item": map[string]any{"id": "native-tool-item-1", "type": "mcpToolCall", "server": "yijie_market", "tool": "public_lookup", "status": "inProgress"}}))
	result, _ = s.ReadMarketToolEvents(context.Background(), agent, turn)
	if result.Items[0].State != "completed" {
		t.Fatal("queued started event replaced observed completion")
	}
	runtime.toolRuntimeNotReady = true
	if _, err := s.ReadMarketToolEvents(context.Background(), agent, turn); MarketFailureCode(err) != wire.ErrorCodeNotReady {
		t.Fatal("cached data presented as current while Runtime is unavailable")
	}
}

func TestMarketP2ColdHistoryIsNotReplacedByALateEmptyStartedSnapshot(t *testing.T) {
	for _, includeItem := range []bool{false, true} {
		t.Run(fmt.Sprintf("with-started-item-%t", includeItem), func(t *testing.T) {
			s, runtime, scope, home := marketService(t)
			accepted, err := s.SubmitMarket(context.Background(), registerMarket(t, s, scope, marketSubmission(t, home)))
			if err != nil {
				t.Fatal(err)
			}
			turn, thread, agent := string(*accepted.NativeTurnId), string(*accepted.NativeThreadId), string(*accepted.AgentSessionId)
			runtime.toolTurnID = turn
			runtime.toolReadHook = func() {
				s.publishNativeNotification(RuntimeNotificationTurnStarted, rawJSON(t, map[string]any{"threadId": thread, "turn": map[string]any{"id": turn, "status": "inProgress", "items": []any{}}}))
				if includeItem {
					s.publishNativeNotification(RuntimeNotificationItemStarted, rawJSON(t, map[string]any{"threadId": thread, "turnId": turn, "item": map[string]any{"id": "native-tool-item-1", "type": "mcpToolCall", "server": "yijie_market", "tool": "public_lookup", "status": "inProgress"}}))
				}
			}
			view, err := s.ReadMarketToolEvents(context.Background(), agent, turn)
			if err != nil || len(view.Items) != 1 || view.Items[0].State != "completed" {
				t.Fatal("late started snapshot hid the real history", view, err)
			}
			view, err = s.ReadMarketToolEvents(context.Background(), agent, turn)
			if err != nil || runtime.toolReads != 2 || len(view.Items) != 1 || view.Items[0].State != "completed" {
				t.Fatal("incomplete baseline was not refreshed", view, err)
			}
		})
	}
}

func TestMarketP2JSONDedupeIgnoresMemberOrderWithoutRoundingNumbers(t *testing.T) {
	text := `{"z":9007199254740993,"a":[1,2]}`
	if !sameMarketJSON(text, []byte(`{"a":[1,2],"z":9007199254740993}`)) {
		t.Fatal("same JSON was duplicated by key order")
	}
	if sameMarketJSON(text, []byte(`{"a":[1,2],"z":9007199254740992}`)) || sameMarketJSON(text, []byte(`{"a":[2,1],"z":9007199254740993}`)) {
		t.Fatal("different values were discarded")
	}
}

func TestMarketP2SnapshotIdentityAndEviction(t *testing.T) {
	var cache marketToolSnapshots
	const sessionID = "15700000-0000-4000-8000-000000000001"
	for i := range wire.MaxLiveToolTurns + 1 {
		key := marketSnapshotKey{sessionID, "thread", fmt.Sprintf("turn-%d", i), "scope"}
		cache.observe(key, nil, true)
	}
	if len(cache.entries) != wire.MaxLiveToolTurns || cache.bytes > wire.MaxLiveToolBytes {
		t.Fatal("unbounded snapshots")
	}
	if _, ok := cache.get(marketSnapshotKey{sessionID, "thread", "turn-0", "scope"}); ok {
		t.Fatal("oldest snapshot retained")
	}
	if _, ok := cache.get(marketSnapshotKey{sessionID, "thread", "turn-1", "scope"}); !ok {
		t.Fatal("live snapshot lost")
	}
	if _, ok := cache.get(marketSnapshotKey{"15700000-0000-4000-8000-000000000002", "thread", "turn-1", "scope"}); ok {
		t.Fatal("snapshot escaped session")
	}
}

func TestMarketP2SnapshotDoesNotReviveConflictingLinksOrStaleReads(t *testing.T) {
	var cache marketToolSnapshots
	key := marketSnapshotKey{"15700000-0000-4000-8000-000000000001", "thread", "turn", "scope"}
	call := wire.CanonicalId("15700000-0000-4000-8000-000000000002")
	receipt := &wire.GatewayCallReceipt{SchemaVersion: 1, CallRef: call, ServiceId: "sorftime", Reference: wire.SelectionRef{InstallationId: "15700000-0000-4000-8000-000000000003", Revision: 1, Generation: 1}, NativeThreadId: "thread", NativeTurnId: "turn", TurnOperationId: "15700000-0000-4000-8000-000000000004", SelectionDigest: wire.SelectionDigest(strings.Repeat("a", 64)), ToolName: "lookup", ArgsDigest: wire.ArgsDigest(strings.Repeat("b", 64)), ArgsEncoding: "worker-json-v1", DecisionReceipt: nil, AdmissionConsumed: false}
	first := wire.ToolObservation{NativeItemId: "first", NativeThreadId: "thread", NativeTurnId: "turn", ServerName: "yijie_market", ToolName: "lookup", State: "failed", Correlation: receipt, CallRef: &call}
	second := first
	second.NativeItemId = "second"
	cache.observe(key, &first, true)
	cache.observe(key, &second, false)
	for range 2 {
		view, ok := cache.get(key)
		if !ok || len(view.Items) != 2 {
			t.Fatal("missing snapshot")
		}
		for _, item := range view.Items {
			if item.Correlation != nil || item.CallRef != nil {
				t.Fatal("ambiguous native association revived")
			}
		}
		cache.observe(key, &first, false)
	}
	// An ordinary invalidation during a read must not permit an ABA cache seed.
	version := cache.version(key)
	cache.invalidate(key)
	cache.seed(key, wire.ToolSnapshot{AgentSessionId: wire.CanonicalId(key.session), NativeTurnId: "turn", Items: []wire.ToolObservation{first}}, version)
	if _, ok := cache.get(key); ok {
		t.Fatal("invalidated history read repopulated the snapshot")
	}
}
