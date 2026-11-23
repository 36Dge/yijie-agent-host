package codex

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Stage 1 deliberately stops before turn/start: it exercises the real pinned
// app-server and Host transport without a provider request or tool execution.
func TestPinnedRuntimePermissionConfigurationIntegration(t *testing.T) {
	binary, manifest := integrationArtifactPaths(t)
	config := integrationConfig(binary, manifest, integrationPrivateTempDir(t), "configuration-only-placeholder")
	config.RuntimePermissionsEnabled = true
	artifact, err := VerifyArtifact(context.Background(), binary, manifest, config.StartupTimeout)
	if err != nil {
		t.Fatal(err)
	}
	config.ChatModelsEnabled, config.KimiK31Candidate = artifact.SupportsKimiK31(), artifact.SupportsKimiK31()
	config.InteractionToolsEnabled, config.NativeToolsEnabled = artifact.supportsInteractionTools(), artifact.supportsNativeTools()
	config.HostImagesEnabled = artifact.supportsHostImages() && !config.NativeToolsEnabled
	if config.KimiK31Candidate {
		config.KimiAPIKey = "unused-local-configuration-key"
	}

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

// The workspace file is created by the actual bundled Runtime through an
// ordinary conversation. The programmatic local model only requests the tool.
func TestDailyNativePermissionsApplyToOrdinaryFile(t *testing.T) {
	h := newNativeToolsHarness(t, func(_ *nativeToolsHarness, _ map[string]any, n int64) map[string]any {
		if n == 1 {
			return feat164AuxTool("", "apply_patch", "ordinary_patch", map[string]any{"input": "*** Begin Patch\n*** Add File: parity-note.txt\n+approved workspace note\n*** End Patch\n"})
		}
		return feat164AuxText("已完成普通文件操作。")
	}, func(c *Config) { c.InteractionToolsEnabled = true })
	ctx := WithPermissionMode(WithModelProfile(context.Background(), "minimax-m3-high-v1"), PermissionFull)
	thread, err := h.manager.StartThread(ctx, h.cwd)
	if err != nil {
		t.Fatal(err)
	}
	input, err := TextUserInput("请在本轮工作目录写入一条普通笔记。")
	if err != nil {
		t.Fatal(err)
	}
	turn, err := h.manager.StartTurnV2(ctx, thread.ID, []UserInput{input}, "high")
	if err != nil {
		t.Fatal(err)
	}
	status, _, err := h.notifications.waitForTurn(turn.ID, 25*time.Second)
	if err != nil || status != "completed" {
		t.Fatalf("turn %s: %v", status, err)
	}
	body, err := os.ReadFile(filepath.Join(h.cwd, "parity-note.txt"))
	if err != nil || string(body) != "approved workspace note\n" || len(h.manager.ListRuntimeApprovals(thread.ID)) != 0 {
		t.Fatalf("file or native approval result differs: %v", err)
	}
	if _, err := h.manager.ResumeThread(WithPermissionMode(ctx, PermissionAuto), thread.ID); err != nil {
		t.Fatal(err)
	}
}
