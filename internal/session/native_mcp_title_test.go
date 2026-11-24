package session

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestDiscoveredMCPRowUsesTheBoundMarketMethod(t *testing.T) {
	const method = "lookup_stock"
	tool := marketplaceToolName("caoliao", method)
	receipt := map[string]any{
		"schemaVersion": 1, "callRef": "15700000-0000-4000-8000-000000000001", "serviceId": "caoliao",
		"reference":      map[string]any{"installationId": "15700000-0000-4000-8000-000000000002", "revision": 1, "generation": 1},
		"nativeThreadId": "rt", "nativeTurnId": "turn", "turnOperationId": "15700000-0000-4000-8000-000000000003",
		"selectionDigest": strings.Repeat("b", 64), "argsDigest": strings.Repeat("a", 64), "argsEncoding": "worker-json-v1",
		"toolName": tool, "decisionReceipt": nil, "admissionConsumed": false,
	}
	for _, sample := range []struct{ method, want string }{{method, "lookup stock · PEN-12"}, {"", "使用 caoliao · PEN-12"}} {
		meta := map[string]any{"yijie.market.call": receipt}
		if sample.method != "" {
			meta["yijie.market.method"] = sample.method
		}
		raw, _ := json.Marshal(map[string]any{"id": "ordinary-item", "type": "mcpToolCall", "status": "completed", "server": "yijie_market", "tool": tool, "arguments": map[string]any{"sku": "PEN-12"}, "result": map[string]any{"_meta": meta, "content": []any{}}})
		out, err := projectNativeItem(raw, "", false)
		if err != nil || out.ToolDisplay == nil || out.ToolDisplay.Title == nil || *out.ToolDisplay.Title != sample.want {
			t.Fatalf("wanted %s, got %#v (%v)", sample.want, out.ToolDisplay, err)
		}
	}
}

func TestMCPRowTitleAcrossProviders(t *testing.T) {
	tests := []struct {
		server, method, args, title string
	}{
		{"inventory", "lookup_stock", `{"sku":"PEN-12"}`, "lookup stock · PEN-12"},
		{"weather", "forecast", `{"city":"西安"}`, "forecast · 西安"},
		{"catalog", "search_items", `{"request":{"keyword":"gel pen","page":2}}`, "search items · gel pen · 第 2 页"},
		{"calendar", "event_lookup", `{"eventId":"CAL-19"}`, "event lookup · CAL-19"},
		{"library", "catalog_entry", `{"request":{"bookId":"BK-27"}}`, "catalog entry · BK-27"},
		{"library", "find_book", `{"name":"Notes on Ink"}`, "find book · Notes on Ink"},
		{"finance", "daily", `{"ts_code":"000001.SZ","trade_date":"20260102"}`, "daily · 000001.SZ · 20260102"},
		{"yijie_market", marketplaceToolName("sorftime", "product_trend"), `{"asin":"B000000001","type":"DailySalesVolume"}`, "查询商品日销量 · B000000001"},
		{"sorftime", "product_trend", `{"asin":"B000000002","type":"DailySalesVolume"}`, "查询商品日销量 · B000000002"},
		{"sorftime", "category_products_extended", `{"node_id":"1234","page":3}`, "查询类目商品 · 1234 · 第 3 页"},
	}
	for _, tc := range tests {
		t.Run(tc.server+"/"+tc.method, func(t *testing.T) {
			raw, err := json.Marshal(map[string]any{"id": "item", "type": "mcpToolCall", "status": "completed", "server": tc.server, "tool": tc.method, "arguments": json.RawMessage(tc.args), "result": map[string]any{"content": []any{}}})
			if err != nil {
				t.Fatal(err)
			}
			for _, observed := range []bool{true, false} {
				item, err := projectNativeItemWithSearchDetails(raw, "", false, observed)
				if err != nil || item.ToolDisplay == nil || item.ToolDisplay.Title == nil || *item.ToolDisplay.Title != tc.title || item.ToolDisplay.SourceLabel != nil || item.Status == nil || *item.Status != "completed" {
					t.Fatalf("observation=%v title=%#v err=%v", observed, item.ToolDisplay, err)
				}
			}
		})
	}
}
