package codex

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestNativeProjectAndSkillConfigUsesCanonicalSchema(t *testing.T) {
	content := []byte("[projects.\"/ordinary workspace\"]\ntrust_level = \"trusted\"\n" +
		"[[skills.config]]\npath = \"/ordinary skills/sample/SKILL.md\"\nenabled = false\n" +
		"[[skills.config]]\nname = \"another-sample\"\nenabled = false\n")
	if err := validateNativeProjectConfig(content); err != nil {
		t.Fatal(err)
	}
	// Canonical schema validation must remain effective, without duplicating
	// its SkillConfig fields or enum rules inside the Host.
	if err := validateNativeProjectConfig([]byte("[[skills.config]]\nname = 'sample'\nenabled = 'false'\n")); err == nil {
		t.Fatal("non-boolean native enabled setting accepted")
	}
}

func TestPinnedRuntimeNativeSkillDisableSurvivesRestart(t *testing.T) {
	binary, manifest := integrationArtifactPaths(t)
	root := integrationPrivateTempDir(t)
	home := filepath.Join(root, "runtime")
	workspace := filepath.Join(root, "workspace")
	skillDir := filepath.Join(root, "skills", "ordinary-sample")
	for _, directory := range []string{home, workspace, skillDir} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	skillPath := filepath.Join(skillDir, "SKILL.md")
	if err := os.WriteFile(skillPath, []byte("---\nname: ordinary-sample\ndescription: Explain ordinary arithmetic.\n---\nExplain the requested calculation briefly.\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Error("configuration-only test made a model request")
	}))
	defer server.Close()
	config := integrationConfig(binary, manifest, home, "ordinary-loopback-placeholder")
	config.RuntimePermissionsEnabled, config.ChatModelsEnabled, config.KimiK31Candidate = true, true, true
	config.KimiAPIKey, config.PermissionVerificationBaseURL = "ordinary-loopback-placeholder", server.URL+"/v1"
	ctx := WithModelProfile(context.Background(), KimiK31ProfileID)
	manager := NewManager(config, nil)
	if err := manager.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer shutdownIntegrationManager(t, manager, config.ShutdownTimeout)
	if _, err := manager.StartThread(ctx, workspace); err != nil {
		t.Fatal(err)
	}
	if err := manager.SetSkillsExtraRoots(ctx, []string{filepath.Dir(skillDir)}); err != nil {
		t.Fatal(err)
	}
	if enabled, err := manager.WriteSkillConfig(ctx, skillPath, false); err != nil || enabled {
		t.Fatalf("native disable: enabled=%v error=%v", enabled, err)
	}
	before, err := os.ReadFile(filepath.Join(home, "config.toml"))
	if err != nil || validateNativeProjectConfig(before) != nil {
		t.Fatal("native-generated project/skill state was rejected")
	}
	shutdownIntegrationManager(t, manager, config.ShutdownTimeout)
	reopened := NewManager(config, nil)
	if err := reopened.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer shutdownIntegrationManager(t, reopened, config.ShutdownTimeout)
	after, err := os.ReadFile(filepath.Join(home, "config.toml"))
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("normal restart changed native-owned configuration bytes")
	}
	if err := reopened.SetSkillsExtraRoots(ctx, []string{filepath.Dir(skillDir)}); err != nil {
		t.Fatal(err)
	}
	entries, err := reopened.ListSkills(ctx, []string{workspace}, true)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, entry := range entries {
		for _, skill := range entry.Skills {
			if skill.Path == skillPath {
				found = true
				if skill.Enabled {
					t.Fatal("native disabled state was lost after restart")
				}
			}
		}
	}
	if !found {
		t.Fatal("native skill discovery lost the ordinary sample")
	}
}
