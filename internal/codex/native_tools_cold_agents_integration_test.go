package codex

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestNativeToolsAuditColdSessionKeepsHistoryAndNewV2Approval(t *testing.T) {
	var reopened atomic.Bool
	var first, second, child atomic.Int64
	h := newNativeToolsHarness(t, func(h *nativeToolsHarness, body map[string]any, _ int64) map[string]any {
		if strings.Contains(nativeToolsUserText(body), "FEAT169_COLD_CHILD") {
			switch child.Add(1) {
			case 1:
				return feat164AuxText("First ordinary child result: 42.")
			case 2:
				file := filepath.Join(h.root, "cold-child-approved.txt")
				return feat164AuxTool("", "exec_command", "cold_write169", map[string]any{"cmd": "printf 'ordinary reopened child\n' > '" + strings.ReplaceAll(file, "'", "'\\''") + "'", "sandbox_permissions": "require_escalated", "justification": "Write the requested ordinary note after native approval."})
			default:
				return feat164AuxText("Reopened child completed the approved ordinary note.")
			}
		}
		if !reopened.Load() {
			switch first.Add(1) {
			case 1:
				return feat164AuxTool("", "collaboration__spawn_agent", "cold_spawn169", map[string]any{"task_name": "retained_child", "fork_turns": "none", "message": "FEAT169_COLD_CHILD: return 42 without tools."})
			case 2:
				return feat164AuxTool("", "collaboration__wait_agent", "cold_wait169", map[string]any{"timeout_ms": 10000})
			default:
				return feat164AuxText("First parent turn finished.")
			}
		}
		switch second.Add(1) {
		case 1:
			nativeToolsDeclarations(t, body)
			return feat164AuxTool("", "collaboration__list_agents", "cold_list169", map[string]any{})
		case 2:
			// Native list_agents lists live agents in this process, not historical
			// descendants. Preserve this behavior instead of rebuilding a tree.
			if strings.Contains(nativeToolsOutput(body, "cold_list169"), "retained_child") {
				t.Error("old child was unexpectedly restarted")
			}
			return feat164AuxTool("", "collaboration__spawn_agent", "cold_new169", map[string]any{"task_name": "continued_child", "fork_turns": "none", "message": "FEAT169_COLD_CHILD: write the requested ordinary note after native approval."})
		case 3:
			return feat164AuxTool("", "collaboration__wait_agent", "cold_rewait169", map[string]any{"timeout_ms": 10000})
		default:
			return feat164AuxText("New V2 child completed in the preserved conversation.")
		}
	})
	thread, err := h.manager.StartThread(feat164AuxCtx(), h.cwd)
	if err != nil {
		t.Fatal(err)
	}
	h.turn(t, KimiK31ProfileID, thread.ID, "Create a native child for a normal calculation.")
	h.restart(t, h.manager.config)
	reopened.Store(true)
	if _, err = h.manager.ResumeThread(feat164AuxCtx(), thread.ID); err != nil {
		t.Fatal(err)
	}
	history, err := h.manager.ReadThread(feat164AuxCtx(), thread.ID)
	if err != nil || !strings.Contains(string(history), "retained_child") || !strings.Contains(string(history), "First parent turn finished") {
		t.Fatal("old V2 conversation history not preserved", err)
	}
	decided := make(chan error, 1)
	go func() {
		deadline := time.Now().Add(25 * time.Second)
		for time.Now().Before(deadline) {
			for _, approval := range h.manager.ListRuntimeApprovals(thread.ID) {
				if approval.Status == "pending" {
					if !strings.HasPrefix(approval.Summary, "子任务：") {
						decided <- fmt.Errorf("cold child approval identity missing")
						return
					}
					_, err := h.manager.DecideRuntimeApproval(context.Background(), thread.ID, approval.ID, "approve_once")
					decided <- err
					return
				}
			}
			time.Sleep(10 * time.Millisecond)
		}
		decided <- fmt.Errorf("cold child approval not attached to original root")
	}()
	h.turn(t, KimiK31ProfileID, thread.ID, "Continue the old conversation using a new V2 child and write the ordinary external note after my approval.")
	if err := <-decided; err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(h.root, "cold-child-approved.txt"))
	if err != nil || string(data) != "ordinary reopened child\n" {
		t.Fatal("approved reopened child output missing", err)
	}
	if first.Load() != 3 || second.Load() != 4 || child.Load() != 3 {
		t.Fatal("cold native lifecycle differed", first.Load(), second.Load(), child.Load())
	}
}
