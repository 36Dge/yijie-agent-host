package codex

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	wire "github.com/36Dge/yijie-agent-host/internal/contracts/builtinweb"
)

// Capture the actual next provider request from the retained Kimi Runtime.
// The local MCP reads the fixed replay's validated gateway results.
func TestRecordedKimiSearchModelReceivesThreeTopics(t *testing.T) {
	dir := os.Getenv("SCOPED_REPLAY_DIRECTORY")
	if dir == "" {
		t.Skip("requires recorded Hangzhou gateway results")
	}
	raw, err := os.ReadFile(filepath.Join(dir, "mcp-three.json"))
	if err != nil {
		t.Fatal(err)
	}
	var rows []struct {
		Arguments map[string]any
		Result    json.RawMessage
	}
	if err = json.Unmarshal(raw, &rows); err != nil {
		t.Fatal(err)
	}
	byQuery := map[string]json.RawMessage{}
	var args []map[string]any
	for _, row := range rows {
		q := row.Arguments["query"].(string)
		if _, ok := byQuery[q]; !ok {
			args = append(args, row.Arguments)
			byQuery[q] = row.Result
		}
	}
	if len(args) != 3 {
		t.Fatal("missing fixed research topic")
	}
	var descriptors []map[string]any
	_ = json.Unmarshal(wire.Tools, &descriptors)
	for _, item := range descriptors {
		if item["name"] == "kimi_web_search_pro" {
			item["_meta"] = map[string]any{"ai.yijie/coalescesExactTurnRequests": true, "ai.yijie/searchReceiptVersion": 2}
		}
	}
	local := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var frame struct {
			ID     json.RawMessage
			Method string
			Params struct{ Arguments map[string]any }
		}
		if r.Header.Get("Authorization") != "Bearer "+strings.Repeat("a", 64) || json.NewDecoder(r.Body).Decode(&frame) != nil {
			w.WriteHeader(403)
			return
		}
		if len(frame.ID) == 0 {
			w.WriteHeader(202)
			return
		}
		var result any
		switch frame.Method {
		case "initialize":
			result = map[string]any{"protocolVersion": "2025-03-26", "capabilities": map[string]any{"tools": map[string]any{}}, "serverInfo": map[string]any{"name": "recorded-hangzhou", "version": "2"}}
		case "tools/list":
			result = map[string]any{"tools": descriptors}
		case "tools/call":
			q, _ := frame.Params.Arguments["query"].(string)
			result = byQuery[q]
		default:
			result = map[string]any{}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": frame.ID, "result": result})
	}))
	defer local.Close()
	var seen atomic.Bool
	h := newNativeToolsHarnessWithSetup(t, func(_ *nativeToolsHarness, body map[string]any, n int64) map[string]any {
		switch n {
		case 1:
			return feat164AuxTool("", "tool_search", "discovery", map[string]any{"query": "kimi_web_search_pro web research", "limit": 3})
		case 2:
			encoded, _ := json.Marshal(args)
			return feat164AuxTool("", "exec", "three", map[string]any{"input": "const rows=await Promise.all(" + string(encoded) + ".map(a=>tools.mcp__yijie_web__kimi_web_search_pro(a)));text(rows);"})
		case 3:
			var display []struct{ Text string }
			output := nativeToolsOutput(body, "three")
			var text strings.Builder
			if json.Unmarshal([]byte(output), &display) == nil {
				for _, row := range display {
					text.WriteString(row.Text)
				}
			} else {
				var s string
				_ = json.Unmarshal([]byte(output), &s)
				text.WriteString(s)
			}
			_, data, ok := strings.Cut(text.String(), "Output:")
			var views []struct {
				Query     string
				Count     int
				SearchRef string `json:"search_ref"`
				Results   []struct {
					Index int
					URL   string
				}
			}
			if !ok || json.Unmarshal([]byte(strings.TrimSpace(data)), &views) != nil || len(views) != 3 {
				t.Error("Kimi received an incomplete batch")
				break
			}
			for _, v := range views {
				var original struct {
					Structured struct{ Results []struct{ URL string } } `json:"structuredContent"`
				}
				if err := json.Unmarshal(byQuery[v.Query], &original); err != nil || v.Count != 5 || v.SearchRef == "" || len(v.Results) != 5 || len(original.Structured.Results) != 5 {
					t.Errorf("missing retained sources for %s", v.Query)
					continue
				}
				for j, s := range v.Results {
					if s.Index != j || s.URL != original.Structured.Results[j].URL {
						t.Errorf("Kimi did not receive original source %s[%d]", v.Query, j)
					}
				}
			}
			seen.Store(true)
			t.Logf("Kimi provider input: 3 themes, 15 indexed sources, %d bytes", len(data))
		default:
			t.Error("unexpected continuation")
		}
		return feat164AuxText("已收到预约、票价和天气方面的检索资料。")
	}, func(m *Manager) {
		if err := m.SetBuiltinWebMCP(local.URL, strings.Repeat("a", 64), nil, nil); err != nil {
			t.Fatal(err)
		}
	}, feat175Config)
	ctx := WithPermissionMode(WithModelProfile(context.Background(), KimiK31ProfileID), PermissionAsk)
	thread, err := h.manager.StartThread(ctx, h.cwd)
	if err != nil {
		t.Fatal(err)
	}
	turn := feat175Start(t, h, KimiK31ProfileID, thread.ID, "核对杭州预约、游船和天气三个主题。")
	feat175Wait(t, h, thread.ID, turn.ID, "completed", 45*time.Second)
	if !seen.Load() {
		t.Fatal("next Kimi input was not observed")
	}
}
