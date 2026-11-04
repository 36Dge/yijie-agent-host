package codex

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// Use the existing Runtime and ordinary read-only command tool. The loopback
// response models a selection decision; it does not assess supplier judgment.
func TestNativeSkillsExplicitImplicitAndProgressiveDisclosure(t *testing.T) {
	var mu sync.Mutex
	var requests []string
	var selectedPath string
	h := feat164AuxRuntime(t, func(body map[string]any, n int64) map[string]any {
		raw, _ := json.Marshal(body)
		mu.Lock()
		requests = append(requests, string(raw))
		mu.Unlock()
		if n == 1 {
			return feat164AuxTool("", "exec_command", "ordinary_skill_read", map[string]any{
				"cmd": "cat -- '" + selectedPath + "'", "max_output_tokens": 2000,
			})
		}
		return feat164AuxText("Ordinary multiplication explanation completed.")
	})
	ctx := feat164AuxCtx()
	root := filepath.Join(h.cwd, "ordinary-skills")
	for _, name := range []string{"multiply-note", "explicit-note", "unrelated-note"} {
		dir := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Join(dir, "agents"), 0700); err != nil {
			t.Fatal(err)
		}
		contents := "---\nname: " + name + "\ndescription: metadata-" + name + " explains ordinary arithmetic notes.\n---\nbody-" + name + "-start: Explain the supplied local note.\nbody-" + name + "-end.\n"
		if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(contents), 0600); err != nil {
			t.Fatal(err)
		}
		if name == "explicit-note" {
			if err := os.WriteFile(filepath.Join(dir, "agents/openai.yaml"), []byte("policy:\n  allow_implicit_invocation: false\n"), 0600); err != nil {
				t.Fatal(err)
			}
		}
	}
	selectedPath = filepath.Join(root, "multiply-note", "SKILL.md")
	if strings.Contains(selectedPath, "'") {
		t.Fatal("test path is not shell-quotable")
	}
	if err := h.manager.SetSkillsExtraRoots(ctx, []string{root}); err != nil {
		t.Fatal(err)
	}
	runTurn := func(thread ThreadInfo, inputs []UserInput) {
		t.Helper()
		turn, err := h.manager.StartTurnV2(ctx, thread.ID, inputs, "max")
		if err != nil {
			t.Fatal(err)
		}
		status, _, err := h.notifications.waitForTurn(turn.ID, 30*time.Second)
		if err != nil || status != "completed" {
			t.Fatal(status, err)
		}
	}
	run := func(inputs []UserInput) {
		t.Helper()
		thread, err := h.manager.StartThread(ctx, h.cwd)
		if err != nil {
			t.Fatal(err)
		}
		runTurn(thread, inputs)
	}
	text, _ := TextUserInput("请解释六乘七，使用合适的本地说明即可。")
	run([]UserInput{text}) // no name or structured selection
	for _, name := range []string{"multiply-note", "explicit-note"} {
		text, _ = TextUserInput("$" + name + "\n\n请解释六乘七。")
		skill, err := SkillUserInput(name, filepath.Join(root, name, "SKILL.md"))
		if err != nil {
			t.Fatal(err)
		}
		run([]UserInput{text, skill})
	}
	text, _ = TextUserInput("$multiply-note\n\n请解释六乘七。")
	run([]UserInput{text})
	// Resume replaces the current configuration/base, but native host Skill
	// metadata is initial context. Verify its existing compaction refresh rather
	// than inventing a Host catalogue injection into retained old history.
	model, err := h.manager.modelForContext(ctx)
	if err != nil {
		t.Fatal(err)
	}
	config := applyModelConfig(h.manager.permissionVerificationConfig(), model)
	config["skills.include_instructions"] = false
	var response threadResponse
	err = h.manager.request(ctx, RuntimeMethodThreadStart, map[string]any{
		"model": model.Model, "modelProvider": model.Provider, "cwd": h.cwd,
		"approvalPolicy": h.manager.sessionApprovalPolicy(), "sandbox": SessionSandbox,
		"baseInstructions":      "Skills are opt-in: use only explicit mentions.",
		"developerInstructions": h.manager.sessionDeveloperInstructions(),
		"config":                config, "ephemeral": false,
	}, &response)
	if err != nil {
		t.Fatal(err)
	}
	old, err := validateThreadResponse(response)
	if err != nil {
		t.Fatal(err)
	}
	h.manager.bindKimiK31Thread(ctx, old)
	text, _ = TextUserInput("请解释普通计算。")
	runTurn(old, []UserInput{text})
	restored, err := h.manager.SwitchThread(ctx, old.ID)
	if err != nil || restored.ID != old.ID {
		t.Fatal("same thread cold resume", err)
	}
	runTurn(restored, []UserInput{text})
	compactPromptThread(t, h, ctx, restored.ID, 8)
	runTurn(restored, []UserInput{text})
	mu.Lock()
	defer mu.Unlock()
	if len(requests) != 9 {
		t.Fatalf("request count=%d want 9", len(requests))
	}
	first := requests[0]
	for _, expected := range []string{"multiply-note", "metadata-multiply-note", "SKILL.md", "metadata-unrelated-note"} {
		if !strings.Contains(first, expected) {
			t.Fatalf("native metadata missing %s", expected)
		}
	}
	for _, expected := range []string{"OR the task clearly matches a skill's description", "How to use a skill (progressive disclosure)", "read its `SKILL.md` completely"} {
		if !strings.Contains(first, expected) {
			t.Fatalf("native Skill usage guidance missing: %s", expected)
		}
	}
	for _, excluded := range []string{"body-multiply-note", "body-explicit-note", "body-unrelated-note", "metadata-explicit-note", "Skills are opt-in"} {
		if strings.Contains(first, excluded) {
			t.Fatalf("initial metadata leaked or overrode native policy: %s", excluded)
		}
	}
	if !strings.Contains(requests[1], "body-multiply-note-start") || !strings.Contains(requests[1], "body-multiply-note-end") || strings.Contains(requests[1], "body-unrelated-note") {
		t.Fatal("native read-only tool did not load the selected complete Skill body on demand")
	}
	for index, name := range map[int]string{2: "multiply-note", 3: "explicit-note", 4: "multiply-note"} {
		if strings.Count(requests[index], "body-"+name+"-start") != 1 || !strings.Contains(requests[index], "body-"+name+"-end") || strings.Contains(requests[index], "body-unrelated-note") {
			t.Fatalf("explicit native loading/deduplication failed at request %d", index+1)
		}
	}
	if strings.Contains(requests[5], "metadata-multiply-note") || strings.Contains(requests[6], "Skills are opt-in") || strings.Contains(requests[6], "body-multiply-note") || !strings.Contains(requests[8], "metadata-multiply-note") || strings.Contains(requests[8], "body-multiply-note") {
		t.Fatal("old-thread current policy/native compaction metadata restoration differs")
	}
	t.Log("native metadata first, implicit read on demand, structured+text deduplication, plain-text mention, per-skill implicit opt-out; old-thread metadata refresh via native compaction; 9 loopback requests; 0 supplier requests")
}

func TestNativeSkillsStayIsolatedFromInputOnlyDraft(t *testing.T) {
	h := feat164AuxRuntime(t, func(body map[string]any, _ int64) map[string]any {
		raw, _ := json.Marshal(body["input"])
		// The pinned Runtime uses tool_choice=auto with an empty tool list.
		// Admission additionally verifies the live native input-only policy;
		// do not impose an older provider's tool_choice spelling here.
		if strings.Contains(string(raw), "metadata-draft-isolation") || strings.Contains(string(raw), "body-draft-isolation") || len(body["tools"].([]any)) != 0 {
			t.Errorf("input-only draft: metadata=%t body=%t tools=%d choice=%v", strings.Contains(string(raw), "metadata-draft-isolation"), strings.Contains(string(raw), "body-draft-isolation"), len(body["tools"].([]any)), body["tool_choice"])
		}
		return feat164AuxText(`{"schema_version":1,"kind":"needs_clarification","missing_fields":["schedule"],"question":"何时执行？"}`)
	})
	root := filepath.Join(h.cwd, "ordinary-skills", "draft-isolation")
	if err := os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "SKILL.md"), []byte("---\nname: draft-isolation\ndescription: metadata-draft-isolation\n---\nbody-draft-isolation\n"), 0600); err != nil {
		t.Fatal(err)
	}
	ctx := feat164AuxCtx()
	if err := h.manager.SetSkillsExtraRoots(ctx, []string{filepath.Dir(root)}); err != nil {
		t.Fatal(err)
	}
	ctx = WithScheduledDraft(ctx, h.cwd)
	thread, err := h.manager.StartThread(ctx, h.cwd)
	if err != nil {
		t.Fatal(err)
	}
	h.turn(t, ctx, thread.ID, "请拟定一个普通提醒草案。")
	thread, err = h.manager.ResumeThread(ctx, thread.ID)
	if err != nil {
		t.Fatal(err)
	}
	h.turn(t, ctx, thread.ID, "继续完善提醒草案。")
	if h.calls.Load() != 2 {
		t.Fatal("unexpected draft request count", h.calls.Load())
	}
	t.Log("native input-only start/resume keeps Skills and tools isolated;2 loopback requests;0 supplier requests")
}
