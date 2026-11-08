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

func nativeToolsUserText(body map[string]any) string {
	items, _ := body["input"].([]any)
	var result string
	for _, raw := range items {
		item, _ := raw.(map[string]any)
		if item["role"] != "user" {
			continue
		}
		if text, ok := item["content"].(string); ok {
			result = text
			continue
		}
		parts, _ := item["content"].([]any)
		var texts []string
		for _, part := range parts {
			value, _ := part.(map[string]any)
			if text, ok := value["text"].(string); ok {
				texts = append(texts, text)
			}
		}
		if len(texts) > 0 {
			result = strings.Join(texts, "\n")
		}
	}
	return result
}

func TestNativeToolsRuntimeV2(t *testing.T) {
	for _, profile := range []string{KimiK31ProfileID, "minimax-m3-high-v1"} {
		t.Run(profile, func(t *testing.T) {
			var parent, children atomic.Int64
			h := newNativeToolsHarness(t, func(_ *nativeToolsHarness, body map[string]any, _ int64) map[string]any {
				if strings.Contains(nativeToolsUserText(body), "FEAT169_CHILD") {
					children.Add(1)
					time.Sleep(25 * time.Millisecond)
					return feat164AuxText("子任务完成了普通计算，结果为 42。")
				}
				switch parent.Add(1) {
				case 1:
					nativeToolsDeclarations(t, body)
					return feat164AuxTool("", "collaboration__spawn_agent", "spawn169", map[string]any{"task_name": "native_child", "message": "FEAT169_CHILD_ONE: calculate 6 * 7 without tools.", "fork_turns": "none"})
				case 2:
					t.Log("native spawn receipt:", nativeToolsOutput(body, "spawn169"))
					return feat164AuxTool("", "collaboration__wait_agent", "wait_agent169a", map[string]any{"timeout_ms": 10000})
				case 3:
					return feat164AuxTool("", "collaboration__list_agents", "agents169", map[string]any{})
				case 4:
					t.Log("native agent list:", nativeToolsOutput(body, "agents169"))
					if !strings.Contains(nativeToolsOutput(body, "agents169"), "native_child") {
						t.Error("native V2 child not listed")
					}
					return feat164AuxTool("", "collaboration__send_message", "message169", map[string]any{"target": "/root/native_child", "message": "普通补充信息。"})
				case 5:
					return feat164AuxTool("", "collaboration__followup_task", "followup169", map[string]any{"target": "/root/native_child", "message": "FEAT169_CHILD_TWO: return a second ordinary answer without tools."})
				case 6:
					t.Log("native followup receipt:", nativeToolsOutput(body, "followup169"))
					return feat164AuxTool("", "collaboration__wait_agent", "wait_agent169b", map[string]any{"timeout_ms": 10000})
				case 7:
					return feat164AuxTool("", "collaboration__interrupt_agent", "interrupt169", map[string]any{"target": "/root/native_child"})
				case 8:
					if nativeToolsOutput(body, "interrupt169") == "" {
						t.Error("native interrupt receipt missing")
					}
					return feat164AuxText("六个 V2 工具已完成本地流程。")
				default:
					t.Error("unexpected parent continuation")
					return feat164AuxText("正常结束。")
				}
			})
			thread, err := h.manager.StartThread(WithPermissionMode(WithModelProfile(context.Background(), profile), PermissionAsk), h.cwd)
			if err != nil {
				t.Fatal(err)
			}
			h.turn(t, profile, thread.ID, "使用 V2 子代理完成普通计算、消息和后续任务。")
			if parent.Load() != 8 || children.Load() != 2 {
				t.Fatalf("parent=%d children=%d", parent.Load(), children.Load())
			}
			t.Log("six real V2 handlers, two child turns, normal EOF, no supplier calls")

		})
	}
}

func TestNativeToolsRuntimeV2Cancel(t *testing.T) {
	var parent atomic.Int64
	h := newNativeToolsHarness(t, func(h *nativeToolsHarness, body map[string]any, _ int64) map[string]any {
		switch parent.Add(1) {
		case 1:
			return feat164AuxTool("", "collaboration__spawn_agent", "cancel_spawn169", map[string]any{"task_name": "pending_child", "message": "FEAT169_WAIT_FOR_CANCEL: return an ordinary answer.", "fork_turns": "none"})
		case 2:
			select {
			case <-h.pending:
			case <-time.After(5 * time.Second):
				t.Error("child was not running")
			}
			return feat164AuxTool("", "collaboration__interrupt_agent", "cancel169", map[string]any{"target": "/root/pending_child"})
		case 3:
			if !strings.Contains(nativeToolsOutput(body, "cancel169"), "running") {
				t.Error("native interrupt did not observe a running child", nativeToolsOutput(body, "cancel169"))
			}
			return feat164AuxTool("", "collaboration__list_agents", "cancel_status169", map[string]any{})
		case 4:
			status := nativeToolsOutput(body, "cancel_status169")
			if !strings.Contains(status, "pending_child") || !strings.Contains(status, "interrupted") {
				t.Error("native child did not reach interrupted state", status)
			}
			select {
			case h.finishPending <- struct{}{}:
			default:
			}
			return feat164AuxText("运行中的子任务已按请求中断。")
		default:
			t.Error("unexpected cancellation continuation")
			return feat164AuxText("正常结束。")
		}
	})
	thread, err := h.manager.StartThread(feat164AuxCtx(), h.cwd)
	if err != nil {
		t.Fatal(err)
	}
	h.turn(t, KimiK31ProfileID, thread.ID, "开始一个普通子任务，再正常取消。")
	if parent.Load() != 4 || h.calls.Load() != 5 {
		t.Fatal("interrupted child continued inference", parent.Load(), h.calls.Load())
	}
	t.Log("native child reached interrupted; transport cancellation is not a server-side billing guarantee")
}

func TestNativeToolsRuntimeChildApproval(t *testing.T) {
	var parent, child atomic.Int64
	h := newNativeToolsHarness(t, func(h *nativeToolsHarness, body map[string]any, _ int64) map[string]any {
		if strings.Contains(nativeToolsUserText(body), "FEAT169_APPROVAL_CHILD") {
			if child.Add(1) == 1 {
				file := filepath.Join(h.root, "approved-child.txt")
				command := "printf 'ordinary child note\\n' > '" + strings.ReplaceAll(file, "'", "'\\''") + "'"
				return feat164AuxTool("", "exec_command", "child_exec169", map[string]any{"cmd": command, "sandbox_permissions": "require_escalated", "justification": "Write the ordinary integration note into the selected external folder."})
			}
			return feat164AuxText("已写入本次批准的普通文件。")
		}
		switch parent.Add(1) {
		case 1:
			return feat164AuxTool("", "collaboration__spawn_agent", "approved_spawn169", map[string]any{"task_name": "approved_child", "message": "FEAT169_APPROVAL_CHILD: write the ordinary integration note after native approval.", "fork_turns": "none"})
		case 2:
			return feat164AuxTool("", "collaboration__wait_agent", "approved_wait169", map[string]any{"timeout_ms": 10000})
		default:
			return feat164AuxText("子任务的原生审批流程已结束。")
		}
	})
	thread, err := h.manager.StartThread(feat164AuxCtx(), h.cwd)
	if err != nil {
		t.Fatal(err)
	}
	decided := make(chan error, 1)
	go func() {
		deadline := time.Now().Add(20 * time.Second)
		for time.Now().Before(deadline) {
			for _, approval := range h.manager.ListRuntimeApprovals(thread.ID) {
				if approval.Status == "pending" {
					if !strings.Contains(approval.Summary, "子任务") {
						decided <- fmt.Errorf("child ownership not visible")
						return
					}
					_, err := h.manager.DecideRuntimeApproval(context.Background(), thread.ID, approval.ID, "approve_once")
					decided <- err
					return
				}
			}
			time.Sleep(10 * time.Millisecond)
		}
		decided <- fmt.Errorf("root did not receive child approval")
	}()
	h.turn(t, KimiK31ProfileID, thread.ID, "让子代理在我批准后写入所选的普通外部文件。")
	if err := <-decided; err != nil {
		t.Fatal(err)
	}
	bytes, err := os.ReadFile(filepath.Join(h.root, "approved-child.txt"))
	if err != nil || string(bytes) != "ordinary child note\n" {
		t.Fatalf("approved native child result missing: %v", err)
	}
}
