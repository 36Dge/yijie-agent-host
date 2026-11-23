package codex

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

// These are ordinary callbacks on live turns. Only the handle crosses the UI.
func TestNativeApprovalChoiceRoundTrip(t *testing.T) {
	cases := []struct{ name, method, fields, handle, want string }{
		{"command session", RuntimeMethodCommandApproval, `"itemId":"item","command":"pwd","availableDecisions":["accept","acceptForSession","cancel"]`, "native_1", `{"decision":"acceptForSession"}`},
		{"file session", "item/fileChange/requestApproval", `"itemId":"item","grantRoot":"/workspace"`, "native_1", `{"decision":"acceptForSession"}`},
		{"permissions strict review", "item/permissions/requestApproval", `"itemId":"item","permissions":{"network":{"enabled":true}}`, "native_1", `{"permissions":{"network":{"enabled":true}},"scope":"turn","strictAutoReview":true}`},
		{"MCP persistent option", "mcpServer/elicitation/request", `"serverName":"local-notes","mode":"form","message":"Check the local note","requestedSchema":{"type":"object","properties":{}},"_meta":{"codex_approval_kind":"mcp_tool_call","persist":["always"],"tool_title":"Local note"}`, "native_1", `{"_meta":{"persist":"always"},"action":"accept","content":{}}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := DefaultConfig()
			c.RuntimePermissionsEnabled = true
			m := NewManager(c, nil)
			m.handlePermissionNotification("turn/started", json.RawMessage(`{"threadId":"thread","turn":{"id":"turn"}}`))
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			answered := make(chan serverRequestResult, 1)
			go func() {
				answered <- m.handleServerRequest(ctx, serverRequest{method: tc.method, idKey: "n:7", exactEnvelope: true, params: json.RawMessage(`{"threadId":"thread","turnId":"turn",` + tc.fields + "}")})
			}()
			var v RuntimeApproval
			for v.ID == "" {
				select {
				case <-ctx.Done():
					t.Fatal("missing native offer")
				case <-time.After(time.Millisecond):
					if list := m.ListRuntimeApprovals("thread"); len(list) > 0 {
						v = list[0]
					}
				}
			}
			if len(v.NativeChoices) < 2 {
				t.Fatal("missing choices")
			}
			if _, err := m.DecideRuntimeApproval(ctx, "thread", v.ID, "native_127"); err == nil {
				t.Fatal("accepted an unoffered choice")
			}
			done := make(chan error, 1)
			go func() { _, err := m.DecideRuntimeApproval(ctx, "thread", v.ID, tc.handle); done <- err }()
			result := <-answered
			raw, err := json.Marshal(result.value)
			if err != nil || string(raw) != tc.want {
				t.Fatalf("native payload = %s, want %s", raw, tc.want)
			}
			result.onResponseWritten(nil)
			if err := <-done; err != nil {
				t.Fatal(err)
			}
		})
	}
}
