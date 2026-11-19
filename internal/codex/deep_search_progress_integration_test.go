package codex

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	wire "github.com/36Dge/yijie-agent-host/internal/contracts/builtinweb"
	"github.com/google/uuid"
)

// This is a normal local MCP peer. The model transcript is chosen by the
// fixture so it can deliberately ask again after receiving the correction.
func TestScopedSearchSamplingBoundaries(t *testing.T) {
	for _, profile := range []string{KimiK31ProfileID, "minimax-m3-high-v1"} {
		for _, mixed := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/mixed=%t", profile, mixed), func(t *testing.T) {
				var mu sync.Mutex
				aliases := map[string]string{}
				calls := 0
				var corrections atomic.Int32
				var descriptors []map[string]any
				_ = json.Unmarshal(wire.Tools, &descriptors)
				for _, tool := range descriptors {
					if tool["name"] == "kimi_web_search_pro" {
						tool["_meta"] = map[string]any{"ai.yijie/coalescesExactTurnRequests": true, "ai.yijie/searchReceiptVersion": 2}
					}
				}
				peer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					var req struct {
						ID     json.RawMessage
						Method string
						Params struct {
							Name      string
							Arguments map[string]any
						}
					}
					if r.Header.Get("Authorization") != "Bearer "+strings.Repeat("a", 64) || json.NewDecoder(r.Body).Decode(&req) != nil {
						w.WriteHeader(403)
						return
					}
					if len(req.ID) == 0 {
						w.WriteHeader(202)
						return
					}
					var result any
					switch req.Method {
					case "initialize":
						result = map[string]any{"protocolVersion": "2025-03-26", "capabilities": map[string]any{"tools": map[string]any{}}, "serverInfo": map[string]any{"name": "scoped-local", "version": "2"}}
					case "tools/list":
						result = map[string]any{"tools": descriptors}
					case "tools/call":
						query, _ := req.Params.Arguments["query"].(string)
						mu.Lock()
						calls++
						ref := aliases[query]
						rel := "reused"
						if ref == "" {
							id, _ := uuid.NewV7()
							ref = id.String()
							aliases[query] = ref
							rel = "first"
						}
						mu.Unlock()
						rows := []any{}
						outcome := "empty"
						if query == "weather" {
							outcome = "results"
							rows = append(rows, map[string]any{"title": "weather source", "url": "https://example.org/weather", "date": "", "site_name": "doc", "snippet": "weather summary", "authority": "", "content_status": "available", "chunks": []any{map[string]any{"text": "selected new weather passage", "score": 1}}})
						}
						business := map[string]any{"results": rows, "truncated": false, "dropped_results": 0}
						raw, _ := json.Marshal(business)
						fact := map[string]any{"version": 2, "relation": rel, "operation_id": ref, "dispatch": "responded", "outcome": outcome, "availability": "available"}
						result = map[string]any{"content": []any{map[string]any{"type": "text", "text": string(raw)}}, "structuredContent": business, "isError": false, "_meta": map[string]any{"ai.yijie/deep-search-receipt": fact}}
					default:
						result = map[string]any{}
					}
					w.Header().Set("Content-Type", "application/json")
					_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": result})
				}))
				defer peer.Close()
				h := newNativeToolsHarnessWithSetup(t, func(_ *nativeToolsHarness, body map[string]any, n int64) map[string]any {
					if n == 1 && profile == KimiK31ProfileID {
						return feat164AuxTool("", "tool_search", "discover", map[string]any{"query": "kimi_web_search_pro web research", "limit": 3})
					}
					if profile == KimiK31ProfileID {
						n--
					}
					declarations, _ := body["tools"].([]any)
					if len(declarations) == 0 {
						if mixed || n != 4 {
							t.Errorf("premature or late finalization: step %d mixed %v", n, mixed)
						}
						return feat164AuxText("现有证据不足。")
					}
					if n > 6 {
						t.Error("unbounded pure repeat")
						return feat164AuxText("正常结束。")
					}
					name := "mcp__yijie_web__kimi_web_search_pro"
					js := "text(await tools." + name + "({query:'temple'}));"
					if n == 3 {
						raw, _ := json.Marshal(body["input"])
						if strings.Contains(string(raw), "The last step only requested") {
							corrections.Add(1)
						} else {
							t.Errorf("missing scoped correction; ordinary=%t model output=%s", strings.Contains(string(raw), "[yijie-answer-completion-v1]"), nativeToolsOutput(body, "q2"))
						}
					}
					if mixed && n == 3 {
						js = "const r=await Promise.all(['temple','weather'].map(query=>tools." + name + "({query})));r.forEach((r,i)=>text(JSON.stringify({i,r})));text(r[1].structuredContent.results[0].chunks[0].text);"
					}
					if mixed && n == 4 {
						mixedOutput := nativeToolsOutput(body, "q3")
						if !strings.Contains(mixedOutput, "selected new weather passage") || !strings.Contains(mixedOutput, "no_matches") {
							t.Error("mixed sampling step did not deliver both the empty receipt and the new source passage")
						}
						js = "const r=await tools." + name + "({query:'weather'});text(r.structuredContent.results[0].chunks[0].text);"
					}
					if mixed && n == 5 {
						if !strings.Contains(nativeToolsOutput(body, "q4"), "selected new weather passage") {
							t.Error("retained passage not delivered")
						}
						return feat164AuxText("杭州天气资料已收集。")
					}
					if profile != KimiK31ProfileID && !mixed {
						return feat164AuxTool("", name, fmt.Sprintf("q%d", n), map[string]any{"query": "temple"})
					}
					return feat164AuxTool("", "exec", fmt.Sprintf("q%d", n), map[string]any{"input": js})
				}, func(m *Manager) {
					if err := m.SetBuiltinWebMCP(peer.URL, strings.Repeat("a", 64), nil, nil); err != nil {
						t.Fatal(err)
					}
				}, feat175Config)
				ctx := WithPermissionMode(WithModelProfile(context.Background(), profile), PermissionAsk)
				thread, err := h.manager.StartThread(ctx, h.cwd)
				if err != nil {
					t.Fatal(err)
				}
				turn := feat175Start(t, h, profile, thread.ID, "请从已有来源研究杭州预约和天气，有缺口时说明。")
				feat175Wait(t, h, thread.ID, turn.ID, "completed", 45*time.Second)
				mu.Lock()
				defer mu.Unlock()
				want := 3
				if mixed {
					want = 5
				}
				if calls != want || corrections.Load() != 1 {
					t.Fatalf("calls=%d want=%d corrections=%d", calls, want, corrections.Load())
				}
			})
		}
	}
}
