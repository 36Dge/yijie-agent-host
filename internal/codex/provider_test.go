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
	home := canonicalOwnedTempDir(t)
	authority := acquireTestCodexHomeAuthority(t, home)
	if err := prepareMiniMaxCodexHome(authority, ManagedReasoningProfileDefault); err != nil {
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
	if err := prepareMiniMaxCodexHome(authority, ManagedReasoningProfileDefault); err != nil {
		t.Fatalf("managed config should be idempotent: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(home, managedRulesDirectory)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("default-off Runtime profile retained an exec-policy authority: %v", err)
	}
}

func TestMiniMaxManagedConfigGateOffPreservesBaselineBytes(t *testing.T) {
	const codexHome = "/managed/codex-home"
	wantDefault := managedConfigMarker + `model = "MiniMax-M3"
model_provider = "minimax"
model_context_window = 1000000
model_reasoning_effort = "none"
model_reasoning_summary = "none"
model_catalog_json = "/managed/codex-home/minimax-m3-model-catalog.json"

[model_providers.minimax]
name = "MiniMax"
base_url = "https://api.minimaxi.com/v1"
env_key = "MINIMAX_API_KEY"
wire_api = "responses"
requires_openai_auth = false
supports_websockets = false
`
	wantHighRaw := managedConfigMarker + `model = "MiniMax-M3"
model_provider = "minimax"
model_context_window = 1000000
model_reasoning_effort = "high"
model_reasoning_summary = "none"
show_raw_agent_reasoning = true
model_catalog_json = "/managed/codex-home/minimax-m3-model-catalog.json"

[model_providers.minimax]
name = "MiniMax"
base_url = "https://api.minimaxi.com/v1"
env_key = "MINIMAX_API_KEY"
wire_api = "responses"
requires_openai_auth = false
supports_websockets = false
`
	for _, test := range []struct {
		name    string
		profile ManagedReasoningProfile
		want    string
	}{
		{name: "default", profile: ManagedReasoningProfileDefault, want: wantDefault},
		{name: "high raw", profile: ManagedReasoningProfileHighRaw, want: wantHighRaw},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := miniMaxManagedConfig(codexHome, test.profile)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, []byte(test.want)) {
				t.Fatalf("gate-off managed config bytes drifted:\n%s", got)
			}
			if strings.Contains(string(got), "[features]") {
				t.Fatal("gate-off managed config added a feature section")
			}
		})
	}
}

func TestFEAT137MiniMaxManagedConfigClosesAmbientProductSurfacesExactly(t *testing.T) {
	home := canonicalOwnedTempDir(t)
	authority := acquireTestCodexHomeAuthority(t, home)
	if err := prepareMiniMaxCodexHomeForAuthority(authority, ManagedReasoningProfileHighRaw, true); err != nil {
		t.Fatal(err)
	}
	first, err := os.ReadFile(filepath.Join(home, "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if err := validateFEAT137ManagedConfig(first, true); err != nil {
		t.Fatalf("validate fresh FEAT-137 config: %v", err)
	}
	for _, required := range []string{
		"[features]",
		"hooks = false",
		"plugins = false",
		"apps = false",
		"tool_suggest = false",
		"shell_snapshot = false",
		"request_max_retries = 0",
		"stream_max_retries = 0",
	} {
		if strings.Count(string(first), required) != 1 {
			t.Fatalf("FEAT-137 managed config occurrence count for %q is not one", required)
		}
	}
	for _, forbidden := range []string{"[mcp_servers", "[plugins", "[apps", "dynamic_tools"} {
		if strings.Contains(string(first), forbidden) {
			t.Fatalf("FEAT-137 managed config contains forbidden product surface %q", forbidden)
		}
	}
	if err := prepareMiniMaxCodexHomeForAuthority(authority, ManagedReasoningProfileHighRaw, true); err != nil {
		t.Fatalf("fresh FEAT-137 config was not idempotent: %v", err)
	}
	second, err := os.ReadFile(filepath.Join(home, "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, second) {
		t.Fatal("idempotent FEAT-137 config preparation changed bytes")
	}
	if err := prepareMiniMaxCodexHome(authority, ManagedReasoningProfileHighRaw); err != nil {
		t.Fatalf("return to gate-off managed profile: %v", err)
	}
	gateOff, err := os.ReadFile(filepath.Join(home, "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	wantGateOff, err := miniMaxManagedConfig(home, ManagedReasoningProfileHighRaw)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(gateOff, wantGateOff) {
		t.Fatal("gate-off transition did not restore exact baseline bytes")
	}
}

func TestFEAT137ManagedConfigValidationRejectsIncompleteClosedAuthority(t *testing.T) {
	config, err := miniMaxManagedConfigForAuthority("/managed/codex-home", ManagedReasoningProfileHighRaw, true)
	if err != nil {
		t.Fatal(err)
	}
	for _, missing := range []string{
		"hooks = false\n",
		"plugins = false\n",
		"apps = false\n",
		"tool_suggest = false\n",
		"shell_snapshot = false\n",
		"request_max_retries = 0\n",
		"stream_max_retries = 0\n",
	} {
		t.Run(strings.Fields(missing)[0], func(t *testing.T) {
			incomplete := bytes.Replace(config, []byte(missing), nil, 1)
			if err := validateFEAT137ManagedConfig(incomplete, true); err == nil {
				t.Fatalf("approval config without %q was accepted", strings.TrimSpace(missing))
			}
		})
	}
}

func TestFEAT137ManagedConfigValidationRejectsRetryExpansion(t *testing.T) {
	config, err := miniMaxManagedConfigForAuthority("/managed/codex-home", ManagedReasoningProfileHighRaw, true)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name   string
		mutate func([]byte) []byte
	}{
		{
			name: "request retry nonzero",
			mutate: func(input []byte) []byte {
				return bytes.Replace(input, []byte("request_max_retries = 0"), []byte("request_max_retries = 1"), 1)
			},
		},
		{
			name: "stream retry nonzero",
			mutate: func(input []byte) []byte {
				return bytes.Replace(input, []byte("stream_max_retries = 0"), []byte("stream_max_retries = 1"), 1)
			},
		},
		{
			name: "duplicate retry authority",
			mutate: func(input []byte) []byte {
				return append(input, []byte("request_max_retries = 0\n")...)
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			if err := validateFEAT137ManagedConfig(test.mutate(append([]byte(nil), config...)), true); err == nil {
				t.Fatal("approval config accepted expanded Provider retry authority")
			}
		})
	}
}

func TestPrepareMiniMaxCodexHomeWritesHighRawManagedProfile(t *testing.T) {
	home := canonicalOwnedTempDir(t)
	authority := acquireTestCodexHomeAuthority(t, home)
	if err := prepareMiniMaxCodexHome(authority, ManagedReasoningProfileHighRaw); err != nil {
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

func TestFEAT137ExecPolicyRejectsGateOffManagedConfig(t *testing.T) {
	home := canonicalOwnedTempDir(t)
	authority := acquireTestCodexHomeAuthority(t, home)
	if err := prepareMiniMaxCodexHome(authority, ManagedReasoningProfileHighRaw); err != nil {
		t.Fatal(err)
	}
	plan, err := authority.preflightFEAT137ExecPolicy()
	if err != nil {
		t.Fatal(err)
	}
	if err := authority.applyFEAT137ExecPolicy(true, plan); err == nil ||
		!strings.Contains(err.Error(), "closed managed MiniMax config") {
		t.Fatalf("FEAT-137 exec policy accepted the gate-off managed config: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(home, managedRulesDirectory)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("rejected exec policy mutated the managed rules directory: %v", err)
	}
}

func TestPrepareMiniMaxCodexHomeRefusesUnmanagedConfig(t *testing.T) {
	home := canonicalOwnedTempDir(t)
	if err := os.WriteFile(filepath.Join(home, "config.toml"), []byte("model = \"other\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	authority := acquireTestCodexHomeAuthority(t, home)
	if err := prepareMiniMaxCodexHome(authority, ManagedReasoningProfileDefault); err == nil {
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
	home := canonicalOwnedTempDir(t)
	authority := acquireTestCodexHomeAuthority(t, home)
	rulesPath := filepath.Join(home, managedRulesDirectory, managedFEAT137RulesFile)
	if err := prepareMiniMaxCodexHomeForAuthority(authority, ManagedReasoningProfileHighRaw, true); err != nil {
		t.Fatal(err)
	}
	plan, err := authority.preflightFEAT137ExecPolicy()
	if err != nil {
		t.Fatal(err)
	}
	if err := authority.applyFEAT137ExecPolicy(true, plan); err != nil {
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
	plan, err = authority.preflightFEAT137ExecPolicy()
	if err != nil {
		t.Fatal(err)
	}
	if err := authority.applyFEAT137ExecPolicy(true, plan); err != nil {
		t.Fatalf("managed FEAT-137 exec policy should be idempotent: %v", err)
	}
	plan, err = authority.preflightFEAT137ExecPolicy()
	if err != nil {
		t.Fatal(err)
	}
	if err := authority.applyFEAT137ExecPolicy(false, plan); err != nil {
		t.Fatalf("disable managed FEAT-137 exec policy: %v", err)
	}
	if _, err := os.Lstat(rulesPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("gate-off retained the FEAT-137 exec policy: %v", err)
	}
	rulesDirectoryInfo, err := os.Lstat(filepath.Dir(rulesPath))
	if err != nil || !rulesDirectoryInfo.IsDir() || rulesDirectoryInfo.Mode().Perm() != 0o700 {
		t.Fatalf("gate-off did not retain only an inert owner-only rules directory: info=%v err=%v", rulesDirectoryInfo, err)
	}
	entries, err := os.ReadDir(filepath.Dir(rulesPath))
	if err != nil || len(entries) != 0 {
		t.Fatalf("gate-off rules directory is not inert: entries=%v err=%v", entries, err)
	}
}

func TestPrepareMiniMaxCodexHomeRefusesForeignExecPolicyAuthority(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		t.Run(map[bool]string{false: "gate off", true: "gate on"}[enabled], func(t *testing.T) {
			home := canonicalOwnedTempDir(t)
			rulesDirectory := filepath.Join(home, managedRulesDirectory)
			if err := os.Mkdir(rulesDirectory, 0o700); err != nil {
				t.Fatal(err)
			}
			rulesPath := filepath.Join(rulesDirectory, managedFEAT137RulesFile)
			foreign := []byte(`prefix_rule(pattern=["git"], decision="allow")` + "\n")
			if err := os.WriteFile(rulesPath, foreign, 0o600); err != nil {
				t.Fatal(err)
			}
			authority, err := acquireManagedCodexHomeAuthority(home)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := authority.preflightFEAT137ExecPolicy(); err == nil {
				t.Fatal("unmanaged Runtime exec policy was accepted")
			}
			if err := authority.Close(); err == nil {
				t.Fatal("foreign Runtime exec policy cleanup did not fail closed")
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
	home := canonicalOwnedTempDir(t)
	authority := acquireTestCodexHomeAuthority(t, home)
	config := FakeResponsesConfig{
		Enabled: true, BaseURL: FEAT126FakeBaseURL,
		RunID: "123e4567-e89b-42d3-a456-426614174000", FixtureID: FEAT126FakeFixtureID,
	}
	if err := prepareFakeResponsesCodexHome(authority, config); err != nil {
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
	if _, err := acquireManagedCodexHomeAuthority(home); err == nil {
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

func canonicalOwnedTempDir(t *testing.T) string {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	home := filepath.Join(root, "managed-codex-home")
	if err := os.Mkdir(home, 0o700); err != nil {
		t.Fatal(err)
	}
	return home
}

func acquireTestCodexHomeAuthority(t *testing.T, home string) *managedCodexHomeAuthority {
	t.Helper()
	authority, err := acquireManagedCodexHomeAuthority(home)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := authority.Close(); err != nil {
			t.Errorf("close managed CODEX_HOME authority: %v", err)
		}
	})
	return authority
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
	}, "/codex", "scoped-secret", false)
	joined := strings.Join(environment, "\n")
	if strings.Contains(joined, "ambient") || strings.Contains(joined, "host-secret") || strings.Contains(joined, "/secret/path") {
		t.Fatalf("ambient MiniMax credential leaked: %s", joined)
	}
	if strings.Count(joined, "MINIMAX_API_KEY=scoped-secret") != 1 {
		t.Fatalf("scoped Runtime credential missing: %s", joined)
	}
}
