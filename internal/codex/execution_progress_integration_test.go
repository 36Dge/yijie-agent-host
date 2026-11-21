package codex

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// Real reviewed Runtime, normal loopback responses, no supplier request.
// Proves the existing shared native budget, rather than a Host counter.
func TestExecutionProgressNativeBudgetRuntime(t *testing.T) {
	binary, manifest := integrationArtifactPaths(t)
	root := integrationPrivateTempDir(t)
	var mu sync.Mutex
	requests := 0
	reminders := []string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if r.Method != "POST" || json.NewDecoder(r.Body).Decode(&body) != nil {
			t.Error("unexpected local request")
			return
		}
		var developer string
		for _, raw := range body["input"].([]any) {
			item, _ := raw.(map[string]any)
			if item["role"] != "developer" {
				continue
			}
			content, _ := item["content"].([]any)
			for _, rawPart := range content {
				part, _ := rawPart.(map[string]any)
				text, _ := part["text"].(string)
				developer += text + "\n"
			}
		}
		if body["instructions"] != executionBaseInstructions || strings.Count(developer, permissionInstructions) != 1 {
			t.Error("business policy missing or duplicated")
		}
		if !strings.Contains(developer, retrievalCapabilityInstructions) {
			t.Error("actual hosted retrieval capabilities missing")
		}
		mu.Lock()
		requests++
		n := requests
		reminders = append(reminders, developer)
		mu.Unlock()
		if n == 1 {
			tools, _ := json.Marshal(body["tools"])
			for _, name := range []string{"multi_agent_v1", "get_goal", "create_goal", "update_goal"} {
				if !strings.Contains(string(tools), name) {
					t.Errorf("ordinary chat lost native tool %s", name)
				}
			}
		}
		id := fmt.Sprintf("native_budget_%d", n)
		item := map[string]any{"id": "message_" + id, "type": "message", "role": "assistant", "status": "completed", "content": []any{map[string]any{"type": "output_text", "text": "普通本机结果：6 × 7 = 42。"}}}
		w.Header().Set("Content-Type", "text/event-stream")
		emit := func(v any) { b, _ := json.Marshal(v); fmt.Fprintf(w, "data: %s\n\n", b); w.(http.Flusher).Flush() }
		emit(map[string]any{"type": "response.created", "response": map[string]any{"id": id, "status": "in_progress", "output": []any{}}})
		emit(map[string]any{"type": "response.output_item.done", "output_index": 0, "item": item})
		emit(map[string]any{"type": "response.completed", "response": map[string]any{"id": id, "status": "completed", "output": []any{item}, "usage": map[string]any{"input_tokens": 10, "input_tokens_details": map[string]any{"cached_tokens": 8}, "output_tokens": 1, "total_tokens": 11}}})
	}))
	defer server.Close()
	cfg := integrationConfig(binary, manifest, root, "ordinary-loopback-placeholder")
	cfg.ChatModelsEnabled, cfg.KimiK31Candidate, cfg.RuntimePermissionsEnabled = true, true, true
	cfg.KimiAPIKey, cfg.PermissionVerificationBaseURL = "ordinary-loopback-placeholder", server.URL+"/v1"
	cfg.RolloutBudgetLimitTokens = 5
	m := NewManager(cfg, nil)
	notifications := newIntegrationNotifications()
	if err := m.SetNotificationHandler(notifications.handle); err != nil {
		t.Fatal(err)
	}
	if err := m.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer shutdownIntegrationManager(t, m, 25*time.Second)
	ctx := WithPermissionMode(WithModelProfile(context.Background(), KimiK31ProfileID), PermissionAsk)
	thread, err := m.StartThread(ctx, filepath.Dir(root))
	if err != nil {
		t.Fatal(err)
	}
	for i, want := range []string{"completed", "failed"} {
		input, _ := TextUserInput(fmt.Sprintf("普通预算验证 %d", i+1))
		turn, err := m.StartTurnV2(ctx, thread.ID, []UserInput{input}, "max")
		if err != nil {
			t.Fatal(err)
		}
		status, _, err := notifications.waitForTurn(turn.ID, 30*time.Second)
		if err != nil || status != want {
			t.Fatalf("turn %d status=%s want=%s error=%v", i+1, status, want, err)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if requests != 2 {
		t.Fatalf("requests=%d want=2", requests)
	}
	if !strings.Contains(reminders[0], "You have 5 weighted tokens left") || !strings.Contains(reminders[1], "You have 2 weighted tokens left") {
		t.Fatal("native shared budget reminders/weights missing")
	}
	t.Log("native rollout budget: cached input excluded, weighted usage shared across two user turns, native failure terminal, business instructions once; 0 supplier requests")
}

func TestDailyReportFinalAnswerAfterLongResearch(t *testing.T) {
	const document = "<!doctype html><title>城市比较</title><p>本地报告校验。</p>"
	// Replay the observed weighted usage with ordinary local responses. Fold
	// the research steps into one charge, then use the two recorded charges
	// for writing the report and checking it. No supplier traffic or source data.
	usage := func(input, cached, output int) map[string]any {
		return map[string]any{"input_tokens": input, "input_tokens_details": map[string]any{"cached_tokens": cached},
			"output_tokens": output, "total_tokens": input + output}
	}
	h := newNativeToolsProviderHarness(t, func(h *nativeToolsHarness, body map[string]any, n int64) (map[string]any, map[string]any) {
		switch n {
		case 1:
			return feat164AuxTool("", "exec", "research", map[string]any{"input": "text('资料已整理');"}), usage(175376, 0, 14527)
		case 2:
			return feat164AuxTool("", "apply_patch", "write", map[string]any{"input": "*** Begin Patch\n*** Add File: report.html\n+" + document + "\n*** End Patch\n"}), usage(84851, 82688, 29962)
		case 3:
			if got, err := os.ReadFile(filepath.Join(h.cwd, "report.html")); err != nil || strings.TrimSpace(string(got)) != document {
				t.Error("saved report did not match the requested document", err)
			}
			return feat164AuxTool("", "exec", "check", map[string]any{"input": "text(await tools.exec_command({cmd:'wc -c report.html'}));"}), usage(114921, 84736, 1199)
		case 4:
			if !strings.Contains(nativeToolsOutput(body, "check"), "report.html") {
				t.Error("final response did not receive the file check")
			}
		default:
			t.Errorf("unexpected provider request %d", n)
		}
		return feat164AuxText("报告已生成：[打开报告](report.html)。"), usage(20, 10, 10)
	}, nil, toolSearchConfig, dailySkillMarkets(t))
	h.manager.SetSkillSubmissionReady(true)
	ctx := WithPermissionMode(WithModelProfile(context.Background(), KimiK31ProfileID), PermissionAsk)
	thread, err := h.manager.StartThread(ctx, h.cwd)
	if err != nil {
		t.Fatal(err)
	}
	h.turn(t, KimiK31ProfileID, thread.ID, "请生成一份 HTML 报告。")
	if h.calls.Load() != 4 {
		t.Fatalf("report completed before verification and handoff, requests=%d", h.calls.Load())
	}
}

func TestDailySwitchModelAfterNativeBudgetStop(t *testing.T) {
	const next = "minimax-m3-high-v1"
	h := newNativeToolsHarness(t, func(_ *nativeToolsHarness, body map[string]any, n int64) map[string]any {
		if n == 3 && body["model"] != MiniMaxModel {
			t.Error("next turn used the previous model")
		}
		return feat164AuxText("本地校验已完成。")
	}, toolSearchConfig, dailySkillMarkets(t), func(c *Config) { c.RolloutBudgetLimitTokens = 3 })
	h.manager.SetSkillSubmissionReady(true)
	ctx := WithPermissionMode(WithModelProfile(context.Background(), KimiK31ProfileID), PermissionAsk)
	thread, err := h.manager.StartThread(ctx, h.cwd)
	if err != nil {
		t.Fatal(err)
	}
	h.turn(t, KimiK31ProfileID, thread.ID, "核对第一项。")
	input, _ := TextUserInput("核对第二项。")
	failed, err := h.manager.StartTurnV2(ctx, thread.ID, []UserInput{input}, "max")
	if err != nil {
		t.Fatal(err)
	}
	if status, _, err := h.notifications.waitForTurn(failed.ID, 30*time.Second); err != nil || status != "failed" {
		t.Fatalf("expected native budget stop, got %s (%v)", status, err)
	}
	switched, err := h.manager.SwitchThread(WithModelProfile(ctx, next), thread.ID)
	if err != nil || switched.ID != thread.ID || switched.ModelProvider != MiniMaxProviderID ||
		switched.Model != MiniMaxModel || switched.ReasoningEffort != "high" || h.calls.Load() != 2 {
		t.Fatalf("failed to switch the settled thread without sending a prompt: %v, %+v", err, switched)
	}
	h.turn(t, next, thread.ID, "核对下一项。")
	raw, err := h.manager.ReadThread(ctx, thread.ID)
	var history struct {
		Turns []struct {
			ID, Status string
			Error      *struct{ CodexErrorInfo string }
		}
	}
	if err != nil || json.Unmarshal(raw, &history) != nil || len(history.Turns) != 3 {
		t.Fatal("original thread history unavailable", err)
	}
	prior := history.Turns[1]
	if prior.ID != failed.ID || prior.Status != "failed" || prior.Error == nil || prior.Error.CodexErrorInfo != "sessionBudgetExceeded" ||
		history.Turns[2].Status != "completed" || h.calls.Load() != 3 {
		t.Fatal("failed history, follow-up, or request count changed")
	}
}

func TestExecutionProgressRepeatedPlanRuntime(t *testing.T) {
	binary, manifest := integrationArtifactPaths(t)
	root := integrationPrivateTempDir(t)
	var mu sync.Mutex
	counts := map[string]int{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if r.Method != "POST" || json.NewDecoder(r.Body).Decode(&body) != nil {
			t.Error("unexpected local request")
			return
		}
		input, _ := json.Marshal(body["input"])
		name := "answer"
		if strings.Contains(string(input), "case_no_answer") {
			name = "no_answer"
		}
		mu.Lock()
		counts[name]++
		n := counts[name]
		mu.Unlock()
		tools, _ := body["tools"].([]any)
		if n <= 3 && len(tools) == 0 {
			t.Error("tools removed before repeat threshold")
		}
		if n >= 4 {
			if len(tools) != 0 {
				t.Error("finalizer still exposes tools, including hosted search")
			}
			if !strings.Contains(string(input), "same plan was successfully updated repeatedly") {
				t.Error("repeat-plan finalization feedback missing")
			}
		}
		id := fmt.Sprintf("plan_loop_%s_%d", name, n)
		var output []any
		if n <= 3 {
			args, _ := json.Marshal(map[string]any{"explanation": fmt.Sprintf("ordinary explanation %d", n), "plan": []any{map[string]any{"step": "核对普通参考资料", "status": "in_progress"}, map[string]any{"step": "整理结论", "status": "pending"}}})
			output = []any{
				map[string]any{"id": "ws_" + id, "type": "web_search_call", "status": "completed", "action": map[string]any{"type": "search", "query": "ordinary reference", "sources": []any{map[string]any{"type": "url", "url": "https://example.org/ordinary-reference", "title": fmt.Sprintf("ordinary reference %d", n)}}}},
				map[string]any{"id": "fc_" + id, "type": "function_call", "name": "update_plan", "call_id": "call_" + id, "arguments": string(args)},
			}
		} else {
			text := "普通参考资料已核对：现有证据只支持有限结论，缺少其余资料。"
			if name == "no_answer" {
				text = "我会继续核实并整理结果。"
			}
			output = []any{map[string]any{"id": "msg_" + id, "type": "message", "role": "assistant", "status": "completed", "content": []any{map[string]any{"type": "output_text", "text": text}}}}
		}
		w.Header().Set("Content-Type", "text/event-stream")
		emit := func(v any) { b, _ := json.Marshal(v); fmt.Fprintf(w, "data: %s\n\n", b); w.(http.Flusher).Flush() }
		emit(map[string]any{"type": "response.created", "response": map[string]any{"id": id, "status": "in_progress", "output": []any{}}})
		for i, item := range output {
			emit(map[string]any{"type": "response.output_item.added", "output_index": i, "item": item})
			emit(map[string]any{"type": "response.output_item.done", "output_index": i, "item": item})
		}
		emit(map[string]any{"type": "response.completed", "response": map[string]any{"id": id, "status": "completed", "output": output, "usage": map[string]any{"input_tokens": 1, "output_tokens": 1, "total_tokens": 2}}})
	}))
	defer server.Close()
	cfg := integrationConfig(binary, manifest, root, "ordinary-loopback-placeholder")
	cfg.ChatModelsEnabled, cfg.KimiK31Candidate, cfg.RuntimePermissionsEnabled = true, true, true
	cfg.KimiAPIKey, cfg.PermissionVerificationBaseURL = "ordinary-loopback-placeholder", server.URL+"/v1"
	m := NewManager(cfg, nil)
	notifications := newIntegrationNotifications()
	if err := m.SetNotificationHandler(notifications.handle); err != nil {
		t.Fatal(err)
	}
	if err := m.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer shutdownIntegrationManager(t, m, 25*time.Second)
	ctx := WithPermissionMode(WithModelProfile(context.Background(), KimiK31ProfileID), PermissionAsk)
	for _, tc := range []struct {
		name, status string
		requests     int
	}{{"answer", "completed", 4}, {"no_answer", "failed", 5}} {
		thread, err := m.StartThread(ctx, filepath.Dir(root))
		if err != nil {
			t.Fatal(err)
		}
		input, _ := TextUserInput("case_" + tc.name + " 请给出普通参考资料的结论。")
		turn, err := m.StartTurnV2(ctx, thread.ID, []UserInput{input}, "max")
		if err != nil {
			t.Fatal(err)
		}
		status, _, err := notifications.waitForTurn(turn.ID, 35*time.Second)
		if err != nil || status != tc.status {
			t.Fatalf("%s status=%s want=%s error=%v", tc.name, status, tc.status, err)
		}
		mu.Lock()
		n := counts[tc.name]
		mu.Unlock()
		if n != tc.requests {
			t.Fatalf("%s requests=%d want=%d", tc.name, n, tc.requests)
		}
	}
	t.Log("same native plan with changing explanation/search IDs: 3 updates then existing bounded finalization; hosted search removed; completed in 4 or failed in 5; 0 supplier requests")
}
