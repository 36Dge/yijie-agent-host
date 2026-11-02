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
	"testing"
	"time"
)

// Canonical Runtime with normal loopback Responses data. No supplier request,
// executable substitution, fault injection, signal termination or task replay.
func TestYijieAnswerCompletionRuntime(t *testing.T) {
	binary, manifest := integrationArtifactPaths(t)
	root := integrationPrivateTempDir(t)
	cwd, runtimeHome := filepath.Join(root, "workspace"), filepath.Join(root, "runtime")
	for _, p := range []string{cwd, runtimeHome} {
		if err := os.Mkdir(p, 0700); err != nil {
			t.Fatal(err)
		}
	}
	var mu sync.Mutex
	counts := map[string]int{}
	commandStarts := 0
	warnings, answers := "", ""
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if r.Method != "POST" || json.NewDecoder(r.Body).Decode(&body) != nil {
			t.Error("unexpected request")
			w.WriteHeader(400)
			return
		}
		input, _ := body["input"].([]any)
		name := ""
		for _, raw := range input {
			m, _ := raw.(map[string]any)
			if m["role"] == "user" {
				b, _ := json.Marshal(m["content"])
				for _, key := range []string{"promise", "empty", "repeated", "tool_again", "short", "plan", "commentary"} {
					if strings.Contains(string(b), "case_"+key) {
						name = key
					}
				}
			}
		}
		mu.Lock()
		counts[name]++
		n := counts[name]
		mu.Unlock()
		if name == "" || n > 3 {
			t.Error("unbounded/unrecognized finalization")
			w.WriteHeader(400)
			return
		}
		text, phase := "已有结果：6 × 7 = 42。", "final_answer"
		if n == 1 && name != "short" {
			text, phase = "我会先核实并整理结果。", ""
		}
		if name == "plan" {
			text = "我会先核实并整理结果。"
		}
		if name == "repeated" {
			text, phase = "我会继续核实并整理结果。", ""
		}
		if name == "commentary" && n == 1 {
			phase = "commentary"
		}
		message := map[string]any{"id": fmt.Sprintf("msg_%s_%d", name, n), "type": "message", "role": "assistant", "status": "completed", "content": []any{map[string]any{"type": "output_text", "text": text}}}
		if phase != "" {
			message["phase"] = phase
		}
		output := []any{message}
		if name == "empty" && n == 1 {
			output = []any{map[string]any{"id": "empty_reasoning", "type": "reasoning", "summary": []any{map[string]any{"type": "summary_text", "text": ""}}}}
		}
		if name == "promise" && n == 1 {
			output = append([]any{map[string]any{"id": "search_reference", "type": "web_search_call", "status": "completed", "action": map[string]any{"type": "search", "query": "ordinary reference", "sources": []any{map[string]any{"type": "url", "url": "https://example.org", "title": "Ordinary reference"}}}}}, output...)
		}
		if name == "tool_again" && n == 2 {
			args, _ := json.Marshal(map[string]any{"cmd": "python3 -c 'print(6 * 7)'", "workdir": cwd})
			output = []any{map[string]any{"id": "unneeded_compute", "type": "function_call", "name": "exec_command", "call_id": "unneeded_compute", "arguments": string(args)}}
		}
		if n > 1 {
			tools, _ := body["tools"].([]any)
			if len(tools) != 0 {
				t.Error("answer-only finalization still advertised tools")
			}
			b, _ := json.Marshal(input)
			if !strings.Contains(string(b), "bounded, answer-only finalization") {
				t.Error("finalization context lost")
			}
		}
		id := fmt.Sprintf("response_%s_%d", name, n)
		w.Header().Set("Content-Type", "text/event-stream")
		emit := func(v any) { b, _ := json.Marshal(v); fmt.Fprintf(w, "data: %s\n\n", b); w.(http.Flusher).Flush() }
		emit(map[string]any{"type": "response.created", "response": map[string]any{"id": id, "status": "in_progress", "output": []any{}}})
		for i, item := range output {
			emit(map[string]any{"type": "response.output_item.added", "output_index": i, "item": item})
			emit(map[string]any{"type": "response.output_item.done", "output_index": i, "item": item})
		}
		emit(map[string]any{"type": "response.completed", "response": map[string]any{"id": id, "status": "completed", "output": output, "usage": map[string]any{"input_tokens": 1, "output_tokens": 1, "total_tokens": 2}}})
	}))
	defer server.Close()
	config := integrationConfig(binary, manifest, runtimeHome, "ordinary-loopback-key")
	config.ChatModelsEnabled, config.KimiK31Candidate, config.RuntimePermissionsEnabled = true, true, true
	config.KimiAPIKey, config.PermissionVerificationBaseURL = "ordinary-loopback-kimi", server.URL+"/v1"
	manager := NewManager(config, nil)
	notifications := newIntegrationNotifications()
	if err := manager.SetNotificationHandler(func(method string, raw json.RawMessage) {
		notifications.handle(method, raw)
		var p struct {
			Message string `json:"message"`
			Error   struct {
				Message string `json:"message"`
			} `json:"error"`
			Item struct{ Type, Text string } `json:"item"`
		}
		_ = json.Unmarshal(raw, &p)
		if p.Message == "" {
			p.Message = p.Error.Message
		}
		mu.Lock()
		defer mu.Unlock()
		if method == "item/started" && p.Item.Type == "commandExecution" {
			commandStarts++
		}
		if method == "warning" || method == "error" {
			warnings += p.Message + "\n"
		}
		if method == "item/completed" && p.Item.Type == "agentMessage" {
			answers += p.Item.Text + "\n"
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
	for _, tc := range []struct {
		name, prompt, status string
		requests             int
	}{
		{"promise", "请说明已有参考资料的结论", "completed", 2},
		{"empty", "请给出计算结论", "completed", 2},
		{"repeated", "请给出资料结论", "failed", 3},
		{"tool_again", "请根据已有结果回答", "completed", 3},
		{"short", "6乘7是多少", "completed", 1},
		{"plan", "只给计划，不要执行", "completed", 1},
		{"commentary", "请给出结果", "completed", 2},
	} {
		input, _ := TextUserInput("case_" + tc.name + " " + tc.prompt)
		turn, err := manager.StartTurnV2(ctx, thread.ID, []UserInput{input}, "max")
		if err != nil {
			t.Fatal(err)
		}
		status, _, err := notifications.waitForTurn(turn.ID, 35*time.Second)
		if err != nil || status != tc.status {
			t.Fatalf("%s: status=%s err=%v", tc.name, status, err)
		}
		mu.Lock()
		n := counts[tc.name]
		mu.Unlock()
		if n != tc.requests {
			t.Fatalf("%s requests=%d want=%d", tc.name, n, tc.requests)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if commandStarts != 0 {
		t.Fatal("finalization executed an unnecessary command")
	}
	for _, code := range []string{"YIJIE_ANSWER_CONTINUATION", "YIJIE_ANSWER_INCOMPLETE", "YIJIE_ANSWER_FINALIZING"} {
		if !strings.Contains(warnings, code) {
			t.Errorf("missing %s", code)
		}
	}
	if !strings.Contains(answers, "6 × 7 = 42") {
		t.Fatal("no actual answer delivered")
	}
	t.Log("7 turns; same-turn recovery, capped failure, command blocked before start, short answer and requested plan preserved; zero supplier requests")
}
