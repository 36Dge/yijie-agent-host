package codex

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// These regressions use the existing normal loopback harness and unchanged
// Runtime. They measure actual context, not simulated supplier token savings.
type promptRequests struct {
	mu     sync.Mutex
	bodies []string
}

func (p *promptRequests) respond(body map[string]any, _ int64) map[string]any {
	raw, _ := json.Marshal(body)
	p.mu.Lock()
	p.bodies = append(p.bodies, string(raw))
	p.mu.Unlock()
	return feat164AuxText("Ordinary summary: the user supplied note PROMPT_USER_NOTE_42; no external operations occurred.")
}

func (p *promptRequests) last(t *testing.T, want int) string {
	t.Helper()
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.bodies) != want {
		t.Fatalf("request count=%d want=%d", len(p.bodies), want)
	}
	return p.bodies[len(p.bodies)-1]
}

func promptTurn(t *testing.T, h *feat164Auxiliary, ctx context.Context, thread ThreadInfo, effort, message string) {
	t.Helper()
	input, err := TextUserInput(message)
	if err != nil {
		t.Fatal(err)
	}
	turn, err := h.manager.StartTurnV2(ctx, thread.ID, []UserInput{input}, effort)
	if err != nil {
		t.Fatal(err)
	}
	status, _, err := h.notifications.waitForTurn(turn.ID, 30*time.Second)
	if err != nil || status != "completed" {
		t.Fatal(status, err)
	}
}

func compactPromptThread(t *testing.T, h *feat164Auxiliary, ctx context.Context, id string, wantRequests int64) {
	t.Helper()
	var response map[string]any
	if err := h.manager.request(ctx, "thread/compact/start", map[string]any{"threadId": id}, &response); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		raw, err := h.manager.ReadThread(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		var info struct {
			Turns []struct {
				Status string `json:"status"`
			} `json:"turns"`
		}
		if err = json.Unmarshal(raw, &info); err != nil {
			t.Fatal(err)
		}
		if len(info.Turns) > 0 && info.Turns[len(info.Turns)-1].Status == "completed" &&
			strings.Contains(string(raw), "contextCompaction") && h.calls.Load() == wantRequests {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("normal native compaction did not complete")
}

func TestPromptExpertSwitchHistoryAndNativeCompaction(t *testing.T) {
	p := &promptRequests{}
	h := feat164AuxRuntimeProfiles(t, p.respond, true)
	kimi := feat164AuxCtx()
	mini := WithPermissionMode(WithModelProfile(context.Background(), "minimax-m3-high-v1"), PermissionAsk)
	const role = "ordinary-expert-boundary-role"
	expert := func(ctx context.Context) context.Context {
		value, err := WithExpertInstructions(ctx, role, `[{"Title":"Ordinary note","Path":"/ordinary/reference.txt"}]`)
		if err != nil {
			t.Fatal(err)
		}
		return value
	}
	current := expert(kimi)
	thread, err := h.manager.StartThread(current, h.cwd)
	if err != nil {
		t.Fatal(err)
	}
	for i, step := range []struct {
		ctx    context.Context
		effort string
		copies int
	}{
		{current, "max", 1}, {current, "max", 1}, {expert(mini), "high", 2}, {current, "max", 3},
	} {
		if i > 0 {
			thread, err = h.manager.SwitchThread(step.ctx, thread.ID)
			if err != nil {
				t.Fatal(err)
			}
		}
		message := "Continue the ordinary explanation without adding new facts."
		if i == 0 {
			message = "Please explain the ordinary local note PROMPT_USER_NOTE_42 without tools."
		}
		promptTurn(t, h, step.ctx, thread, step.effort, message)
		body := p.last(t, i+1)
		if strings.Count(body, role) != step.copies || strings.Count(body, "[yijie-answer-completion-v1]") != 1 {
			t.Fatalf("step=%d expert copies=%d baseline copies=%d", i, strings.Count(body, role), strings.Count(body, "[yijie-answer-completion-v1]"))
		}
	}
	compactPromptThread(t, h, current, thread.ID, 5)
	promptTurn(t, h, current, thread, "max", "Recall the earlier note without adding new facts.")
	compacted := p.last(t, 6)
	if strings.Count(compacted, role) != 1 || strings.Count(compacted, "[yijie-answer-completion-v1]") != 1 || !strings.Contains(compacted, "PROMPT_USER_NOTE_42") {
		t.Fatal("native compaction did not retain the current role/baseline and ordinary note")
	}
	removed, err := WithExpertInstructions(kimi, "", "")
	if err != nil {
		t.Fatal(err)
	}
	thread, err = h.manager.SwitchThread(removed, thread.ID)
	if err != nil {
		t.Fatal(err)
	}
	promptTurn(t, h, removed, thread, "max", "Finish the ordinary explanation.")
	if strings.Contains(p.last(t, 7), role) {
		t.Fatal("current role cleared after native compaction but stale role remains")
	}
	t.Log("expert role counts=1/1/2/3; native compact then current role=1; clear role=0; shared baseline=1;7 loopback requests;0 supplier requests; no Host history rewriting")
}

func TestPromptLegacyResumeAndNativeCompaction(t *testing.T) {
	p := &promptRequests{}
	h := feat164AuxRuntime(t, p.respond)
	ctx := feat164AuxCtx()
	skillRoot := filepath.Join(h.root, "ordinary-skills")
	skillDir := filepath.Join(skillRoot, "ordinary-note")
	if err := os.MkdirAll(skillDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\nname: ordinary-note\ndescription: ordinary-legacy-catalog-marker\n---\nExplain a normal note.\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := h.manager.SetSkillsExtraRoots(ctx, []string{skillRoot}); err != nil {
		t.Fatal(err)
	}
	model, err := h.manager.modelForContext(ctx)
	if err != nil {
		t.Fatal(err)
	}
	config := applyModelConfig(h.manager.executionProgressConfig(ctx, h.manager.permissionVerificationConfig()), model)
	config["skills.include_instructions"] = true
	var response threadResponse
	if err = h.manager.request(ctx, RuntimeMethodThreadStart, map[string]any{
		"model": model.Model, "modelProvider": model.Provider, "cwd": h.cwd,
		"approvalPolicy": h.manager.sessionApprovalPolicy(), "sandbox": SessionSandbox,
		"baseInstructions": executionBaseInstructions, "developerInstructions": "ordinary-legacy-developer-marker",
		"config": config, "ephemeral": false,
	}, &response); err != nil {
		t.Fatal(err)
	}
	thread, err := validateThreadResponse(response)
	if err != nil {
		t.Fatal(err)
	}
	h.manager.bindKimiK31Thread(ctx, thread)
	promptTurn(t, h, ctx, thread, "max", "Remember this ordinary local note: PROMPT_USER_NOTE_42.")
	for i := 1; i <= 2; i++ {
		if i == 2 {
			thread, err = h.manager.SwitchThread(ctx, thread.ID)
			if err != nil {
				t.Fatal(err)
			}
			promptTurn(t, h, ctx, thread, "max", "Continue without adding new facts.")
		}
		body := p.last(t, i)
		if strings.Count(body, "ordinary-legacy-developer-marker") != 1 || !strings.Contains(body, "ordinary-legacy-catalog-marker") || strings.Contains(body, "[yijie-answer-completion-v1]") {
			t.Fatal("legacy resume history boundary changed", i)
		}
	}
	compactPromptThread(t, h, ctx, thread.ID, 3)
	promptTurn(t, h, ctx, thread, "max", "Recall the earlier note without adding new facts.")
	body := p.last(t, 4)
	if strings.Contains(body, "ordinary-legacy-developer-marker") || !strings.Contains(body, "ordinary-legacy-catalog-marker") ||
		strings.Count(body, "[yijie-answer-completion-v1]") != 1 || !strings.Contains(body, "PROMPT_USER_NOTE_42") {
		t.Fatal("native compaction did not replace legacy context with current instructions while retaining the note")
	}
	t.Log("resume keeps old developer history; native compaction installs current baseline and native metadata catalogue with retained ordinary note;4 loopback requests;0 supplier requests; no history edits")
}
