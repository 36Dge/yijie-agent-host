package codex

import (
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"
	"time"
)

// Ordinary in-process wire fixtures; no executable or native review outcome is
// replaced. This verifies the Host's ancestry and callback projection only.
func TestNativeToolsAuditChildGuardianKeepsOriginalThread(t *testing.T) {
	outReader, outWriter := io.Pipe()
	inReader, inWriter := io.Pipe()
	client := NewClient(outWriter, inReader, 1<<20, 8, nil, nil)
	client.Start()
	t.Cleanup(func() { _ = client.CloseInput(); _ = inWriter.Close(); _ = outReader.Close() })
	cfg := DefaultConfig()
	cfg.NativeToolsEnabled, cfg.RuntimePermissionsEnabled = true, true
	m := NewManager(cfg, nil)
	m.client, m.status.Ready, m.status.State = client, true, StateReady
	m.registerNativeToolsRoot("ordinary-root")
	m.registerNativeToolsRoot("other-root")
	wire := make(chan map[string]any, 4)
	go func() {
		decoder := json.NewDecoder(outReader)
		for {
			var request map[string]any
			if decoder.Decode(&request) != nil {
				return
			}
			wire <- request
			result := map[string]any{}
			if request["method"] == "thread/read" {
				result["thread"] = map[string]any{"id": "ordinary-child", "parentThreadId": "ordinary-root", "turns": []any{map[string]any{"id": "finished-turn", "status": "completed"}}}
			}
			if json.NewEncoder(inWriter).Encode(map[string]any{"id": request["id"], "result": result}) != nil {
				return
			}
		}
	}()
	// No thread/started notification was observed for the cold child. The
	// canonical thread/read ancestry must still connect its denied action.
	m.handlePermissionNotification("item/autoApprovalReview/completed", json.RawMessage(`{"threadId":"ordinary-child","turnId":"finished-turn","reviewId":"ordinary-review","targetItemId":"ordinary-item","startedAtMs":1,"completedAtMs":2,"decisionSource":"agent","review":{"status":"denied","riskLevel":"medium","userAuthorization":"low","rationale":"Ask the user before the ordinary external note"},"action":{"type":"command","source":"unifiedExec","command":"printf sample","cwd":"/workspace"}}`))
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	var entries []RuntimeApproval
	for len(entries) == 0 {
		select {
		case <-ctx.Done():
			t.Fatal("child denied action not attached to owned root")
		case <-time.After(time.Millisecond):
		}
		entries = m.ListRuntimeApprovals("ordinary-root")
	}
	if len(m.ListRuntimeApprovals("other-root")) != 0 || !strings.HasPrefix(entries[0].Summary, "子任务：") {
		t.Fatal("denied action owner projection differs")
	}
	for i := 0; i < 2; i++ {
		result, err := m.DecideRuntimeApproval(ctx, "ordinary-root", entries[0].ID, "approve_once")
		if err != nil || result.Status != "approved" {
			t.Fatal("native action acceptance failed", err)
		}
	}
	read, approval := <-wire, <-wire
	if read["method"] != "thread/read" || approval["method"] != "thread/approveGuardianDeniedAction" {
		t.Fatal("unexpected native operations")
	}
	params := approval["params"].(map[string]any)
	event := params["event"].(map[string]any)
	if params["threadId"] != "ordinary-child" || event["turn_id"] != "finished-turn" || event["target_item_id"] != "ordinary-item" {
		t.Fatal("native action moved to a different thread or turn")
	}
	if len(wire) != 0 {
		t.Fatal("duplicate acceptance was sent to native Runtime")
	}
	m.permissionCallbacks.mu.Lock()
	_, active := m.permissionCallbacks.activeTurns["ordinary-child"]
	m.permissionCallbacks.mu.Unlock()
	if active {
		t.Fatal("completed child turn was revived in active-turn projection")
	}
}
