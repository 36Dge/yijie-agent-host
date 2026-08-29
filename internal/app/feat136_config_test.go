package app

import (
	"path/filepath"
	"testing"

	"github.com/36Dge/yijie-agent-host/internal/codex"
)

func TestFEAT136CommandToolItemsProfileRequiresExactDependentGate(t *testing.T) {
	tests := []struct {
		name        string
		flag        string
		environment string
		profile     string
		feat134     bool
		want        bool
		wantErr     bool
	}{
		{name: "exact", flag: "true", environment: "local", profile: "demo_fast", feat134: true, want: true},
		{name: "default off", environment: "production", profile: "other"},
		{name: "explicit false", flag: "false", environment: "production", profile: "other"},
		{name: "non exact flag", flag: "TRUE", environment: "local", profile: "demo_fast", feat134: true, wantErr: true},
		{name: "implicit environment", flag: "true", profile: "demo_fast", feat134: true, wantErr: true},
		{name: "missing profile", flag: "true", environment: "local", feat134: true, wantErr: true},
		{name: "profile whitespace", flag: "true", environment: "local", profile: "demo_fast ", feat134: true, wantErr: true},
		{name: "non local environment", flag: "true", environment: "production", profile: "demo_fast", feat134: true, wantErr: true},
		{name: "FEAT-134 disabled", flag: "true", environment: "local", profile: "demo_fast", wantErr: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("YIJIE_FEAT136_COMMAND_TOOL_ITEMS_ENABLED", test.flag)
			t.Setenv("YIJIE_ENV", test.environment)
			t.Setenv("YIJIE_LOCAL_PROFILE", test.profile)
			enabled, err := loadFEAT136CommandToolItemsProfile(test.environment, test.feat134)
			if (err != nil) != test.wantErr || enabled != test.want {
				t.Fatalf("profile got=(%t,%v), want=(%t,err=%t)", enabled, err, test.want, test.wantErr)
			}
		})
	}
}

func TestFEAT136LoadConfigWiresV5WithoutExperimentalTools(t *testing.T) {
	for _, key := range []string{
		"YIJIE_AGENT_HOST_HOME", "YIJIE_AGENT_HOST_INSTANCE_NONCE", "YIJIE_AGENT_HOST_PARENT_PID",
		"YIJIE_AGENT_HOST_V2_CLEANUP_ENABLED", "YIJIE_AGENT_HOST_V2_MULTIMODAL_TURNS_ENABLED",
		"YIJIE_AGENT_HOST_V2_RAW_REASONING_ENABLED", "YIJIE_AGENT_HOST_V2_TITLE_ENABLED",
		"YIJIE_AGENT_HOST_V3_ARTIFACTS_ENABLED", "YIJIE_CODEX_BINARY", "YIJIE_CODEX_HOME",
		"YIJIE_CODEX_MANIFEST", "YIJIE_ENV", "YIJIE_FEAT126_S10_TEST_PROFILE_ENABLED",
		"YIJIE_FEAT128_IMAGE_GENERATION_ENABLED", "YIJIE_FEAT128_S10_TEST_PROFILE_ENABLED",
		"YIJIE_FEAT128_SYNTHETIC_ENABLED", "YIJIE_FEAT128_SYNTHETIC_MANIFEST",
		"YIJIE_FEAT134_STREAMING_ENABLED", "YIJIE_FEAT136_COMMAND_TOOL_ITEMS_ENABLED",
		"YIJIE_LOCAL_PROFILE", "YIJIE_MINIMAX_API_KEY", "YIJIE_MINIMAX_API_KEY_FILE",
		"YIJIE_MODEL_PROVIDER", skillBundleRootEnv, skillInstallRootEnv,
	} {
		t.Setenv(key, "")
	}
	t.Setenv("YIJIE_ENV", "local")
	t.Setenv("YIJIE_LOCAL_PROFILE", "demo_fast")
	t.Setenv("YIJIE_MODEL_PROVIDER", codex.MiniMaxProviderID)
	t.Setenv("YIJIE_MINIMAX_API_KEY", "synthetic-test-key")
	t.Setenv("YIJIE_AGENT_HOST_HOME", filepath.Join(t.TempDir(), "host-home"))
	t.Setenv("YIJIE_CODEX_HOME", filepath.Join(t.TempDir(), "codex-home"))
	t.Setenv("YIJIE_FEAT134_STREAMING_ENABLED", "true")
	t.Setenv("YIJIE_FEAT136_COMMAND_TOOL_ITEMS_ENABLED", "true")

	config, err := LoadConfig()
	if err != nil {
		t.Fatalf("load exact FEAT-136 configuration: %v", err)
	}
	if !config.FEAT134StreamingEnabled || !config.FEAT136CommandToolItemsEnabled ||
		config.Runtime.ManagedReasoningProfile != codex.ManagedReasoningProfileHighRaw {
		t.Fatalf("FEAT-136 dependent gates were not wired: %#v", config)
	}
	if config.ImageGenerationEnabled || config.Runtime.DynamicToolsEnabled ||
		codex.NewManager(config.Runtime, nil).Snapshot().ExperimentalAPI {
		t.Fatalf("FEAT-136 enabled an experimental Tool producer: %#v", config.Runtime)
	}
}
