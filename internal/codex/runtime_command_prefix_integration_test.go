package codex

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// A local Responses provider requests ordinary echo commands. The pinned
// app-server issues every callback, executes the commands and writes its own
// rule into a fresh CODEX_HOME. No paid model or daily permissions are used.
func TestRuntimeCommandPrefixNativeRoundTrip(t *testing.T) {
	binary, manifest := integrationArtifactPaths(t)
	home, cwd := integrationPrivateTempDir(t), integrationPrivateTempDir(t)
	var calls atomic.Int32
	labels := []string{"once", "reject", "similar", "match", "reopened"}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := int(calls.Add(1))
		if n > 2*len(labels) {
			t.Error("unexpected additional provider request")
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		label := labels[(n-1)/2]
		var item map[string]any
		if n%2 == 1 {
			args, _ := json.Marshal(map[string]any{"cmd": "echo approval-example " + label, "sandbox_permissions": "require_escalated", "justification": "允许输出这条示例文字吗？", "prefix_rule": []string{"echo", "approval-example"}})
			item = map[string]any{"type": "function_call", "name": "exec_command", "id": "item_" + label, "call_id": "call_" + label, "arguments": string(args)}
		} else {
			var body struct {
				Input []struct {
					Type   string `json:"type"`
					CallID string `json:"call_id"`
					Output string `json:"output"`
				} `json:"input"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			var result string
			for _, entry := range body.Input {
				if entry.Type == "function_call_output" && entry.CallID == "call_"+label {
					result = entry.Output
				}
			}
			if label == "reject" {
				if !strings.Contains(strings.ToLower(result), "reject") {
					t.Errorf("command refusal was not returned: %q", result)
				}
			} else if !strings.Contains(result, "\napproval-example "+label) {
				t.Errorf("native command output missing for %s: %q", label, result)
			}
			item = map[string]any{"type": "message", "role": "assistant", "id": "msg_" + label, "status": "completed", "phase": "final_answer", "content": []any{map[string]any{"type": "output_text", "text": "示例已结束。"}}}
		}
		id := fmt.Sprintf("resp_%d", n)
		w.Header().Set("Content-Type", "text/event-stream")
		emit := func(data any) {
			b, _ := json.Marshal(data)
			fmt.Fprintf(w, "data: %s\n\n", b)
			w.(http.Flusher).Flush()
		}
		emit(map[string]any{"type": "response.created", "response": map[string]any{"id": id, "status": "in_progress", "output": []any{}}})
		emit(map[string]any{"type": "response.output_item.added", "output_index": 0, "item": item})
		emit(map[string]any{"type": "response.output_item.done", "output_index": 0, "item": item})
		emit(map[string]any{"type": "response.completed", "response": map[string]any{"id": id, "status": "completed", "output": []any{item}, "usage": map[string]int{"input_tokens": 1, "output_tokens": 1, "total_tokens": 2}}})
	}))
	defer server.Close()
	config := integrationConfig(binary, manifest, home, "loopback-minimax")
	config.ChatModelsEnabled, config.KimiK31Candidate, config.RuntimePermissionsEnabled = true, true, true
	config.KimiAPIKey, config.PermissionVerificationBaseURL = "loopback-kimi", server.URL+"/v1"
	manager := NewManager(config, nil)
	notifications := newIntegrationNotifications()
	if err := manager.SetNotificationHandler(notifications.handle); err != nil {
		t.Fatal(err)
	}
	defer func() { shutdownIntegrationManager(t, manager, 25*time.Second) }()
	if err := manager.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	ctx := WithPermissionMode(WithModelProfile(context.Background(), KimiK31ProfileID), PermissionAsk)
	thread, err := manager.StartThread(ctx, cwd)
	if err != nil {
		t.Fatal(err)
	}
	for i, label := range labels {
		if i == 4 {
			if got := len(manager.ListRuntimeApprovals(thread.ID)); got != 3 {
				t.Fatalf("expected three callbacks across four distinct commands; got %d", got)
			}
			stop, cancel := context.WithTimeout(context.Background(), 25*time.Second)
			err := manager.Shutdown(stop)
			cancel()
			if err != nil {
				t.Fatal("normal close", err)
			}
			manager = NewManager(config, nil)
			notifications = newIntegrationNotifications()
			if err := manager.SetNotificationHandler(notifications.handle); err != nil {
				t.Fatal(err)
			}
			if err := manager.Start(ctx); err != nil {
				t.Fatal("normal restart with native rules", err)
			}
			thread, err = manager.StartThread(ctx, cwd)
			if err != nil {
				t.Fatal(err)
			}
		}
		input, _ := TextUserInput("请输出示例文字 " + label)
		turn, err := manager.StartTurnV2(ctx, thread.ID, []UserInput{input}, "max")
		if err != nil {
			t.Fatal(err)
		}
		if i < 3 {
			var pending *RuntimeApproval
			for deadline := time.Now().Add(12 * time.Second); time.Now().Before(deadline) && pending == nil; {
				for _, view := range manager.ListRuntimeApprovals(thread.ID) {
					if view.Status == "pending" {
						pending = &view
						break
					}
				}
				if pending == nil {
					time.Sleep(20 * time.Millisecond)
				}
			}
			if pending == nil || !slices.Equal(pending.CommandPrefix, []string{"echo", "approval-example"}) {
				t.Fatalf("turn %s: expected a native command prefix, got %+v", label, pending)
			}
			decision := []string{"approve_once", "reject", ApproveSimilar}[i]
			receipt, err := manager.DecideRuntimeApproval(ctx, thread.ID, pending.ID, decision)
			expectedStatus := "approved"
			if decision == "reject" {
				expectedStatus = "rejected"
			}
			if err != nil || receipt.Status != expectedStatus || len(receipt.CommandPrefix) != 0 {
				t.Fatalf("turn %s: unexpected receipt: %+v %v", label, receipt, err)
			}
		}
		status, _, err := notifications.waitForTurn(turn.ID, 15*time.Second)
		if err != nil || status != "completed" {
			t.Fatalf("turn %s: %s %v", label, status, err)
		}
	}
	if got := len(manager.ListRuntimeApprovals(thread.ID)); got != 0 {
		t.Fatalf("reopened Runtime prompted for a prefix already approved: %d", got)
	}
	rules, err := os.ReadFile(filepath.Join(home, "rules", "default.rules"))
	if err != nil || !strings.Contains(string(rules), `prefix_rule(pattern=["echo", "approval-example"], decision="allow")`) {
		t.Fatal("Codex did not persist the reviewed prefix in its own rules file", err)
	}
}
