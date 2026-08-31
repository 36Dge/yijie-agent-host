package codex

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPrepareMiniMaxCodexHomeWritesSecretFreeManagedConfig(t *testing.T) {
	home := t.TempDir()
	if err := os.Chmod(home, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := prepareMiniMaxCodexHome(home, ManagedReasoningProfileDefault, false); err != nil {
		t.Fatal(err)
	}
	config, err := os.ReadFile(filepath.Join(home, "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(config, []byte(managedConfigMarker)) {
		t.Fatal("managed config marker is missing")
	}
	for _, required := range []string{
		`model = "MiniMax-M3"`,
		`model_reasoning_effort = "none"`,
		`model_reasoning_summary = "none"`,
		`base_url = "https://api.minimaxi.com/v1"`,
		`env_key = "MINIMAX_API_KEY"`,
		`wire_api = "responses"`,
		`requires_openai_auth = false`,
	} {
		if !strings.Contains(string(config), required) {
			t.Fatalf("managed config omitted %q: %s", required, config)
		}
	}
	if strings.Contains(string(config), "test-secret") {
		t.Fatal("managed config contains a secret")
	}
	if strings.Contains(string(config), "show_raw_agent_reasoning") {
		t.Fatal("default managed profile enabled raw reasoning")
	}
	homeInfo, err := os.Stat(home)
	if err != nil {
		t.Fatal(err)
	}
	if homeInfo.Mode().Perm()&0o077 != 0 {
		t.Fatalf("managed CODEX_HOME is accessible to group/other: %o", homeInfo.Mode().Perm())
	}
	for _, name := range []string{"config.toml", managedModelCatalogName} {
		info, err := os.Stat(filepath.Join(home, name))
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm()&0o077 != 0 {
			t.Fatalf("managed file %s is accessible to group/other: %o", name, info.Mode().Perm())
		}
	}
	if err := prepareMiniMaxCodexHome(home, ManagedReasoningProfileDefault, false); err != nil {
		t.Fatalf("managed config should be idempotent: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(home, managedRulesDirectory)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("default-off Runtime profile retained an exec-policy authority: %v", err)
	}
}

func TestPrepareMiniMaxCodexHomeWritesHighRawManagedProfile(t *testing.T) {
	home := t.TempDir()
	if err := prepareMiniMaxCodexHome(home, ManagedReasoningProfileHighRaw, false); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(filepath.Join(home, "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(content)
	for _, required := range []string{
		`model_reasoning_effort = "high"`,
		`model_reasoning_summary = "none"`,
		`show_raw_agent_reasoning = true`,
	} {
		if strings.Count(text, required) != 1 {
			t.Fatalf("high/raw managed config must contain exactly one %q: %s", required, text)
		}
	}
	if strings.Contains(text, `model_reasoning_effort = "none"`) {
		t.Fatal("high/raw managed config retained the default reasoning effort")
	}
}

func TestPrepareMiniMaxCodexHomeRefusesUnmanagedConfig(t *testing.T) {
	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, "config.toml"), []byte("model = \"other\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := prepareMiniMaxCodexHome(home, ManagedReasoningProfileDefault, false); err == nil {
		t.Fatal("expected unmanaged config to be preserved")
	}
	config, err := os.ReadFile(filepath.Join(home, "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if string(config) != "model = \"other\"\n" {
		t.Fatalf("unmanaged config changed: %q", config)
	}
}

func TestPrepareMiniMaxCodexHomeOwnsExactFEAT137ExecPolicyLifecycle(t *testing.T) {
	home := t.TempDir()
	rulesPath := filepath.Join(home, managedRulesDirectory, managedDefaultRulesFile)
	if err := prepareMiniMaxCodexHome(home, ManagedReasoningProfileHighRaw, true); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(rulesPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != managedFEAT137ExecPolicy {
		t.Fatalf("managed FEAT-137 exec policy drifted: %q", content)
	}
	rulesInfo, err := os.Lstat(filepath.Dir(rulesPath))
	if err != nil {
		t.Fatal(err)
	}
	ruleInfo, err := os.Lstat(rulesPath)
	if err != nil {
		t.Fatal(err)
	}
	if rulesInfo.Mode().Perm() != 0o700 || ruleInfo.Mode().Perm() != 0o600 || !ruleInfo.Mode().IsRegular() {
		t.Fatalf("managed FEAT-137 rule authority is not owner-only: dir=%o file=%o",
			rulesInfo.Mode().Perm(), ruleInfo.Mode().Perm())
	}
	if err := prepareMiniMaxCodexHome(home, ManagedReasoningProfileHighRaw, true); err != nil {
		t.Fatalf("managed FEAT-137 exec policy should be idempotent: %v", err)
	}
	if err := prepareMiniMaxCodexHome(home, ManagedReasoningProfileHighRaw, false); err != nil {
		t.Fatalf("disable managed FEAT-137 exec policy: %v", err)
	}
	if _, err := os.Lstat(rulesPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("gate-off retained the FEAT-137 exec policy: %v", err)
	}
	if _, err := os.Lstat(filepath.Dir(rulesPath)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("gate-off retained the managed rules directory: %v", err)
	}
}

func TestPrepareMiniMaxCodexHomeRefusesForeignExecPolicyAuthority(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		t.Run(map[bool]string{false: "gate off", true: "gate on"}[enabled], func(t *testing.T) {
			home := t.TempDir()
			rulesDirectory := filepath.Join(home, managedRulesDirectory)
			if err := os.Mkdir(rulesDirectory, 0o700); err != nil {
				t.Fatal(err)
			}
			rulesPath := filepath.Join(rulesDirectory, managedDefaultRulesFile)
			foreign := []byte(`prefix_rule(pattern=["git"], decision="allow")` + "\n")
			if err := os.WriteFile(rulesPath, foreign, 0o600); err != nil {
				t.Fatal(err)
			}
			if err := prepareMiniMaxCodexHome(home, ManagedReasoningProfileHighRaw, enabled); err == nil {
				t.Fatal("unmanaged Runtime exec policy was accepted")
			}
			content, err := os.ReadFile(rulesPath)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(content, foreign) {
				t.Fatalf("unmanaged Runtime exec policy changed: %q", content)
			}
		})
	}
}

func TestPrepareFakeResponsesCodexHomeUsesFrozenKeylessLoopbackConfig(t *testing.T) {
	home, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(home, 0o700); err != nil {
		t.Fatal(err)
	}
	config := FakeResponsesConfig{
		Enabled: true, BaseURL: FEAT126FakeBaseURL,
		RunID: "123e4567-e89b-42d3-a456-426614174000", FixtureID: FEAT126FakeFixtureID,
	}
	if err := prepareFakeResponsesCodexHome(home, config); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(filepath.Join(home, "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(content)
	for _, required := range []string{
		`model = "MiniMax-M3"`, `model_provider = "minimax"`,
		`base_url = "http://127.0.0.1:18082/v1"`, `wire_api = "responses"`,
		`requires_openai_auth = false`, `show_raw_agent_reasoning = true`,
		`[features]`, `plugins = false`,
		`request_max_retries = 0`, `stream_max_retries = 0`,
		`"X-Yijie-Feat126-Run-Id" = "123e4567-e89b-42d3-a456-426614174000"`,
		`"X-Yijie-Feat126-Fixture-Id" = "normal-000"`,
	} {
		if !strings.Contains(text, required) {
			t.Fatalf("managed fake config omitted %q", required)
		}
	}
	for _, retry := range []string{`request_max_retries = 0`, `stream_max_retries = 0`} {
		if strings.Count(text, retry) != 1 {
			t.Fatalf("managed fake config must contain exactly one %q", retry)
		}
	}
	if strings.Count(text, "[features]") != 1 || strings.Count(text, "plugins = false") != 1 {
		t.Fatal("managed fake config must disable plugins exactly once")
	}
	for _, forbidden := range []string{"env_key", "API_KEY", "Authorization", "https://api.minimaxi.com"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("managed fake config contains forbidden value %q", forbidden)
		}
	}
}

func TestPrepareFakeResponsesCodexHomeDoesNotRepairDirectoryAuthority(t *testing.T) {
	home, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(home, 0o755); err != nil {
		t.Fatal(err)
	}
	config := FakeResponsesConfig{
		Enabled: true, BaseURL: FEAT126FakeBaseURL,
		RunID: "123e4567-e89b-42d3-a456-426614174000", FixtureID: FEAT126FakeFixtureID,
	}
	if err := prepareFakeResponsesCodexHome(home, config); err == nil {
		t.Fatal("FEAT-126 provider repaired and accepted an unsafe CODEX_HOME")
	}
	info, err := os.Lstat(home)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o755 {
		t.Fatalf("FEAT-126 CODEX_HOME permissions changed to %o", info.Mode().Perm())
	}
}

func TestMiniMaxConfigValidation(t *testing.T) {
	if err := (MiniMaxConfig{Enabled: true}).validate(); err == nil {
		t.Fatal("expected missing key to fail")
	}
	if err := (MiniMaxConfig{APIKey: "secret"}).validate(); err == nil {
		t.Fatal("expected disabled provider with key to fail")
	}
	if err := (MiniMaxConfig{Enabled: true, APIKey: "secret\n"}).validate(); err == nil {
		t.Fatal("expected key with newline to fail")
	}
	if err := (MiniMaxConfig{Enabled: true, APIKey: "test-secret"}).validate(); err != nil {
		t.Fatal(err)
	}
}

func TestManagedReasoningProfileValidation(t *testing.T) {
	if err := ManagedReasoningProfileDefault.validate(false, false); err != nil {
		t.Fatalf("default profile should preserve the unconfigured baseline: %v", err)
	}
	if err := ManagedReasoningProfileHighRaw.validate(true, false); err != nil {
		t.Fatalf("valid managed high/raw profile was rejected: %v", err)
	}
	if err := ManagedReasoningProfileHighRaw.validate(false, false); err == nil {
		t.Fatal("high/raw profile accepted a disabled MiniMax provider")
	}
	if err := ManagedReasoningProfileHighRaw.validate(true, true); err == nil {
		t.Fatal("high/raw profile accepted experimental Runtime tools")
	}
	if err := ManagedReasoningProfile(255).validate(true, false); err == nil {
		t.Fatal("unknown managed reasoning profile was accepted")
	}
}

func TestFakeResponsesConfigValidation(t *testing.T) {
	valid := FakeResponsesConfig{
		Enabled: true, BaseURL: FEAT126FakeBaseURL,
		RunID: "123e4567-e89b-42d3-a456-426614174000", FixtureID: FEAT126FakeFixtureID,
	}
	if err := valid.validate(); err != nil {
		t.Fatal(err)
	}
	for _, invalid := range []FakeResponsesConfig{
		{Enabled: true, BaseURL: "http://localhost:18082/v1", RunID: valid.RunID, FixtureID: valid.FixtureID},
		{Enabled: true, BaseURL: valid.BaseURL, FixtureID: valid.FixtureID},
		{Enabled: true, BaseURL: valid.BaseURL, RunID: "019fbd88-cbc3-7bf1-934d-7b05cd693f80", FixtureID: valid.FixtureID},
		{Enabled: true, BaseURL: valid.BaseURL, RunID: "6ba7b810-9dad-11d1-80b4-00c04fd430c8", FixtureID: valid.FixtureID},
		{Enabled: true, BaseURL: valid.BaseURL, RunID: "123e4567-e89b-42d3-4456-426614174003", FixtureID: valid.FixtureID},
		{Enabled: true, BaseURL: valid.BaseURL, RunID: "123E4567-E89B-42D3-A456-426614174003", FixtureID: valid.FixtureID},
		{Enabled: true, BaseURL: valid.BaseURL, RunID: " 123e4567-e89b-42d3-a456-426614174003 ", FixtureID: valid.FixtureID},
		{Enabled: true, BaseURL: valid.BaseURL, RunID: valid.RunID, FixtureID: "other"},
		{RunID: valid.RunID},
	} {
		if err := invalid.validate(); err == nil {
			t.Fatalf("expected invalid fake config to fail: %#v", invalid)
		}
	}
}

func TestRuntimeEnvironmentScopesMiniMaxCredential(t *testing.T) {
	environment := runtimeEnvironment([]string{
		"PATH=/bin",
		"MINIMAX_API_KEY=ambient",
		"YIJIE_MINIMAX_API_KEY=host-secret",
		"YIJIE_MINIMAX_API_KEY_FILE=/secret/path",
	}, "/codex", "scoped-secret")
	joined := strings.Join(environment, "\n")
	if strings.Contains(joined, "ambient") || strings.Contains(joined, "host-secret") || strings.Contains(joined, "/secret/path") {
		t.Fatalf("ambient MiniMax credential leaked: %s", joined)
	}
	if strings.Count(joined, "MINIMAX_API_KEY=scoped-secret") != 1 {
		t.Fatalf("scoped Runtime credential missing: %s", joined)
	}
}
