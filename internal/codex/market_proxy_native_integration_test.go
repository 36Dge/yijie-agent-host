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

	broker "github.com/36Dge/yijie-agent-host/internal/contracts/marketbrokercontrol"
	"github.com/google/uuid"
)

// Real fixed Runtime and native MCP transport with ordinary loopback protocol
// peers. This isolates the native Approve/server-elicitation distinction; the
// product Broker's dual decision/HTTP gate is exercised in worker unit tests.
// No supplier, model account, replacement executable, or live user Home.
func TestMarketProxyNativeElicitationAndResultMetadata(t *testing.T) {
	integrationArtifactPaths(t)
	var business, prompts atomic.Int64
	var mu sync.Mutex
	pending := map[string]chan string{}
	refs := map[string]bool{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(405)
			return
		}
		var request struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
			Result json.RawMessage `json:"result"`
		}
		if json.NewDecoder(r.Body).Decode(&request) != nil {
			w.WriteHeader(400)
			return
		}
		if len(request.ID) == 0 {
			w.WriteHeader(202)
			return
		}
		if request.Method == "" {
			var id string
			var result struct {
				Action string `json:"action"`
			}
			_ = json.Unmarshal(request.ID, &id)
			_ = json.Unmarshal(request.Result, &result)
			mu.Lock()
			reply := pending[id]
			mu.Unlock()
			if reply != nil {
				select {
				case reply <- result.Action:
				default:
				}
			}
			w.WriteHeader(202)
			return
		}
		respond := func(result any) {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": request.ID, "result": result})
		}
		switch request.Method {
		case "initialize":
			respond(map[string]any{"protocolVersion": "2025-11-25", "capabilities": map[string]any{"tools": map[string]any{}}, "serverInfo": map[string]any{"name": "ordinary-market-peer", "version": "1"}})
		case "tools/list":
			respond(map[string]any{"tools": []any{map[string]any{"name": "ordinary_lookup", "description": "Read an ordinary local sample after a one-shot decision.", "inputSchema": map[string]any{"type": "object", "properties": map[string]any{"key": map[string]any{"type": "string"}}, "required": []string{"key"}, "additionalProperties": false}, "annotations": map[string]any{"readOnlyHint": false, "destructiveHint": true, "idempotentHint": false, "openWorldHint": true}}}})
		case "tools/call":
			var params struct {
				Name      string                     `json:"name"`
				Arguments map[string]string          `json:"arguments"`
				Meta      map[string]json.RawMessage `json:"_meta"`
			}
			if json.Unmarshal(request.Params, &params) != nil || params.Name != "ordinary_lookup" || params.Arguments["key"] != "blue" {
				t.Error("ordinary call shape changed")
				w.WriteHeader(400)
				return
			}
			var thread string
			var turn struct {
				ID string `json:"turn_id"`
			}
			_ = json.Unmarshal(params.Meta["threadId"], &thread)
			_ = json.Unmarshal(params.Meta["x-codex-turn-metadata"], &turn)
			ref := uuid.NewString()
			id := "approval-" + ref
			reply := make(chan string, 1)
			mu.Lock()
			pending[id] = reply
			refs[ref] = true
			mu.Unlock()
			defer func() { mu.Lock(); delete(pending, id); mu.Unlock() }()
			w.Header().Set("Content-Type", "text/event-stream")
			send := func(v any) {
				b, _ := json.Marshal(v)
				fmt.Fprintf(w, "event: message\ndata: %s\n\n", b)
				w.(http.Flusher).Flush()
			}
			send(map[string]any{"jsonrpc": "2.0", "id": id, "method": "elicitation/create", "params": map[string]any{"mode": "form", "message": "Ordinary local one-shot decision", "requestedSchema": map[string]any{"type": "object", "properties": map[string]any{}}, "_meta": map[string]any{"yijieMarketCallRef": ref, "yijieKind": "gateway_call_approval"}}})
			var action string
			select {
			case action = <-reply:
			case <-r.Context().Done():
				return
			case <-time.After(15 * time.Second):
				t.Error("ordinary elicitation timed out")
				return
			}
			decision := broker.DecisionReject
			if action == "accept" {
				decision = broker.DecisionApproveOnce
				business.Add(1)
			}
			receipt := broker.GatewayCallReceipt{SchemaVersion: 1, CallRef: broker.CanonicalId(ref), ServiceId: "sorftime", Reference: broker.SelectionRef{InstallationId: broker.CanonicalId(uuid.NewString()), Revision: 1, Generation: 1}, NativeThreadId: broker.NativeId(thread), NativeTurnId: broker.NativeId(turn.ID), TurnOperationId: broker.CanonicalId(uuid.NewString()), SelectionDigest: broker.SelectionDigest(strings.Repeat("a", 64)), ToolName: "ordinary_lookup", ArgsDigest: broker.ArgsDigest(strings.Repeat("b", 64)), ArgsEncoding: "worker-json-v1", DecisionReceipt: &broker.GatewayDecisionReceipt{ApprovalId: broker.CanonicalId(uuid.NewString()), DecisionId: broker.CanonicalId(uuid.NewString()), Decision: decision}, AdmissionConsumed: action == "accept"}
			if err := receipt.Validate(); err != nil {
				t.Error(err)
			}
			send(map[string]any{"jsonrpc": "2.0", "id": request.ID, "result": map[string]any{"content": []any{map[string]any{"type": "text", "text": "ordinary sample decision finished"}}, "isError": action != "accept", "_meta": map[string]any{broker.GatewayResultKey: receipt}}})
		case "ping":
			respond(map[string]any{})
		default:
			t.Errorf("unexpected local MCP method %s", request.Method)
			w.WriteHeader(400)
		}
	}))
	t.Cleanup(server.Close)
	var step atomic.Int64
	h := newNativeToolsHarness(t, func(_ *nativeToolsHarness, _ map[string]any, _ int64) map[string]any {
		switch step.Add(1) % 3 {
		case 1:
			return feat164AuxTool("", "tool_search", uuid.NewString(), map[string]any{"query": "ordinary local sample lookup", "limit": 1})
		case 2:
			return feat164AuxTool("", "exec", uuid.NewString(), map[string]any{"input": "text(await tools.mcp__yijie_market__ordinary_lookup({key: 'blue'}));"})
		}
		return feat164AuxText("普通本地调用已结束。")
	}, toolSearchConfig)
	type prompt struct{ reply chan string }
	decisions := make(chan prompt, 1)
	var expectOverride atomic.Bool
	var overridePrompts atomic.Int64
	h.manager.client.setServerRequestHandler(func(ctx context.Context, request serverRequest) serverRequestResult {
		if request.method != "mcpServer/elicitation/request" {
			return h.manager.handleServerRequest(ctx, request)
		}
		var params struct {
			Server string          `json:"serverName"`
			Meta   json.RawMessage `json:"_meta"`
		}
		var meta broker.ElicitationMetadata
		if json.Unmarshal(request.params, &params) != nil || params.Server != marketServerName || json.Unmarshal(params.Meta, &meta) != nil {
			var nativeMeta struct {
				Kind string `json:"codex_approval_kind"`
			}
			if expectOverride.Load() && json.Unmarshal(params.Meta, &nativeMeta) == nil && nativeMeta.Kind == "mcp_tool_call" {
				overridePrompts.Add(1)
				return serverRequestResult{respond: true, value: map[string]any{"action": "decline"}}
			}
			t.Error("duplicate native pre-call prompt reached the gateway-only adapter")
			return serverRequestResult{respond: true, value: map[string]any{"action": "decline"}}
		}
		prompts.Add(1)
		p := prompt{make(chan string, 1)}
		select {
		case decisions <- p:
		case <-ctx.Done():
			return serverRequestResult{}
		}
		select {
		case action := <-p.reply:
			return serverRequestResult{respond: true, value: map[string]any{"action": action, "content": map[string]any{}, "_meta": nil}}
		case <-ctx.Done():
			return serverRequestResult{}
		}
	})
	proxy := ownedMarketServer(server.URL, "ordinary-fixture", false)
	delete(proxy, "bearer_token_env_var") // This in-process protocol peer has no account or capability bearer.
	ctx := WithMarketPrepared(feat164AuxCtx(), &MarketPrepared{Config: map[string]any{"mcp_servers": map[string]any{marketServerName: proxy}, "features.tool_call_mcp_elicitation": true}})
	thread, err := h.manager.StartThread(ctx, h.cwd)
	if err != nil {
		t.Fatal(err)
	}
	for index, action := range []string{"decline", "accept", "accept"} {
		before := business.Load()
		input, _ := TextUserInput("Use the ordinary local sample once.")
		turn, err := h.manager.StartTurnV2(feat164AuxCtx(), thread.ID, []UserInput{input}, "max")
		if err != nil {
			t.Fatal(err)
		}
		select {
		case p := <-decisions:
			if business.Load() != before {
				t.Error("business ran before approval")
			}
			p.reply <- action
		case <-time.After(20 * time.Second):
			_ = h.manager.InterruptTurn(context.Background(), thread.ID, turn.ID)
			t.Fatal("one-shot server elicitation missing")
		}
		status, _, err := h.notifications.waitForTurn(turn.ID, 30*time.Second)
		if err != nil || status != "completed" {
			t.Fatal(status, err)
		}
		want := int64(index)
		if business.Load() != want || prompts.Load() != int64(index+1) {
			t.Fatal("decision count differs from business count", business.Load(), prompts.Load())
		}
	}
	history, err := h.manager.ReadThread(context.Background(), thread.ID)
	if err != nil {
		t.Fatal(err)
	}
	var native struct {
		Turns []struct {
			Items []struct {
				Type   string `json:"type"`
				Result struct {
					Meta map[string]json.RawMessage `json:"_meta"`
				} `json:"result"`
			} `json:"items"`
		} `json:"turns"`
	}
	if json.Unmarshal(history, &native) != nil {
		t.Fatal("native history unavailable")
	}
	retained := 0
	for _, turn := range native.Turns {
		for _, item := range turn.Items {
			if item.Type != "mcpToolCall" {
				continue
			}
			var receipt broker.GatewayCallReceipt
			if json.Unmarshal(item.Result.Meta[broker.GatewayResultKey], &receipt) != nil {
				t.Fatal("native result metadata lost")
			}
			mu.Lock()
			known := refs[string(receipt.CallRef)]
			mu.Unlock()
			if !known {
				t.Fatal("native result has unrelated call identity")
			}
			retained++
		}
	}
	if retained != 3 {
		t.Fatal("native result count", retained)
	}
	// Existing explicit stricter per-tool policy keeps precedence over the owned
	// default. Its native prompt is declined and never reaches this MCP peer.
	expectOverride.Store(true)
	proxy["tools"] = map[string]any{"ordinary_lookup": map[string]any{"approval_mode": "prompt"}}
	if _, err = h.manager.SwitchThread(ctx, thread.ID); err != nil {
		t.Fatal(err)
	}
	h.turn(t, KimiK31ProfileID, thread.ID, "Use the ordinary local sample under the explicit prompt policy.")
	if overridePrompts.Load() != 1 || business.Load() != 2 || prompts.Load() != 3 {
		t.Fatal("explicit per-tool policy was overwritten")
	}
	before := h.calls.Load()
	h.restart(t, h.manager.config)
	history, err = h.manager.ReadThread(context.Background(), thread.ID)
	if err != nil || json.Unmarshal(history, &native) != nil {
		t.Fatal("normal history reopen failed", err)
	}
	retained = 0
	for _, turn := range native.Turns {
		for _, item := range turn.Items {
			if _, ok := item.Result.Meta[broker.GatewayResultKey]; item.Type == "mcpToolCall" && ok {
				retained++
			}
		}
	}
	if retained != 3 || h.calls.Load() != before || business.Load() != 2 {
		t.Fatal("history lost correlation or repeated work")
	}
	t.Log("real native approve + three fresh server elicitations; explicit per-tool prompt preserved; metadata survives normal EOF restart; loopback peers only")
}
