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
			// An ambient Runtime-only producer gate must not bypass the Host's
			// exact local/demo_fast + FEAT-134/136/137 profile validation.
			t.Setenv("YIJIE_FEAT137_DETERMINISTIC_APPROVAL_PRODUCER", "1")
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

func TestFEAT137D4DeterministicProducerRequiresExactApprovalAuthority(t *testing.T) {
	tests := []struct {
		name            string
		value           string
		commandApproval bool
		want            bool
		wantErr         bool
	}{
		{name: "default off"},
		{name: "explicit false", value: "false", commandApproval: true},
		{name: "exact enabled", value: "true", commandApproval: true, want: true},
		{name: "approval gate off", value: "true", wantErr: true},
		{name: "non exact true", value: "TRUE", commandApproval: true, wantErr: true},
		{name: "numeric true", value: "1", commandApproval: true, wantErr: true},
		{name: "whitespace", value: "true ", commandApproval: true, wantErr: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv(feat137D4DeterministicProducerEnv, test.value)
			enabled, err := loadFEAT137D4DeterministicProducerProfile(test.commandApproval)
			if (err != nil) != test.wantErr || enabled != test.want {
				t.Fatalf("profile got=(%t,%v), want=(%t,err=%t)", enabled, err, test.want, test.wantErr)
			}
		})
	}
}

func TestFEAT137D4DeterministicProducerCannotBypassRejectedProfiles(t *testing.T) {
	canonicalHome := filepath.Join(t.TempDir(), "host-home")
	stableRuntime := codex.DefaultConfig()
	stableRuntime.MiniMax = codex.MiniMaxConfig{Enabled: true, APIKey: "synthetic-test-key"}

	tests := []struct {
		name        string
		environment string
		profile     string
		mutate      func(*codex.Config)
	}{
		{name: "production", environment: "production", profile: "demo_fast"},
		{
			name: "fake provider", environment: "local", profile: "demo_fast",
			mutate: func(config *codex.Config) { config.FakeResponses.Enabled = true },
		},
		{
			name: "dynamic tools", environment: "local", profile: "demo_fast",
			mutate: func(config *codex.Config) { config.DynamicToolsEnabled = true },
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("YIJIE_FEAT137_COMMAND_APPROVAL_ENABLED", "true")
			t.Setenv(feat137D4DeterministicProducerEnv, "true")
			t.Setenv("YIJIE_ENV", test.environment)
			t.Setenv("YIJIE_LOCAL_PROFILE", test.profile)
			runtimeConfig := stableRuntime
			if test.mutate != nil {
				test.mutate(&runtimeConfig)
			}
			commandApproval, err := loadFEAT137CommandApprovalProfile(
				test.environment, canonicalHome, true, true, runtimeConfig,
			)
			if err == nil {
				_, err = loadFEAT137D4DeterministicProducerProfile(commandApproval)
			}
			if err == nil {
				t.Fatal("D4 deterministic producer accepted a rejected FEAT-137 profile")
			}
		})
	}
}

func TestFEAT137RetirementKeepsV4V5AndRejectsApprovalActivation(t *testing.T) {
	for _, key := range []string{
		"YIJIE_AGENT_HOST_HOME", "YIJIE_AGENT_HOST_INSTANCE_NONCE", "YIJIE_AGENT_HOST_PARENT_PID",
		"YIJIE_AGENT_HOST_V2_CLEANUP_ENABLED", "YIJIE_AGENT_HOST_V2_MULTIMODAL_TURNS_ENABLED",
		"YIJIE_AGENT_HOST_V2_RAW_REASONING_ENABLED", "YIJIE_AGENT_HOST_V2_TITLE_ENABLED",
		"YIJIE_AGENT_HOST_V3_ARTIFACTS_ENABLED", "YIJIE_CODEX_BINARY", "YIJIE_CODEX_HOME",
		"YIJIE_CODEX_MANIFEST", "YIJIE_ENV", "YIJIE_FEAT126_S10_TEST_PROFILE_ENABLED",
		"YIJIE_FEAT128_IMAGE_GENERATION_ENABLED", "YIJIE_FEAT128_S10_TEST_PROFILE_ENABLED",
		"YIJIE_FEAT128_SYNTHETIC_ENABLED", "YIJIE_FEAT128_SYNTHETIC_MANIFEST",
		"YIJIE_FEAT134_STREAMING_ENABLED", "YIJIE_FEAT136_COMMAND_TOOL_ITEMS_ENABLED",
		"YIJIE_FEAT137_COMMAND_APPROVAL_ENABLED", feat137D4DeterministicProducerEnv,
		"YIJIE_LOCAL_PROFILE", "YIJIE_MINIMAX_API_KEY",
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
	if disabled.Runtime.DeterministicApprovalProducerEnabled {
		t.Fatalf("FEAT-137 D4 producer was enabled by default: %#v", disabled.Runtime)
	}
	t.Setenv(feat137D4DeterministicProducerEnv, "true")
	if _, err := LoadConfig(); err == nil {
		t.Fatal("FEAT-137 D4 producer bypassed the disabled command approval gate")
	}
	t.Setenv(feat137D4DeterministicProducerEnv, "false")

	t.Setenv("YIJIE_FEAT137_COMMAND_APPROVAL_ENABLED", "true")
	if _, err := LoadConfig(); err == nil {
		t.Fatal("permanently terminated FEAT-137 accepted its former exact opt-in")
	}
	if !disabled.FEAT134StreamingEnabled || !disabled.FEAT136CommandToolItemsEnabled ||
		disabled.Runtime.ManagedReasoningProfile != codex.ManagedReasoningProfileHighRaw {
		t.Fatal("retirement disabled retained v4/v5 capabilities")
	}
	t.Setenv("YIJIE_FEAT137_COMMAND_APPROVAL_ENABLED", "false")
	t.Setenv("YIJIE_FEAT137_DETERMINISTIC_APPROVAL_PRODUCER", "1")
	if _, err := LoadConfig(); err == nil {
		t.Fatal("retired Runtime producer environment was accepted")
	}
}
