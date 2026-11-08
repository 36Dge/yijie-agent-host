package codex

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Explicit manual Kimi acceptance only. The owned HTTP meter enforces the task
// budget across this root and every native child; no other provider is admitted.
func TestNativeToolsLiveKimi(t *testing.T) {
	if os.Getenv("YIJIE_NATIVE_TOOLS_LIVE_APPROVED") != "40" {
		t.Skip("current-task Kimi authorization and owned request meter required")
	}
	root := os.Getenv("YIJIE_NATIVE_TOOLS_LIVE_ROOT")
	if !filepath.IsAbs(root) {
		t.Fatal("absolute isolated evidence root required")
	}
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal("use a fresh isolated evidence root", err)
	}
	home, cwd := filepath.Join(root, "runtime"), filepath.Join(root, "workspace")
	for _, directory := range []string{home, cwd} {
		if err := os.Mkdir(directory, 0700); err != nil {
			t.Fatal(err)
		}
	}
	keyPath := os.Getenv("YIJIE_KIMI_API_KEY_FILE")
	info, err := os.Lstat(keyPath)
	if err != nil || !filepath.IsAbs(keyPath) || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
		t.Fatal("approved Kimi credential source unavailable")
	}
	key, err := os.ReadFile(keyPath)
	if err != nil {
		t.Fatal("approved Kimi credential cannot be read")
	}
	binary, manifest := integrationArtifactPaths(t)
	cfg := integrationConfig(binary, manifest, home, "unused-local-minimax-profile")
	cfg.ChatModelsEnabled, cfg.KimiK31Candidate, cfg.RuntimePermissionsEnabled, cfg.NativeToolsEnabled = true, true, true, true
	cfg.KimiAPIKey = strings.TrimSpace(string(key))
	cfg.PermissionVerificationBaseURL = "http://127.0.0.1:18083/v1"
	manager, notifications := NewManager(cfg, nil), newIntegrationNotifications()
	if err := manager.SetNotificationHandler(notifications.handle); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
		defer cancel()
		if err := manager.Shutdown(ctx); err != nil {
			t.Error("normal Runtime shutdown pending; preserve evidence", err)
		}
	})
	if err := manager.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	ctx := feat164AuxCtx()
	ctx, err = WithExpertInstructions(ctx, "Verify only the requested native tools in the ordinary temporary workspace. Use no web or external service.", "This isolated local verification has no external reference sources.")
	if err != nil {
		t.Fatal(err)
	}
	thread, err := manager.StartThread(ctx, cwd)
	if err != nil {
		t.Fatal(err)
	}
	const memoryName = "2026-11-07T15-34-44-native-live.md"
	prompt := `请完成这次已批准的本机工具验收，所有文件和笔记都是普通合成验收数据。只操作当前临时工作区；不调用网络、连接器、生图或外部业务工具。请实际调用以下原生工具，不要仅描述计划或用别的工具代替：
1. 用 apply_patch 新增 native-live.txt，文件内容为 FEAT169 native live file。
2. 用 memories__add_ad_hoc_note 保存文件名 2026-11-07T15-34-44-native-live.md，内容为 FEAT169 ordinary verification note is blue. 然后分别调用 memories__list、memories__read、memories__search，确认该笔记存在、能读回、能搜索到 blue。
3. 调用 exec，input 字符串为下面原生 JavaScript：
// @exec: {"yield_time_ms": 1}
await new Promise(resolve => setTimeout(resolve, 500)); text(6 * 7);
获得 Script running with cell ID 后，使用 wait 等待同一个 cell_id，确认得到 42。
4. 直接调用 collaboration__spawn_agent，task_name=calculator，fork_turns=none，message=请直接回答 6*7，不要使用任何工具。然后调用 collaboration__list_agents 和 collaboration__wait_agent 观察完成；向 calculator 用 collaboration__send_message 发送普通补充信息“下一次只需给数字”；再用 collaboration__followup_task 布置“请直接回答7*8，不要使用任何工具”，用 collaboration__wait_agent 等待完成，最后用 collaboration__interrupt_agent 结束其当前轮次（已空闲也按原生结果记录）。
所有四组完成后简短报告实际结果，不创建额外产物、不使用 V1 工具。函数工具按各自声明格式传参，exec/apply_patch 的输入是 JSON 中的 input 字符串。`
	input, _ := TextUserInput(prompt)
	turn, err := manager.StartTurnV2(ctx, thread.ID, []UserInput{input}, "max")
	if err != nil {
		t.Fatal(err)
	}
	status, _, err := notifications.waitForTurn(turn.ID, 8*time.Minute)
	if err != nil {
		_ = manager.InterruptTurn(context.Background(), thread.ID, turn.ID)
		t.Fatal("live native turn did not finish; normal interrupt requested", err)
	}
	if status != "completed" {
		t.Fatal("live native turn ended", status)
	}
	raw, err := manager.ReadThread(ctx, thread.ID)
	if err != nil {
		t.Fatal(err)
	}
	var identity struct {
		Path string `json:"path"`
	}
	if json.Unmarshal(raw, &identity) != nil || identity.Path == "" {
		t.Fatal("native history unavailable")
	}
	file, err := os.Open(identity.Path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	calls := map[string]int{}
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), 16*1024*1024)
	for scanner.Scan() {
		var entry struct {
			Type    string `json:"type"`
			Payload struct {
				Type string `json:"type"`
				Name string `json:"name"`
			} `json:"payload"`
		}
		if json.Unmarshal(scanner.Bytes(), &entry) == nil && entry.Type == "response_item" && entry.Payload.Type == "function_call" {
			calls[entry.Payload.Name]++
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"apply_patch", "exec", "wait", "collaboration__spawn_agent", "collaboration__list_agents", "collaboration__wait_agent", "collaboration__send_message", "collaboration__followup_task", "collaboration__interrupt_agent", "memories__add_ad_hoc_note", "memories__list", "memories__read", "memories__search"} {
		if calls[name] == 0 {
			t.Errorf("live native invocation not observed: %s", name)
		}
	}
	actual, err := os.ReadFile(filepath.Join(cwd, "native-live.txt"))
	if err != nil || strings.TrimSpace(string(actual)) != "FEAT169 native live file" {
		t.Error("live native patch result differs", err)
	}
	note, err := os.ReadFile(filepath.Join(home, "memories/extensions/ad_hoc/notes", memoryName))
	if err != nil || !strings.Contains(string(note), "FEAT169 ordinary verification note is blue.") {
		t.Error("live native memory result differs", err)
	}
	receipt, _ := json.MarshalIndent(map[string]any{"model": "kimi-k3.1-pro", "thread_id": thread.ID, "turn_id": turn.ID, "status": status, "native_function_calls": calls, "passed": !t.Failed(), "supplier_requests": "owned shared meter ledger"}, "", "  ")
	if err := os.WriteFile(filepath.Join(root, "receipt.json"), append(receipt, '\n'), 0600); err != nil {
		t.Error(err)
	}
	t.Log("real Kimi acceptance recorded; request budget belongs to the shared meter")
}
