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

// Current-task approval only, with a separately owned Kimi-only HTTP meter.
// The MCP data source is an ordinary local read-only fixture, not a supplier.
func TestToolSearchLiveKimi(t *testing.T) {
	if os.Getenv("YIJIE_TOOL_SEARCH_LIVE_APPROVED") != "12" {
		t.Skip("current-task Kimi authorization required")
	}
	root := os.Getenv("YIJIE_TOOL_SEARCH_LIVE_ROOT")
	if !filepath.IsAbs(root) {
		t.Fatal("fresh absolute evidence root required")
	}
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal("fresh evidence root required", err)
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
		t.Fatal("approved credential cannot be read")
	}
	binary, manifest := integrationArtifactPaths(t)
	cfg := integrationConfig(binary, manifest, home, "unused-local-minimax-profile")
	cfg.ChatModelsEnabled, cfg.KimiK31Candidate, cfg.RuntimePermissionsEnabled, cfg.NativeToolsEnabled, cfg.InteractionToolsEnabled, cfg.ToolSearchEnabled = true, true, true, true, true, true
	cfg.KimiAPIKey = strings.TrimSpace(string(key))
	cfg.PermissionVerificationBaseURL = "http://127.0.0.1:18083/v1"
	server, businessCalls := ordinarySearchMCP(t, true)
	manager, notifications := NewManager(cfg, nil), newIntegrationNotifications()
	if err = manager.SetNotificationHandler(notifications.handle); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
		defer cancel()
		if err := manager.Shutdown(ctx); err != nil {
			t.Error("normal shutdown pending; evidence retained", err)
		}
	})
	if err = manager.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	ctx, err := WithExpertInstructions(toolSearchContext(server.URL), "This isolated verification only uses tool_search, then native exec to call the discovered local read-only MCP tool. Do not use files, shell, web, agents, memories, image/video, or external services. Return one short sentence when finished.", "The only data is ordinary synthetic local fixture data. Tool discovery does not authorize additional operations.")
	if err != nil {
		t.Fatal(err)
	}
	thread, err := manager.StartThread(ctx, cwd)
	if err != nil {
		t.Fatal(err)
	}
	statuses := []string{}
	for _, prompt := range []string{
		"完成已批准的本地工具发现验收：先直接调用 tool_search，query 为 sample lookup value，limit 为 1。然后根据真实返回定义，用 exec 调用找到的本地只读工具，key=blue。不要用其它工具。得到结果后只用一句中文报告数值。",
		"只做无命中验收：直接调用 tool_search 一次，query 为 unmatchedzebraqxy，limit 为 1。若结果为空，简短说明没有匹配工具并结束；不要调用 exec 或任何业务工具，不要重复搜索。",
	} {
		input, _ := TextUserInput(prompt)
		turn, err := manager.StartTurnV2(ctx, thread.ID, []UserInput{input}, "max")
		if err != nil {
			t.Fatal(err)
		}
		status, _, err := notifications.waitForTurn(turn.ID, 3*time.Minute)
		if err != nil {
			_ = manager.InterruptTurn(context.Background(), thread.ID, turn.ID)
			t.Fatal("live verification interrupted normally", err)
		}
		statuses = append(statuses, status)
		if status != "completed" {
			t.Fatal("live turn ended", status)
		}
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
	file, err := os.Open(history.Path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 65536), 16<<20)
	functions := map[string]int{}
	for scanner.Scan() {
		var row struct {
			Type    string `json:"type"`
			Payload struct {
				Type string `json:"type"`
				Name string `json:"name"`
			} `json:"payload"`
		}
		if json.Unmarshal(scanner.Bytes(), &row) == nil && row.Type == "response_item" && row.Payload.Type == "function_call" {
			functions[row.Payload.Name]++
		}
	}
	if err = scanner.Err(); err != nil {
		t.Fatal(err)
	}
	if functions["tool_search"] != 2 || functions["exec"] < 1 || businessCalls.Load() != 1 {
		t.Error("native search/call result mismatch", functions, businessCalls.Load())
	}
	for name := range functions {
		if name != "tool_search" && name != "exec" && name != "wait" {
			t.Errorf("unrequested function observed: %s", name)
		}
	}
	receipt, _ := json.MarshalIndent(map[string]any{"feature": "FEAT-170", "model": "kimi-k3.1-pro", "statuses": statuses, "native_function_calls": functions, "local_read_only_mcp_calls": businessCalls.Load(), "external_business_calls": 0, "passed": !t.Failed(), "model_http": "owned twelve-request task meter"}, "", "  ")
	if err = os.WriteFile(filepath.Join(root, "receipt.json"), append(receipt, '\n'), 0600); err != nil {
		t.Error(err)
	}
}
