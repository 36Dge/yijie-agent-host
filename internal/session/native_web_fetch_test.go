package session

import (
	"encoding/json"
	"strings"
	"testing"

	native "github.com/36Dge/yijie-agent-host/internal/contracts/nativeconversationv6"
)

func TestWebFetchReadingDetails(t *testing.T) {
	for _, tc := range []struct {
		name, payload, outcome, title string
		failed                        bool
	}{
		{"page", `{"url":"https://example.org/article","title":"Reading guide","markdown":"A paragraph in the document.","truncated":false}`, "已读取网页正文。", "Reading guide", false},
		{"partial", `{"url":"https://example.org/article","title":"","markdown":"A paragraph in the document.","truncated":true}`, "已读取部分正文，网页内容较长。", "", false},
		{"no content", `{"error":{"code":"no_content","message":"网页未返回可用正文。"}}`, "网页未返回可用正文。", "", true},
		{"URL blocked", `{"error":{"code":"url_blocked","message":"网页读取服务的安全策略拒绝了此网址。"}}`, "网页读取服务的安全策略拒绝了此网址。", "", true},
		{"upstream", `{"error":{"code":"upstream_unavailable","message":"网页检索服务暂不可用。"}}`, "网页检索服务暂不可用。", "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw := rawJSON(t, map[string]any{"type": "mcpToolCall", "id": "fetch", "server": "yijie_web", "tool": "kimi_web_fetch", "status": "completed",
				"arguments": map[string]string{"url": "https://example.org/article?edition=2"}, "durationMs": 6818,
				"result": map[string]any{"content": []any{map[string]any{"type": "text", "text": "Wall time: 6.818 seconds\nOutput:\n" + tc.payload}}, "isError": tc.failed}})
			item, err := projectNativeItemWithSearchDetails(raw, "", false, true)
			if err != nil || item.ArgumentsSummary == nil || *item.ArgumentsSummary != "example.org" || item.Mcp.ResultKind != native.Text ||
				len(item.Mcp.Texts) != 1 || item.Mcp.Texts[0].Text != tc.outcome || (*item.Status == native.NativeItemStatusFailed) != tc.failed {
				t.Fatalf("incorrect observed fetch: %+v, %v", item, err)
			}
			if tc.title == "" && item.ResultSummary != nil || tc.title != "" && (item.ResultSummary == nil || *item.ResultSummary != tc.title) {
				t.Fatal("incorrect projected page title")
			}
			projected, _ := json.Marshal(item)
			if strings.Contains(string(projected), "example.org/article") || strings.Contains(string(projected), "A paragraph") {
				t.Fatal("display includes page content or request path")
			}
			omitLegacyWebDetails(&item)
			if item.Mcp.ResultKind != native.Omitted || *item.ArgumentsSummary == "example.org" || *item.ResultSummary != "结果已接收" {
				t.Fatal("older event version must retain metadata-only display")
			}
			cold, err := projectNativeItem(raw, "", false)
			if err != nil || cold.Mcp.ResultKind != native.Omitted || (cold.ArgumentsSummary != nil && *cold.ArgumentsSummary == "example.org") {
				t.Fatal("a cold read cannot backfill the recorded live receipt")
			}
		})
	}
}
