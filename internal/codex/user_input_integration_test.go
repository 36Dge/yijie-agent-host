package codex

import (
	"context"
	"encoding/json"
	"fmt"
	wire "github.com/36Dge/yijie-agent-host/internal/contracts/userinput"
	"github.com/google/uuid"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func interactionConfig(c *Config) { c.InteractionToolsEnabled = true }
func waitUserInput(t *testing.T, m *Manager, thread string) wire.UserInputRequest {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		s := m.UserInputSnapshot(thread)
		if len(s.Requests) > 0 {
			return s.Requests[0]
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("native question not observed")
	return wire.UserInputRequest{}
}
func TestUserInputRuntimeQuestionAndResume(t *testing.T) {
	var previous string
	h := newNativeToolsHarness(t, func(_ *nativeToolsHarness, body map[string]any, n int64) map[string]any {
		switch n {
		case 1, 3:
			tools, _ := json.Marshal(body["tools"])
			for _, name := range []string{"request_user_input", "get_context_remaining", "new_context", "collaboration__spawn_agent", "apply_patch", "exec", "wait", "memories__search"} {
				if !strings.Contains(string(tools), `"name":"`+name+`"`) {
					t.Errorf("tool %s not exposed", name)
				}
			}
			return feat164AuxTool("", "request_user_input", fmt.Sprintf("question%d", n), map[string]any{"questions": []any{map[string]any{"id": "choice", "header": "选择", "question": "选择普通样例", "options": []any{map[string]any{"label": "蓝色", "description": "普通选项"}, map[string]any{"label": "绿色", "description": "另一个选项"}}}, map[string]any{"id": "text", "header": "输入", "question": "输入普通合成说明", "isSecret": true}}})
		case 2, 4:
			output := nativeToolsOutput(body, fmt.Sprintf("question%d", n-1))
			if !strings.Contains(output, "synthetic-masked-note") || !strings.Contains(output, "蓝色") {
				t.Error("native answer missing")
			}
			return feat164AuxText("普通问答完成。")
		default:
			t.Error("unexpected local continuation")
			return feat164AuxText("结束。")
		}
	}, interactionConfig)
	thread, err := h.manager.StartThread(feat164AuxCtx(), h.cwd)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if i > 0 {
			h.restart(t, h.manager.config)
			if _, err = h.manager.ResumeThread(feat164AuxCtx(), thread.ID); err != nil {
				t.Fatal(err)
			}
			if _, err = h.manager.GetUserInput(thread.ID, previous); err == nil {
				t.Fatal("old pending request revived")
			}
		}
		input, _ := TextUserInput("请使用问题工具询问普通样例。")
		turn, err := h.manager.StartTurnV2(feat164AuxCtx(), thread.ID, []UserInput{input}, "max")
		if err != nil {
			t.Fatal(err)
		}
		request := waitUserInput(t, h.manager, thread.ID)
		previous = request.RequestId.String()
		if request.TurnId != turn.ID || len(request.Questions) != 2 || request.Questions[1].IsSecret == nil || !*request.Questions[1].IsSecret || request.Questions[1].Options != nil {
			t.Fatal("canonical question mismatch")
		}
		answer := wire.UserInputAnswerRequest{OperationId: uuid.New(), Answers: map[string]wire.ToolRequestUserInputAnswer{"choice": {Answers: []string{"蓝色"}}, "text": {Answers: []string{"synthetic-masked-note"}}}}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		receipt, err := h.manager.AnswerUserInput(ctx, thread.ID, previous, answer)
		cancel()
		if err != nil || receipt.State != "answered" {
			t.Fatal("answer delivery failed", err, receipt.State)
		}
		status, _, err := h.notifications.waitForTurn(turn.ID, 20*time.Second)
		if err != nil || status != "completed" {
			t.Fatal("turn did not continue", err, status)
		}
		if strings.Contains(h.manager.stderr.String(), "synthetic-masked-note") {
			t.Fatal("answer leaked into diagnostics")
		}
	}
}
func TestUserInputRuntimeNewContextAndHistory(t *testing.T) {
	const task = "ORDINARY_TASK_BEFORE_FRESH_CONTEXT"
	h := newNativeToolsHarness(t, func(_ *nativeToolsHarness, body map[string]any, n int64) map[string]any {
		switch n {
		case 1:
			return feat164AuxTool("", "get_context_remaining", "remaining", map[string]any{})
		case 2:
			if !strings.Contains(nativeToolsOutput(body, "remaining"), "tokens left in this context window") {
				t.Error("native remaining result missing")
			}
			return feat164AuxTool("", "new_context", "fresh", map[string]any{})
		case 3:
			raw, _ := json.Marshal(body["input"])
			if strings.Contains(string(raw), task) {
				t.Error("explicit fresh window kept old user task")
			}
			return feat164AuxText("新窗口已经开始。")
		default:
			t.Error("new_context made an unexpected summary request")
			return feat164AuxText("结束。")
		}
	}, interactionConfig)
	thread, err := h.manager.StartThread(feat164AuxCtx(), h.cwd)
	if err != nil {
		t.Fatal(err)
	}
	h.turn(t, KimiK31ProfileID, thread.ID, task)
	history, err := h.manager.ReadThread(context.Background(), thread.ID)
	if err != nil || !strings.Contains(string(history), task) || !strings.Contains(string(history), "contextCompaction") {
		t.Fatal("native history missing task or context event", err)
	}
	if h.calls.Load() != 3 {
		t.Fatal("unexpected provider requests", h.calls.Load())
	}
}

func TestUserInputRuntimeOldSessionUpgradesWithoutReplaying(t *testing.T) {
	newBinary, newManifest := integrationArtifactPaths(t)
	oldRoot := filepath.Join(filepath.Dir(filepath.Dir(filepath.Dir(filepath.Dir(newBinary)))), "native-tools-v1", "dfa05c556627e781a1cd1386c17f773a7a67012feeee7525268e8889ad2473a6", "aarch64-apple-darwin")
	if _, err := os.Stat(filepath.Join(oldRoot, "codex")); err != nil {
		t.Fatal("retained canonical native-tools baseline missing", err)
	}
	h := newNativeToolsHarness(t, func(_ *nativeToolsHarness, body map[string]any, n int64) map[string]any {
		switch n {
		case 1:
			return feat164AuxText("原四组版本的普通历史。")
		case 2:
			return feat164AuxTool("", "request_user_input", "old-session-question", map[string]any{"questions": []any{map[string]any{"id": "plain", "header": "回复", "question": "旧会话继续后，请输入普通说明。"}}})
		case 3:
			if !strings.Contains(nativeToolsOutput(body, "old-session-question"), "普通新回答") {
				t.Error("resumed answer missing")
			}
			return feat164AuxText("旧会话继续完成。")
		default:
			t.Error("old session was replayed")
			return feat164AuxText("结束。")
		}
	}, func(c *Config) {
		c.BinaryPath = filepath.Join(oldRoot, "codex")
		c.ManifestPath = filepath.Join(oldRoot, "runtime-manifest.json")
	})
	thread, err := h.manager.StartThread(feat164AuxCtx(), h.cwd)
	if err != nil {
		t.Fatal(err)
	}
	h.turn(t, KimiK31ProfileID, thread.ID, "原始会话内容。")
	cfg := h.manager.config
	cfg.BinaryPath = newBinary
	cfg.ManifestPath = newManifest
	cfg.InteractionToolsEnabled = true
	h.restart(t, cfg)
	if _, err = h.manager.ResumeThread(feat164AuxCtx(), thread.ID); err != nil {
		t.Fatal(err)
	}
	if h.calls.Load() != 1 {
		t.Fatal("resume replayed a model request")
	}
	input, _ := TextUserInput("继续提问。")
	turn, err := h.manager.StartTurnV2(feat164AuxCtx(), thread.ID, []UserInput{input}, "max")
	if err != nil {
		t.Fatal(err)
	}
	r := waitUserInput(t, h.manager, thread.ID)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	answer := wire.UserInputAnswerRequest{OperationId: uuid.New(), Answers: map[string]wire.ToolRequestUserInputAnswer{"plain": {Answers: []string{"普通新回答"}}}}
	if v, e := h.manager.AnswerUserInput(ctx, thread.ID, r.RequestId.String(), answer); e != nil || v.State != "answered" {
		t.Fatal(e, v.State)
	}
	if status, _, e := h.notifications.waitForTurn(turn.ID, 20*time.Second); e != nil || status != "completed" {
		t.Fatal(e, status)
	}
	history, e := h.manager.ReadThread(ctx, thread.ID)
	if e != nil || !strings.Contains(string(history), "原始会话内容。") {
		t.Fatal("original history changed", e)
	}
}

func TestUserInputRuntimeActualExpertSkillAndInputOnlyBoundaries(t *testing.T) {
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	skills := filepath.Clean(filepath.Join(cwd, "../../../yijie-skills/plugins/yijie-desktop-skills"))
	expertRoot := filepath.Join(skills, "experts/chuhaijiang-tiktok-shop-operations-expert/skills/tiktok-shop-operations")
	role, err := os.ReadFile(filepath.Join(expertRoot, "ROLE.md"))
	if err != nil {
		t.Fatal(err)
	}
	index, err := os.ReadFile(filepath.Join(expertRoot, "references/index.md"))
	if err != nil {
		t.Fatal(err)
	}
	skill, err := os.ReadFile(filepath.Join(skills, "skills/copywriting/SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	roleText, indexText, skillText := strings.TrimSpace(string(role)), strings.TrimSpace(string(index)), strings.TrimSpace(string(skill))
	h := newNativeToolsHarness(t, func(_ *nativeToolsHarness, body map[string]any, n int64) map[string]any {
		instructions, _ := body["instructions"].(string)
		input, _ := json.Marshal(body["input"])
		var messages []map[string]any
		_ = json.Unmarshal(input, &messages)
		allText := ""
		for _, m := range messages {
			content, _ := m["content"].([]any)
			for _, raw := range content {
				v, _ := raw.(map[string]any)
				text, _ := v["text"].(string)
				allText += text + "\n"
			}
		}
		if n <= 2 {
			if !strings.Contains(instructions, roleText) || !strings.Contains(instructions, indexText) {
				t.Error("actual expert role/index lost")
			}
		}
		switch n {
		case 1:
			if !strings.Contains(allText, skillText) {
				t.Error("actual explicit skill body not loaded")
			}
			return feat164AuxTool("", "new_context", "expert-new-window", map[string]any{})
		case 2:
			if strings.Contains(allText, skillText) || strings.Contains(allText, "ORDINARY_CURRENT_EXPERT_TASK") {
				t.Error("fresh window retained previous task/explicit skill body")
			}
			return feat164AuxText("专家配置保留，旧工具内容需重新读取。")
		case 3:
			tools, _ := body["tools"].([]any)
			if len(tools) != 0 {
				t.Error("input-only gained tools")
			}
			return feat164AuxText(`{"schema_version":1,"kind":"needs_clarification","missing_fields":["content"],"question":"需要定时执行什么？"}`)
		default:
			t.Error("unexpected continuation")
			return feat164AuxText("结束。")
		}
	}, interactionConfig)
	skillPath := filepath.Join(h.cwd, ".agents/skills/copywriting/SKILL.md")
	if err = os.MkdirAll(filepath.Dir(skillPath), 0700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(skillPath, skill, 0600); err != nil {
		t.Fatal(err)
	}
	note := filepath.Join(h.cwd, "ordinary-note.txt")
	if err = os.WriteFile(note, []byte("ordinary file remains"), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, err := WithExpertInstructions(feat164AuxCtx(), roleText, indexText)
	if err != nil {
		t.Fatal(err)
	}
	thread, err := h.manager.StartThread(ctx, h.cwd)
	if err != nil {
		t.Fatal(err)
	}
	text, _ := TextUserInput("ORDINARY_CURRENT_EXPERT_TASK")
	selected, err := SkillUserInput("copywriting", skillPath)
	if err != nil {
		t.Fatal(err)
	}
	turn, err := h.manager.StartTurnV2(ctx, thread.ID, []UserInput{text, selected}, "max")
	if err != nil {
		t.Fatal(err)
	}
	if status, _, e := h.notifications.waitForTurn(turn.ID, 20*time.Second); e != nil || status != "completed" {
		t.Fatal(e, status)
	}
	if b, e := os.ReadFile(note); e != nil || string(b) != "ordinary file remains" {
		t.Fatal("environment state changed")
	}
	draftCtx := WithScheduledDraft(WithModelProfile(context.Background(), KimiK31ProfileID), "ordinary-input-only-check")
	draft, e := h.manager.StartThread(draftCtx, h.cwd)
	if e != nil {
		t.Fatal(e)
	}
	input, _ := TextUserInput("准备定时草稿。")
	draftTurn, e := h.manager.StartTurnV2(draftCtx, draft.ID, []UserInput{input}, "max")
	if e != nil {
		t.Fatal(e)
	}
	if status, _, e := h.notifications.waitForTurn(draftTurn.ID, 20*time.Second); e != nil || status != "completed" {
		t.Fatal(e, status)
	}
}
