package codex

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/pelletier/go-toml/v2"
)

func TestFEAT144NativeRootMigrationPreservesTrustBytes(t *testing.T) {
	for _, initialMcp := range []bool{false, true} {
		home := canonicalOwnedTempDir(t)
		a, err := acquireManagedCodexHomeAuthority(home)
		if err != nil {
			t.Fatal(err)
		}
		defer a.Close()
		legacy, _ := miniMaxManagedConfigForRuntime(home, ManagedReasoningProfileDefault, false, initialMcp)
		native := []byte("\n[projects.\"/ordinary project\"]\ntrust_level = \"trusted\"\n\n[projects.\"/另一个项目\"]\ntrust_level = \"untrusted\"\n")
		if err := os.WriteFile(filepath.Join(home, "config.toml"), append(legacy, native...), 0o600); err != nil {
			t.Fatal(err)
		}
		for _, enabled := range []bool{true, false, false} {
			args, err := prepareNativeLayeredConfig(a, ManagedReasoningProfileDefault, enabled)
			if err != nil {
				t.Fatal(err)
			}
			current, _ := os.ReadFile(filepath.Join(home, "config.toml"))
			if !bytes.Equal(current, native) {
				t.Fatal("native trust records were rewritten")
			}
			expected, _ := miniMaxManagedConfigForRuntime(home, ManagedReasoningProfileDefault, false, enabled)
			if err := a.validateNativeLayeredConfig(expected); err != nil {
				t.Fatal(err)
			}
			var reconstructed map[string]any
			var lines []string
			for i := 0; i < len(args); i += 2 {
				if args[i] != "-c" {
					t.Fatal("non-native configuration argument")
				}
				lines = append(lines, args[i+1])
			}
			if err := toml.Unmarshal([]byte(strings.Join(lines, "\n")), &reconstructed); err != nil {
				t.Fatal("native argument values are not valid TOML")
			}
			if reconstructed["projects"] != nil || reconstructed["model"] != MiniMaxModel {
				t.Fatal("managed arguments invented trust or lost model configuration")
			}
			var source map[string]any
			if toml.Unmarshal(expected, &source) != nil || !reflect.DeepEqual(source, reconstructed) {
				t.Fatal("native CLI values differ from the exact managed source")
			}
		}
	}
}

func TestFEAT144NativeRootRejectsUnownedConfiguration(t *testing.T) {
	// Plain local configuration mistakes; no executable, attack or fault fixture.
	for _, content := range []string{
		"model = 'other'\n",
		"[projects.'/ordinary']\nnotes = 'ordinary note'\n",
		"[projects.'/ordinary']\ntrust_level = 'unknown'\n",
		"[projects.relative]\ntrust_level = 'trusted'\n",
	} {
		home := canonicalOwnedTempDir(t)
		a, err := acquireManagedCodexHomeAuthority(home)
		if err != nil {
			t.Fatal(err)
		}
		defer a.Close()
		path := filepath.Join(home, "config.toml")
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := prepareNativeLayeredConfig(a, ManagedReasoningProfileDefault, false); err == nil {
			t.Fatal("unowned or unsupported native configuration accepted")
		}
		actual, _ := os.ReadFile(path)
		if string(actual) != content {
			t.Fatal("rejected configuration changed")
		}
		if _, err := os.Stat(filepath.Join(home, managedNativeConfigName)); !os.IsNotExist(err) {
			t.Fatal("failed preflight wrote a managed file")
		}
	}
}

func TestFEAT144NativeManagedFileStillRequiresExactSource(t *testing.T) {
	home := canonicalOwnedTempDir(t)
	a, err := acquireManagedCodexHomeAuthority(home)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	if _, err := prepareNativeLayeredConfig(a, ManagedReasoningProfileDefault, false); err != nil {
		t.Fatal(err)
	}
	config, _ := miniMaxManagedConfigForRuntime(home, ManagedReasoningProfileDefault, false, false)
	if err := a.validateManagedFileExact(managedNativeConfigName, append(config, '\n')); err == nil {
		t.Fatal("managed template validation became semantic instead of exact")
	}
	if err := a.validateNativeLayeredConfig(config); err != nil {
		t.Fatal(err)
	}
}

func TestPinnedRuntimeFEAT144NativeTrustSurvivesRestart(t *testing.T) {
	binary, manifest := integrationArtifactPaths(t)
	home := integrationPrivateTempDir(t)
	config := integrationConfig(binary, manifest, home, "ordinary-configuration-only")
	config.RuntimePermissionsEnabled = true
	// initialize/config/read do not start a native MCP client. No thread is
	// created until the normal deactivation has cleared this entire profile.
	config.Sorftime = SorftimeConfig{Enabled: true, Token: "ordinary-configuration-only"}
	manager := NewManager(config, nil)
	if err := manager.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer shutdownIntegrationManager(t, manager, config.ShutdownTimeout)
	if err := manager.validateSorftimeConfig(context.Background(), ""); err != nil {
		t.Fatal(err)
	}
	active, restarted, err := manager.PrepareMcpPermissionScope(context.Background(), PermissionAuto)
	if err != nil || active || !restarted || !manager.Snapshot().Ready {
		t.Fatalf("normal deactivation/restart failed: %v", err)
	}
	workspace := t.TempDir()
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
			ApprovalPolicy string         `json:"approvalPolicy"`
			Reviewer       string         `json:"approvalsReviewer"`
			Sandbox        map[string]any `json:"sandbox"`
		}
		if err := manager.request(context.Background(), "thread/start", map[string]any{"cwd": workspace, "approvalPolicy": policy, "approvalsReviewer": reviewer, "sandbox": legacySandbox, "ephemeral": true}, &response); err != nil {
			t.Fatal(err)
		}
		if response.ApprovalPolicy != policy || response.Reviewer != reviewer || response.Sandbox["type"] != sandbox["type"] {
			t.Fatal("native permission semantics changed")
		}
	}
	before, err := os.ReadFile(filepath.Join(home, "config.toml"))
	if err != nil || !bytes.Contains(before, []byte("trust_level")) || validateNativeProjectConfig(before) != nil {
		t.Fatal("native project trust was not saved")
	}
	shutdownIntegrationManager(t, manager, config.ShutdownTimeout)
	config.Sorftime = SorftimeConfig{}
	second := NewManager(config, nil)
	if err := second.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer shutdownIntegrationManager(t, second, config.ShutdownTimeout)
	after, _ := os.ReadFile(filepath.Join(home, "config.toml"))
	if !bytes.Equal(before, after) {
		t.Fatal("normal reopen changed native trust bytes")
	}
	if err := second.validateSorftimeConfig(context.Background(), ""); err != nil {
		t.Fatal(err)
	}
}
