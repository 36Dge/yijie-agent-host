package app

import (
	"path/filepath"
	"testing"

	"github.com/36Dge/yijie-agent-host/internal/codex"
)

func TestFEAT137CommandApprovalProfileRequiresExactConjunction(t *testing.T) {
	canonicalHome := filepath.Join(t.TempDir(), "host-home")
	stableRuntime := codex.DefaultConfig()
	stableRuntime.MiniMax = codex.MiniMaxConfig{Enabled: true, APIKey: "synthetic-test-key"}

	tests := []struct {
		name        string
		flag        string
		environment string
		profile     string
		hostHome    string
		feat134     bool
		feat136     bool
		mutate      func(*codex.Config)
		want        bool
		wantErr     bool
	}{
		{name: "default off", environment: "production", profile: "other", hostHome: "relative"},
		{name: "explicit false", flag: "false", environment: "production", profile: "other", hostHome: "relative"},
		{
			name: "exact stable local profile", flag: "true", environment: "local", profile: "demo_fast",
			hostHome: canonicalHome, feat134: true, feat136: true, want: true,
		},
		{
			name: "non exact flag", flag: "TRUE", environment: "local", profile: "demo_fast",
			hostHome: canonicalHome, feat134: true, feat136: true, wantErr: true,
		},
		{
			name: "implicit environment", flag: "true", profile: "demo_fast",
			hostHome: canonicalHome, feat134: true, feat136: true, wantErr: true,
		},
		{
			name: "missing profile", flag: "true", environment: "local",
			hostHome: canonicalHome, feat134: true, feat136: true, wantErr: true,
		},
		{
			name: "profile whitespace", flag: "true", environment: "local", profile: "demo_fast ",
			hostHome: canonicalHome, feat134: true, feat136: true, wantErr: true,
		},
		{
			name: "non local", flag: "true", environment: "production", profile: "demo_fast",
			hostHome: canonicalHome, feat134: true, feat136: true, wantErr: true,
		},
		{
			name: "FEAT-134 disabled", flag: "true", environment: "local", profile: "demo_fast",
			hostHome: canonicalHome, feat136: true, wantErr: true,
		},
		{
			name: "FEAT-136 disabled", flag: "true", environment: "local", profile: "demo_fast",
			hostHome: canonicalHome, feat134: true, wantErr: true,
		},
		{
			name: "relative Host home", flag: "true", environment: "local", profile: "demo_fast",
			hostHome: "relative/host-home", feat134: true, feat136: true, wantErr: true,
		},
		{
			name: "non canonical Host home", flag: "true", environment: "local", profile: "demo_fast",
			hostHome: canonicalHome + "/../host-home", feat134: true, feat136: true, wantErr: true,
		},
		{
			name: "managed provider absent", flag: "true", environment: "local", profile: "demo_fast",
			hostHome: canonicalHome, feat134: true, feat136: true,
			mutate: func(config *codex.Config) { config.MiniMax = codex.MiniMaxConfig{} }, wantErr: true,
		},
		{
			name: "fake provider", flag: "true", environment: "local", profile: "demo_fast",
			hostHome: canonicalHome, feat134: true, feat136: true,
			mutate: func(config *codex.Config) { config.FakeResponses.Enabled = true }, wantErr: true,
		},
		{
			name: "experimental tools", flag: "true", environment: "local", profile: "demo_fast",
			hostHome: canonicalHome, feat134: true, feat136: true,
			mutate: func(config *codex.Config) { config.DynamicToolsEnabled = true }, wantErr: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("YIJIE_FEAT137_COMMAND_APPROVAL_ENABLED", test.flag)
			t.Setenv("YIJIE_ENV", test.environment)
			t.Setenv("YIJIE_LOCAL_PROFILE", test.profile)
			runtimeConfig := stableRuntime
			if test.mutate != nil {
				test.mutate(&runtimeConfig)
			}
			enabled, err := loadFEAT137CommandApprovalProfile(
				test.environment, test.hostHome, test.feat134, test.feat136, runtimeConfig,
			)
			if (err != nil) != test.wantErr || enabled != test.want {
				t.Fatalf("profile got=(%t,%v), want=(%t,err=%t)", enabled, err, test.want, test.wantErr)
			}
		})
	}
}

func TestFEAT137LoadConfigWiresApprovalWithoutExperimentalTools(t *testing.T) {
	for _, key := range []string{
		"YIJIE_AGENT_HOST_HOME", "YIJIE_AGENT_HOST_INSTANCE_NONCE", "YIJIE_AGENT_HOST_PARENT_PID",
		"YIJIE_AGENT_HOST_V2_CLEANUP_ENABLED", "YIJIE_AGENT_HOST_V2_MULTIMODAL_TURNS_ENABLED",
		"YIJIE_AGENT_HOST_V2_RAW_REASONING_ENABLED", "YIJIE_AGENT_HOST_V2_TITLE_ENABLED",
		"YIJIE_AGENT_HOST_V3_ARTIFACTS_ENABLED", "YIJIE_CODEX_BINARY", "YIJIE_CODEX_HOME",
		"YIJIE_CODEX_MANIFEST", "YIJIE_ENV", "YIJIE_FEAT126_S10_TEST_PROFILE_ENABLED",
		"YIJIE_FEAT128_IMAGE_GENERATION_ENABLED", "YIJIE_FEAT128_S10_TEST_PROFILE_ENABLED",
		"YIJIE_FEAT128_SYNTHETIC_ENABLED", "YIJIE_FEAT128_SYNTHETIC_MANIFEST",
		"YIJIE_FEAT134_STREAMING_ENABLED", "YIJIE_FEAT136_COMMAND_TOOL_ITEMS_ENABLED",
		"YIJIE_FEAT137_COMMAND_APPROVAL_ENABLED", "YIJIE_LOCAL_PROFILE", "YIJIE_MINIMAX_API_KEY",
		"YIJIE_MINIMAX_API_KEY_FILE", "YIJIE_MODEL_PROVIDER", skillBundleRootEnv, skillInstallRootEnv,
	} {
		t.Setenv(key, "")
	}
	hostRoot := t.TempDir()
	t.Setenv("YIJIE_ENV", "local")
	t.Setenv("YIJIE_LOCAL_PROFILE", "demo_fast")
	t.Setenv("YIJIE_MODEL_PROVIDER", codex.MiniMaxProviderID)
	t.Setenv("YIJIE_MINIMAX_API_KEY", "synthetic-test-key")
	t.Setenv("YIJIE_AGENT_HOST_HOME", filepath.Join(hostRoot, "host-home"))
	t.Setenv("YIJIE_CODEX_HOME", filepath.Join(hostRoot, "codex-home"))
	t.Setenv("YIJIE_FEAT134_STREAMING_ENABLED", "true")
	t.Setenv("YIJIE_FEAT136_COMMAND_TOOL_ITEMS_ENABLED", "true")

	disabled, err := LoadConfig()
	if err != nil {
		t.Fatalf("load default-off FEAT-137 configuration: %v", err)
	}
	if disabled.FEAT137CommandApprovalEnabled || disabled.Runtime.CommandApprovalEnabled {
		t.Fatalf("FEAT-137 approval was enabled by default: %#v", disabled.Runtime)
	}

	t.Setenv("YIJIE_FEAT137_COMMAND_APPROVAL_ENABLED", "true")
	enabled, err := LoadConfig()
	if err != nil {
		t.Fatalf("load exact FEAT-137 configuration: %v", err)
	}
	if !enabled.FEAT134StreamingEnabled || !enabled.FEAT136CommandToolItemsEnabled ||
		!enabled.FEAT137CommandApprovalEnabled || !enabled.Runtime.CommandApprovalEnabled {
		t.Fatalf("FEAT-137 dependent gates were not wired: %#v", enabled)
	}
	if enabled.ImageGenerationEnabled || enabled.Runtime.DynamicToolsEnabled ||
		codex.NewManager(enabled.Runtime, nil).Snapshot().ExperimentalAPI {
		t.Fatalf("FEAT-137 enabled an experimental Runtime surface: %#v", enabled.Runtime)
	}
}
