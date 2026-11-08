package codex

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func toolSearchConfig(c *Config) { c.InteractionToolsEnabled, c.ToolSearchEnabled = true, true }

// A normal HTTP MCP server with ordinary read-only data. No executable stand-in,
// injected failure, credential or external platform is used.
func ordinarySearchMCP(t *testing.T, short ...bool) (*httptest.Server, *atomic.Int64) {
	t.Helper()
	calls := &atomic.Int64{}
	description := "Read the ordinary sample lookup value. "
	if len(short) == 0 || !short[0] {
		description += strings.Repeat("Ordinary schema documentation. ", 650)
	}
	description += "COMPLETE_DEFINITION_END"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		var request struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if json.NewDecoder(r.Body).Decode(&request) != nil {
			w.WriteHeader(400)
			return
		}
		if len(request.ID) == 0 {
			w.WriteHeader(202)
			return
		}
		var result any
		switch request.Method {
		case "initialize":
			result = map[string]any{"protocolVersion": "2025-03-26", "capabilities": map[string]any{"tools": map[string]any{}}, "serverInfo": map[string]any{"name": "ordinary-search", "version": "1"}}
		case "tools/list":
			result = map[string]any{"tools": []any{map[string]any{
				"name": "lookup_value", "description": description,
				"inputSchema": map[string]any{"type": "object", "properties": map[string]any{"key": map[string]any{"type": "string", "description": "The ordinary sample key."}}, "required": []string{"key"}, "additionalProperties": false},
				"annotations": map[string]any{"readOnlyHint": true, "destructiveHint": false, "idempotentHint": true, "openWorldHint": false},
			}}}
		case "tools/call":
			var p struct {
				Name      string            `json:"name"`
				Arguments map[string]string `json:"arguments"`
			}
			if json.Unmarshal(request.Params, &p) != nil || p.Name != "lookup_value" || p.Arguments["key"] != "blue" {
				t.Error("ordinary MCP call mismatch")
			}
			calls.Add(1)
			result = map[string]any{"content": []any{map[string]any{"type": "text", "text": "ordinary sample blue = 42"}}, "isError": false}
		case "ping":
			result = map[string]any{}
		default:
			t.Errorf("unexpected MCP method %s", request.Method)
			result = map[string]any{}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": request.ID, "result": result})
	}))
	t.Cleanup(server.Close)
	return server, calls
}

func toolSearchContext(endpoint string) context.Context {
	return WithMarketPrepared(feat164AuxCtx(), &MarketPrepared{Config: map[string]any{"mcp_servers": map[string]any{"ordinary": map[string]any{"url": endpoint, "enabled": true}}}})
}

func assertSearchDeclaration(t *testing.T, body map[string]any, enabled bool) {
	t.Helper()
	found := false
	tools, _ := body["tools"].([]any)
	for _, raw := range tools {
		tool, _ := raw.(map[string]any)
		if tool["type"] == "tool_search" {
			t.Error("unsupported native search wire leaked")
		}
		if tool["name"] == "tool_search" {
			found = tool["type"] == "function"
		}
	}
	if found != enabled {
		t.Errorf("search declaration mismatch: got %v want %v", found, enabled)
	}
}

func decodedSearchResult(t *testing.T, body map[string]any, call string) map[string]any {
	t.Helper()
	var text string
	var value map[string]any
	if json.Unmarshal([]byte(nativeToolsOutput(body, call)), &text) != nil || json.Unmarshal([]byte(text), &value) != nil {
		t.Error("search result is not complete JSON")
	}
	return value
}

func TestToolSearchRuntimeDiscoveryInvocationAndDisable(t *testing.T) {
	server, calls := ordinarySearchMCP(t)
	var stage atomic.Int64
	var perStage [4]atomic.Int64
	h := newNativeToolsHarness(t, func(_ *nativeToolsHarness, body map[string]any, _ int64) map[string]any {
		active := stage.Load()
		n := perStage[active].Add(1)
		assertSearchDeclaration(t, body, active == 0 || active == 3)
		declarations, _ := json.Marshal(body["tools"])
		if active == 0 || active == 3 {
			if strings.Contains(string(declarations), "lookup_value") || strings.Contains(string(declarations), "COMPLETE_DEFINITION_END") {
				t.Error("full deferred definition remained in initial tools/exec description")
			}
			switch n {
			case 1:
				return feat164AuxTool("", "tool_search", fmt.Sprintf("search-%d", active), map[string]any{"query": "sample lookup value", "limit": 1})
			case 2:
				result := decodedSearchResult(t, body, fmt.Sprintf("search-%d", active))
				encoded, _ := json.Marshal(result)
				if !strings.Contains(string(encoded), "COMPLETE_DEFINITION_END") || !strings.Contains(string(encoded), "mcp__ordinary__lookup_value") {
					t.Error("complete native definition/exec name missing")
				}
				return feat164AuxTool("", "exec", fmt.Sprintf("lookup-%d", active), map[string]any{"input": "const value = await tools.mcp__ordinary__lookup_value({key: 'blue'}); text(value);"})
			case 3:
				if !strings.Contains(nativeToolsOutput(body, fmt.Sprintf("lookup-%d", active)), "blue = 42") {
					t.Error("native MCP result missing")
				}
				return feat164AuxText("普通查询完成，值为42。")
			default:
				t.Error("unexpected search continuation")
			}
		} else if active == 1 && !strings.Contains(string(declarations), "lookup_value") {
			t.Error("disabling search did not restore direct tools")
		}
		return feat164AuxText("普通历史保持。")
	}, toolSearchConfig)
	ctx := toolSearchContext(server.URL)
	thread, err := h.manager.StartThread(ctx, h.cwd)
	if err != nil {
		t.Fatal(err)
	}
	h.turn(t, KimiK31ProfileID, thread.ID, "Find the ordinary sample lookup tool and read blue.")
	if calls.Load() != 1 {
		t.Fatal("native tool was not invoked once", calls.Load())
	}
	history, err := h.manager.ReadThread(context.Background(), thread.ID)
	if err != nil || !strings.Contains(string(history), "mcpToolCall") {
		t.Fatal("native MCP history missing", err)
	}
	// Normal EOF restart; same retained Home and ordinary conversation.
	stage.Store(1)
	cfg := h.manager.config
	cfg.ToolSearchEnabled = false
	h.restart(t, cfg)
	if _, err = h.manager.ResumeThread(ctx, thread.ID); err != nil {
		t.Fatal(err)
	}
	if h.calls.Load() != 3 {
		t.Fatal("resume issued inference")
	}
	h.turn(t, KimiK31ProfileID, thread.ID, "Continue without searching.")
	if calls.Load() != 1 {
		t.Fatal("history replayed a business operation")
	}
	// Native idle switch to the unqualified model with an empty current selection.
	stage.Store(2)
	mini := WithMarketPrepared(WithPermissionMode(WithModelProfile(context.Background(), "minimax-m3-high-v1"), PermissionAsk), &MarketPrepared{Config: map[string]any{"mcp_servers": map[string]any{}}})
	if _, err = h.manager.SwitchThread(mini, thread.ID); err != nil {
		t.Fatal(err)
	}
	h.turn(t, "minimax-m3-high-v1", thread.ID, "Ordinary model switch, no search.")
	// Re-enable in a fresh owned process and resume the same history.
	stage.Store(3)
	cfg.ToolSearchEnabled = true
	h.restart(t, cfg)
	if _, err = h.manager.ResumeThread(ctx, thread.ID); err != nil {
		t.Fatal(err)
	}
	h.turn(t, KimiK31ProfileID, thread.ID, "Find the ordinary lookup tool again and read blue.")
	if calls.Load() != 2 {
		t.Fatal("unexpected ordinary tool count", calls.Load())
	}
	t.Log("8 loopback model requests; native search -> exec -> MCP, complete long definitions, normal disable/resume/model switch/re-enable; 0 supplier calls")
}

func TestToolSearchRuntimeEmptyAndAuxiliaryScope(t *testing.T) {
	server, calls := ordinarySearchMCP(t)
	var stage atomic.Int64
	h := newNativeToolsHarness(t, func(_ *nativeToolsHarness, body map[string]any, n int64) map[string]any {
		if stage.Load() == 0 {
			assertSearchDeclaration(t, body, true)
			if n == 1 {
				return feat164AuxTool("", "tool_search", "empty-result", map[string]any{"query": "unmatchedzebraqxy"})
			}
			result := decodedSearchResult(t, body, "empty-result")
			values, _ := result["tools"].([]any)
			if len(values) != 0 {
				t.Error("unexpected search match")
			}
			return feat164AuxText("没有匹配的工具。")
		}
		assertSearchDeclaration(t, body, false)
		if strings.Contains(nativeToolsUserText(body), "Produce an ordinary title only.") {
			declarations, _ := json.Marshal(body["tools"])
			if !strings.Contains(string(declarations), "lookup_value") {
				t.Error("auxiliary catalogue probe had no MCP tools")
			}
		}
		return feat164AuxText(`{"title":"普通标题"}`)
	}, toolSearchConfig)
	thread, err := h.manager.StartThread(toolSearchContext(server.URL), h.cwd)
	if err != nil {
		t.Fatal(err)
	}
	h.turn(t, KimiK31ProfileID, thread.ID, "Search an ordinary absent term.")
	stage.Store(1)
	if _, err = h.manager.GenerateTitle(feat164AuxCtx(), "普通标题"); err != nil {
		t.Fatal(err)
	}
	// Real native ephemeral path with available MCP metadata, no ordinary
	// catalogue override. This distinguishes scope from an empty catalogue.
	var auxiliary threadResponse
	if err = h.manager.request(context.Background(), RuntimeMethodThreadStart, map[string]any{
		"model": "kimi-k3.1-pro", "modelProvider": "kimi", "cwd": h.cwd, "approvalPolicy": "never", "sandbox": "read-only", "ephemeral": true,
		"config": map[string]any{"mcp_servers": map[string]any{"ordinary": map[string]any{"url": server.URL, "enabled": true}}},
	}, &auxiliary); err != nil {
		t.Fatal(err)
	}
	var result struct {
		Turn struct {
			ID string `json:"id"`
		} `json:"turn"`
	}
	if err = h.manager.request(context.Background(), RuntimeMethodTurnStart, map[string]any{"threadId": auxiliary.Thread.ID, "input": []any{map[string]any{"type": "text", "text": "Produce an ordinary title only."}}}, &result); err != nil {
		t.Fatal(err)
	}
	status, _, err := h.notifications.waitForTurn(result.Turn.ID, 20*time.Second)
	if err != nil || status != "completed" {
		t.Fatal(status, err)
	}
	if calls.Load() != 0 {
		t.Fatal("discovery or title executed a tool")
	}
}

func TestToolSearchRuntimeChildAndInputOnlyBoundaries(t *testing.T) {
	server, calls := ordinarySearchMCP(t)
	var parents, children atomic.Int64
	h := newNativeToolsHarness(t, func(_ *nativeToolsHarness, body map[string]any, _ int64) map[string]any {
		if strings.Contains(nativeToolsUserText(body), "FEAT170_CHILD") {
			children.Add(1)
			assertSearchDeclaration(t, body, false)
			bytes, _ := json.Marshal(body["tools"])
			if strings.Contains(string(bytes), "lookup_value") {
				t.Error("child inherited root MCP")
			}
			return feat164AuxText("子任务普通计算完成。")
		}
		if strings.Contains(nativeToolsUserText(body), "FEAT170_DRAFT") {
			tools, _ := body["tools"].([]any)
			if len(tools) != 0 {
				t.Error("input-only tools changed")
			}
			return feat164AuxText(`{"schema_version":1,"kind":"needs_clarification","missing_fields":["content"],"question":"需要定时执行什么？"}`)
		}
		assertSearchDeclaration(t, body, true)
		switch parents.Add(1) {
		case 1:
			return feat164AuxTool("", "collaboration__spawn_agent", "search-child", map[string]any{"task_name": "ordinary_child", "message": "FEAT170_CHILD: calculate 6 * 7 without tools.", "fork_turns": "none"})
		case 2:
			return feat164AuxTool("", "collaboration__wait_agent", "search-child-wait", map[string]any{"timeout_ms": 10000})
		default:
			return feat164AuxText("主线程工具范围保持。")
		}
	}, toolSearchConfig)
	thread, err := h.manager.StartThread(toolSearchContext(server.URL), h.cwd)
	if err != nil {
		t.Fatal(err)
	}
	h.turn(t, KimiK31ProfileID, thread.ID, "Delegate an ordinary calculation.")
	draft := WithScheduledDraft(WithModelProfile(context.Background(), KimiK31ProfileID), "ordinary-search-draft")
	private, err := h.manager.StartThread(draft, h.cwd)
	if err != nil {
		t.Fatal(err)
	}
	input, _ := TextUserInput("FEAT170_DRAFT: prepare a scheduled task without enough details.")
	turn, err := h.manager.StartTurnV2(draft, private.ID, []UserInput{input}, "max")
	if err != nil {
		t.Fatal(err)
	}
	status, _, err := h.notifications.waitForTurn(turn.ID, 25*time.Second)
	if err != nil || status != "completed" {
		t.Fatal(status, err)
	}
	if children.Load() != 1 || calls.Load() != 0 {
		t.Fatal("unexpected child/discovery execution", children.Load(), calls.Load())
	}
}

func TestToolSearchRuntimeCancelAndNextTurn(t *testing.T) {
	server, calls := ordinarySearchMCP(t)
	followUp := make(chan struct{}, 1)
	h := newNativeToolsHarness(t, func(_ *nativeToolsHarness, body map[string]any, n int64) map[string]any {
		switch n {
		case 1:
			return feat164AuxTool("", "tool_search", "cancel-search", map[string]any{"query": "sample lookup"})
		case 2:
			select {
			case followUp <- struct{}{}:
			default:
			}
			return feat164AuxTool("", "exec", "ordinary-wait", map[string]any{"input": "await new Promise(resolve => setTimeout(resolve, 1500)); text('ordinary wait complete');"})
		default:
			return feat164AuxText("新一轮正常结束。")
		}
	}, toolSearchConfig)
	ctx := toolSearchContext(server.URL)
	thread, err := h.manager.StartThread(ctx, h.cwd)
	if err != nil {
		t.Fatal(err)
	}
	input, _ := TextUserInput("Search tools, then wait briefly.")
	turn, err := h.manager.StartTurnV2(feat164AuxCtx(), thread.ID, []UserInput{input}, "max")
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-followUp:
	case <-time.After(20 * time.Second):
		t.Fatal("search did not continue")
	}
	if err := h.manager.InterruptTurn(context.Background(), thread.ID, turn.ID); err != nil {
		t.Fatal(err)
	}
	status, _, err := h.notifications.waitForTurn(turn.ID, 20*time.Second)
	if err != nil || status != "interrupted" {
		t.Fatal(status, err)
	}
	h.turn(t, KimiK31ProfileID, thread.ID, "Continue with an ordinary new reply, no tool use.")
	if calls.Load() != 0 {
		t.Fatal("cancelled discovery replayed a business call")
	}
}
