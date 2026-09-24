package codex

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	inputonly "github.com/36Dge/yijie-agent-host/internal/contracts/runtimeinputonly"
	wire "github.com/36Dge/yijie-agent-host/internal/contracts/scheduledraft"
	"github.com/google/uuid"
)

// The actual fixed Runtime serves ordinary image-capable threads and restricted
// drafts in one process. Only local text is returned; no image tool is called.
func TestFEAT155DailyImagesAndDraftNativeCoexistence(t *testing.T) {
	binary, manifest := integrationArtifactPaths(t)
	listener, err := net.Listen("tcp", "127.0.0.1:18083")
	if err != nil {
		t.Fatal("verification port is owned; no process was stopped")
	}
	var requests, drafts, ordinary, imageCalls atomic.Int64
	server := &http.Server{ReadHeaderTimeout: 5 * time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/responses" {
			t.Error("unexpected local route")
			w.WriteHeader(404)
			return
		}
		var body map[string]any
		if json.NewDecoder(r.Body).Decode(&body) != nil {
			t.Error("invalid local request")
			w.WriteHeader(400)
			return
		}
		ordinal := requests.Add(1)
		if ordinal > 4 {
			t.Error("request bound exceeded")
			w.WriteHeader(429)
			return
		}
		text, _ := body["text"].(map[string]any)
		format, _ := text["format"].(map[string]any)
		tools, _ := body["tools"].([]any)
		answer := "普通聊天本机验证完成"
		if format["schema"] != nil {
			drafts.Add(1)
			actual, _ := json.Marshal(format["schema"])
			var schema any
			_ = json.Unmarshal(wire.OutputSchema(), &schema)
			expected, _ := json.Marshal(schema)
			if string(actual) != string(expected) || len(tools) != 0 || body["tool_choice"] != "none" {
				t.Error("draft inherited tools or lost the canonical output schema")
			}
			answer = `{"schema_version":1,"kind":"needs_clarification","missing_fields":["time"],"question":"何时执行？"}`
		} else {
			ordinary.Add(1)
			found := false
			for _, v := range tools {
				tool, _ := v.(map[string]any)
				if tool["name"] == "generate_image" {
					found = true
				}
			}
			if !found {
				t.Error("ordinary thread lost the existing image tool")
			}
		}
		id := fmt.Sprintf("local_daily_%d", ordinal)
		item := map[string]any{"id": "message_" + id, "type": "message", "role": "assistant", "phase": "final_answer", "status": "completed", "content": []any{map[string]any{"type": "output_text", "text": answer, "annotations": []any{}}}}
		w.Header().Set("Content-Type", "text/event-stream")
		for _, event := range []any{
			map[string]any{"type": "response.created", "response": map[string]any{"id": id, "status": "in_progress", "output": []any{}}},
			map[string]any{"type": "response.output_item.added", "output_index": 0, "item": item},
			map[string]any{"type": "response.output_item.done", "output_index": 0, "item": item},
			map[string]any{"type": "response.completed", "response": map[string]any{"id": id, "status": "completed", "output": []any{item}, "usage": map[string]any{"input_tokens": 1, "output_tokens": 1, "total_tokens": 2}}},
		} {
			data, _ := json.Marshal(event)
			_, _ = fmt.Fprintf(w, "data: %s\n\n", data)
			w.(http.Flusher).Flush()
		}
	})}
	go func() { _ = server.Serve(listener) }()
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(ctx); err != nil {
			t.Error(err)
		}
	}()
	root, err := os.MkdirTemp("", "feat155-daily-coexist-")
	if err != nil {
		t.Fatal(err)
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	home, cwd := filepath.Join(root, "runtime"), filepath.Join(root, "workspace")
	for _, p := range []string{home, cwd} {
		if err := os.Mkdir(p, 0700); err != nil {
			t.Fatal(err)
		}
	}
	config := integrationConfig(binary, manifest, home, "declared-local-text-only")
	config.RuntimePermissionsEnabled = true
	config.DynamicToolsEnabled = true
	config.PermissionVerificationBaseURL = "http://127.0.0.1:18083/v1"
	var managers []*Manager
	defer func() {
		clean := true
		for _, m := range managers {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			err := m.Shutdown(ctx)
			cancel()
			if err != nil {
				clean = false
				t.Errorf("normal stop pending: %v", err)
			}
		}
		if clean {
			_ = os.RemoveAll(root)
		}
	}()
	draftContext := WithScheduledDraft(context.Background(), uuid.NewString())
	threads := make([]string, 2)
	for generation := 0; generation < 2; generation++ {
		m := NewManager(config, nil)
		managers = append(managers, m)
		if err := m.SetDynamicToolHandler(func(context.Context, DynamicToolCall) DynamicToolResult {
			imageCalls.Add(1)
			return DynamicToolResult{Success: false, Text: "not invoked in text-only verification"}
		}); err != nil {
			t.Fatal(err)
		}
		notifications := newIntegrationNotifications()
		if err := m.SetNotificationHandler(notifications.handle); err != nil {
			t.Fatal(err)
		}
		if err := m.Start(context.Background()); err != nil {
			t.Fatal(err)
		}
		if !m.Snapshot().ExperimentalAPI || !m.ScheduledDraftReady() {
			t.Fatal("ordinary image negotiation and native draft qualification must coexist")
		}
		for i, ctx := range []context.Context{context.Background(), draftContext} {
			var thread ThreadInfo
			if generation == 0 {
				thread, err = m.StartThread(ctx, cwd)
			} else {
				thread, err = m.ResumeThread(ctx, threads[i])
			}
			if err != nil {
				t.Fatal(err)
			}
			if generation == 1 && thread.ID != threads[i] {
				t.Fatal("normal reopen replaced a thread")
			}
			threads[i] = thread.ID
			if i == 0 {
				var raw json.RawMessage
				if err := m.request(ctx, inputonly.Method, inputonly.ThreadInputOnlyPolicyReadParams{ThreadId: thread.ID}, &raw); err != nil {
					t.Fatal(err)
				}
				policy, err := inputonly.DecodePolicy(raw)
				if err != nil || policy.Policy != nil {
					t.Fatal("ordinary thread became a restricted draft")
				}
			} else if err := m.verifyDraftPolicy(ctx, thread.ID, cwd, m.runtimeGeneration); err != nil {
				t.Fatal(err)
			}
			input, _ := TextUserInput("仅返回文字，不使用工具")
			turnContext := WithClientUserMessageID(ctx, uuid.NewString())
			if i == 0 {
				turnContext = WithPermissionMode(turnContext, PermissionAsk)
			}
			turn, err := m.StartTurnV2(turnContext, thread.ID, []UserInput{input}, "none")
			if err != nil {
				t.Fatal(err)
			}
			status, _, err := notifications.waitForTurn(turn.ID, 30*time.Second)
			if err != nil || status != "completed" {
				t.Fatalf("local text turn: %s %v", status, err)
			}
		}
		stop, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		err = m.Shutdown(stop)
		cancel()
		if err != nil {
			t.Fatal(err)
		}
	}
	if requests.Load() != 4 || drafts.Load() != 2 || ordinary.Load() != 2 || imageCalls.Load() != 0 {
		t.Fatalf("requests=%d drafts=%d ordinary=%d image calls=%d", requests.Load(), drafts.Load(), ordinary.Load(), imageCalls.Load())
	}
	t.Log("same pinned Runtime: ordinary image tool preserved, draft tools empty with native proof, both normal resume; 4 local text requests, 0 paid and 0 image calls")
}
