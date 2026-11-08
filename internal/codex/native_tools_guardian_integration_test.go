package codex

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// The native reviewer is driven by an ordinary local structured response. The
// Runtime emits the real denied-action event and executes its real takeover API.
// This is not evidence of a real supplier's review decision.
func TestNativeToolsAuditNativeChildAutoReviewTakeover(t *testing.T) {
	var parent, child, reviews, resumedParent atomic.Int64
	var continued atomic.Bool
	h := newNativeToolsHarness(t, func(h *nativeToolsHarness, body map[string]any, _ int64) map[string]any {
		format, _ := body["text"].(map[string]any)
		definition, _ := format["format"].(map[string]any)
		schema, _ := definition["schema"].(map[string]any)
		properties, _ := schema["properties"].(map[string]any)
		if _, review := properties["outcome"]; review {
			tools, _ := body["tools"].([]any)
			for _, raw := range tools {
				tool, _ := raw.(map[string]any)
				name, _ := tool["name"].(string)
				if name == "multi_agent_v1" || strings.HasPrefix(name, "collaboration") || name == "spawn_agent" {
					t.Error("reviewer unexpectedly received multi-agent tools")
				}
			}
			if reviews.Add(1) == 1 {
				return feat164AuxText(`{"risk_level":"medium","user_authorization":"low","outcome":"deny","rationale":"The ordinary external note needs the user's explicit approval."}`)
			}
			return feat164AuxText(`{"risk_level":"medium","user_authorization":"high","outcome":"allow","rationale":"The user approved this exact ordinary note in the native child context."}`)
		}
		if strings.Contains(nativeToolsUserText(body), "FEAT169_GUARDIAN_CHILD") {
			n := child.Add(1)
			if n == 1 || n == 3 {
				if n == 3 {
					input, _ := json.Marshal(body["input"])
					if !strings.Contains(string(input), "Approved action:") {
						t.Error("native approval was not delivered to the original child")
					}
				}
				file := filepath.Join(h.root, "guardian-approved-child.txt")
				return feat164AuxTool("", "exec_command", fmt.Sprintf("guardian_cmd169_%d", n), map[string]any{"cmd": "printf 'ordinary reviewed child\n' > '" + strings.ReplaceAll(file, "'", "'\\''") + "'", "sandbox_permissions": "require_escalated", "justification": "Write the ordinary requested external note after approval."})
			}
			return feat164AuxText("The native decision is recorded; this task did not claim the denied write succeeded.")
		}
		if continued.Load() {
			input, _ := json.Marshal(body["input"])
			if strings.Contains(string(input), "Approved action:") {
				t.Error("child approval was incorrectly injected into root")
			}
			switch resumedParent.Add(1) {
			case 1:
				return feat164AuxTool("", "collaboration__followup_task", "guardian_retry169", map[string]any{"target": "/root/reviewed_child", "message": "FEAT169_GUARDIAN_CHILD: continue the exact ordinary note after the user's native approval."})
			case 2:
				return feat164AuxTool("", "collaboration__wait_agent", "guardian_rewait169", map[string]any{"timeout_ms": 10000})
			default:
				return feat164AuxText("The explicitly continued child action completed.")
			}
		}
		switch parent.Add(1) {
		case 1:
			return feat164AuxTool("", "collaboration__spawn_agent", "guardian_spawn169", map[string]any{"task_name": "reviewed_child", "fork_turns": "none", "message": "FEAT169_GUARDIAN_CHILD: prepare the ordinary external note, requiring explicit approval."})
		case 2:
			return feat164AuxTool("", "collaboration__wait_agent", "guardian_wait169", map[string]any{"timeout_ms": 10000})
		default:
			return feat164AuxText("The ordinary child review has finished; await human decision.")
		}
	})
	ctx := WithPermissionMode(WithModelProfile(context.Background(), KimiK31ProfileID), PermissionAuto)
	thread, err := h.manager.StartThread(ctx, h.cwd)
	if err != nil {
		t.Fatal(err)
	}
	input, _ := TextUserInput("Let a native child prepare an ordinary external note, and preserve the native review decision for my approval.")
	turn, err := h.manager.StartTurnV2(ctx, thread.ID, []UserInput{input}, "max")
	if err != nil {
		t.Fatal(err)
	}
	status, _, err := h.notifications.waitForTurn(turn.ID, 45*time.Second)
	if err != nil || status != "completed" {
		_ = h.manager.InterruptTurn(context.Background(), thread.ID, turn.ID)
		t.Fatal("native review flow did not finish", status, err)
	}
	if reviews.Load() != 1 {
		t.Fatal("native reviewer did not run exactly once", reviews.Load())
	}
	file := filepath.Join(h.root, "guardian-approved-child.txt")
	if _, err := os.Stat(file); !os.IsNotExist(err) {
		t.Fatal("denied native action wrote before user decision", err)
	}
	var approval RuntimeApproval
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		for _, entry := range h.manager.ListRuntimeApprovals(thread.ID) {
			if entry.Kind == "auto_review" && entry.Status == "pending" {
				approval = entry
				break
			}
		}
		if approval.ID != "" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if approval.ID == "" || !strings.HasPrefix(approval.Summary, "子任务：") {
		t.Fatal("native denied child action was not presented under its root")
	}
	decisionCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err = h.manager.DecideRuntimeApproval(decisionCtx, thread.ID, approval.ID, "approve_once"); err != nil {
		t.Fatal("native child takeover failed", err)
	}
	// Native takeover injects an exact-action approval without starting a new
	// turn. The Host must not automatically replay a denied action.
	if _, err := os.Stat(file); !os.IsNotExist(err) {
		t.Fatal("approval itself unexpectedly replayed a write", err)
	}
	continued.Store(true)
	retryInput, _ := TextUserInput("Continue the same child action after my explicit native approval.")
	retry, err := h.manager.StartTurnV2(ctx, thread.ID, []UserInput{retryInput}, "max")
	if err != nil {
		t.Fatal(err)
	}
	status, _, err = h.notifications.waitForTurn(retry.ID, 45*time.Second)
	if err != nil || status != "completed" {
		_ = h.manager.InterruptTurn(context.Background(), thread.ID, retry.ID)
		t.Fatal("explicit continuation did not complete", status, err)
	}
	data, err := os.ReadFile(file)
	if err != nil || string(data) != "ordinary reviewed child\n" {
		t.Fatal("original child did not execute the approved action on explicit continuation", err)
	}
}
