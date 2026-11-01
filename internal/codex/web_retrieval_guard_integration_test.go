package codex

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// Real canonical Runtime and normal commands in a private temporary workspace.
// The model transport is loopback only. No supplier call, executable substitution,
// permission manipulation, process fault or attack fixture is used.
func TestYijieRetrievalExecutionGuardRuntime(t *testing.T) {
	binary, manifest := integrationArtifactPaths(t)
	root := integrationPrivateTempDir(t)
	cwd := filepath.Join(root, "workspace")
	if err := os.Mkdir(cwd, 0700); err != nil {
		t.Fatal(err)
	}
	var requests atomic.Int64
	var replID atomic.Int64
	call := func(id, command string) map[string]any {
		args := map[string]any{"cmd": command, "workdir": cwd, "max_output_tokens": 1000}
		if id == "guard_repl" {
			args["tty"] = true
			args["yield_time_ms"] = 1000
		}
		arguments, _ := json.Marshal(args)
		return map[string]any{"type": "function_call", "id": "fc_" + id, "name": "exec_command", "call_id": id, "arguments": string(arguments)}
	}
	stdinCall := func(id, chars string) map[string]any {
		arguments, _ := json.Marshal(map[string]any{"session_id": replID.Load(), "chars": chars, "max_output_tokens": 1000, "yield_time_ms": 1000})
		return map[string]any{"type": "function_call", "id": "fc_" + id, "name": "write_stdin", "call_id": id, "arguments": string(arguments)}
	}
	assertOutput := func(body map[string]any, id, text string) {
		t.Helper()
		items, _ := body["input"].([]any)
		for _, raw := range items {
			item, _ := raw.(map[string]any)
			if item["type"] == "function_call_output" && item["call_id"] == id {
				value, _ := json.Marshal(item["output"])
				if !strings.Contains(string(value), text) {
					t.Errorf("%s response missing expected result %q", id, text)
				}
				return
			}
		}
		t.Errorf("%s response missing", id)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if r.Method != "POST" || r.URL.Path != "/v1/responses" || json.NewDecoder(r.Body).Decode(&body) != nil {
			t.Error("unexpected local request")
			w.WriteHeader(400)
			return
		}
		n := requests.Add(1)
		id := fmt.Sprintf("guard_response_%d", n)
		final := map[string]any{"id": "message_" + id, "type": "message", "role": "assistant", "phase": "final_answer", "status": "completed", "content": []any{map[string]any{"type": "output_text", "text": "普通本机验证完成", "annotations": []any{}}}}
		output := []any{final}
		switch n {
		case 1:
			search := map[string]any{"id": "guard_search", "type": "web_search_call", "status": "completed", "action": map[string]any{"type": "search", "query": "ordinary public reference", "sources": []any{map[string]any{"type": "url", "url": "https://example.com/", "title": "Example"}}}}
			output = []any{search, call("guard_web", `python3 -c 'import urllib.request; urllib.request.urlopen("https://example.com/")'`)}
		case 2:
			assertOutput(body, "guard_web", "YIJIE_WEB_RETRIEVAL_ONLY")
			output = []any{call("guard_compute", `python3 -c 'from pathlib import Path; Path("result.txt").write_text(str(6 * 7))'`)}
		case 3:
			assertOutput(body, "guard_compute", "code 0")
			output = []any{call("guard_file", "cat result.txt")}
		case 4:
			assertOutput(body, "guard_file", "42")
			output = []any{call("guard_development", "git --version")}
		case 5:
			assertOutput(body, "guard_development", "git version")
			output = []any{call("guard_repl", "python3 -i -q")}
		case 6:
			items, _ := body["input"].([]any)
			for _, raw := range items {
				item, _ := raw.(map[string]any)
				if item["type"] == "function_call_output" && item["call_id"] == "guard_repl" {
					value, _ := json.Marshal(item["output"])
					match := regexp.MustCompile(`session ID ([0-9]+)`).FindStringSubmatch(string(value))
					if len(match) == 2 {
						value, _ := strconv.ParseInt(match[1], 10, 64)
						replID.Store(value)
					}
				}
			}
			if replID.Load() == 0 {
				t.Error("normal interactive session unavailable")
			}
			output = []any{stdinCall("guard_web_stdin", "import urllib.request\nurllib.request.urlopen('https://example.com/')\n")}
		case 7:
			assertOutput(body, "guard_web_stdin", "YIJIE_WEB_RETRIEVAL_ONLY")
			output = []any{stdinCall("guard_repl_exit", "exit()\n")}
		case 8:
			assertOutput(body, "guard_repl_exit", "code 0")
		case 9:
			// A normally missing input, not a filesystem/permission fault.
			output = []any{call("guard_missing", "cat supplied-later.txt")}
		case 10:
			assertOutput(body, "guard_missing", "code 1")
			output = []any{call("guard_unchanged", "cat supplied-later.txt")}
		case 11:
			assertOutput(body, "guard_unchanged", "YIJIE_UNCHANGED_FAILURE")
			output = []any{call("guard_supply", "printf 'ready' > supplied-later.txt")}
		case 12:
			assertOutput(body, "guard_supply", "code 0")
			output = []any{call("guard_corrected", "cat supplied-later.txt")}
		case 13:
			assertOutput(body, "guard_corrected", "ready")
		default:
			t.Error("unexpected extra model request")
			w.WriteHeader(429)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		emit := func(event any) {
			raw, _ := json.Marshal(event)
			fmt.Fprintf(w, "data: %s\n\n", raw)
			w.(http.Flusher).Flush()
		}
		emit(map[string]any{"type": "response.created", "response": map[string]any{"id": id, "status": "in_progress", "output": []any{}}})
		for i, item := range output {
			emit(map[string]any{"type": "response.output_item.added", "output_index": i, "item": item})
			emit(map[string]any{"type": "response.output_item.done", "output_index": i, "item": item})
		}
		emit(map[string]any{"type": "response.completed", "response": map[string]any{"id": id, "status": "completed", "output": output, "usage": map[string]any{"input_tokens": 1, "output_tokens": 1, "total_tokens": 2}}})
	}))
	defer server.Close()
	runtimeHome := filepath.Join(root, "runtime")
	if err := os.Mkdir(runtimeHome, 0700); err != nil {
		t.Fatal(err)
	}
	config := integrationConfig(binary, manifest, runtimeHome, "ordinary-loopback-key")
	config.ChatModelsEnabled, config.KimiK31Candidate, config.RuntimePermissionsEnabled = true, true, true
	config.KimiAPIKey, config.PermissionVerificationBaseURL = "ordinary-loopback-kimi", server.URL+"/v1"
	manager := NewManager(config, nil)
	notifications := newIntegrationNotifications()
	var mu sync.Mutex
	started := map[string]int{}
	warnings := ""
	if err := manager.SetNotificationHandler(func(method string, raw json.RawMessage) {
		notifications.handle(method, raw)
		var event struct {
			Message string                    `json:"message"`
			Item    struct{ ID, Type string } `json:"item"`
		}
		if json.Unmarshal(raw, &event) != nil {
			return
		}
		mu.Lock()
		defer mu.Unlock()
		if method == "item/started" && event.Item.Type == "commandExecution" {
			started[event.Item.ID]++
		}
		if method == "warning" {
			warnings += event.Message + "\n"
		}
	}); err != nil {
		t.Fatal(err)
	}
	defer shutdownIntegrationManager(t, manager, 25*time.Second)
	if err := manager.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	ctx := WithPermissionMode(WithModelProfile(context.Background(), KimiK31ProfileID), PermissionAsk)
	thread, err := manager.StartThread(ctx, cwd)
	if err != nil {
		t.Fatal(err)
	}
	for _, prompt := range []string{"检索参考资料后进行普通本地计算与文件处理。", "读取给定输入，若缺少则补充普通输入文件后继续。"} {
		input, _ := TextUserInput(prompt)
		turn, err := manager.StartTurnV2(ctx, thread.ID, []UserInput{input}, "max")
		if err != nil {
			t.Fatal(err)
		}
		state, _, err := notifications.waitForTurn(turn.ID, 30*time.Second)
		if err != nil || state != "completed" {
			t.Fatal(state, err)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	for _, id := range []string{"guard_web", "guard_web_stdin", "guard_unchanged"} {
		if started[id] != 0 {
			t.Errorf("blocked operation %s reached the command executor", id)
		}
	}
	for _, id := range []string{"guard_compute", "guard_file", "guard_development", "guard_repl", "guard_missing", "guard_supply", "guard_corrected"} {
		if started[id] != 1 {
			t.Errorf("ordinary operation %s did not execute exactly once: %d", id, started[id])
		}
	}
	for _, code := range []string{"YIJIE_WEB_RETRIEVAL_ONLY", "YIJIE_UNCHANGED_FAILURE"} {
		if !strings.Contains(warnings, code) {
			t.Errorf("visible warning missing %s", code)
		}
	}
	if requests.Load() != 13 {
		t.Fatal("unexpected loopback request count", requests.Load())
	}
	if !t.Failed() {
		t.Log("13 loopback Responses; three execution refusals including interactive stdin; seven normal command starts; graceful interpreter exit; changed input retry succeeds; both warnings visible; zero supplier calls")
	}
}
