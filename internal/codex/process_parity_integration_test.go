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
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// Real pinned Runtime, original Manager, an explicitly local Responses fixture,
// and one ordinary read per requested turn. No external model or replaced executable.
func TestFEAT163NativeProgressFeedbackAndColdRead(t *testing.T) {
	binary, manifest := integrationArtifactPaths(t)
	listener, err := net.Listen("tcp", "127.0.0.1:18083")
	if err != nil {
		t.Fatal("verification port occupied; no process stopped")
	}
	var requests atomic.Int64
	server := &http.Server{ReadHeaderTimeout: 5 * time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if r.Method != "POST" || r.URL.Path != "/v1/responses" || json.NewDecoder(r.Body).Decode(&body) != nil {
			t.Error("unexpected local request")
			w.WriteHeader(400)
			return
		}
		n := requests.Add(1)
		if n > 4 {
			t.Error("unexpected continuation")
			w.WriteHeader(400)
			return
		}
		encoded, _ := json.Marshal(body)
		if !strings.Contains(string(encoded), "Before a group of substantive tool calls") || !strings.Contains(string(encoded), "preserve and inspect HTTP errors") {
			t.Error("effective progress/retrieval guidance missing")
		}
		if n%2 == 0 {
			found := false
			for _, raw := range body["input"].([]any) {
				v, _ := raw.(map[string]any)
				if v["type"] == "function_call_output" {
					b, _ := json.Marshal(v["output"])
					found = found || strings.Contains(string(b), "FEAT163_ORDINARY_NOTE")
				}
			}
			if !found {
				t.Error("actual command feedback missing from next request")
			}
		}
		w.Header().Set("Content-Type", "text/event-stream")
		emit := func(v any) { b, _ := json.Marshal(v); fmt.Fprintf(w, "data: %s\n\n", b); w.(http.Flusher).Flush() }
		id := fmt.Sprintf("response_%d", n)
		emit(map[string]any{"type": "response.created", "response": map[string]any{"id": id, "status": "in_progress"}})
		phase, prose := "commentary", "我会读取本地说明，确认内容后给出结果。"
		if n%2 == 0 {
			phase, prose = "final_answer", "已读取本地说明，内容为 FEAT163_ORDINARY_NOTE。"
		}
		message := map[string]any{"id": fmt.Sprintf("message_%d", n), "type": "message", "role": "assistant", "phase": phase, "status": "completed", "content": []any{map[string]any{"type": "output_text", "text": prose, "annotations": []any{}}}}
		items := []any{message}
		if n%2 == 1 {
			items = append(items, map[string]any{"id": fmt.Sprintf("tool_%d", n), "type": "function_call", "call_id": fmt.Sprintf("call_%d", n), "name": "exec_command", "arguments": `{"cmd":"cat note.md","max_output_tokens":1000}`})
		}
		for i, item := range items {
			emit(map[string]any{"type": "response.output_item.added", "output_index": i, "item": item})
			emit(map[string]any{"type": "response.output_item.done", "output_index": i, "item": item})
		}
		emit(map[string]any{"type": "response.completed", "response": map[string]any{"id": id, "status": "completed", "output": items, "usage": map[string]any{"input_tokens": 1, "output_tokens": 1, "total_tokens": 2}}})
	})}
	go func() { _ = server.Serve(listener) }()
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(ctx); err != nil {
			t.Error(err)
		}
	}()
	root, err := os.MkdirTemp("", "feat163-native-parity-")
	if err != nil {
		t.Fatal(err)
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	home, cwd := filepath.Join(root, "runtime"), filepath.Join(root, "workspace")
	for _, dir := range []string{home, cwd} {
		if err := os.Mkdir(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(cwd, "note.md"), []byte("FEAT163_ORDINARY_NOTE\n"), 0600); err != nil {
		t.Fatal(err)
	}
	config := integrationConfig(binary, manifest, home, "declared-local-fixture")
	config.RuntimePermissionsEnabled = true
	config.PermissionVerificationBaseURL = "http://127.0.0.1:18083/v1"
	var managers []*Manager
	defer func() {
		clean := true
		for _, m := range managers {
			ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
			err := m.Shutdown(ctx)
			cancel()
			if err != nil {
				clean = false
				t.Error("normal shutdown pending", err)
			}
		}
		if clean {
			_ = os.RemoveAll(root)
		}
	}()
	var mu sync.Mutex
	var completed []string
	threadID := ""
	for round := 0; round < 2; round++ {
		m := NewManager(config, nil)
		managers = append(managers, m)
		events := newIntegrationNotifications()
		if err := m.SetNotificationHandler(func(method string, raw json.RawMessage) {
			if method == "item/completed" {
				var e struct {
					Item struct {
						Type, Phase, Text, Status string
						ExitCode                  *int
					} `json:"item"`
				}
				if json.Unmarshal(raw, &e) == nil {
					mu.Lock()
					if e.Item.Type == "agentMessage" {
						completed = append(completed, e.Item.Phase)
					}
					if e.Item.Type == "commandExecution" {
						completed = append(completed, "command")
						if e.Item.Status != "completed" || e.Item.ExitCode == nil || *e.Item.ExitCode != 0 {
							t.Error("ordinary read failed")
						}
					}
					mu.Unlock()
				}
			}
			events.handle(method, raw)
		}); err != nil {
			t.Fatal(err)
		}
		if err := m.Start(context.Background()); err != nil {
			t.Fatal(err)
		}
		if round == 0 {
			thread, err := m.StartThread(context.Background(), cwd)
			if err != nil {
				t.Fatal(err)
			}
			threadID = thread.ID
		} else {
			before := requests.Load()
			raw, err := m.ReadThread(context.Background(), threadID)
			if err != nil {
				t.Fatal(err)
			}
			for _, s := range []string{"commentary", "final_answer", "FEAT163_ORDINARY_NOTE"} {
				if !strings.Contains(string(raw), s) {
					t.Error("cold native read lost", s)
				}
			}
			// Legacy native history intentionally omits tool events. This is a
			// recovery limitation, not proof of full cold-history parity.
			if strings.Contains(string(raw), `"historyMode":"legacy"`) && !strings.Contains(string(raw), "commandExecution") {
				t.Log("LIMITATION: legacy cold read retains public messages but omits command details; full cold-history parity NOT VERIFIED")
			}
			if requests.Load() != before {
				t.Fatal("history read invoked provider")
			}
			if _, err := m.ResumeThread(context.Background(), threadID); err != nil {
				t.Fatal(err)
			}
		}
		input, _ := TextUserInput("读取本地 note.md 后汇报内容。")
		turn, err := m.StartTurnV2(WithPermissionMode(context.Background(), PermissionAsk), threadID, []UserInput{input}, "high")
		if err != nil {
			t.Fatal(err)
		}
		status, _, err := events.waitForTurn(turn.ID, 45*time.Second)
		if err != nil || status != "completed" {
			t.Fatalf("normal turn: %s %v", status, err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
		err = m.Shutdown(ctx)
		cancel()
		if err != nil {
			t.Fatal("normal shutdown pending", err)
		}
	}
	mu.Lock()
	got := strings.Join(completed, ",")
	mu.Unlock()
	if got != "commentary,command,final_answer,commentary,command,final_answer" || requests.Load() != 4 {
		t.Fatalf("order/count: %s; requests=%d", got, requests.Load())
	}
	if !t.Failed() {
		t.Log("real pinned Runtime: public message -> actual file read -> feedback -> answer, normal EOF/reopen, cold public-message read without request and explicit continuation; 4 local requests, 0 paid")
	}
}
