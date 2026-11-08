package codex

import (
	"bufio"
	"context"
	"encoding/json"
	wire "github.com/36Dge/yijie-agent-host/internal/contracts/userinput"
	"github.com/google/uuid"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Manual, current-stage Kimi-only qualification. It never runs without the
// separate eight-request approval AND the owned, capped provider meter.
func TestUserInputLiveKimi(t *testing.T) {
	if os.Getenv("YIJIE_INTERACTION_TOOLS_LIVE_APPROVED") != "8" {
		t.Skip("current-stage authorization required")
	}
	root := os.Getenv("YIJIE_INTERACTION_TOOLS_LIVE_ROOT")
	if !filepath.IsAbs(root) {
		t.Fatal("fresh absolute evidence root required")
	}
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal("use a fresh evidence root")
	}
	home, cwd := filepath.Join(root, "runtime"), filepath.Join(root, "workspace")
	for _, p := range []string{home, cwd} {
		if err := os.Mkdir(p, 0700); err != nil {
			t.Fatal(err)
		}
	}
	keyPath := os.Getenv("YIJIE_KIMI_API_KEY_FILE")
	info, err := os.Lstat(keyPath)
	if err != nil || !filepath.IsAbs(keyPath) || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
		t.Fatal("approved Kimi credential unavailable")
	}
	key, err := os.ReadFile(keyPath)
	if err != nil {
		t.Fatal("approved Kimi credential cannot be read")
	}
	binary, manifest := integrationArtifactPaths(t)
	cfg := integrationConfig(binary, manifest, home, "unused-local-minimax-profile")
	cfg.ChatModelsEnabled, cfg.KimiK31Candidate, cfg.RuntimePermissionsEnabled, cfg.NativeToolsEnabled, cfg.InteractionToolsEnabled = true, true, true, true, true
	cfg.KimiAPIKey = strings.TrimSpace(string(key))
	cfg.PermissionVerificationBaseURL = "http://127.0.0.1:18083/v1"
	manager, notifications := NewManager(cfg, nil), newIntegrationNotifications()
	if err = manager.SetNotificationHandler(notifications.handle); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
		defer cancel()
		if err := manager.Shutdown(ctx); err != nil {
			t.Error("normal Runtime shutdown pending; evidence retained", err)
		}
	})
	if err = manager.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	ctx, err := WithExpertInstructions(feat164AuxCtx(), "This isolated test only verifies request_user_input, get_context_remaining and new_context. Use these native functions directly. Never use files, shell, Code Mode, memories, agents, web, connectors, or external business tools. If a fresh window has no active user request, end with a short plain final response instead of calling another tool.", "No external references are required.")
	if err != nil {
		t.Fatal(err)
	}
	thread, err := manager.StartThread(ctx, cwd)
	if err != nil {
		t.Fatal(err)
	}
	prompt := `进行本次已批准的普通合成验收。严格顺序：先直接调用 request_user_input，一次问两题：第一题 id=colour、header=颜色，选项蓝色/绿色，描述均用普通说明；第二题 id=note、header=说明，省略 options、isSecret=true，请用户输入普通合成说明，不设超时。收到回答后，直接调用 get_context_remaining 一次。最后直接调用 new_context 一次；新窗口内按固定角色指引简短结束。不要调用其它工具，不需要创建产物。`
	input, _ := TextUserInput(prompt)
	turn, err := manager.StartTurnV2(ctx, thread.ID, []UserInput{input}, "max")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if len(manager.UserInputSnapshot(thread.ID).Requests) > 0 {
			_ = manager.InterruptTurn(context.Background(), thread.ID, turn.ID)
		}
	})
	var pending wire.UserInputRequest
	deadline := time.Now().Add(2 * time.Minute)
	for time.Now().Before(deadline) {
		rows := manager.UserInputSnapshot(thread.ID).Requests
		if len(rows) > 0 {
			pending = rows[0]
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if pending.RequestId == uuid.Nil {
		_ = manager.InterruptTurn(context.Background(), thread.ID, turn.ID)
		t.Fatal("Kimi question not observed; interrupted normally")
	}
	if len(pending.Questions) != 2 {
		_ = manager.InterruptTurn(context.Background(), thread.ID, turn.ID)
		t.Fatal("question shape differs")
	}
	answers := map[string]wire.ToolRequestUserInputAnswer{}
	for _, q := range pending.Questions {
		value := "普通合成说明"
		if q.Options != nil && len(*q.Options) > 0 {
			value = (*q.Options)[0].Label
		}
		answers[q.Id] = wire.ToolRequestUserInputAnswer{Answers: []string{value}}
	}
	replyCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	receipt, err := manager.AnswerUserInput(replyCtx, thread.ID, pending.RequestId.String(), wire.UserInputAnswerRequest{OperationId: uuid.New(), Answers: answers})
	cancel()
	if err != nil || receipt.State != "answered" {
		_ = manager.InterruptTurn(context.Background(), thread.ID, turn.ID)
		t.Fatal("answer delivery unconfirmed", err, receipt.State)
	}
	status, _, err := notifications.waitForTurn(turn.ID, 3*time.Minute)
	if err != nil {
		_ = manager.InterruptTurn(context.Background(), thread.ID, turn.ID)
		t.Fatal("qualification interrupted normally", err)
	}
	raw, err := manager.ReadThread(ctx, thread.ID)
	if err != nil {
		t.Fatal(err)
	}
	var history struct {
		Path string `json:"path"`
	}
	if json.Unmarshal(raw, &history) != nil || history.Path == "" {
		t.Fatal("native history unavailable")
	}
	f, err := os.Open(history.Path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 65536), 16<<20)
	calls := map[string]int{}
	for scanner.Scan() {
		var e struct {
			Type    string `json:"type"`
			Payload struct {
				Type string `json:"type"`
				Name string `json:"name"`
			} `json:"payload"`
		}
		if json.Unmarshal(scanner.Bytes(), &e) == nil && e.Type == "response_item" && e.Payload.Type == "function_call" {
			calls[e.Payload.Name]++
		}
	}
	if err = scanner.Err(); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"request_user_input", "get_context_remaining", "new_context"} {
		if calls[name] != 1 {
			t.Errorf("native call %s count %d", name, calls[name])
		}
	}
	if len(calls) != 3 || status != "completed" || !strings.Contains(string(raw), "contextCompaction") {
		t.Error("qualification did not finish with exactly the requested native tools")
	}
	report, _ := json.MarshalIndent(map[string]any{"model": "kimi-k3.1-pro", "stage": "user-input-context", "status": status, "native_calls": calls, "passed": !t.Failed(), "thread_id": thread.ID, "turn_id": turn.ID, "answers_in_host_receipt": false, "supplier_requests": "owned current-stage meter ledger"}, "", "  ")
	if err = os.WriteFile(filepath.Join(root, "receipt.json"), append(report, '\n'), 0600); err != nil {
		t.Fatal(err)
	}
}
