package codex

import (
	"context"
	"encoding/json"
	"io"
	"sync/atomic"
	"testing"
	"time"
)

func TestRuntimePermissionModes(t *testing.T) {
	for _, tc := range []struct {
		mode                      PermissionMode
		policy, reviewer, sandbox string
	}{
		{PermissionAsk, "on-request", "user", "workspaceWrite"},
		{PermissionAuto, "on-request", "auto_review", "workspaceWrite"},
		{PermissionFull, "never", "user", "dangerFullAccess"},
	} {
		p, r, s, err := permissionPolicy(tc.mode)
		if err != nil || p != tc.policy || r != tc.reviewer || s["type"] != tc.sandbox {
			t.Fatalf("incorrect mode %s", tc.mode)
		}
		if tc.mode != PermissionFull && s["networkAccess"] != false {
			t.Fatal("workspace network must request approval")
		}
	}
	if err := NewManager(DefaultConfig(), nil).ValidatePermissionMode(PermissionFull); err == nil {
		t.Fatal("inactive local permission adapter must remain unavailable")
	}
}

func TestRuntimePermissionVerificationPolicyIsOptIn(t *testing.T) {
	config := DefaultConfig()
	config.RuntimePermissionsEnabled = true
	config.PermissionVerificationBaseURL = "http://127.0.0.1:18083/v1"
	manager := NewManager(config, nil)
	if _, exists := manager.permissionVerificationConfig()["auto_review.policy"]; exists {
		t.Fatal("default native risk policy must not change")
	}
	config.PermissionVerificationPolicy = "Only files in the designated review directory require human review."
	manager = NewManager(config, nil)
	if manager.permissionVerificationConfig()["auto_review.policy"] != config.PermissionVerificationPolicy {
		t.Fatal("native policy input was not passed through")
	}
	config.RuntimePermissionsEnabled = false
	if NewManager(config, nil).permissionVerificationConfig() != nil {
		t.Fatal("verification policy must remain behind the local permission gate")
	}
}

func TestRuntimeApprovalCallbackRoundTrip(t *testing.T) {
	for _, decision := range []string{"approve_once", "reject"} {
		t.Run(decision, func(t *testing.T) {
			config := DefaultConfig()
			config.RuntimePermissionsEnabled = true
			m := NewManager(config, nil)
			m.handlePermissionNotification("turn/started", json.RawMessage(`{"threadId":"task-one","turn":{"id":"turn-one"}}`))
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			response := make(chan serverRequestResult, 1)
			go func() {
				response <- m.handleServerRequest(ctx, serverRequest{method: RuntimeMethodCommandApproval, idKey: "n:1", params: json.RawMessage(`{"threadId":"task-one","turnId":"turn-one","itemId":"item-one","command":"printf hello","cwd":"/workspace","reason":"Read the sample output"}`)})
			}()
			var views []RuntimeApproval
			for len(views) == 0 {
				select {
				case <-ctx.Done():
					t.Fatal("callback did not become pending")
				case <-time.After(time.Millisecond):
					views = m.ListRuntimeApprovals("task-one")
				}
			}
			if len(m.ListRuntimeApprovals("task-two")) != 0 {
				t.Fatal("task state crossed scope")
			}
			completed := make(chan error, 1)
			go func() { _, err := m.DecideRuntimeApproval(ctx, "task-one", views[0].ID, decision); completed <- err }()
			result := <-response
			wire := result.value.(map[string]string)
			want := "decline"
			status := "rejected"
			if decision == "approve_once" {
				want = "accept"
				status = "approved"
			}
			if wire["decision"] != want {
				t.Fatal("incorrect native decision")
			}
			select {
			case <-completed:
				t.Fatal("UI completion preceded the actual RPC write")
			default:
			}
			result.onResponseWritten(nil)
			if err := <-completed; err != nil {
				t.Fatal(err)
			}
			view, err := m.DecideRuntimeApproval(ctx, "task-one", views[0].ID, decision)
			if err != nil || view.Status != status {
				t.Fatal("same decision must be idempotent")
			}
			if _, err := m.DecideRuntimeApproval(ctx, "task-one", views[0].ID, map[string]string{"approve_once": "reject", "reject": "approve_once"}[decision]); err == nil {
				t.Fatal("different decision must not execute")
			}
		})
	}
}

func TestRuntimePermissionGrantUsesRequestedTurnScope(t *testing.T) {
	config := DefaultConfig()
	config.RuntimePermissionsEnabled = true
	m := NewManager(config, nil)
	m.handlePermissionNotification("turn/started", json.RawMessage(`{"threadId":"task","turn":{"id":"turn"}}`))
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	response := make(chan serverRequestResult, 1)
	go func() {
		response <- m.handleServerRequest(ctx, serverRequest{method: "item/permissions/requestApproval", idKey: "n:2", params: json.RawMessage(`{"threadId":"task","turnId":"turn","itemId":"item","cwd":"/workspace","permissions":{"network":{"enabled":true}},"reason":"Read public reference"}`)})
	}()
	var entries []RuntimeApproval
	for len(entries) == 0 {
		select {
		case <-ctx.Done():
			t.Fatal("missing request")
		case <-time.After(time.Millisecond):
			entries = m.ListRuntimeApprovals("task")
		}
	}
	done := make(chan error, 1)
	go func() { _, err := m.DecideRuntimeApproval(ctx, "task", entries[0].ID, "approve_once"); done <- err }()
	r := <-response
	body, _ := json.Marshal(r.value)
	if string(body) != `{"permissions":{"network":{"enabled":true}},"scope":"turn"}` {
		t.Fatalf("grant differs: %s", body)
	}
	r.onResponseWritten(nil)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestRuntimeAutomaticReviewRemainsNative(t *testing.T) {
	config := DefaultConfig()
	config.RuntimePermissionsEnabled = true
	m := NewManager(config, nil)
	m.handlePermissionNotification("item/autoApprovalReview/completed", json.RawMessage(`{"threadId":"task","turnId":"turn","reviewId":"review","startedAtMs":1,"completedAtMs":2,"decisionSource":"agent","review":{"status":"denied","riskLevel":"medium","userAuthorization":"low","rationale":"Ask the user"},"action":{"type":"command","source":"unifiedExec","command":"printf hello","cwd":"/workspace"}}`))
	views := m.ListRuntimeApprovals("task")
	if len(views) != 1 || views[0].Kind != "auto_review" || views[0].Status != "pending" {
		t.Fatal("native denial missing")
	}
	var event map[string]any
	if err := json.Unmarshal(m.permissionCallbacks.entries[0].guardian, &event); err != nil {
		t.Fatal(err)
	}
	action := event["action"].(map[string]any)
	if event["status"] != "denied" || action["source"] != "unified_exec" || event["turn_id"] != "turn" {
		t.Fatal("native core event projection mismatch")
	}
	value, err := m.DecideRuntimeApproval(context.Background(), "task", views[0].ID, "reject")
	if err != nil || value.Status != "rejected" {
		t.Fatal("manual rejection must not create a model call")
	}
}

// In-process protocol unit test only: no Runtime process or model is started.
// This does not stand in for a naturally denied review in the real application.
func TestRuntimeAutomaticTakeoverIsBoundIdempotentAndReportsFailure(t *testing.T) {
	for _, unavailable := range []bool{false, true} {
		name := "accepted"
		if unavailable {
			name = "unavailable"
		}
		t.Run(name, func(t *testing.T) {
			outReader, outWriter := io.Pipe()
			inReader, inWriter := io.Pipe()
			client := NewClient(outWriter, inReader, 1<<20, 8, nil, nil)
			client.Start()
			t.Cleanup(func() { _ = client.CloseInput(); _ = inWriter.Close(); _ = outReader.Close() })
			config := DefaultConfig()
			config.RuntimePermissionsEnabled = true
			m := NewManager(config, nil)
			m.client = client
			m.status.Ready = true
			m.status.State = StateReady
			m.handlePermissionNotification("item/autoApprovalReview/completed", json.RawMessage(`{"threadId":"task","turnId":"finished-turn","reviewId":"review","startedAtMs":1,"completedAtMs":2,"decisionSource":"agent","review":{"status":"denied","riskLevel":"medium","userAuthorization":"low","rationale":"Ask the user"},"action":{"type":"command","source":"unifiedExec","command":"printf sample","cwd":"/workspace"}}`))
			view := m.ListRuntimeApprovals("task")[0]
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			if _, err := m.DecideRuntimeApproval(ctx, "another-task", view.ID, "approve_once"); err == nil {
				t.Fatal("request crossed task scope")
			}
			var requests atomic.Int32
			wire := make(chan map[string]any, 2)
			go func() {
				decoder := json.NewDecoder(outReader)
				for {
					var request map[string]any
					if decoder.Decode(&request) != nil {
						return
					}
					requests.Add(1)
					wire <- request
					response := map[string]any{"id": request["id"], "result": map[string]any{}}
					if unavailable {
						delete(response, "result")
						response["error"] = map[string]any{"code": -32603, "message": "Approval unavailable"}
					}
					if json.NewEncoder(inWriter).Encode(response) != nil {
						return
					}
				}
			}()
			for attempt := 0; attempt < 2; attempt++ {
				result, err := m.DecideRuntimeApproval(ctx, "task", view.ID, "approve_once")
				if unavailable {
					if err == nil || result.Status != "unavailable" {
						t.Fatalf("failed submission became approved: %+v %v", result, err)
					}
				} else if err != nil || result.Status != "approved" {
					t.Fatalf("approval was not acknowledged: %+v %v", result, err)
				}
			}
			if requests.Load() != 1 {
				t.Fatalf("duplicate approval sent %d requests", requests.Load())
			}
			request := <-wire
			if request["method"] != "thread/approveGuardianDeniedAction" {
				t.Fatal("manual takeover started an unexpected operation")
			}
			params := request["params"].(map[string]any)
			event := params["event"].(map[string]any)
			if params["threadId"] != "task" || event["id"] != "review" || event["turn_id"] != "finished-turn" || event["action"].(map[string]any)["command"] != "printf sample" {
				t.Fatal("manual takeover lost the original action identity")
			}
		})
	}
}
