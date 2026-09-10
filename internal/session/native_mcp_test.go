package session

import (
	"encoding/json"
	"strings"
	"testing"

	native "github.com/36Dge/yijie-agent-host/internal/contracts/nativeconversationv2"
	"github.com/getkin/kin-openapi/openapi3"
)

func TestFEAT144NativeMcpPreservesIndexedTextAndAvailability(t *testing.T) {
	for _, tc := range []struct {
		name, result, kind string
		indexes            []int
		texts              []string
	}{
		{"absent", "", "absent", nil, nil},
		{"null", `,"result":null`, "null", nil, nil},
		{"empty", `,"result":{"content":[]}`, "empty", nil, nil},
		{"empty text", `,"result":{"content":[{"type":"text","text":""}]}`, "text", []int{0}, []string{""}},
		{"indexes", `,"result":{"content":[{"type":"text","text":"首段"},{"type":"image","data":"ordinary-placeholder"},{"type":"text","text":"后段"}]}`, "text", []int{0, 2}, []string{"首段", "后段"}},
		{"unsupported", `,"result":{"content":[{"type":"resource","resource":{"uri":"ordinary-placeholder"}}]}`, "unsupported", nil, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw := json.RawMessage(`{"id":"native-tool","type":"mcpToolCall","server":"sorftime","tool":"product_detail","status":"completed","arguments":{"asin":"B07H9PZDQW","amz_site":"US"}` + tc.result + `}`)
			item, err := projectNativeItem(raw, "/workspace", false)
			if err != nil || item.Mcp == nil || string(item.Mcp.ResultKind) != tc.kind || len(item.Mcp.Texts) != len(tc.indexes) || item.Status == nil || string(*item.Status) != "completed" {
				t.Fatal("native result shape or status changed")
			}
			for i, block := range item.Mcp.Texts {
				if block.Index != tc.indexes[i] || block.Text != tc.texts[i] {
					t.Fatal("native index/text changed")
				}
			}
			if item.ArgumentsSummary == nil || *item.ArgumentsSummary != "ASIN: B07H9PZDQW · 站点: US" {
				t.Fatal("actual approved scope absent")
			}
			if item.ResultSummary != nil && *item.ResultSummary != "结果已接收" {
				t.Fatal("metadata became business content")
			}
		})
	}
}

func TestFEAT144NativeMcpBoundsCompleteObjectsWithoutFinalization(t *testing.T) {
	blocks := make([]any, 34)
	for i := range blocks {
		blocks[i] = map[string]any{"type": "text", "text": strings.Repeat("正常资料", 10000)}
	}
	raw, _ := json.Marshal(map[string]any{"id": "native-tool", "type": "mcpToolCall", "server": "sorftime", "tool": "product_detail", "status": "completed", "result": map[string]any{"content": blocks}})
	item, err := projectNativeItem(raw, "/workspace", false)
	if err != nil || item.Mcp == nil || item.Availability != native.NativeItemAvailabilityPartial || item.Status == nil || string(*item.Status) != "completed" {
		t.Fatal("capacity changed native result")
	}
	bytes := 0
	for _, block := range item.Mcp.Texts {
		bytes += len(block.Text)
		if block.Index >= 32 {
			t.Fatal("native index limit absent")
		}
	}
	if bytes > nativeTextLimit {
		t.Fatal("complete object exceeds existing UTF-8 budget")
	}
	encoded, _ := json.Marshal(item)
	if strings.Contains(string(encoded), "finalized") {
		t.Fatal("projection synthesized a lifecycle event")
	}
}

func TestFEAT144LegacyWireKeepsClosedShapeAndSharedIdentity(t *testing.T) {
	s := NewService(&fakeRuntime{}, openBoundFEAT134Store(t), NewEventHub(32, 8), nil)
	s.HandleNotification(RuntimeNotificationItemCompleted, rawJSON(t, map[string]any{"threadId": testThreadID, "turnId": testTurnID, "item": map[string]any{"id": "mcp-item", "type": "mcpToolCall", "server": "sorftime", "tool": "product_detail", "status": "completed", "arguments": map[string]any{"asin": "B07H9PZDQW", "amz_site": "US"}, "result": map[string]any{"content": []any{map[string]any{"type": "text", "text": "普通商品资料"}}}}}))
	_, events, _, cancel, err := s.SubscribeNativeEvents(testSessionID, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	defer cancel()
	if len(events) != 1 {
		t.Fatal("expected one native event")
	}
	for _, version := range []int{7, 8} {
		event := events[0]
		path := "../../api/openapi/native-conversation-v2.yaml"
		if version == 7 {
			event = LegacyNativeEvent(event)
			path = "../../api/openapi/native-conversation.yaml"
		}
		doc, err := openapi3.NewLoader().LoadFromFile(path)
		if err != nil {
			t.Fatal(err)
		}
		data, _ := json.Marshal(event)
		var value any
		_ = json.Unmarshal(data, &value)
		if err := doc.Components.Schemas["NativeEvent"].Value.VisitJSON(value); err != nil {
			t.Fatal(err)
		}
		if event.EventID != events[0].EventID || event.Sequence != events[0].Sequence || event.SchemaVersion != version {
			t.Fatal("transport projection changed native event identity")
		}
		if version == 7 && strings.Contains(string(data), "普通商品资料") {
			t.Fatal("v1 metadata leaked new business text")
		}
	}
	if events[0].Payload.Native.Item.Mcp == nil {
		t.Fatal("legacy projection mutated the sole buffer")
	}
}

func TestFEAT144EmptyResultIsIndependentOfParameterAvailability(t *testing.T) {
	item, err := projectNativeItem(json.RawMessage(`{"id":"native-tool","type":"mcpToolCall","server":"sorftime","tool":"product_detail","status":"completed","arguments":{},"result":{"content":[]}}`), "/workspace", false)
	if err != nil || item.Mcp == nil || item.Mcp.ResultKind != native.Empty || item.Availability != native.NativeItemAvailabilityPartial {
		t.Fatal("parameter availability reclassified native empty content")
	}
}

func TestFEAT144McpJSONTransportLimitKeepsNativeCompletedHeader(t *testing.T) {
	raw, _ := json.Marshal(map[string]any{"id": "native-tool", "type": "mcpToolCall", "server": "sorftime", "tool": "product_detail", "status": "completed", "result": map[string]any{"content": []any{map[string]any{"type": "text", "text": strings.Repeat("&", nativeTextLimit)}}}})
	item, err := projectNativeItem(raw, "/workspace", false)
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(item)
	if len(encoded) > maxRetainedEventDataBytes-(32<<10) || item.Id != "native-tool" || item.Status == nil || string(*item.Status) != "completed" || item.Availability != native.NativeItemAvailabilityPartial {
		t.Fatal("JSON capacity lost the native completion or exceeded transport")
	}
}
