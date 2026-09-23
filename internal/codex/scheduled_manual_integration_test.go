package codex

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/google/uuid"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// Real fixed Runtime and Host adapter; declared local text only. No upstream,
// native tools, external MCP, destructive fixtures or paid model requests.
func TestFEAT1554C1OrdinaryAskMeterStartResume(t *testing.T) {
	binary, manifest := integrationArtifactPaths(t)
	listener, err := net.Listen("tcp", "127.0.0.1:18083")
	if err != nil {
		t.Fatal("meter port is occupied; no process was stopped")
	}
	var requests atomic.Int64
	server := &http.Server{ReadHeaderTimeout: 5 * time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/v1/responses" {
			t.Error("unexpected route")
			w.WriteHeader(404)
			return
		}
		var body map[string]any
		if json.NewDecoder(r.Body).Decode(&body) != nil {
			t.Error("invalid request")
			return
		}
		if body["model"] != MiniMaxModel {
			t.Error("wrong model")
		}
		toolset, _ := body["tools"].([]any)
		for _, v := range toolset {
			tool, _ := v.(map[string]any)
			name, _ := tool["name"].(string)
			if strings.Contains(name, "mcp") || strings.Contains(name, "generate_image") || strings.Contains(name, "sorftime") {
				t.Error("external tool configured")
			}
		}
		ordinal := requests.Add(1)
		id := fmt.Sprintf("response_manual_%d", ordinal)
		item := map[string]any{"id": fmt.Sprintf("message_manual_%d", ordinal), "type": "message", "role": "assistant", "phase": "final_answer", "status": "completed", "content": []any{map[string]any{"type": "output_text", "text": "普通 Ask 合成验证完成", "annotations": []any{}}}}
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
	root, err := os.MkdirTemp("", "feat155-4c1-ask-")
	if err != nil {
		t.Fatal(err)
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	cwd := filepath.Join(root, "workspace")
	home := filepath.Join(root, "runtime")
	for _, p := range []string{cwd, home} {
		if err := os.Mkdir(p, 0700); err != nil {
			t.Fatal(err)
		}
	}
	config := integrationConfig(binary, manifest, home, "local-declarative-provider")
	config.RuntimePermissionsEnabled = true
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
				t.Errorf("normal shutdown: %v", err)
			}
		}
		if clean {
			_ = os.RemoveAll(root)
		}
	}()
	thread := ""
	for i := 0; i < 2; i++ {
		m := NewManager(config, nil)
		managers = append(managers, m)
		events := newIntegrationNotifications()
		if err := m.SetNotificationHandler(events.handle); err != nil {
			t.Fatal(err)
		}
		if err := m.Start(context.Background()); err != nil {
			t.Fatal(err)
		}
		// These are the original ordinary start/resume, not the draft-only path.
		var info ThreadInfo
		if i == 0 {
			info, err = m.StartThread(context.Background(), cwd)
		} else {
			info, err = m.ResumeThread(context.Background(), thread)
		}
		if err != nil {
			t.Fatal(err)
		}
		if i == 1 && info.ID != thread {
			t.Fatal("resume changed thread")
		}
		thread = info.ID
		overrides := m.permissionVerificationConfig()
		if overrides["model_providers.minimax.base_url"] != "http://127.0.0.1:18083/v1" || overrides["model_providers.minimax.request_max_retries"] != 0 || overrides["model_providers.minimax.stream_max_retries"] != 0 {
			t.Fatal("meter/retry scope differs")
		}
		input, _ := TextUserInput("仅回复合成验证完成，不使用工具")
		ctx := WithClientUserMessageID(WithPermissionMode(context.Background(), PermissionAsk), uuid.NewString())
		turn, err := m.StartTurnV2(ctx, thread, []UserInput{input}, "none")
		if err != nil {
			t.Fatal(err)
		}
		status, _, err := events.waitForTurn(turn.ID, 30*time.Second)
		if err != nil || status != "completed" {
			t.Fatalf("ordinary Ask result: %s %v", status, err)
		}
		// Read the actual effective thread policy without passing policy overrides.
		var observed threadResponse
		if err := m.request(context.Background(), RuntimeMethodThreadResume, map[string]any{"threadId": thread}, &observed); err != nil {
			t.Fatal(err)
		}
		var sandbox map[string]any
		_ = json.Unmarshal(observed.Sandbox, &sandbox)
		if observed.ApprovalPolicy != "on-request" || observed.ApprovalsReviewer != "user" || sandbox["type"] != "workspaceWrite" || sandbox["networkAccess"] != false {
			t.Fatalf("effective Ask policy differs: %s %s %s", observed.ApprovalPolicy, observed.ApprovalsReviewer, observed.Sandbox)
		}
		if len(m.ListRuntimeApprovals(thread)) != 0 {
			t.Fatal("plain text unexpectedly requested approval")
		}
		stop, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		err = m.Shutdown(stop)
		cancel()
		if err != nil {
			t.Fatal(err)
		}
	}
	if requests.Load() != 2 {
		t.Fatalf("meter request count: %d", requests.Load())
	}
	t.Log("fixed Runtime ordinary Ask start/turn/normal stop/resume passed; effective on-request/user/workspaceWrite/networkAccess=false; 2 local requests, 0 paid")
}
