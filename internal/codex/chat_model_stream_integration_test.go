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

	"github.com/google/uuid"
)

// A normal loopback Responses transcript for the observed empty-intermediate /
// complete-terminal variant. Uses the exact real Runtime and only /bin/pwd.
// No executable replacement, forced shutdown, remote API or provider credential.
func TestFEAT156NativeTerminalToolArguments(t *testing.T) {
	binary, manifest := integrationArtifactPaths(t)
	listener, err := net.Listen("tcp", "127.0.0.1:18083")
	if err != nil {
		t.Fatal("Local verification port occupied; no process stopped")
	}
	var requests, commands atomic.Int64
	server := &http.Server{ReadHeaderTimeout: 5 * time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if r.Method != "POST" || r.URL.Path != "/v1/responses" || json.NewDecoder(r.Body).Decode(&body) != nil {
			t.Error("Unexpected loopback request")
			w.WriteHeader(400)
			return
		}
		n := requests.Add(1)
		if n > 2 {
			t.Error("Unexpected tool continuation")
			w.WriteHeader(400)
			return
		}
		effort, _ := body["reasoning"].(map[string]any)
		if body["model"] != "MiniMax-M3" || effort["effort"] != "high" {
			t.Error("Incorrect model profile")
		}
		w.Header().Set("Content-Type", "text/event-stream")
		emit := func(event any) {
			raw, _ := json.Marshal(event)
			fmt.Fprintf(w, "data: %s\n\n", raw)
			w.(http.Flusher).Flush()
		}
		emit(map[string]any{"type": "response.created", "response": map[string]any{"id": "local_response", "status": "in_progress"}})
		var item map[string]any
		if n == 1 {
			arguments := `{"cmd":"/bin/pwd","max_output_tokens":1000}`
			item = map[string]any{"type": "function_call", "id": "ordinary_function", "call_id": "ordinary_call", "name": "exec_command", "arguments": ""}
			emit(map[string]any{"type": "response.output_item.added", "output_index": 0, "item": item})
			emit(map[string]any{"type": "response.function_call_arguments.delta", "output_index": 0, "item_id": "ordinary_function", "delta": arguments})
			emit(map[string]any{"type": "response.function_call_arguments.done", "output_index": 0, "item_id": "ordinary_function", "arguments": ""})
			emit(map[string]any{"type": "response.output_item.done", "output_index": 0, "item": item})
			item["arguments"] = arguments
		} else {
			item = map[string]any{"type": "message", "id": "ordinary_message", "role": "assistant", "status": "completed", "phase": "final_answer", "content": []any{map[string]any{"type": "output_text", "text": "LOCAL_TOOL_COMPLETE", "annotations": []any{}}}}
			emit(map[string]any{"type": "response.output_item.added", "output_index": 0, "item": item})
			emit(map[string]any{"type": "response.output_item.done", "output_index": 0, "item": item})
		}
		emit(map[string]any{"type": "response.completed", "response": map[string]any{"id": fmt.Sprintf("local_%d", n), "status": "completed", "output": []any{item}, "usage": map[string]any{"input_tokens": 1, "output_tokens": 1, "total_tokens": 2}}})
	})}
	go func() { _ = server.Serve(listener) }()
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if server.Shutdown(ctx) != nil {
			t.Error("Normal local server shutdown pending")
		}
	}()
	root, err := os.MkdirTemp("", "feat156-native-stream-")
	if err != nil {
		t.Fatal(err)
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	home, cwd := filepath.Join(root, "runtime"), filepath.Join(root, "workspace")
	for _, p := range []string{home, cwd} {
		if os.Mkdir(p, 0700) != nil {
			t.Fatal("Cannot prepare ordinary workspace")
		}
	}
	config := integrationConfig(binary, manifest, home, "local-minimax-fixture")
	config.ChatModelsEnabled = true
	config.KimiAPIKey = "local-kimi-fixture"
	config.RuntimePermissionsEnabled = true
	config.PermissionVerificationBaseURL = "http://127.0.0.1:18083/v1"
	manager := NewManager(config, nil)
	notifications := newIntegrationNotifications()
	if manager.SetNotificationHandler(func(method string, raw json.RawMessage) {
		if method == "item/completed" {
			var event struct {
				Item struct {
					Type     string `json:"type"`
					Status   string `json:"status"`
					ExitCode *int   `json:"exitCode"`
				} `json:"item"`
			}
			if json.Unmarshal(raw, &event) == nil && event.Item.Type == "commandExecution" {
				if event.Item.Status != "completed" || event.Item.ExitCode == nil || *event.Item.ExitCode != 0 {
					t.Error("Ordinary read-only command failed")
				} else {
					commands.Add(1)
				}
			}
		}
		notifications.handle(method, raw)
	}) != nil {
		t.Fatal("Notification setup failed")
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
		defer cancel()
		if manager.Shutdown(ctx) != nil {
			t.Error("Normal runtime shutdown pending; files preserved")
		} else {
			_ = os.RemoveAll(root)
		}
	}()
	if err = manager.Start(context.Background()); err != nil {
		t.Fatalf("Runtime startup: %v", err)
	}
	ctx := WithModelProfile(context.Background(), "minimax-m3-high-v1")
	thread, err := manager.StartThread(ctx, cwd)
	if err != nil {
		t.Fatal(err)
	}
	input, _ := TextUserInput("Run only the read-only command /bin/pwd, then report completion.")
	ctx = WithClientUserMessageID(WithPermissionMode(ctx, PermissionAsk), uuid.NewString())
	turn, err := manager.StartTurnV2(ctx, thread.ID, []UserInput{input}, "high")
	if err != nil {
		t.Fatal(err)
	}
	status, _, err := notifications.waitForTurn(turn.ID, time.Minute)
	if err != nil || status != "completed" || requests.Load() != 2 || commands.Load() != 1 {
		t.Fatalf("Native compatibility incomplete: status=%s requests=%d commands=%d", status, requests.Load(), commands.Load())
	}
	t.Log("real fixed Runtime: verified terminal arguments -> one normal command exit0 -> final answer;2loopback requests,0paid")
}
