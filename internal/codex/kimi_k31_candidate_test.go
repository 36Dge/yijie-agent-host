package codex

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestNativeSkillGuidanceCatalogUpgradePreservesExistingSettings(t *testing.T) {
	home := canonicalOwnedTempDir(t)
	authority := acquireTestCodexHomeAuthority(t, home)
	prior, err := kimiK31PriorSkillCatalog()
	if err != nil {
		t.Fatal(err)
	}
	current, err := kimiK31Catalog()
	if err != nil {
		t.Fatal(err)
	}
	var oldValue, newValue map[string]any
	if json.Unmarshal(prior, &oldValue) != nil || json.Unmarshal(current, &newValue) != nil {
		t.Fatal("catalogue JSON")
	}
	for _, raw := range newValue["models"].([]any) {
		entry := raw.(map[string]any)
		if entry["include_skills_usage_instructions"] != true {
			t.Fatal("native Skill usage is not enabled")
		}
		delete(entry, "include_skills_usage_instructions")
	}
	oldJSON, _ := json.Marshal(oldValue)
	newJSON, _ := json.Marshal(newValue)
	if !bytes.Equal(oldJSON, newJSON) {
		t.Fatal("unrelated model settings changed")
	}
	writeS1File(t, home, kimiK31CatalogName, prior)
	config := kimiK31Config(home)
	writeS1File(t, home, kimiK31ConfigName, config)
	trust := []byte("# ordinary existing user configuration\n")
	writeS1File(t, home, "config.toml", trust)
	for i := 0; i < 2; i++ {
		if _, err = prepareKimiK31Layer(authority); err != nil {
			t.Fatal("normal catalogue upgrade/reopen", err)
		}
	}
	for name, want := range map[string][]byte{kimiK31CatalogName: current, kimiK31ConfigName: config, "config.toml": trust} {
		got, err := os.ReadFile(filepath.Join(home, name))
		if err != nil || !bytes.Equal(got, want) {
			t.Fatal("catalogue upgrade changed unrelated content", name, err)
		}
	}
	if _, err = authority.preflightManagedFile(kimiK31CatalogName, prior, prior); err == nil {
		t.Fatal("old exact writer accepted new metadata without review")
	}
}

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
