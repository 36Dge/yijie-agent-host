package codex

import (
	"context"
	"encoding/json"
	"sync/atomic"
	"testing"
	"time"
)

func TestFEAT175CurrentNativeChildWebOwnership(t *testing.T) {
	var parentCalls atomic.Int32
	var root string
	h := newNativeToolsHarness(t, func(h *nativeToolsHarness, _ map[string]any, _ int64) map[string]any {
		switch parentCalls.Add(1) {
		case 1:
			return feat164AuxTool("", "collaboration__spawn_agent", "web-child-spawn", map[string]any{"task_name": "web_guard_child", "message": "FEAT169_WAIT_FOR_CANCEL: wait for the ordinary product ownership check.", "fork_turns": "none"})
		case 2:
			select {
			case <-h.pending:
			case <-time.After(5 * time.Second):
				t.Error("native child did not start")
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			var response map[string]json.RawMessage
			if err := h.manager.request(ctx, "thread/read", map[string]any{"threadId": root, "includeTurns": true}, &response); err != nil {
				t.Error(err)
			}
			var thread map[string]json.RawMessage
			_ = json.Unmarshal(response["thread"], &thread)
			var turns []map[string]json.RawMessage
			_ = json.Unmarshal(thread["turns"], &turns)
			child := ""
			for _, turn := range turns {
				var items []map[string]json.RawMessage
				_ = json.Unmarshal(turn["items"], &items)
				for _, item := range items {
					var kind, activity, id string
					_ = json.Unmarshal(item["type"], &kind)
					_ = json.Unmarshal(item["kind"], &activity)
					_ = json.Unmarshal(item["agentThreadId"], &id)
					if kind == "subAgentActivity" && activity == "started" {
						child = id
					}
				}
			}
			if child == "" {
				t.Error("canonical native started Item unavailable")
			} else {
				var c map[string]json.RawMessage
				if err := h.manager.request(ctx, "thread/read", map[string]any{"threadId": child, "includeTurns": true}, &c); err != nil {
					t.Error(err)
				}
				var ct map[string]json.RawMessage
				_ = json.Unmarshal(c["thread"], &ct)
				var childTurns []map[string]json.RawMessage
				_ = json.Unmarshal(ct["turns"], &childTurns)
				active := ""
				for _, turn := range childTurns {
					var id, status string
					_ = json.Unmarshal(turn["id"], &id)
					_ = json.Unmarshal(turn["status"], &status)
					if status == "inProgress" {
						active = id
					}
				}
				owner, _, err := h.manager.BuiltinWebOwner(ctx, child, active)
				if err != nil || owner != root {
					t.Error("current native child web ownership", err)
				}
			}
			return feat164AuxTool("", "collaboration__interrupt_agent", "web-child-stop", map[string]any{"target": "/root/web_guard_child"})
		default:
			return feat164AuxText("Local native child ownership completed.")
		}
	}, feat175Config)
	ctx := WithPermissionMode(WithModelProfile(context.Background(), KimiK31ProfileID), PermissionAsk)
	thread, err := h.manager.StartThread(ctx, h.cwd)
	if err != nil {
		t.Fatal(err)
	}
	root = thread.ID
	h.turn(t, KimiK31ProfileID, thread.ID, "Run the local child ownership protocol example.")
}
