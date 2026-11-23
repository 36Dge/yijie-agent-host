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

	options "github.com/36Dge/yijie-agent-host/internal/contracts/runtimeapprovalsv4"
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
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		prompt := nativeToolsUserText(body)
		label := ""
		for _, candidate := range labels {
			if strings.Contains(prompt, "请输出示例文字 "+candidate) {
				label = candidate
				break
			}
		}
		if label == "" {
			t.Error("expected the ordinary test prompt")
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		result := nativeToolsOutput(body, "call_"+label)
		var item map[string]any
		if result == "" {
			args, _ := json.Marshal(map[string]any{"cmd": "echo approval-example " + label, "sandbox_permissions": "require_escalated", "justification": "允许输出这条示例文字吗？", "prefix_rule": []string{"echo", "approval-example"}})
			item = map[string]any{"type": "function_call", "name": "exec_command", "id": "item_" + label, "call_id": "call_" + label, "arguments": string(args)}
		} else {
			if label == "reject" {
				if !strings.Contains(strings.ToLower(result), "reject") {
					t.Errorf("command refusal was not returned: %q", result)
				}
			} else if !strings.Contains(result, "approval-example "+label) {
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
	artifact, err := VerifyArtifact(context.Background(), binary, manifest, config.StartupTimeout)
	if err != nil {
		t.Fatal(err)
	}
	config.InteractionToolsEnabled, config.NativeToolsEnabled = artifact.supportsInteractionTools(), artifact.supportsNativeTools()
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
	profile, _ := ModelProfile(KimiK31ProfileID)
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
		turn, err := manager.StartTurnV2(ctx, thread.ID, []UserInput{input}, profile.Effort)
		if err != nil {
			t.Fatal(err)
		}
		expectedTurn := "completed"
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
			desired := []options.NativeApprovalOptionKind{options.AllowOnce, options.Decline, options.CommandPrefix}[i]
			var choice, cancelChoice options.NativeApprovalOption
			for _, candidate := range pending.NativeChoices {
				if candidate.Kind == nil {
					t.Fatal("missing native choice kind")
				}
				if *candidate.Kind == desired {
					choice = candidate
				}
				if *candidate.Kind == options.Cancel {
					cancelChoice = candidate
				}
			}
			if choice.Id == "" && desired == options.Decline {
				choice = cancelChoice
			}
			if choice.Id == "" {
				t.Fatalf("%s not offered: %+v", desired, pending.NativeChoices)
			}
			if desired == options.CommandPrefix {
				if choice.Detail == nil || *choice.Detail != `["echo","approval-example"]` {
					t.Fatal("wrong native scope")
				}
				if file := os.Getenv("YIJIE_APPROVAL_MENU_EVIDENCE"); file != "" {
					snapshot := map[string]any{"id": pending.ID, "kind": pending.Kind, "summary": pending.Summary, "reason": pending.Reason, "scope": pending.Scope, "status": pending.Status, "nativeChoices": pending.NativeChoices}
					raw, err := json.MarshalIndent(snapshot, "", "  ")
					if err != nil {
						t.Fatal(err)
					}
					if err = os.WriteFile(file, raw, 0600); err != nil {
						t.Fatal(err)
					}
				}
			}
			receipt, err := manager.DecideRuntimeApproval(ctx, thread.ID, pending.ID, choice.Id)
			expectedStatus := "approved"
			if *choice.Kind == options.Decline {
				expectedStatus = "rejected"
			} else if *choice.Kind == options.Cancel {
				expectedStatus = "cancelled"
				expectedTurn = "interrupted"
			}
			t.Logf("native approval choice %s delivered for %s", *choice.Kind, label)
			if err != nil || receipt.Status != expectedStatus || len(receipt.CommandPrefix) != 0 {
				t.Fatalf("turn %s: unexpected receipt: %+v %v", label, receipt, err)
			}
		}
		status, _, err := notifications.waitForTurn(turn.ID, 15*time.Second)
		if err != nil || status != expectedTurn {
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
