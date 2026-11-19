package session

import (
	"encoding/json"
	"strings"
	"testing"

	web "github.com/36Dge/yijie-agent-host/internal/contracts/builtinweb"
	native "github.com/36Dge/yijie-agent-host/internal/contracts/nativeconversationv6"
)

func TestObservedDeepWebSearchProjectsOnlySourceSummaries(t *testing.T) {
	result := `{"results":[{"title":"杭州天气","url":"https://example.org/weather?city=hangzhou#today","snippet":"近日天气资料","site_name":"示例站","date":"","authority":"","content_status":"available","chunks":[{"text":"仅供模型使用的正文片段","score":0.7}]}],"truncated":false,"dropped_results":0}`
	wire := rawJSON(t, map[string]any{
		"id": "weather", "type": "mcpToolCall", "status": "completed", "durationMs": 8333,
		"server": web.ServerName, "tool": web.KimiWebSearchPro,
		"arguments": map[string]any{"query": "杭州天气", "limit": 5},
		"result":    map[string]any{"content": []any{map[string]any{"type": "text", "text": result}}, "structuredContent": json.RawMessage(result)},
	})
	item, err := projectNativeItemWithSearchDetails(wire, "", false, true)
	if err != nil || item.ArgumentsSummary == nil || *item.ArgumentsSummary != "杭州天气" || item.DurationMs == nil || *item.DurationMs != 8333 ||
		item.Mcp.ResultKind != native.Text || len(item.Mcp.Texts) != 1 || item.Mcp.Texts[0].Index != 0 || item.Availability != native.NativeItemAvailabilityAvailable {
		t.Fatal("missing native search query, timing, or result")
	}
	block := item.Mcp.Texts[0].Text
	var rows web.SearchSuccess
	if web.Validate("SearchSuccess", []byte(block)) != nil || json.Unmarshal([]byte(block), &rows) != nil ||
		len(rows.Results) != 1 || rows.Results[0].Title != "杭州天气" || rows.Results[0].Url != "https://example.org/weather?city=hangzhou#today" ||
		rows.Results[0].Snippet != "近日天气资料" || rows.Truncated || strings.Contains(block, "正文片段") {
		t.Fatal("source summary did not match the original page")
	}
	for _, downgrade := range []func(Event) Event{NativeEventV8, NativeEventV10} {
		old := downgrade(Event{Payload: EventPayload{Native: &native.NativeNotification{Item: &item}}}).Payload.Native.Item
		if len(old.Mcp.Texts) != 0 || *old.ArgumentsSummary == "杭州天气" || len(item.Mcp.Texts) != 1 {
			t.Fatal("legacy projection exposed details or mutated the original")
		}
	}
	old, err := projectNativeItem(wire, "", false)
	if err != nil || old.Mcp.ResultKind != native.Omitted || len(old.Mcp.Texts) != 0 || *old.ArgumentsSummary != "参数不可展示" {
		t.Fatal("thread history backfilled an older call")
	}
}

func TestDeepSearchSourceURLKeepsDisplayableSite(t *testing.T) {
	const site = "https://example.org"
	for _, address := range []string{site, site + "/travel/weekly-guide-to-visiting-west-lake-in-autumn"} {
		if got := deepSearchSourceURL(address); got != site {
			t.Errorf("expected the recorded site origin, got %q", got)
		}
	}
}

func TestObservedDeepWebSearchOutcome(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		isError    bool
		wantKind   native.NativeMcpDisplayResultKind
		wantStatus native.NativeItemStatus
	}{
		{"empty success", `{"results":[],"truncated":false,"dropped_results":0}`, false, native.Text, native.NativeItemStatusCompleted},
		{"partial success", `{"results":[],"truncated":true,"dropped_results":2}`, false, native.Text, native.NativeItemStatusCompleted},
		{"public error", `{"error":{"code":"quota_exceeded","message":"网页检索额度已用尽。"}}`, true, native.Text, native.NativeItemStatusFailed},
		{"unavailable details", "服务未提供详情", false, native.Omitted, native.NativeItemStatusCompleted},
	} {
		t.Run(tc.name, func(t *testing.T) {
			wire := rawJSON(t, map[string]any{"id": "search", "type": "mcpToolCall", "server": web.ServerName,
				"tool": web.KimiWebSearchPro, "status": "completed", "result": map[string]any{
					"isError": tc.isError, "content": []any{map[string]any{"type": "text", "text": tc.body}},
				}})
			item, err := projectNativeItemWithSearchDetails(wire, "", false, true)
			if err != nil || item.Mcp.ResultKind != tc.wantKind || item.Status == nil || *item.Status != tc.wantStatus {
				t.Fatal("search result or status misclassified")
			}
			if tc.wantKind == native.Text && !tc.isError {
				var display web.SearchSuccess
				if json.Unmarshal([]byte(item.Mcp.Texts[0].Text), &display) != nil || display.Truncated != (tc.name == "partial success") {
					t.Fatal("incorrect partial-result flag")
				}
			}
		})
	}
}
