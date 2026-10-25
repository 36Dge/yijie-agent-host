package codex

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type feat164Auxiliary struct {
	manager       *Manager
	notifications *integrationNotifications
	root, cwd     string
	calls         atomic.Int64
}

func feat164AuxRuntime(t *testing.T, respond func(map[string]any, int64) map[string]any) *feat164Auxiliary {
	t.Helper()
	binary, manifest := integrationArtifactPaths(t)
	root, e := os.MkdirTemp("", "feat164-aux-")
	if e != nil {
		t.Fatal(e)
	}
	root, e = filepath.EvalSymlinks(root)
	if e != nil {
		t.Fatal(e)
	}
	h := &feat164Auxiliary{root: root, cwd: filepath.Join(root, "workspace"), notifications: newIntegrationNotifications()}
	for _, name := range []string{"runtime", "workspace", "review-output"} {
		if e = os.Mkdir(filepath.Join(root, name), 0700); e != nil {
			t.Fatal(e)
		}
	}
	if e = os.WriteFile(filepath.Join(h.cwd, "note.txt"), []byte("ordinary local note 164\n"), 0600); e != nil {
		t.Fatal(e)
	}
	listener, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	server := &http.Server{ReadHeaderTimeout: 5 * time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if r.Method != "POST" || r.URL.Path != "/v1/responses" || json.NewDecoder(r.Body).Decode(&body) != nil {
			t.Error("unexpected local model route")
			w.WriteHeader(400)
			return
		}
		n := h.calls.Add(1)
		if n > 10 {
			t.Error("extra loopback requests")
			w.WriteHeader(429)
			return
		}
		reasoning, _ := body["reasoning"].(map[string]any)
		if body["model"] != "kimi-k3.1-pro" || reasoning["effort"] != "max" {
			t.Errorf("auxiliary identity differs: model=%v effort=%v", body["model"], reasoning["effort"])
		}
		item := respond(body, n)
		id := fmt.Sprintf("aux_response_%d", n)
		item["id"] = fmt.Sprintf("aux_item_%d", n)
		w.Header().Set("Content-Type", "text/event-stream")
		for _, v := range []any{map[string]any{"type": "response.created", "response": map[string]any{"id": id, "status": "in_progress", "output": []any{}}}, map[string]any{"type": "response.output_item.added", "output_index": 0, "item": item}, map[string]any{"type": "response.output_item.done", "output_index": 0, "item": item}, map[string]any{"type": "response.completed", "response": map[string]any{"id": id, "status": "completed", "output": []any{item}, "usage": map[string]any{"input_tokens": 1, "output_tokens": 1, "total_tokens": 2}}}} {
			raw, _ := json.Marshal(v)
			fmt.Fprintf(w, "data: %s\n\n", raw)
			w.(http.Flusher).Flush()
		}
	})}
	go func() { _ = server.Serve(listener) }()
	cfg := integrationConfig(binary, manifest, filepath.Join(root, "runtime"), "ordinary-loopback-minimax")
	cfg.ChatModelsEnabled, cfg.KimiK31Candidate, cfg.RuntimePermissionsEnabled = true, true, true
	cfg.KimiAPIKey = "ordinary-loopback-kimi"
	cfg.PermissionVerificationBaseURL = "http://" + listener.Addr().String() + "/v1"
	h.manager = NewManager(cfg, nil)
	if e = h.manager.SetNotificationHandler(h.notifications.handle); e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		stopped := true
		if e := h.manager.Shutdown(ctx); e != nil {
			t.Error("normal Runtime cleanup pending", e)
			stopped = false
		}
		if e := server.Shutdown(ctx); e != nil {
			t.Error(e)
			stopped = false
		}
		if stopped {
			_ = os.RemoveAll(root)
		}
	})
	if e = h.manager.Start(context.Background()); e != nil {
		t.Fatal(e)
	}
	var effective struct {
		Config struct {
			Providers map[string]struct {
				URL string `json:"base_url"`
			} `json:"model_providers"`
		} `json:"config"`
	}
	if e = h.manager.request(context.Background(), "config/read", map[string]any{"includeLayers": false}, &effective); e != nil {
		t.Fatal(e)
	}
	for _, name := range []string{"kimi", "minimax"} {
		if effective.Config.Providers[name].URL != cfg.PermissionVerificationBaseURL {
			t.Fatal("process meter not effective", name)
		}
	}
	return h
}
func feat164AuxText(text string) map[string]any {
	return map[string]any{"type": "message", "role": "assistant", "phase": "final_answer", "status": "completed", "content": []any{map[string]any{"type": "output_text", "text": text, "annotations": []any{}}}}
}
func feat164AuxTool(namespace, name, call string, args any) map[string]any {
	b, _ := json.Marshal(args)
	v := map[string]any{"type": "function_call", "name": name, "call_id": call, "arguments": string(b)}
	if namespace != "" {
		v["namespace"] = namespace
	}
	return v
}
func feat164AuxCtx() context.Context {
	return WithPermissionMode(WithModelProfile(context.Background(), KimiK31ProfileID), PermissionAsk)
}
func (h *feat164Auxiliary) turn(t *testing.T, ctx context.Context, thread, text string) string {
	t.Helper()
	input, _ := TextUserInput(text)
	turn, e := h.manager.StartTurnV2(ctx, thread, []UserInput{input}, "max")
	if e != nil {
		t.Fatal(e)
	}
	state, _, e := h.notifications.waitForTurn(turn.ID, 30*time.Second)
	if e != nil || state != "completed" {
		t.Fatal(state, e)
	}
	return turn.ID
}

// Tests the product's native child model inheritance, not delegation of this
// implementation task. The child sees only one synthetic local text and no tools.
func TestFEAT164NativeAuxiliaryAgent(t *testing.T) {
	var parents, children atomic.Int64
	var childID atomic.Value
	h := feat164AuxRuntime(t, func(body map[string]any, _ int64) map[string]any {
		input, _ := body["input"].([]any)
		child := false
		for _, raw := range input {
			v, _ := raw.(map[string]any)
			if v["role"] == "user" {
				b, _ := json.Marshal(v["content"])
				if strings.Contains(string(b), "FEAT164_CHILD_ONLY") {
					child = true
				}
			}
		}
		if child {
			children.Add(1)
			return feat164AuxText("FEAT164 child normal completion")
		}
		n := parents.Add(1)
		if n == 1 {
			return feat164AuxTool("multi_agent_v1", "spawn_agent", "spawn_164", map[string]any{"message": "FEAT164_CHILD_ONLY: return the supplied normal note in one sentence; use no tools.", "fork_context": false})
		}
		if n == 2 {
			var id string
			for _, raw := range input {
				v, _ := raw.(map[string]any)
				if v["type"] == "function_call_output" && v["call_id"] == "spawn_164" {
					text, _ := v["output"].(string)
					var out struct {
						ID string `json:"agent_id"`
					}
					_ = json.Unmarshal([]byte(text), &out)
					id = out.ID
				}
			}
			if id == "" {
				t.Error("native spawn receipt missing")
			}
			childID.Store(id)
			return feat164AuxTool("multi_agent_v1", "wait_agent", "wait_164", map[string]any{"targets": []string{id}, "timeout_ms": 10000})
		}
		observed := false
		for _, raw := range input {
			v, _ := raw.(map[string]any)
			if v["type"] == "function_call_output" && v["call_id"] == "wait_164" {
				output, _ := v["output"].(string)
				var wait struct {
					Status   map[string]any `json:"status"`
					TimedOut bool           `json:"timed_out"`
				}
				if json.Unmarshal([]byte(output), &wait) != nil || wait.TimedOut || len(wait.Status) != 1 {
					t.Errorf("native wait did not observe completion: %s", output)
				} else {
					observed = true
				}
			}
		}
		if !observed {
			t.Error("native wait completion missing")
		}

		return feat164AuxText("FEAT164 parent normal completion")
	})
	ctx := feat164AuxCtx()
	thread, e := h.manager.StartThread(ctx, h.cwd)
	if e != nil {
		t.Fatal(e)
	}
	h.turn(t, ctx, thread.ID, "For this local product qualification, explicitly create one native child agent to echo a supplied normal note, wait for it, then finish. No external calls or file changes.")
	if parents.Load() != 3 || children.Load() != 1 || h.calls.Load() != 4 {
		t.Fatal("native agent loopback count", parents.Load(), children.Load(), h.calls.Load())
	}
	raw, e := h.manager.ReadThread(ctx, childID.Load().(string))
	if e != nil || !strings.Contains(string(raw), "FEAT164 child normal completion") {
		t.Fatal("native child output not persisted", e)
	}
	t.Log("native parent3 + child1 loopback requests; inherited K3.1 Pro/max; actual child completion; normal Runtime shutdown")
}

func TestFEAT164NativeAuxiliaryCompaction(t *testing.T) {
	h := feat164AuxRuntime(t, func(_ map[string]any, n int64) map[string]any {
		if n == 1 {
			return feat164AuxText("An ordinary local note is present.")
		}
		return feat164AuxText("The local task only contains an ordinary note; no external operation has occurred.")
	})
	ctx := feat164AuxCtx()
	thread, e := h.manager.StartThread(ctx, h.cwd)
	if e != nil {
		t.Fatal(e)
	}
	h.turn(t, ctx, thread.ID, "Summarize this normal local note without tools.")
	var response map[string]any
	if e = h.manager.request(ctx, "thread/compact/start", map[string]any{"threadId": thread.ID}, &response); e != nil {
		t.Fatal(e)
	}
	deadline := time.Now().Add(20 * time.Second)
	for h.calls.Load() < 2 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	compacted := false
	// Read the real native thread until its compaction turn terminates normally.
	for time.Now().Before(deadline) {
		raw, e := h.manager.ReadThread(ctx, thread.ID)
		if e != nil {
			t.Fatal(e)
		}
		var info struct {
			Turns []struct {
				Status string           `json:"status"`
				Items  []map[string]any `json:"items"`
			} `json:"turns"`
		}
		if e = json.Unmarshal(raw, &info); e != nil {
			t.Fatal(e)
		}
		if len(info.Turns) > 1 && info.Turns[len(info.Turns)-1].Status == "completed" && strings.Contains(string(raw), "contextCompaction") && h.calls.Load() == 2 {
			compacted = true
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if h.calls.Load() != 2 || !compacted {
		t.Fatal("compaction request count", h.calls.Load())
	}
	t.Log("normal turn + explicit native compaction;2 loopback Responses; active model identity retained")
}

func TestFEAT164NativeGuardianModel(t *testing.T) {
	var h *feat164Auxiliary
	var reviews atomic.Int64
	var ordinary atomic.Int64
	h = feat164AuxRuntime(t, func(body map[string]any, _ int64) map[string]any {
		text, _ := body["text"].(map[string]any)
		format, _ := text["format"].(map[string]any)
		schema, _ := json.Marshal(format["schema"])
		if strings.Contains(string(schema), "risk_level") {
			reviews.Add(1)
			return feat164AuxText(`{"risk_level":"low","user_authorization":"high","outcome":"allow","rationale":"The explicitly requested copy only writes the designated ordinary temporary output."}`)
		}
		n := ordinary.Add(1)
		if n == 1 {
			return feat164AuxTool("", "exec_command", "copy_164", map[string]any{"cmd": "cp note.txt '" + filepath.Join(h.root, "review-output", "note.txt") + "'", "workdir": h.cwd, "sandbox_permissions": "require_escalated", "justification": "Copy the normal local note into the explicitly requested temporary output directory?", "max_output_tokens": 100})
		}
		return feat164AuxText("The local note copy completed through native approval.")
	})
	ctx := WithPermissionMode(WithModelProfile(context.Background(), KimiK31ProfileID), PermissionAuto)
	thread, e := h.manager.StartThread(ctx, h.cwd)
	if e != nil {
		t.Fatal(e)
	}
	h.turn(t, ctx, thread.ID, "Copy note.txt into "+filepath.Join(h.root, "review-output", "note.txt")+" using the normal native approval flow. This is a designated ordinary temporary output owned by this test.")
	data, e := os.ReadFile(filepath.Join(h.root, "review-output", "note.txt"))
	if e != nil || string(data) != "ordinary local note 164\n" {
		t.Fatal("normal copy not completed", e)
	}
	if reviews.Load() != 1 || ordinary.Load() != 2 || h.calls.Load() != 3 {
		t.Fatal("Guardian/parent request count", reviews.Load(), ordinary.Load(), h.calls.Load())
	}
	t.Log("native Guardian1 + ordinary2 loopback; K3.1 Pro/max; ordinary authorized temporary copy; no supplied Host approval callback")
}
