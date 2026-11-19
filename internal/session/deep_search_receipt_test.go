package session

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	native "github.com/36Dge/yijie-agent-host/internal/contracts/nativeconversationv6"
	"github.com/google/uuid"
)

func TestDeepSearchScopeSurvivesNormalReopenAndSeparatesOwners(t *testing.T) {
	path, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	open := func() *Store {
		s, err := OpenStore(path)
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	s := open()
	scope := func(binding, root, rootTurn, caller, turn string) uuid.UUID {
		id, err := s.deepSearchScope(binding, root, rootTurn, caller, turn)
		if err != nil || id.Version() != 8 {
			t.Fatalf("invalid native scope: %v", err)
		}
		return id
	}
	original := scope("b", "r", "rt", "a", "t")
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s = open()
	defer s.Close()
	if scope("b", "r", "rt", "a", "t") != original {
		t.Fatal("native scope changed on reopen")
	}
	seen := map[uuid.UUID]bool{original: true}
	for _, id := range []uuid.UUID{scope("c", "r", "rt", "a", "t"), scope("b", "s", "rt", "a", "t"), scope("b", "r", "rt2", "a", "t"), scope("b", "r", "rt", "b", "t"), scope("b", "r", "rt", "a", "t2")} {
		if seen[id] {
			t.Fatal("distinct native contexts shared a scope")
		}
		seen[id] = true
	}
}

func TestDeepSearchReceiptAtEnvelopeLimitAndLegacyView(t *testing.T) {
	id, _ := uuid.NewV7()
	fact := map[string]any{"version": 2, "relation": "reused", "operation_id": id, "dispatch": "responded", "outcome": "empty", "availability": "available"}
	result := map[string]any{"content": []any{map[string]any{"type": "text", "text": `{"results":[],"truncated":false,"dropped_results":0}`}}, "_meta": map[string]any{"ai.yijie/deep-search-receipt": fact, "unread": strings.Repeat("x", 64500)}}
	project := func() native.NativeItem {
		wire := rawJSON(t, map[string]any{"type": "mcpToolCall", "status": "completed", "id": "logical", "server": "yijie_web", "tool": "kimi_web_search_pro", "arguments": map[string]any{"query": "杭州"}, "result": result})
		item, err := projectNativeItem(wire, "", false)
		if err != nil {
			t.Fatal(err)
		}
		return item
	}
	it := project()
	if it.Mcp.SearchReceipt == nil || *it.Mcp.SearchReceipt.OperationId != id || it.Mcp.ResultKind != native.Text {
		t.Fatal("authoritative history lost bounded evidence")
	}
	old := StripSearchReceipt(Event{Payload: EventPayload{Native: &native.NativeNotification{Item: &it}}})
	if old.Payload.Native.Item.Mcp.SearchReceipt != nil || it.Mcp.SearchReceipt == nil {
		t.Fatal("legacy view mutated original")
	}
	for _, version := range []func(Event) Event{NativeEventV8, NativeEventV10} {
		legacy := version(Event{Payload: EventPayload{Native: &native.NativeNotification{Item: &it}}}).Payload.Native.Item
		if legacy.Mcp.SearchReceipt != nil || legacy.Mcp.ResultKind != native.Omitted || len(legacy.Mcp.Texts) != 0 || it.Mcp.ResultKind != native.Text {
			t.Fatal("closed legacy representation exposed new evidence")
		}
	}
	restored, err := json.Marshal(it)
	if err != nil || strings.Contains(string(restored), "unread") {
		t.Fatal("metadata outside allowlist leaked")
	}
	fact["extra"] = "unsupported"
	if project().Mcp.SearchReceipt != nil {
		t.Fatal("noncanonical metadata exposed")
	}
	delete(result, "_meta")
	if project().Mcp.ResultKind == native.Text {
		t.Fatal("legacy record backfilled")
	}
}
