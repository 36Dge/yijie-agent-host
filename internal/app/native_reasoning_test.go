package app

import (
	"github.com/36Dge/yijie-agent-host/internal/codex"
	"path/filepath"
	"testing"
)

func TestNativeDailyReasoningConfig(t *testing.T) {
	for _, key := range []string{"YIJIE_NATIVE_REASONING_ENABLED", "YIJIE_FEAT134_STREAMING_ENABLED", "YIJIE_FEAT136_COMMAND_TOOL_ITEMS_ENABLED", "YIJIE_FEAT137_COMMAND_APPROVAL_ENABLED", "YIJIE_FEAT126_S10_TEST_PROFILE_ENABLED", "YIJIE_MINIMAX_API_KEY_FILE", "YIJIE_FEAT128_SYNTHETIC_ENABLED", "YIJIE_FEAT128_SYNTHETIC_MANIFEST", "YIJIE_AGENT_HOST_V2_RAW_REASONING_ENABLED", skillBundleRootEnv, skillInstallRootEnv} {
		t.Setenv(key, "")
	}
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for key, value := range map[string]string{
		"YIJIE_ENV": "local", "YIJIE_LOCAL_PROFILE": "demo_fast", "YIJIE_RUNTIME_PERMISSIONS_ENABLED": "true",
		"YIJIE_MODEL_PROVIDER": "minimax", "YIJIE_MINIMAX_API_KEY": "normal-test-placeholder",
		"YIJIE_AGENT_HOST_HOME": filepath.Join(root, "host"), "YIJIE_CODEX_HOME": filepath.Join(root, "runtime"),
		"YIJIE_FEAT128_IMAGE_GENERATION_ENABLED": "true", "YIJIE_AGENT_HOST_V3_ARTIFACTS_ENABLED": "true", "YIJIE_AGENT_HOST_V2_MULTIMODAL_TURNS_ENABLED": "true",
	} {
		t.Setenv(key, value)
	}
	baseline, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if baseline.NativeReasoningEnabled || baseline.Runtime.ManagedReasoningProfile != codex.ManagedReasoningProfileDefault {
		t.Fatal("reasoning must require explicit deployment selection")
	}
	t.Setenv("YIJIE_NATIVE_REASONING_ENABLED", "true")
	enabled, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if !enabled.NativeReasoningEnabled || enabled.Runtime.ManagedReasoningProfile != codex.ManagedReasoningProfileNativeHighRaw || !enabled.Runtime.DynamicToolsEnabled || !enabled.ImageGenerationEnabled {
		t.Fatal("daily reasoning and existing image capability must coexist")
	}
	if enabled.FEAT134StreamingEnabled || enabled.RawReasoningV2Enabled || enabled.FEAT136CommandToolItemsEnabled || enabled.FEAT137CommandApprovalEnabled {
		t.Fatal("daily reasoning enabled a legacy event surface")
	}
	for _, change := range []struct{ key, value string }{
		{"YIJIE_ENV", "production"}, {"YIJIE_LOCAL_PROFILE", "default"}, {"YIJIE_RUNTIME_PERMISSIONS_ENABLED", "false"}, {"YIJIE_AGENT_HOST_HOME", filepath.Join(root, "runtime")}, {"YIJIE_NATIVE_REASONING_ENABLED", "TRUE"},
	} {
		t.Run(change.key, func(t *testing.T) {
			t.Setenv(change.key, change.value)
			if _, err := LoadConfig(); err == nil {
				t.Fatal("unsupported deployment accepted")
			}
		})
	}
}
