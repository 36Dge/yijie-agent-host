package codex

import (
	"bytes"
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
	if err := prepareMiniMaxCodexHome(home); err != nil {
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
	if err := prepareMiniMaxCodexHome(home); err != nil {
		t.Fatalf("managed config should be idempotent: %v", err)
	}
}

func TestPrepareMiniMaxCodexHomeRefusesUnmanagedConfig(t *testing.T) {
	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, "config.toml"), []byte("model = \"other\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := prepareMiniMaxCodexHome(home); err == nil {
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

func TestPrepareFakeResponsesCodexHomeUsesFrozenKeylessLoopbackConfig(t *testing.T) {
	home := t.TempDir()
	config := FakeResponsesConfig{
		Enabled: true, BaseURL: FEAT126FakeBaseURL,
		RunID: "019fbd88-cbc3-7bf1-934d-7b05cd693f80", FixtureID: FEAT126FakeFixtureID,
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
		`"X-Yijie-Feat126-Run-Id" = "019fbd88-cbc3-7bf1-934d-7b05cd693f80"`,
		`"X-Yijie-Feat126-Fixture-Id" = "normal-000"`,
	} {
		if !strings.Contains(text, required) {
			t.Fatalf("managed fake config omitted %q", required)
		}
	}
	for _, forbidden := range []string{"env_key", "API_KEY", "Authorization", "https://api.minimaxi.com"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("managed fake config contains forbidden value %q", forbidden)
		}
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

func TestFakeResponsesConfigValidation(t *testing.T) {
	valid := FakeResponsesConfig{
		Enabled: true, BaseURL: FEAT126FakeBaseURL,
		RunID: "019fbd88-cbc3-7bf1-934d-7b05cd693f80", FixtureID: FEAT126FakeFixtureID,
	}
	if err := valid.validate(); err != nil {
		t.Fatal(err)
	}
	for _, invalid := range []FakeResponsesConfig{
		{Enabled: true, BaseURL: "http://localhost:18082/v1", RunID: valid.RunID, FixtureID: valid.FixtureID},
		{Enabled: true, BaseURL: valid.BaseURL, FixtureID: valid.FixtureID},
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
