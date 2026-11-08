package codex

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	imagewire "github.com/36Dge/yijie-agent-host/internal/contracts/runtimehostimages"
)

type nativeToolsHarness struct {
	manager                   *Manager
	notifications             *integrationNotifications
	root, home, cwd, endpoint string
	calls                     atomic.Int64
	pending                   chan struct{}
	finishPending             chan struct{}
	cancelled                 atomic.Int64
}

// Real canonical Runtime and real native executors, with a normal loopback
// provider. No account credentials, supplier requests or substituted executable.
func newNativeToolsHarness(t *testing.T, respond func(*nativeToolsHarness, map[string]any, int64) map[string]any, configure ...func(*Config)) *nativeToolsHarness {
	t.Helper()
	binary, manifest := integrationArtifactPaths(t)
	root, err := os.MkdirTemp("", "feat169-native-")
	if err != nil {
		t.Fatal(err)
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	h := &nativeToolsHarness{root: root, home: filepath.Join(root, "runtime"), cwd: filepath.Join(root, "workspace"), notifications: newIntegrationNotifications(), pending: make(chan struct{}, 1), finishPending: make(chan struct{}, 1)}
	for _, directory := range []string{h.home, h.cwd} {
		if err := os.Mkdir(directory, 0700); err != nil {
			t.Fatal(err)
		}
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	h.endpoint = "http://" + listener.Addr().String() + "/v1"
	server := &http.Server{ReadHeaderTimeout: 5 * time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if r.Method != "POST" || r.URL.Path != "/v1/responses" || json.NewDecoder(r.Body).Decode(&body) != nil {
			t.Error("unexpected local route")
			w.WriteHeader(400)
			return
		}
		if h.manager.config.NativeToolsEnabled {
			nativeToolsProviderInput(t, body)
		}
		n := h.calls.Add(1)
		if n > 64 {
			t.Error("unexpected continuation count")
			w.WriteHeader(429)
			return
		}
		if body["model"] != "kimi-k3.1-pro" && body["model"] != "MiniMax-M3" {
			t.Errorf("unexpected auxiliary model: %v", body["model"])
			w.WriteHeader(400)
			return
		}
		if strings.Contains(nativeToolsUserText(body), "FEAT169_WAIT_FOR_CANCEL") {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = fmt.Fprint(w, "data: {\"type\":\"response.created\",\"response\":{\"id\":\"ordinary_pending169\",\"status\":\"in_progress\",\"output\":[]}}\n\n")
			w.(http.Flusher).Flush()
			select {
			case h.pending <- struct{}{}:
			default:
			}
			select {
			case <-r.Context().Done():
				h.cancelled.Add(1)
				return
			case <-h.finishPending:
				_, _ = fmt.Fprint(w, "data: {\"type\":\"response.completed\",\"response\":{\"id\":\"ordinary_pending169\",\"status\":\"completed\",\"output\":[]}}\n\n")
				w.(http.Flusher).Flush()
				return
			case <-time.After(10 * time.Second):
				return
			}
		}
		item := respond(h, body, n)
		id := fmt.Sprintf("native169_%d", n)
		item["id"] = "item_" + id
		w.Header().Set("Content-Type", "text/event-stream")
		for _, event := range []any{
			map[string]any{"type": "response.created", "response": map[string]any{"id": id, "status": "in_progress", "output": []any{}}},
			map[string]any{"type": "response.output_item.added", "output_index": 0, "item": item},
			map[string]any{"type": "response.output_item.done", "output_index": 0, "item": item},
			map[string]any{"type": "response.completed", "response": map[string]any{"id": id, "status": "completed", "output": []any{item}, "usage": map[string]any{"input_tokens": 1, "output_tokens": 1, "total_tokens": 2}}},
		} {
			bytes, _ := json.Marshal(event)
			fmt.Fprintf(w, "data: %s\n\n", bytes)
			w.(http.Flusher).Flush()
		}
	})}
	go func() { _ = server.Serve(listener) }()
	cfg := integrationConfig(binary, manifest, h.home, "ordinary-local-minimax")
	cfg.ChatModelsEnabled, cfg.KimiK31Candidate, cfg.RuntimePermissionsEnabled, cfg.NativeToolsEnabled = true, true, true, true
	cfg.KimiAPIKey = "ordinary-local-kimi"
	cfg.PermissionVerificationBaseURL = h.endpoint
	for _, apply := range configure {
		apply(&cfg)
	}
	h.manager = NewManager(cfg, nil)
	if cfg.HostImagesEnabled {
		if err := h.manager.SetHostImageHandler(func(context.Context, imagewire.HostImageGenerateParams) bool {
			t.Error("unrequested image generation")
			return false
		}); err != nil {
			t.Fatal(err)
		}
	}
	if err := h.manager.SetNotificationHandler(h.notifications.handle); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		select {
		case h.finishPending <- struct{}{}:
		default:
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		stopped := true
		if err := h.manager.Shutdown(ctx); err != nil {
			t.Error("normal Runtime shutdown pending", err)
			stopped = false
		}
		if err := server.Shutdown(ctx); err != nil {
			t.Error("normal local server shutdown pending", err)
			stopped = false
		}
		if stopped {
			_ = os.RemoveAll(root)
		} else {
			t.Log("retained normal test state", root)
		}
	})
	if err := h.manager.Start(context.Background()); err != nil {
		if h.manager.stderr != nil {
			t.Log("native bootstrap diagnostic:", h.manager.stderr.String())
		}
		t.Fatal(err)
	}
	return h
}

func nativeToolsOutput(body map[string]any, call string) string {
	input, _ := body["input"].([]any)
	for i := len(input) - 1; i >= 0; i-- {
		item, _ := input[i].(map[string]any)
		if item["type"] == "function_call_output" && item["call_id"] == call {
			bytes, _ := json.Marshal(item["output"])
			return string(bytes)
		}
	}
	return ""
}

func nativeToolsDeclarations(t *testing.T, body map[string]any) {
	t.Helper()
	items, _ := body["tools"].([]any)
	found := map[string]string{}
	for _, raw := range items {
		item, _ := raw.(map[string]any)
		name, _ := item["name"].(string)
		kind, _ := item["type"].(string)
		found[name] = kind
		if name == "multi_agent_v1" {
			t.Error("V1 still exposed")
		}
	}
	for _, name := range []string{"apply_patch", "exec", "wait", "collaboration__spawn_agent", "collaboration__send_message", "collaboration__followup_task", "collaboration__wait_agent", "collaboration__interrupt_agent", "collaboration__list_agents", "memories__add_ad_hoc_note", "memories__list", "memories__read", "memories__search"} {
		if found[name] != "function" {
			t.Errorf("native function %s missing or wrong transport: %q", name, found[name])
		}
	}
	for _, name := range []string{"request_permissions", "get_context_remaining", "new_context", "spawn_agents_on_csv", "tool_search"} {
		if _, ok := found[name]; ok {
			t.Errorf("unrequested tool enabled: %s", name)
		}
	}
}

func (h *nativeToolsHarness) turn(t *testing.T, profile, thread, prompt string) string {
	t.Helper()
	ctx := WithPermissionMode(WithModelProfile(context.Background(), profile), PermissionAsk)
	input, _ := TextUserInput(prompt)
	definition, _ := ModelProfile(profile)
	turn, err := h.manager.StartTurnV2(ctx, thread, []UserInput{input}, definition.Effort)
	if err != nil {
		t.Fatal(err)
	}
	status, _, err := h.notifications.waitForTurn(turn.ID, 60*time.Second)
	if err != nil {
		_ = h.manager.InterruptTurn(context.Background(), thread, turn.ID)
		t.Fatal(err)
	}
	if status != "completed" {
		t.Fatalf("native turn status %s", status)
	}
	return turn.ID
}

func TestNativeToolsRuntimePatchMemoryCode(t *testing.T) {
	for _, profile := range []string{KimiK31ProfileID, "minimax-m3-high-v1"} {
		t.Run(profile, func(t *testing.T) {
			const filename = "2026-11-07T12-00-00-native-tools.md"
			const note = "The ordinary integration note is blue.\n"
			memoryPath := "extensions/ad_hoc/notes/" + filename
			h := newNativeToolsHarness(t, func(h *nativeToolsHarness, body map[string]any, n int64) map[string]any {
				switch n {
				case 1:
					nativeToolsDeclarations(t, body)
					return feat164AuxTool("", "apply_patch", "patch169", map[string]any{"input": "*** Begin Patch\n*** Add File: native-note.txt\n+Native patch result.\n*** End Patch\n"})
				case 2:
					if bytes, err := os.ReadFile(filepath.Join(h.cwd, "native-note.txt")); err != nil || string(bytes) != "Native patch result.\n" {
						t.Errorf("native patch output missing: %v", err)
					}
					return feat164AuxTool("", "memories__add_ad_hoc_note", "note169", map[string]any{"filename": filename, "note": note})
				case 3:
					if bytes, err := os.ReadFile(filepath.Join(h.home, "memories", memoryPath)); err != nil || string(bytes) != note {
						t.Errorf("native memory note missing: %v", err)
					}
					return feat164AuxTool("", "memories__list", "list169", map[string]any{"path": "extensions/ad_hoc/notes"})
				case 4:
					if !strings.Contains(nativeToolsOutput(body, "list169"), filename) {
						t.Error("native memory list did not return the note")
					}
					return feat164AuxTool("", "memories__read", "read169", map[string]any{"path": memoryPath})
				case 5:
					if !strings.Contains(nativeToolsOutput(body, "read169"), "ordinary integration note") {
						t.Error("native memory read missing")
					}
					return feat164AuxTool("", "memories__search", "search169", map[string]any{"queries": []string{"blue"}})
				case 6:
					if !strings.Contains(nativeToolsOutput(body, "search169"), filename) {
						t.Error("native memory search missing")
					}
					return feat164AuxTool("", "exec", "exec169", map[string]any{"input": "text(6 * 7);"})
				case 7:
					if !strings.Contains(nativeToolsOutput(body, "exec169"), "42") {
						t.Error("native V8 result missing")
					}
					return feat164AuxText("原生文件与记忆已交付，计算结果为 42。")
				default:
					t.Errorf("unexpected extra model request %d", n)
					return feat164AuxText("正常结束。")
				}
			})
			ctx := WithPermissionMode(WithModelProfile(context.Background(), profile), PermissionAsk)
			thread, err := h.manager.StartThread(ctx, h.cwd)
			if err != nil {
				t.Fatal(err)
			}
			h.turn(t, profile, thread.ID, "创建测试文件，记住一条普通偏好并读取、检索它，再计算 6 × 7。")
			if h.calls.Load() != 7 {
				t.Fatalf("requests=%d; unexpected memory background work", h.calls.Load())
			}
			t.Log("real native patch, four memory tools, V8; 7 loopback requests, 0 supplier requests")
		})
	}
}

func TestNativeToolsRuntimeCodeWait(t *testing.T) {
	var mu sync.Mutex
	cell := ""
	h := newNativeToolsHarness(t, func(_ *nativeToolsHarness, body map[string]any, n int64) map[string]any {
		mu.Lock()
		defer mu.Unlock()
		if n == 1 {
			return feat164AuxTool("", "exec", "yield169", map[string]any{"input": "// @exec: {\"yield_time_ms\": 1}\nconst result = await tools.exec_command({cmd: \"python3 -c 'import time; time.sleep(0.2); print(42)'\", yield_time_ms: 1000}); text(result);"})
		}
		if n == 2 {
			match := regexp.MustCompile(`Script running with cell ID ([A-Za-z0-9_-]+)`).FindStringSubmatch(nativeToolsOutput(body, "yield169"))
			if len(match) != 2 {
				t.Error("native exec did not yield a cell", nativeToolsOutput(body, "yield169"))
				return feat164AuxText("未得到等待标识。")
			}
			cell = match[1]
			return feat164AuxTool("", "wait", "wait169", map[string]any{"cell_id": cell, "yield_time_ms": 1000})
		}
		if n == 3 && strings.Contains(nativeToolsOutput(body, "wait169"), "42") {
			return feat164AuxText("原生等待已完成，结果为 42。")
		}
		t.Error("native wait result missing", nativeToolsOutput(body, "wait169"))
		return feat164AuxText("正常结束。")
	})
	thread, err := h.manager.StartThread(feat164AuxCtx(), h.cwd)
	if err != nil {
		t.Fatal(err)
	}
	h.turn(t, KimiK31ProfileID, thread.ID, "运行普通本地计算并通过原生 wait 等待结果。")
	if cell == "" || h.calls.Load() != 3 {
		t.Fatal("Code Mode wait was not completed")
	}
}

// Both selected providers document these ordinary Responses input kinds. Do not
// let the loopback accept private OpenAI agent_message or encrypted parameters.
func nativeToolsProviderInput(t *testing.T, body map[string]any) {
	t.Helper()
	items, _ := body["input"].([]any)
	for _, raw := range items {
		item, _ := raw.(map[string]any)
		switch item["type"] {
		case "message", "function_call", "function_call_output", "reasoning":
		default:
			t.Errorf("unsupported provider input kind: %v", item["type"])
		}
	}
	items, _ = body["tools"].([]any)
	for _, raw := range items {
		tool, _ := raw.(map[string]any)
		// MiniMax's separate Server Tools guide documents web_search even though
		// its generic Tool OpenAPI enum lists only client-side function tools.
		if tool["type"] != "function" && tool["type"] != "web_search" && !(body["model"] == "kimi-k3.1-pro" && tool["type"] == "namespace") {
			t.Errorf("unsupported provider tool kind: %v", tool["type"])
		}
		encoded, _ := json.Marshal(tool["parameters"])
		if strings.Contains(string(encoded), `"encrypted":`) {
			t.Error("unsupported encrypted function parameter")
		}
	}
}

func (h *nativeToolsHarness) restart(t *testing.T, cfg Config) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := h.manager.Shutdown(ctx); err != nil {
		t.Fatal("normal restart stopped; Runtime remains owned", err)
	}
	h.manager = NewManager(cfg, nil)
	if err := h.manager.SetNotificationHandler(h.notifications.handle); err != nil {
		t.Fatal(err)
	}
	if err := h.manager.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
}
