package codex

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pelletier/go-toml/v2"
)

func TestNativeDailyReasoningPreservesManagedConfigurationAndRollback(t *testing.T) {
	home := canonicalOwnedTempDir(t)
	authority, err := acquireManagedCodexHomeAuthority(home)
	if err != nil {
		t.Fatal(err)
	}
	defer authority.Close()
	trust := []byte("[projects.'/ordinary']\ntrust_level = 'trusted'\n")
	if err := os.WriteFile(filepath.Join(home, "config.toml"), trust, 0600); err != nil {
		t.Fatal(err)
	}
	for _, profile := range []ManagedReasoningProfile{ManagedReasoningProfileDefault, ManagedReasoningProfileNativeHighRaw, ManagedReasoningProfileDefault} {
		args, err := prepareNativeLayeredConfig(authority, profile, false)
		if err != nil {
			t.Fatal(err)
		}
		current, err := os.ReadFile(filepath.Join(home, "config.toml"))
		if err != nil || !bytes.Equal(current, trust) {
			t.Fatal("project trust changed")
		}
		if profile == ManagedReasoningProfileNativeHighRaw {
			var lines []string
			for i := 1; i < len(args); i += 2 {
				lines = append(lines, args[i])
			}
			var values map[string]any
			if toml.Unmarshal([]byte(strings.Join(lines, "\n")), &values) != nil || values["model_reasoning_effort"] != "high" || values["show_raw_agent_reasoning"] != true {
				t.Fatal("native settings missing")
			}
			want, _ := miniMaxManagedConfig(home, ManagedReasoningProfileHighRaw)
			got, _ := miniMaxManagedConfig(home, profile)
			if !bytes.Equal(want, got) {
				t.Fatal("existing high/raw source bytes changed")
			}
		}
	}
	config := DefaultConfig()
	config.BinaryPath = "/ordinary/codex"
	config.ManifestPath = "/ordinary/manifest.json"
	config.CodexHome = home
	config.MiniMax = MiniMaxConfig{Enabled: true, APIKey: "normal-test-placeholder"}
	config.ManagedReasoningProfile = ManagedReasoningProfileNativeHighRaw
	config.DynamicToolsEnabled = true
	if config.validate() == nil {
		t.Fatal("native reasoning without native permissions accepted")
	}
	config.RuntimePermissionsEnabled = true
	if err := config.validate(); err != nil {
		t.Fatal(err)
	}
	config.ManagedReasoningProfile = ManagedReasoningProfileHighRaw
	if config.validate() == nil {
		t.Fatal("legacy experimental-tool exclusion changed")
	}
}
