package codex

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"

	inputonly "github.com/36Dge/yijie-agent-host/internal/contracts/runtimeinputonly"
	wire "github.com/36Dge/yijie-agent-host/internal/contracts/scheduledraft"
	"github.com/google/uuid"
)

// Executes the actual pinned candidate through the original Manager/stdio
// transport. The text provider is an ordinary loopback HTTP fixture, never a
// substituted executable or evidence of real MiniMax outputSchema support.
func TestFEAT155DraftNativeQualificationAndNormalReopen(t *testing.T) {
	binary, manifest := integrationArtifactPaths(t)
	root, err := os.MkdirTemp("", "feat155-native-qualification-")
	if err != nil {
		t.Fatal(err)
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	home, cwd := filepath.Join(root, "runtime"), filepath.Join(root, "draft")
	for _, dir := range []string{home, cwd} {
		if err := os.Mkdir(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	var managers []*Manager
	defer func() {
		allStopped := true
		for _, manager := range managers {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			err := manager.Shutdown(ctx)
			cancel()
			if err != nil {
				allStopped = false
				t.Errorf("normal shutdown pending: %v", err)
			}
		}
		if allStopped {
			_ = os.RemoveAll(root)
		}
	}()

	var mu sync.Mutex
	requests, unexpected := 0, 0
	var providerErrors []string
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if r.URL.Path != "/v1/responses" {
			unexpected++
			w.WriteHeader(http.StatusNotFound)
			return
		}
		requests++
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			providerErrors = append(providerErrors, "request JSON")
			return
		}
		tools, _ := body["tools"].([]any)
		if len(tools) != 0 || body["tool_choice"] != "none" {
			providerErrors = append(providerErrors, "tool surface")
		}
		var schema any
		_ = json.Unmarshal(wire.OutputSchema(), &schema)
		text, _ := body["text"].(map[string]any)
		format, _ := text["format"].(map[string]any)
		if !reflect.DeepEqual(format["schema"], schema) {
			providerErrors = append(providerErrors, "output schema")
		}
		output := `{"schema_version":1,"kind":"needs_clarification","missing_fields":["content"],"question":"需要定时执行什么任务？"}`
		id := fmt.Sprintf("response_local_%d", requests)
		item := map[string]any{"id": fmt.Sprintf("message_local_%d", requests), "type": "message", "role": "assistant", "phase": "final_answer", "status": "completed", "content": []any{map[string]any{"type": "output_text", "text": output, "annotations": []any{}}}}
		w.Header().Set("Content-Type", "text/event-stream")
		emit := func(value any) {
			data, _ := json.Marshal(value)
			_, _ = fmt.Fprintf(w, "data: %s\n\n", data)
			w.(http.Flusher).Flush()
		}
		emit(map[string]any{"type": "response.created", "response": map[string]any{"id": id, "status": "in_progress", "output": []any{}}})
		emit(map[string]any{"type": "response.output_item.added", "output_index": 0, "item": item})
		emit(map[string]any{"type": "response.output_item.done", "output_index": 0, "item": item})
		emit(map[string]any{"type": "response.completed", "response": map[string]any{"id": id, "status": "completed", "output": []any{item}, "usage": map[string]any{"input_tokens": 1, "output_tokens": 1, "total_tokens": 2}}})
	}))
	defer provider.Close()
	config := integrationConfig(binary, manifest, home, "local-only-test-token")
	config.RuntimePermissionsEnabled = true
	config.RequestTimeout = 30 * time.Second
	newManager := func() (*Manager, *integrationNotifications) {
		manager := NewManager(config, nil)
		managers = append(managers, manager)
		notifications := newIntegrationNotifications()
		if err := manager.SetNotificationHandler(notifications.handle); err != nil {
			t.Fatal(err)
		}
		if err := manager.Start(context.Background()); err != nil {
			t.Fatal(err)
		}
		if !manager.ScheduledDraftReady() {
			t.Fatal("actual candidate did not qualify")
		}
		return manager, notifications
	}
	manager, notifications := newManager()
	ctx := WithScheduledDraft(context.Background(), uuid.NewString())
	// The unchanged product creation adapter works without a Provider request.
	productThread, err := manager.StartThread(ctx, cwd)
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.verifyDraftPolicy(ctx, productThread.ID, cwd, manager.runtimeGeneration); err != nil {
		t.Fatal(err)
	}

	// Use a local text fixture only for native turn/follow-up and persisted resume.
	// It changes the declared Provider endpoint, not the execution restriction.
	params, err := scheduledDraftStartParams(cwd, "")
	if err != nil {
		t.Fatal(err)
	}
	overrides := params["config"].(map[string]any)
	overrides["model_providers.minimax.base_url"] = provider.URL + "/v1"
	overrides["model_providers.minimax.request_max_retries"] = 0
	overrides["model_providers.minimax.stream_max_retries"] = 0
	overrides["mcp_servers.ordinary_fixture"] = map[string]any{"url": provider.URL + "/mcp", "enabled": true}
	var response threadResponse
	if err := manager.request(ctx, RuntimeMethodThreadStart, params, &response); err != nil {
		t.Fatal(err)
	}
	thread, err := manager.acceptDraftThreadResponse(ctx, cwd, "", manager.runtimeGeneration, response)
	if err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{"请提出一个任务内容澄清问题。", "继续澄清，不保存计划。"} {
		input, err := TextUserInput(text)
		if err != nil {
			t.Fatal(err)
		}
		turn, err := manager.StartTurnV2(WithClientUserMessageID(ctx, uuid.NewString()), thread.ID, []UserInput{input}, "none")
		if err != nil {
			t.Fatal(err)
		}
		status, _, err := notifications.waitForTurn(turn.ID, 30*time.Second)
		if err != nil || status != "completed" {
			t.Fatalf("local turn: %s %v", status, err)
		}
	}
	mu.Lock()
	if requests != 2 || unexpected != 0 || len(providerErrors) != 0 {
		t.Errorf("local requests=%d unexpected=%d problems=%v", requests, unexpected, providerErrors)
	}
	mu.Unlock()

	stopCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	err = manager.Shutdown(stopCtx)
	cancel()
	if err != nil {
		t.Fatal(err)
	}
	if manager.ScheduledDraftReady() {
		t.Fatal("closed generation retained qualification")
	}
	reopened, _ := newManager()
	if _, err := reopened.ResumeThread(ctx, thread.ID); err != nil {
		t.Fatal(err)
	}
	if err := reopened.verifyDraftPolicy(ctx, thread.ID, cwd, reopened.runtimeGeneration); err != nil {
		t.Fatal(err)
	}
	// Ordinary sessions in the same process retain their ordinary native policy.
	ordinary, err := reopened.StartThread(context.Background(), cwd)
	if err != nil {
		t.Fatal(err)
	}
	var raw json.RawMessage
	if err := reopened.request(context.Background(), inputonly.Method, inputonly.ThreadInputOnlyPolicyReadParams{ThreadId: ordinary.ID}, &raw); err != nil {
		t.Fatal(err)
	}
	ordinaryPolicy, err := inputonly.DecodePolicy(raw)
	if err != nil || ordinaryPolicy.Policy != nil {
		t.Fatal("ordinary thread acquired a restricted-purpose receipt")
	}
	t.Log("actual Runtime: original Host start, no tools in 2 local-only text requests, MCP requests 0, follow-up, normal shutdown/reopen/resume and ordinary coexistence PASS; real Provider NOT RUN")
}
