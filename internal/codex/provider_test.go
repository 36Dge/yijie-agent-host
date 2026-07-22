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
