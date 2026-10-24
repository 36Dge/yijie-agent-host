package codex

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestFEAT164CandidateAdmissionAndFreshConfiguration(t *testing.T) {
	c := DefaultConfig()
	c.ChatModelsEnabled = true
	c.KimiAPIKey = "ordinary-local-fixture"
	c.MiniMax.Enabled = true
	old := NewManager(c, nil)
	if old.ModelAvailable(KimiK31ProfileID) || !old.ModelAvailable("kimi-k3-max-v1") {
		t.Fatal("default model admission changed")
	}
	c.KimiK31Candidate = true
	current := NewManager(c, nil)
	if !current.ModelAvailable(KimiK31ProfileID) || current.ModelAvailable("kimi-k3-max-v1") || !current.ModelAvailable("minimax-m3-high-v1") {
		t.Fatal("candidate admission mismatch")
	}
	home := canonicalOwnedTempDir(t)
	authority := acquireTestCodexHomeAuthority(t, home)
	if _, err := prepareKimiK31Layer(authority); err != nil {
		t.Fatal(err)
	}
	if err := current.validateSelectedChatModelLayer(authority); err != nil {
		t.Fatal(err)
	}
	if _, err := prepareKimiK31Layer(authority); err != nil {
		t.Fatal("normal reopen", err)
	}
	raw, err := os.ReadFile(filepath.Join(home, kimiK31CatalogName))
	if err != nil {
		t.Fatal(err)
	}
	var catalog struct {
		Models []struct {
			Slug string `json:"slug"`
		}
	}
	if json.Unmarshal(raw, &catalog) != nil || len(catalog.Models) != 2 || catalog.Models[0].Slug != "kimi-k3.1-pro" || catalog.Models[1].Slug != "MiniMax-M3" {
		t.Fatal("candidate catalog mismatch")
	}
}

func TestFEAT164ExistingOwnedHomeIsNotImplicitlyMigrated(t *testing.T) {
	home := canonicalOwnedTempDir(t)
	authority := acquireTestCodexHomeAuthority(t, home)
	if _, err := prepareChatModelLayer(authority); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(filepath.Join(home, chatModelsConfigName))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := prepareKimiK31Layer(authority); err == nil {
		t.Fatal("existing home accepted without migration qualification")
	}
	after, _ := os.ReadFile(filepath.Join(home, chatModelsConfigName))
	if string(before) != string(after) {
		t.Fatal("old configuration changed")
	}
	if _, err := os.Stat(filepath.Join(home, kimiK31ConfigName)); !os.IsNotExist(err) {
		t.Fatal("candidate config written")
	}
}
