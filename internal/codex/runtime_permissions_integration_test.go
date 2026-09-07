package codex

import (
	"context"
	"encoding/json"
	"os"
	"testing"
)

// Stage 1 deliberately stops before turn/start: it exercises the real pinned
// app-server and Host transport without a provider request or tool execution.
func TestPinnedRuntimePermissionConfigurationIntegration(t *testing.T) {
	binary, manifest := integrationArtifactPaths(t)
	config := integrationConfig(binary, manifest, integrationPrivateTempDir(t), "configuration-only-placeholder")
	config.RuntimePermissionsEnabled = true
	manager := NewManager(config, nil)
	if err := manager.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer shutdownIntegrationManager(t, manager, config.ShutdownTimeout)
	workspace := t.TempDir()
	observations := make([]map[string]any, 0, 3)
	for _, mode := range []PermissionMode{PermissionAsk, PermissionAuto, PermissionFull} {
		policy, reviewer, sandbox, err := permissionPolicy(mode)
		if err != nil {
			t.Fatal(err)
		}
		legacySandbox := "workspace-write"
		if mode == PermissionFull {
			legacySandbox = "danger-full-access"
		}
		var response struct {
			Thread struct {
				ID string `json:"id"`
			} `json:"thread"`
			ApprovalPolicy    string         `json:"approvalPolicy"`
			ApprovalsReviewer string         `json:"approvalsReviewer"`
			Sandbox           map[string]any `json:"sandbox"`
			Model             string         `json:"model"`
			ModelProvider     string         `json:"modelProvider"`
		}
		params := map[string]any{"model": MiniMaxModel, "modelProvider": MiniMaxProviderID, "cwd": workspace, "approvalPolicy": policy, "approvalsReviewer": reviewer, "sandbox": legacySandbox, "baseInstructions": permissionInstructions, "developerInstructions": permissionInstructions, "ephemeral": false}
		if err := manager.request(context.Background(), RuntimeMethodThreadStart, params, &response); err != nil {
			t.Fatalf("%s real thread/start: %v", mode, err)
		}
		if response.ApprovalPolicy != policy || response.ApprovalsReviewer != reviewer || response.Sandbox["type"] != sandbox["type"] || response.Model != MiniMaxModel || response.ModelProvider != MiniMaxProviderID {
			t.Fatalf("%s effective permission mismatch: policy=%q reviewer=%q sandbox=%v", mode, response.ApprovalPolicy, response.ApprovalsReviewer, response.Sandbox)
		}
		if mode != PermissionFull && response.Sandbox["networkAccess"] != false {
			t.Fatal("workspace network unexpectedly enabled")
		}
		if len(manager.ListRuntimeApprovals(response.Thread.ID)) != 0 {
			t.Fatal("configuration-only thread produced an approval")
		}
		observations = append(observations, map[string]any{"mode": mode, "approvalPolicy": response.ApprovalPolicy, "approvalsReviewer": response.ApprovalsReviewer, "sandbox": response.Sandbox["type"], "provider": response.ModelProvider})
		// thread/resume requires a persisted rollout. A zero-turn thread does
		// not have one in this retained build; recovery is a later real-turn AC.

	}
	if path := os.Getenv("YIJIE_PERMISSION_CONFIGURATION_EVIDENCE"); path != "" {
		evidence := map[string]any{"status": "PASS", "scope": "real Host-to-retained-Runtime configuration only", "provider_calls": 0, "restart_recovery": "NOT RUN: requires a real completed turn", "tool_executions": 0, "runtime_version": manager.Snapshot().RuntimeVersion, "modes": observations}
		bytes, err := json.MarshalIndent(evidence, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, append(bytes, '\n'), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

// Checks only the retained Runtime's supported configuration surface. No
// turn/start, review result, approval callback or provider request is produced.
func TestPinnedRuntimeManualReviewPolicyConfigurationIntegration(t *testing.T) {
	binary, manifest := integrationArtifactPaths(t)
	config := integrationConfig(binary, manifest, integrationPrivateTempDir(t), "configuration-only-placeholder")
	config.RuntimePermissionsEnabled = true
	manager := NewManager(config, nil)
	if err := manager.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer shutdownIntegrationManager(t, manager, config.ShutdownTimeout)
	const policy = "Ordinary files in the designated review directory require explicit human review before a write."
	var writeResponse map[string]any
	if err := manager.request(context.Background(), "config/value/write", map[string]any{
		"keyPath": "auto_review.policy", "value": policy, "mergeStrategy": "replace",
	}, &writeResponse); err != nil {
		t.Fatalf("native policy configuration write: %v", err)
	}
	var readResponse struct {
		Config map[string]any `json:"config"`
	}
	if err := manager.request(context.Background(), "config/read", map[string]any{"includeLayers": false}, &readResponse); err != nil {
		t.Fatalf("native policy configuration read: %v", err)
	}
	autoReview, ok := readResponse.Config["auto_review"].(map[string]any)
	if !ok || autoReview["policy"] != policy {
		t.Fatal("retained Runtime did not return the configured auto-review policy")
	}
	var started struct {
		ApprovalsReviewer string `json:"approvalsReviewer"`
	}
	if err := manager.request(context.Background(), RuntimeMethodThreadStart, map[string]any{
		"model": MiniMaxModel, "modelProvider": MiniMaxProviderID, "cwd": t.TempDir(),
		"approvalPolicy": "on-request", "approvalsReviewer": "auto_review", "sandbox": "workspace-write",
		"config":           map[string]any{"auto_review.policy": policy},
		"baseInstructions": permissionInstructions, "developerInstructions": permissionInstructions,
	}, &started); err != nil {
		t.Fatalf("native thread/start with configured review policy: %v", err)
	}
	if started.ApprovalsReviewer != "auto_review" {
		t.Fatal("native reviewer configuration differs")
	}
	t.Log("retained Runtime accepted auto_review.policy and thread/start; provider calls=0; actual denial/takeover NOT RUN")
}
