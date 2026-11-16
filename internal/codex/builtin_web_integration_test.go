package codex

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	wire "github.com/36Dge/yijie-agent-host/internal/contracts/builtinweb"
	"github.com/google/uuid"
)

// Ordinary protocol fixture, not a replacement executable or a supplier stub
// presented as real search. All returned material is explicitly synthetic.
type feat175MCP struct {
	server                                       *httptest.Server
	mu                                           sync.Mutex
	turns                                        map[string]context.Context
	cancels                                      map[string]context.CancelFunc
	active                                       map[string]int
	maxActive                                    int
	delay                                        time.Duration
	calls, cancelled, nativeCancelled, approvals atomic.Int64
	entered                                      chan string
}

func newFEAT175MCP(t *testing.T) *feat175MCP {
	t.Helper()
	f := &feat175MCP{turns: make(map[string]context.Context), cancels: make(map[string]context.CancelFunc), active: make(map[string]int), entered: make(chan string, 16)}
	var tools []any
	if err := json.Unmarshal(wire.Tools, &tools); err != nil {
		t.Fatal(err)
	}
	f.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		if r.Header.Get("Authorization") != "Bearer "+strings.Repeat("a", 64) {
			t.Error("missing local MCP capability")
			w.WriteHeader(401)
			return
		}
		var req struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if json.NewDecoder(r.Body).Decode(&req) != nil {
			w.WriteHeader(400)
			return
		}
		if req.Method == "notifications/cancelled" {
			f.nativeCancelled.Add(1)
		}
		if len(req.ID) == 0 {
			w.WriteHeader(202)
			return
		}
		var result any
		switch req.Method {
		case "initialize":
			result = map[string]any{"protocolVersion": "2025-03-26", "capabilities": map[string]any{"tools": map[string]any{}}, "serverInfo": map[string]any{"name": "feat175-ordinary-protocol", "version": "1"}}
		case "tools/list":
			result = map[string]any{"tools": tools}
		case "ping":
			result = map[string]any{}
		case "tools/call":
			var p struct {
				Name      string         `json:"name"`
				Arguments map[string]any `json:"arguments"`
				Meta      map[string]any `json:"_meta"`
			}
			if json.Unmarshal(req.Params, &p) != nil {
				t.Error("invalid normal tool call")
				w.WriteHeader(400)
				return
			}
			thread, _ := p.Meta["threadId"].(string)
			meta, _ := p.Meta["x-codex-turn-metadata"].(map[string]any)
			turn, _ := meta["turn_id"].(string)
			key := thread + "/" + turn
			f.mu.Lock()
			ctx, delay := f.turns[key], f.delay
			f.mu.Unlock()
			if ctx == nil || ctx.Err() != nil {
				t.Error("MCP call lacked a live native turn")
				w.WriteHeader(409)
				return
			}
			f.calls.Add(1)
			f.mu.Lock()
			f.active[key]++
			total := 0
			for _, n := range f.active {
				total += n
			}
			if total > f.maxActive {
				f.maxActive = total
			}
			f.mu.Unlock()
			defer func() { f.mu.Lock(); f.active[key]--; f.mu.Unlock() }()
			f.entered <- key
			cancelled := false
			if delay > 0 {
				timer := time.NewTimer(delay)
				select {
				case <-timer.C:
				case <-ctx.Done():
					cancelled = true
				case <-r.Context().Done():
					cancelled = true
				}
				timer.Stop()
			}
			var output map[string]any
			if cancelled {
				f.cancelled.Add(1)
				output = map[string]any{"error": map[string]any{"code": "cancelled", "message": "网页检索已取消。"}}
			} else {
				switch p.Name {
				case "kimi_web_search":
					output = map[string]any{"results": []any{map[string]any{"title": "Q1 synthetic reference", "url": "https://example.com/", "snippet": "Q1 synthetic material"}}, "truncated": false}
				case "kimi_web_search_pro":
					output = map[string]any{"results": []any{}, "truncated": false, "dropped_results": 0}
				case "kimi_web_fetch":
					output = map[string]any{"url": "https://example.com/", "title": "Q1 synthetic page", "markdown": "Q1 synthetic material", "truncated": false}
				default:
					t.Error("unknown source tool")
					w.WriteHeader(400)
					return
				}
			}
			encoded, _ := json.Marshal(output)
			result = map[string]any{"content": []any{map[string]any{"type": "text", "text": string(encoded)}}, "structuredContent": output, "isError": cancelled}
		default:
			t.Errorf("unexpected normal MCP method %s", req.Method)
			w.WriteHeader(400)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": result})
	}))
	t.Cleanup(func() { f.observe(BuiltinWebTurnEvent{State: "closed"}); f.server.Close() })
	return f
}

func (f *feat175MCP) observe(event BuiltinWebTurnEvent) {
	f.mu.Lock()
	defer f.mu.Unlock()
	key := event.ThreadID + "/" + event.TurnID
	if event.State == "started" {
		if _, exists := f.turns[key]; !exists {
			f.turns[key], f.cancels[key] = context.WithCancel(context.Background())
		}
		return
	}
	if event.State == "closed" {
		for _, cancel := range f.cancels {
			cancel()
		}
		return
	}
	if cancel := f.cancels[key]; cancel != nil {
		cancel()
	}
}

func feat175Config(c *Config) {
	c.BuiltinWebEnabled, c.ToolSearchEnabled, c.InteractionToolsEnabled = true, true, true
}
func (f *feat175MCP) setup(t *testing.T, handler BuiltinWebElicitationHandler) func(*Manager) {
	return func(m *Manager) {
		if err := m.SetBuiltinWebMCP(f.server.URL, strings.Repeat("a", 64), handler, f.observe); err != nil {
			t.Fatal(err)
		}
	}
}

func TestFEAT175RuntimeNativeDiscoveryAndThreeTools(t *testing.T) {
	for _, profile := range []string{KimiK31ProfileID, "minimax-m3-high-v1"} {
		t.Run(profile, func(t *testing.T) {
			f := newFEAT175MCP(t)
			var last string
			h := newNativeToolsHarnessWithSetup(t, func(_ *nativeToolsHarness, body map[string]any, n int64) map[string]any {
				if profile == KimiK31ProfileID && n == 1 {
					assertSearchDeclaration(t, body, true)
					return feat164AuxTool("", "tool_search", "web-discovery", map[string]any{"query": "web research source page read", "limit": 3})
				}
				step := n
				if profile == KimiK31ProfileID {
					step--
				}
				if step == 1 && profile == KimiK31ProfileID {
					out := nativeToolsOutput(body, "web-discovery")
					if !strings.Contains(out, "mcp__yijie_web__kimi_web_") {
						t.Error("native discovery did not return builtin tools")
					}
				}
				if step > 1 && nativeToolsOutput(body, last) == "" {
					t.Error("native result not returned to model")
				}
				if step <= 3 {
					name := wire.ToolNames[step-1]
					last = fmt.Sprintf("web-call-%d", step)
					args := `{query:"Q1 synthetic material"}`
					if name == "kimi_web_fetch" {
						args = `{url:"https://example.com/"}`
					}
					if profile == KimiK31ProfileID {
						return feat164AuxTool("", "exec", last, map[string]any{"input": fmt.Sprintf("text(await tools.mcp__yijie_web__%s(%s));", name, args)})
					}
					var arguments map[string]any
					if name == "kimi_web_fetch" {
						arguments = map[string]any{"url": "https://example.com/"}
					} else {
						arguments = map[string]any{"query": "Q1 synthetic material"}
					}
					return feat164AuxTool("", "mcp__yijie_web__"+name, last, arguments)
				}
				return feat164AuxText("Q1 synthetic protocol completed. No real search.")
			}, f.setup(t, func(context.Context, json.RawMessage, wire.McpServerElicitationRequestParams) wire.McpServerElicitationRequestResponse {
				f.approvals.Add(1)
				return wire.McpServerElicitationRequestResponse{Action: wire.Decline}
			}), feat175Config)
			ctx := WithPermissionMode(WithModelProfile(context.Background(), profile), PermissionAsk)
			thread, err := h.manager.StartThread(ctx, h.cwd)
			if err != nil {
				t.Fatal(err)
			}
			h.turn(t, profile, thread.ID, "Exercise the three synthetic builtin web protocol tools.")
			if f.calls.Load() != 3 || f.approvals.Load() != 0 {
				t.Fatalf("calls=%d approvals=%d", f.calls.Load(), f.approvals.Load())
			}
			history, err := h.manager.ReadThread(context.Background(), thread.ID)
			if err != nil || !strings.Contains(string(history), "mcpToolCall") || !strings.Contains(string(history), wire.ServerName) {
				t.Fatal("native item/history missing", err)
			}
			before := h.calls.Load()
			if _, err = h.manager.SwitchThread(ctx, thread.ID); err != nil {
				t.Fatal(err)
			}
			if h.calls.Load() != before || f.calls.Load() != 3 {
				t.Fatal("idle resume replayed requests")
			}
			t.Logf("Q1 protocol: model_profile=%s loopback_model_requests=%d tools=3 approvals=0 supplier_requests=0", profile, h.calls.Load())
		})
	}
}

func feat175Start(t *testing.T, h *nativeToolsHarness, profile, thread, text string) TurnInfo {
	t.Helper()
	ctx := WithPermissionMode(WithModelProfile(context.Background(), profile), PermissionAsk)
	input, err := TextUserInput(text)
	if err != nil {
		t.Fatal(err)
	}
	model, _ := ModelProfile(profile)
	turn, err := h.manager.StartTurnV2(ctx, thread, []UserInput{input}, model.Effort)
	if err != nil {
		t.Fatal(err)
	}
	return turn
}

func feat175Wait(t *testing.T, h *nativeToolsHarness, thread, turn, want string, timeout time.Duration) {
	t.Helper()
	status, _, err := h.notifications.waitForTurn(turn, timeout)
	if err != nil {
		_ = h.manager.InterruptTurn(context.Background(), thread, turn)
		t.Fatal("normal turn wait failed", err)
	}
	if status != want {
		t.Fatalf("turn status=%s want=%s", status, want)
	}
}

func TestFEAT175RuntimePromptWait(t *testing.T) {
	t.Parallel()
	f := newFEAT175MCP(t)
	f.delay = time.Second
	var waited atomic.Int64
	h := newNativeToolsHarnessWithSetup(t, func(_ *nativeToolsHarness, body map[string]any, n int64) map[string]any {
		if n == 1 {
			return feat164AuxTool("", "mcp__yijie_web__kimi_web_fetch", "prompt-fetch", map[string]any{"url": "https://example.com/"})
		}
		if !strings.Contains(nativeToolsOutput(body, "prompt-fetch"), "Q1 synthetic material") {
			t.Error("accepted call did not return its result")
		}
		return feat164AuxText("Normal delayed confirmation completed.")
	}, f.setup(t, func(ctx context.Context, id json.RawMessage, p wire.McpServerElicitationRequestParams) wire.McpServerElicitationRequestResponse {
		f.approvals.Add(1)
		if len(id) == 0 || p.TurnId == nil || f.calls.Load() != 0 {
			t.Error("approval identity or pre-call ordering invalid")
		}
		start := time.Now()
		timer := time.NewTimer(90 * time.Second)
		defer timer.Stop()
		select {
		case <-timer.C:
		case <-ctx.Done():
			return wire.McpServerElicitationRequestResponse{Action: wire.Cancel}
		}
		waited.Store(time.Since(start).Milliseconds())
		if f.calls.Load() != 0 {
			t.Error("MCP executed during approval wait")
		}
		return wire.McpServerElicitationRequestResponse{Action: wire.Accept, Content: map[string]any{}}
	}), feat175Config, func(c *Config) { c.BuiltinWebApprovals = map[string]string{"kimi_web_fetch": "prompt"} })
	ctx := WithPermissionMode(WithModelProfile(context.Background(), "minimax-m3-high-v1"), PermissionAsk)
	thread, err := h.manager.StartThread(ctx, h.cwd)
	if err != nil {
		t.Fatal(err)
	}
	turn := feat175Start(t, h, "minimax-m3-high-v1", thread.ID, "Read the ordinary synthetic page after normal confirmation.")
	feat175Wait(t, h, thread.ID, turn.ID, "completed", 150*time.Second)
	if waited.Load() < 90000 || f.calls.Load() != 1 || f.approvals.Load() != 1 {
		t.Fatalf("wait=%d calls=%d approvals=%d", waited.Load(), f.calls.Load(), f.approvals.Load())
	}
	t.Logf("N03 native prompt: wait_ms=%d execution_after_accept=1 supplier_requests=0", waited.Load())
}

func TestFEAT175RuntimeModelDelay(t *testing.T) {
	t.Parallel()
	f := newFEAT175MCP(t)
	var waited atomic.Int64
	h := newNativeToolsHarnessWithSetup(t, func(_ *nativeToolsHarness, body map[string]any, n int64) map[string]any {
		if n == 1 {
			start := time.Now()
			timer := time.NewTimer(120 * time.Second)
			<-timer.C
			waited.Store(time.Since(start).Milliseconds())
			if f.calls.Load() != 0 {
				t.Error("MCP ran before proposal")
			}
			return feat164AuxTool("", "mcp__yijie_web__kimi_web_fetch", "late-fetch", map[string]any{"url": "https://example.com/"})
		}
		if !strings.Contains(nativeToolsOutput(body, "late-fetch"), "Q1 synthetic material") {
			t.Error("late model proposal lost its tool budget")
		}
		return feat164AuxText("Normal delayed model proposal completed.")
	}, f.setup(t, nil), feat175Config)
	ctx := WithPermissionMode(WithModelProfile(context.Background(), "minimax-m3-high-v1"), PermissionAsk)
	thread, err := h.manager.StartThread(ctx, h.cwd)
	if err != nil {
		t.Fatal(err)
	}
	turn := feat175Start(t, h, "minimax-m3-high-v1", thread.ID, "Wait normally, then read the synthetic page.")
	feat175Wait(t, h, thread.ID, turn.ID, "completed", 150*time.Second)
	if waited.Load() < 120000 || f.calls.Load() != 1 {
		t.Fatalf("wait=%d calls=%d", waited.Load(), f.calls.Load())
	}
	t.Logf("N04 normal model delay: wait_ms=%d tool_calls=1 supplier_requests=0", waited.Load())
}

func TestFEAT175RuntimeParallelCancelIsolation(t *testing.T) {
	f := newFEAT175MCP(t)
	f.delay = 1500 * time.Millisecond
	h := newNativeToolsHarnessWithSetup(t, func(_ *nativeToolsHarness, body map[string]any, _ int64) map[string]any {
		if nativeToolsOutput(body, "parallel-web") != "" {
			return feat164AuxText("Ordinary parallel tools completed.")
		}
		return feat164AuxTool("", "exec", "parallel-web", map[string]any{"input": `await Promise.all([tools.mcp__yijie_web__kimi_web_fetch({url:"https://example.com/"}),tools.mcp__yijie_web__kimi_web_search({query:"Q1 synthetic reference"})]).then(text);`})
	}, f.setup(t, nil), feat175Config, func(c *Config) { c.ToolSearchEnabled = false })
	ctx := feat164AuxCtx()
	a, err := h.manager.StartThread(ctx, h.cwd)
	if err != nil {
		t.Fatal(err)
	}
	b, err := h.manager.StartThread(ctx, h.cwd)
	if err != nil {
		t.Fatal(err)
	}
	ta := feat175Start(t, h, KimiK31ProfileID, a.ID, "Run ordinary parallel synthetic reads A.")
	tb := feat175Start(t, h, KimiK31ProfileID, b.ID, "Run ordinary parallel synthetic reads B.")
	started := make(map[string]int)
	for i := 0; i < 4; i++ {
		select {
		case key := <-f.entered:
			started[key]++
		case <-time.After(10 * time.Second):
			t.Fatal("parallel reads did not all enter")
		}
	}
	if started[a.ID+"/"+ta.ID] != 2 || started[b.ID+"/"+tb.ID] != 2 {
		t.Fatal("native call metadata mixed roots", started)
	}
	f.mu.Lock()
	activeA, activeB, peak := f.active[a.ID+"/"+ta.ID], f.active[b.ID+"/"+tb.ID], f.maxActive
	f.mu.Unlock()
	if activeA == 0 || activeB == 0 || peak < 2 {
		t.Fatalf("ordinary overlap missing activeA=%d activeB=%d peak=%d", activeA, activeB, peak)
	}
	if err = h.manager.InterruptTurn(context.Background(), a.ID, ta.ID); err != nil {
		t.Fatal(err)
	}
	feat175Wait(t, h, a.ID, ta.ID, "interrupted", 10*time.Second)
	feat175Wait(t, h, b.ID, tb.ID, "completed", 10*time.Second)
	if f.calls.Load() != 4 || f.cancelled.Load() != int64(activeA) {
		t.Fatalf("cross-turn I/O isolation failed calls=%d cancelled=%d", f.calls.Load(), f.cancelled.Load())
	}
	t.Logf("N05/N07: calls=4 active_A_at_stop=%d completed_A_before_stop=%d local_IO_cancelled=%d peak_parallel=%d other_turn_completed=1 native_cancelled_notifications=%d supplier_requests=0", activeA, 2-activeA, f.cancelled.Load(), peak, f.nativeCancelled.Load())
}

func TestFEAT175RuntimeExpertPolicyAndColdResume(t *testing.T) {
	for _, profile := range []string{KimiK31ProfileID, "minimax-m3-high-v1"} {
		t.Run(profile, func(t *testing.T) {
			f := newFEAT175MCP(t)
			var disabled atomic.Bool
			var step atomic.Int64
			h := newNativeToolsHarnessWithSetup(t, func(_ *nativeToolsHarness, body map[string]any, _ int64) map[string]any {
				if step.Add(1) == 1 {
					items, _ := body["tools"].([]any)
					hosted, search, mcp := false, false, false
					for _, raw := range items {
						item, _ := raw.(map[string]any)
						if item["type"] == "web_search" {
							hosted = true
						}
						if item["name"] == "tool_search" {
							search = true
						}
						if name, _ := item["name"].(string); strings.HasPrefix(name, "mcp__yijie_web__") {
							mcp = true
						}
					}
					if hosted == disabled.Load() {
						t.Errorf("hosted declaration wrong disabled=%v hosted=%v", disabled.Load(), hosted)
					}
					if profile == "minimax-m3-high-v1" && mcp == disabled.Load() {
						t.Error("direct MCP surface disagrees with policy")
					}
					if profile == KimiK31ProfileID && search {
						return feat164AuxTool("", "tool_search", "policy-discovery", map[string]any{"query": "web research source page read", "limit": 3})
					}
				} else {
					available := strings.Contains(nativeToolsOutput(body, "policy-discovery"), "mcp__yijie_web__")
					if available == disabled.Load() {
						t.Error("deferred MCP surface disagrees with policy")
					}
				}
				return feat164AuxText("Ordinary policy declaration recorded.")
			}, f.setup(t, nil), feat175Config)
			ordinary := WithPermissionMode(WithModelProfile(context.Background(), profile), PermissionAsk)
			thread, err := h.manager.StartThread(ordinary, h.cwd)
			if err != nil {
				t.Fatal(err)
			}
			contexts := []context.Context{ordinary}
			inherit, _ := WithExpertWebPolicy(ordinary, "ordinary-reviewed-expert", "1.0.0", "ordinary-role")
			inherit, err = WithExpertInstructions(inherit, "Ordinary verified role.", "Ordinary reference index.")
			if err != nil {
				t.Fatal(err)
			}
			contexts = append(contexts, inherit)
			for _, p := range wire.ExpertPolicies {
				restricted, err := WithExpertWebPolicy(inherit, p.ExpertID, p.Version, p.RoleSHA256)
				if err != nil {
					t.Fatal(err)
				}
				contexts = append(contexts, restricted)
			}
			contexts = append(contexts, ordinary)
			for i, ctx := range contexts {
				disabled.Store(i >= 2 && i <= 4)
				step.Store(0)
				if i > 0 {
					if _, err = h.manager.SwitchThread(ctx, thread.ID); err != nil {
						t.Fatal(err)
					}
				}
				h.turn(t, profile, thread.ID, "Record the current ordinary tool surface.")
			}
			before := h.calls.Load()
			cfg := h.manager.config
			shutdown, stop := context.WithTimeout(context.Background(), 30*time.Second)
			err = h.manager.Shutdown(shutdown)
			stop()
			if err != nil {
				t.Fatal("normal shutdown pending", err)
			}
			h.manager = NewManager(cfg, nil)
			f.setup(t, nil)(h.manager)
			if err = h.manager.SetNotificationHandler(h.notifications.handle); err != nil {
				t.Fatal(err)
			}
			if err = h.manager.Start(context.Background()); err != nil {
				t.Fatal(err)
			}
			if _, err = h.manager.ResumeThread(ordinary, thread.ID); err != nil {
				t.Fatal(err)
			}
			if h.calls.Load() != before || f.calls.Load() != 0 {
				t.Fatal("cold resume executed an external operation")
			}
			disabled.Store(false)
			step.Store(0)
			h.turn(t, profile, thread.ID, "Record the restored ordinary tool surface.")
			t.Logf("N01/N10 profile=%s: ordinary/inherit/three disabled versions/remove/cold resume; supplier_requests=0", profile)
		})
	}
}

func TestFEAT175RuntimeDeclineAndLateApproval(t *testing.T) {
	for _, late := range []bool{false, true} {
		t.Run(fmt.Sprintf("late=%v", late), func(t *testing.T) {
			f := newFEAT175MCP(t)
			entered := make(chan json.RawMessage, 1)
			release := make(chan struct{})
			h := newNativeToolsHarnessWithSetup(t, func(_ *nativeToolsHarness, body map[string]any, n int64) map[string]any {
				if n == 1 {
					return feat164AuxTool("", "mcp__yijie_web__kimi_web_fetch", "declined-web", map[string]any{"url": "https://example.com/"})
				}
				return feat164AuxText("Ordinary declined read recorded.")
			}, f.setup(t, func(ctx context.Context, id json.RawMessage, _ wire.McpServerElicitationRequestParams) wire.McpServerElicitationRequestResponse {
				f.approvals.Add(1)
				entered <- append(json.RawMessage(nil), id...)
				if late {
					select {
					case <-release:
					case <-ctx.Done():
						return wire.McpServerElicitationRequestResponse{Action: wire.Cancel}
					}
					return wire.McpServerElicitationRequestResponse{Action: wire.Accept, Content: map[string]any{}}
				}
				return wire.McpServerElicitationRequestResponse{Action: wire.Decline}
			}), feat175Config, func(c *Config) { c.BuiltinWebApprovals = map[string]string{"kimi_web_fetch": "prompt"} })
			ctx := WithPermissionMode(WithModelProfile(context.Background(), "minimax-m3-high-v1"), PermissionAsk)
			thread, err := h.manager.StartThread(ctx, h.cwd)
			if err != nil {
				t.Fatal(err)
			}
			turn := feat175Start(t, h, "minimax-m3-high-v1", thread.ID, "Read an ordinary source only after confirmation.")
			select {
			case id := <-entered:
				if len(id) == 0 {
					t.Fatal("missing opaque request ID")
				}
			case <-time.After(10 * time.Second):
				t.Fatal("native prompt missing")
			}
			if late {
				if err = h.manager.InterruptTurn(context.Background(), thread.ID, turn.ID); err != nil {
					t.Fatal(err)
				}
				feat175Wait(t, h, thread.ID, turn.ID, "interrupted", 10*time.Second)
				close(release)
			} else {
				feat175Wait(t, h, thread.ID, turn.ID, "completed", 10*time.Second)
			}
			if f.calls.Load() != 0 || f.approvals.Load() != 1 {
				t.Fatalf("declined/ended approval dispatched calls=%d", f.calls.Load())
			}
			t.Logf("N05 late=%v native_prompts=1 MCP_calls=0 supplier_requests=0", late)
		})
	}
}

func TestFEAT175RuntimeLocalWorkAfterRetrieval(t *testing.T) {
	f := newFEAT175MCP(t)
	var localCalls atomic.Int64
	local := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/health" {
			w.WriteHeader(404)
			return
		}
		localCalls.Add(1)
		_, _ = fmt.Fprint(w, "ordinary-local-service")
	}))
	defer local.Close()
	h := newNativeToolsHarnessWithSetup(t, func(_ *nativeToolsHarness, body map[string]any, n int64) map[string]any {
		switch n {
		case 1:
			return feat164AuxTool("", "exec", "normal-web", map[string]any{"input": `text(await tools.mcp__yijie_web__kimi_web_fetch({url:"https://example.com/"}));`})
		case 2:
			if !strings.Contains(nativeToolsOutput(body, "normal-web"), "Q1 synthetic material") {
				t.Error("retrieval result missing")
			}
			return feat164AuxTool("", "exec_command", "local-file", map[string]any{"cmd": "python3 -c 'from pathlib import Path; p=Path(\"q1-result.txt\"); p.write_text(str(6*7)); print(p.read_text())'", "max_output_tokens": 200})
		case 3:
			if !strings.Contains(nativeToolsOutput(body, "local-file"), "42") {
				t.Error("normal file computation blocked")
			}
			return feat164AuxTool("", "exec_command", "local-service", map[string]any{"cmd": "curl -fsS " + local.URL + "/health", "max_output_tokens": 200, "sandbox_permissions": "require_escalated", "justification": "Check the requested ordinary loopback development service with native approval."})
		default:
			if !strings.Contains(nativeToolsOutput(body, "local-service"), "ordinary-local-service") {
				t.Errorf("normal local service result: %s", nativeToolsOutput(body, "local-service"))
			}
			return feat164AuxText("Normal file work and local service remain usable.")
		}
	}, f.setup(t, nil), feat175Config, func(c *Config) { c.ToolSearchEnabled = false })
	thread, err := h.manager.StartThread(feat164AuxCtx(), h.cwd)
	if err != nil {
		t.Fatal(err)
	}
	turn := feat175Start(t, h, KimiK31ProfileID, thread.ID, "Read the synthetic page, compute 6*7 in a local file, and check the ordinary local development service after native confirmation.")
	approved := false
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) && !approved {
		for _, approval := range h.manager.ListRuntimeApprovals(thread.ID) {
			if approval.Status == "pending" && approval.Kind == "command" {
				if _, err = h.manager.DecideRuntimeApproval(context.Background(), thread.ID, approval.ID, "approve_once"); err != nil {
					t.Fatal(err)
				}
				approved = true
			}
		}
		if !approved {
			time.Sleep(10 * time.Millisecond)
		}
	}
	if !approved {
		_ = h.manager.InterruptTurn(context.Background(), thread.ID, turn.ID)
		t.Fatal("normal local-network native approval missing")
	}
	feat175Wait(t, h, thread.ID, turn.ID, "completed", 10*time.Second)
	if f.calls.Load() != 1 || localCalls.Load() != 1 {
		t.Fatal("unexpected normal invocation counts")
	}
	t.Log("N08: actual native MCP -> file computation -> native-approved loopback development request; original sandbox retained; supplier_requests=0")
}

func TestFEAT175RuntimeAuxiliaryAndModelSwitch(t *testing.T) {
	f := newFEAT175MCP(t)
	var auxiliary atomic.Bool
	h := newNativeToolsHarnessWithSetup(t, func(_ *nativeToolsHarness, body map[string]any, _ int64) map[string]any {
		encoded, _ := json.Marshal(body["tools"])
		if auxiliary.Load() {
			if strings.Contains(string(encoded), "mcp__yijie_web") || strings.Contains(string(encoded), `"type":"web_search"`) {
				t.Error("auxiliary task inherited web tools")
			}
			if strings.Contains(nativeToolsUserText(body), "FEAT175_DRAFT") {
				if tools, _ := body["tools"].([]any); len(tools) != 0 {
					t.Error("input-only registry not empty")
				}
				return feat164AuxText(`{"schema_version":1,"kind":"needs_clarification","missing_fields":["content"],"question":"需要定时执行什么？"}`)
			}
			return feat164AuxText(`{"title":"普通本机验证"}`)
		}
		if !strings.Contains(string(encoded), `"type":"web_search"`) {
			t.Error("model switch removed ordinary hosted capability")
		}
		return feat164AuxText("Ordinary model switch retained native search.")
	}, f.setup(t, nil), feat175Config)
	ctx := feat164AuxCtx()
	thread, err := h.manager.StartThread(ctx, h.cwd)
	if err != nil {
		t.Fatal(err)
	}
	h.turn(t, KimiK31ProfileID, thread.ID, "Ordinary Kimi configuration.")
	mini := WithPermissionMode(WithModelProfile(context.Background(), "minimax-m3-high-v1"), PermissionAsk)
	if _, err = h.manager.SwitchThread(mini, thread.ID); err != nil {
		t.Fatal(err)
	}
	h.turn(t, "minimax-m3-high-v1", thread.ID, "Ordinary MiniMax configuration.")
	if _, err = h.manager.SwitchThread(ctx, thread.ID); err != nil {
		t.Fatal(err)
	}
	h.turn(t, KimiK31ProfileID, thread.ID, "Ordinary return to Kimi.")
	auxiliary.Store(true)
	for _, profile := range []string{KimiK31ProfileID, "minimax-m3-high-v1"} {
		profileCtx := WithModelProfile(context.Background(), profile)
		if _, err = h.manager.GenerateTitle(profileCtx, "普通本机标题"); err != nil {
			t.Fatal(err)
		}
		draft := WithScheduledDraft(profileCtx, "ordinary-feat175-draft")
		private, err := h.manager.StartThread(draft, h.cwd)
		if err != nil {
			t.Fatal(err)
		}
		input, _ := TextUserInput("FEAT175_DRAFT: prepare a scheduled task without enough details.")
		model, _ := ModelProfile(profile)
		turn, err := h.manager.StartTurnV2(draft, private.ID, []UserInput{input}, model.Effort)
		if err != nil {
			t.Fatal(err)
		}
		feat175Wait(t, h, private.ID, turn.ID, "completed", 10*time.Second)
	}
	if f.calls.Load() != 0 {
		t.Fatal("auxiliary or model switching executed retrieval")
	}
	t.Log("N01 auxiliary/model switch: same thread Kimi -> MiniMax -> Kimi; titles and input-only drafts have no web calls; supplier_requests=0")
}

func TestFEAT175RuntimeMarketCoexistence(t *testing.T) {
	manifest := os.Getenv("YIJIE_FEAT175_MARKET_MANIFEST")
	if !filepath.IsAbs(manifest) {
		t.Fatal("fixed canonical market worker manifest required for Q1 lifecycle evidence")
	}
	for _, mode := range []string{"builtin-only", "market-only", "both", "both-with-media"} {
		t.Run(mode, func(t *testing.T) {
			f := newFEAT175MCP(t)
			h := newNativeToolsHarnessWithSetup(t, func(_ *nativeToolsHarness, _ map[string]any, n int64) map[string]any {
				if n == 1 {
					return feat164AuxTool("", "mcp__yijie_web__kimi_web_fetch", "after-market-close", map[string]any{"url": "https://example.com/"})
				}
				return feat164AuxText("Builtin MCP remains usable after ordinary market shutdown.")
			}, func(m *Manager) {
				if mode != "market-only" {
					f.setup(t, nil)(m)
				}
			}, feat175Config, func(c *Config) {
				c.BuiltinWebEnabled = mode != "market-only"
				c.BuiltinMediaEnabled = mode == "both-with-media"
				if mode != "builtin-only" {
					c.Market = MarketConfig{Manifest: manifest, LibraryHome: filepath.Join(c.CodexHome, "market-worker", "library-home"), HostInstanceID: uuid.NewString()}
				}
			})
			if !h.manager.Snapshot().Ready || h.calls.Load() != 0 || f.calls.Load() != 0 {
				t.Fatal("normal startup performed an execution")
			}
			if mode != "builtin-only" {
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				err := h.manager.closeMarketBroker(ctx)
				cancel()
				if err != nil {
					t.Fatal("normal market EOF pending", err)
				}
			}
			if mode != "market-only" {
				ctx := WithPermissionMode(WithModelProfile(context.Background(), "minimax-m3-high-v1"), PermissionAsk)
				thread, err := h.manager.StartThread(ctx, h.cwd)
				if err != nil {
					t.Fatal(err)
				}
				h.turn(t, "minimax-m3-high-v1", thread.ID, "Read an ordinary synthetic source with the market stopped.")
				if f.calls.Load() != 1 {
					t.Fatal("market shutdown disabled independent builtin MCP")
				}
			}
			t.Logf("N10 mode=%s: fixed Runtime plus existing canonical market owner where applicable; ordinary EOF; supplier_requests=0; shared-worker web transport remains S3", mode)
		})
	}
}
