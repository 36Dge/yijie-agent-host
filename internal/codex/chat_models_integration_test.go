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

	draftwire "github.com/36Dge/yijie-agent-host/internal/contracts/scheduledraft"
	"github.com/google/uuid"
)

// Normal fixed Runtime lifecycle. Default: four benign local Responses. Paid
// mode requires the separately running FEAT-156 capped meter and explicit flag.
func TestFEAT156NativeModelProfiles(t *testing.T) {
	binary, manifest := integrationArtifactPaths(t)
	paid := os.Getenv("YIJIE_FEAT156_PAID_VERIFY") == "true"
	mini, kimi := "local-minimax-fixture", "local-kimi-fixture"
	var requests atomic.Int64
	if paid {
		var err error
		mini, err = integrationMiniMaxKey()
		if err != nil {
			t.Fatal("MiniMax owner-only credential unavailable")
		}
		b, err := os.ReadFile(os.Getenv("YIJIE_KIMI_API_KEY_FILE"))
		if err != nil {
			t.Fatal("Kimi owner-only credential unavailable")
		}
		kimi = strings.TrimSpace(string(b))
		if mini == "" || kimi == "" {
			t.Fatal("Missing approved provider credential")
		}
	} else {
		listener, err := net.Listen("tcp", "127.0.0.1:18083")
		if err != nil {
			t.Fatal("Local verification port occupied; no process stopped")
		}
		server := &http.Server{ReadHeaderTimeout: 5 * time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var body map[string]any
			if r.Method != "POST" || r.URL.Path != "/v1/responses" || json.NewDecoder(r.Body).Decode(&body) != nil {
				t.Error("Unexpected local request")
				w.WriteHeader(400)
				return
			}
			n := requests.Add(1)
			if n > 4 {
				t.Error("Unexpected extra request")
				w.WriteHeader(429)
				return
			}
			expectedModel, expectedEffort := "kimi-k3", "max"
			if n == 2 {
				expectedModel, expectedEffort = "MiniMax-M3", "high"
			}
			reason, _ := body["reasoning"].(map[string]any)
			if body["model"] != expectedModel || reason["effort"] != expectedEffort {
				t.Error("Model/effort profile mismatch")
			}
			answer := "记号：青山"
			if n == 4 {
				answer = `{"schema_version":1,"kind":"needs_clarification","missing_fields":["time"],"question":"何时执行？"}`
				tools, _ := body["tools"].([]any)
				if len(tools) != 0 || body["tool_choice"] != "auto" {
					t.Error("Draft lost input-only policy")
				}
			}
			id := fmt.Sprintf("feat156_%d", n)
			item := map[string]any{"id": "message_" + id, "type": "message", "role": "assistant", "phase": "final_answer", "status": "completed", "content": []any{map[string]any{"type": "output_text", "text": answer, "annotations": []any{}}}}
			w.Header().Set("Content-Type", "text/event-stream")
			for _, event := range []any{map[string]any{"type": "response.created", "response": map[string]any{"id": id, "status": "in_progress", "output": []any{}}}, map[string]any{"type": "response.output_item.added", "output_index": 0, "item": item}, map[string]any{"type": "response.output_item.done", "output_index": 0, "item": item}, map[string]any{"type": "response.completed", "response": map[string]any{"id": id, "status": "completed", "output": []any{item}, "usage": map[string]any{"input_tokens": 1, "output_tokens": 1, "total_tokens": 2}}}} {
				b, _ := json.Marshal(event)
				fmt.Fprintf(w, "data: %s\n\n", b)
				w.(http.Flusher).Flush()
			}
		})}
		go func() { _ = server.Serve(listener) }()
		defer func() {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if server.Shutdown(ctx) != nil {
				t.Error("Normal local server shutdown pending")
			}
		}()
	}
	root, err := os.MkdirTemp("", "feat156-host-model-")
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
			t.Fatal("Cannot prepare local directory")
		}
	}
	config := integrationConfig(binary, manifest, home, mini)
	config.ChatModelsEnabled = true
	config.KimiAPIKey = kimi
	config.RuntimePermissionsEnabled = true
	config.PermissionVerificationBaseURL = "http://127.0.0.1:18083/v1"
	m := NewManager(config, nil)
	notifications := newIntegrationNotifications()
	var responseMu sync.Mutex
	responses := make(map[string]string)
	if m.SetNotificationHandler(func(method string, raw json.RawMessage) {
		if method == "item/completed" {
			var event struct {
				TurnID string `json:"turnId"`
				Item   struct {
					Type string `json:"type"`
					Text string `json:"text"`
				} `json:"item"`
			}
			if json.Unmarshal(raw, &event) == nil && event.Item.Type == "agentMessage" {
				responseMu.Lock()
				responses[event.TurnID] += event.Item.Text
				responseMu.Unlock()
			}
		}
		notifications.handle(method, raw)
	}) != nil {
		t.Fatal("Notification setup failed")
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
		defer cancel()
		if m.Shutdown(ctx) != nil {
			t.Error("Normal runtime shutdown pending; files preserved")
		} else {
			_ = os.RemoveAll(root)
		}
	}()
	if err = m.Start(context.Background()); err != nil {
		t.Fatalf("Managed runtime startup: %v", err)
	}
	var thread string
	for i, id := range []string{"kimi-k3-max-v1", "minimax-m3-high-v1", "kimi-k3-max-v1", "kimi-k3-max-v1"} {
		if paid && os.Getenv("YIJIE_FEAT156_DRAFT_ONLY") == "true" && i != 3 {
			continue
		}
		ctx := WithModelProfile(context.Background(), id)
		profile, _ := ModelProfile(id)
		if i == 3 {
			ctx = WithScheduledDraft(ctx, uuid.NewString())
		}
		var info ThreadInfo
		if i == 0 || i == 3 {
			info, err = m.StartThread(ctx, cwd)
		} else {
			info, err = m.SwitchThread(ctx, thread)
		}
		if err != nil {
			t.Fatalf("Normal profile lifecycle step %d failed: %v", i, err)
		}
		if i > 0 && i < 3 && info.ID != thread {
			t.Fatal("Model switch changed native thread")
		}
		thread = info.ID
		if info.Model != profile.Model || info.ModelProvider != profile.Provider || info.ReasoningEffort != profile.Effort {
			t.Fatal("Native effective profile differs")
		}
		prompt := "只回复一句：请记住本对话的测试记号是青山。不调用任何工具。"
		if i == 1 || i == 2 {
			prompt = "只回复此前约定的测试记号，不调用任何工具。"
		}
		if i == 3 {
			prompt = "帮我创建一个每天汇总信息的定时任务，时间还没决定。"
		}
		input, _ := TextUserInput(prompt)
		turnCtx := WithClientUserMessageID(ctx, uuid.NewString())
		if i != 3 {
			turnCtx = WithPermissionMode(turnCtx, PermissionAsk)
		}
		turn, err := m.StartTurnV2(turnCtx, thread, []UserInput{input}, profile.Effort)
		if err != nil {
			t.Fatalf("Model turn %d start failed", i)
		}
		status, _, err := notifications.waitForTurn(turn.ID, 4*time.Minute)
		if err != nil || status != "completed" {
			t.Fatalf("Model turn %d status=%s; no automatic replay", i, status)
		}
		responseMu.Lock()
		answer := responses[turn.ID]
		responseMu.Unlock()
		if i == 3 {
			var output map[string]any
			if draftwire.Decode("Output", []byte(answer), &output) != nil || output["kind"] != "needs_clarification" {
				t.Fatal("Draft did not return valid canonical clarification; response content omitted")
			}
		} else if !strings.Contains(answer, "青山") {
			t.Fatal("Model did not retain the synthetic conversation marker; response content omitted")
		}
		t.Logf("step=%d provider=%s model=%s effort=%s completed same_thread=%t", i, profile.Provider, profile.Model, profile.Effort, i > 0 && i < 3)
	}
	if !paid && requests.Load() != 4 {
		t.Fatal("Unexpected local request count")
	}
}
