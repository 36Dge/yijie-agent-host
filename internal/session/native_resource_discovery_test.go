package session

import (
	"encoding/json"
	"strings"
	"testing"

	native "github.com/36Dge/yijie-agent-host/internal/contracts/nativeconversationv4"
	"github.com/getkin/kin-openapi/openapi3"
)

func TestNativeResourceDiscoveryShowsActualResult(t *testing.T) {
	for _, tc := range []struct{ tool, args, result, want string }{
		{"list_mcp_resources", `{}`, `{"resources":[]}`, "未发现可用资源"},
		{"list_mcp_resource_templates", `{}`, `{"resourceTemplates":[]}`, "未发现可用资源模板"},
		{"list_mcp_resources", `{"server":"reference"}`, `{"resources":[{"name":"guide","title":"旅行参考","uri":"resource://reference/private-path"}]}`, "找到 1 项可用资源\n旅行参考"},
		{"list_mcp_resources", `{}`, `{"resources":[],"nextCursor":"ordinary-page-2"}`, "未发现可用资源（还有后续分页）"},
	} {
		t.Run(tc.tool+tc.result, func(t *testing.T) {
			var args any
			_ = json.Unmarshal([]byte(tc.args), &args)
			raw, _ := json.Marshal(map[string]any{"id": "discovery", "type": "mcpToolCall", "server": "codex", "tool": tc.tool, "status": "completed", "arguments": args, "durationMs": 19, "result": map[string]any{"content": []any{map[string]any{"type": "text", "text": tc.result}}}})
			item, err := projectNativeItem(raw, "/workspace", false)
			if err != nil || item.Mcp == nil || item.Mcp.ResultKind != native.Text || item.Availability != native.NativeItemAvailabilityAvailable || len(item.Mcp.Texts) != 1 || item.Mcp.Texts[0].Text != tc.want {
				t.Fatalf("unexpected discovery projection: %v %#v", err, item)
			}
			if item.Status == nil || *item.Status != "completed" || item.DurationMs == nil || *item.DurationMs != 19 {
				t.Fatal("execution facts changed")
			}
			encoded, _ := json.Marshal(item)
			doc, err := openapi3.NewLoader().LoadFromFile("../../api/openapi/native-conversation-v4.yaml")
			if err != nil {
				t.Fatal(err)
			}
			var value any
			_ = json.Unmarshal(encoded, &value)
			if err := doc.Components.Schemas["NativeItem"].Value.VisitJSON(value); err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(encoded), "private-path") || strings.Contains(string(encoded), "ordinary-page-2") {
				t.Fatal("resource location or cursor exposed")
			}
		})
	}
}

func TestNativeResourceDiscoveryKeepsUnrecognizedShapeHonest(t *testing.T) {
	raw := json.RawMessage(`{"id":"discovery","type":"mcpToolCall","server":"codex","tool":"list_mcp_resources","status":"completed","arguments":{},"result":{"content":[{"type":"text","text":"{}"}]}}`)
	item, err := projectNativeItem(raw, "/workspace", false)
	if err != nil || item.Mcp.ResultKind != native.Omitted || item.Availability != native.NativeItemAvailabilityPartial || *item.Status != "completed" {
		t.Fatal("unknown result was presented as an empty successful list")
	}
}
