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

	"github.com/google/uuid"
)

func TestNativeToolsRuntimeMarketStartup(t *testing.T) {
	manifest := os.Getenv("YIJIE_NATIVE_TOOLS_WORKER_MANIFEST")
	if manifest == "" {
		t.Skip("canonical local market worker required")
	}
	h := newNativeToolsHarness(t, func(_ *nativeToolsHarness, _ map[string]any, _ int64) map[string]any {
		t.Error("startup must not request model inference")
		return feat164AuxText("Unexpected startup inference.")
	}, func(cfg *Config) {
		cfg.Market = MarketConfig{Manifest: manifest, LibraryHome: filepath.Join(cfg.CodexHome, "market-worker", "library-home"), HostInstanceID: uuid.NewString()}
	})
	if !h.manager.Snapshot().Ready || h.calls.Load() != 0 {
		t.Fatal("native tools and original market isolation did not start together")
	}
}

func TestNativeToolsRuntimeChildMCPIsolation(t *testing.T) {
	var discovery, parent, child atomic.Int64
	mcp := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		var req struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
		}
		if json.NewDecoder(r.Body).Decode(&req) != nil {
			w.WriteHeader(400)
			return
		}
		if len(req.ID) == 0 {
			w.WriteHeader(202)
			return
		}
		var result any
		switch req.Method {
		case "initialize":
			discovery.Add(1)
			result = map[string]any{"protocolVersion": "2025-03-26", "capabilities": map[string]any{"tools": map[string]any{}}, "serverInfo": map[string]any{"name": "ordinary-notes", "version": "1"}}
		case "tools/list":
			result = map[string]any{"tools": []any{map[string]any{"name": "read_note", "description": "Read the ordinary sample note.", "inputSchema": map[string]any{"type": "object", "properties": map[string]any{}}}}}
		case "ping":
			result = map[string]any{}
		default:
			t.Errorf("unexpected ordinary MCP method %s", req.Method)
			result = map[string]any{}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": result})
	}))
	t.Cleanup(mcp.Close)
	h := newNativeToolsHarness(t, func(_ *nativeToolsHarness, body map[string]any, _ int64) map[string]any {
		encoded, _ := json.Marshal(body["tools"])
		if strings.Contains(nativeToolsUserText(body), "FEAT169_MCP_CHILD") {
			child.Add(1)
			if strings.Contains(string(encoded), "mcp__ordinary__") {
				t.Error("root MCP discovery was inherited by child")
			}
			return feat164AuxText("Child completed its ordinary calculation.")
		}
		switch parent.Add(1) {
		case 1:
			if !strings.Contains(string(encoded), "mcp__ordinary__read_note") {
				t.Error("root native MCP discovery missing")
			}
			return feat164AuxTool("", "collaboration__spawn_agent", "isolated169", map[string]any{"task_name": "isolated_child", "message": "FEAT169_MCP_CHILD: calculate 6 * 7 without tools.", "fork_turns": "none"})
		case 2:
			return feat164AuxTool("", "collaboration__wait_agent", "isolation_wait169", map[string]any{"timeout_ms": 10000})
		default:
			return feat164AuxText("Parent kept its selected MCP connection; child used native tools only.")
		}
	})
	ctx := WithMarketPrepared(feat164AuxCtx(), &MarketPrepared{Config: map[string]any{"mcp_servers": map[string]any{"ordinary": map[string]any{"url": mcp.URL, "enabled": true}}}})
	thread, err := h.manager.StartThread(ctx, h.cwd)
	if err != nil {
		t.Fatal(err)
	}
	h.turn(t, KimiK31ProfileID, thread.ID, "Use a child for an ordinary local calculation.")
	if discovery.Load() != 1 || child.Load() != 1 {
		t.Fatal("unexpected MCP inheritance/discovery", discovery.Load(), child.Load())
	}
}

func TestNativeToolsRuntimeInputOnly(t *testing.T) {
	for _, profile := range []string{KimiK31ProfileID, "minimax-m3-high-v1"} {
		t.Run(profile, func(t *testing.T) {
			h := newNativeToolsHarness(t, func(_ *nativeToolsHarness, body map[string]any, _ int64) map[string]any {
				tools, _ := body["tools"].([]any)
				if len(tools) != 0 || body["tool_choice"] != "auto" {
					t.Error("input-only native empty registry/upstream choice differs", len(tools), body["tool_choice"])
				}
				return feat164AuxText(`{"schema_version":1,"kind":"needs_clarification","missing_fields":["content"],"question":"需要定时执行什么任务？"}`)
			})
			ctx := WithScheduledDraft(WithModelProfile(context.Background(), profile), "ordinary-native-tools-draft")
			thread, err := h.manager.StartThread(ctx, h.cwd)
			if err != nil {
				t.Fatal(err)
			}
			input, _ := TextUserInput("准备一个定时任务草稿，尚未提供任务内容。")
			definition, _ := ModelProfile(profile)
			turn, err := h.manager.StartTurnV2(ctx, thread.ID, []UserInput{input}, definition.Effort)
			if err != nil {
				t.Fatal(err)
			}
			state, _, err := h.notifications.waitForTurn(turn.ID, 30*time.Second)
			if err != nil || state != "completed" {
				t.Fatal(state, err)
			}
			if h.calls.Load() != 1 {
				t.Fatal("unexpected draft inference")
			}
		})
	}
}
