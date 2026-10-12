package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/36Dge/yijie-agent-host/internal/codex"
	"github.com/google/uuid"
)

func TestFEAT158ExpertLaunchIsExplicitAndRequiresExistingNativeProfile(t *testing.T) {
	t.Setenv("YIJIE_EXPERTS_ENABLED", "")
	config := Config{}
	if e := loadExpertProfile(&config); e != nil || config.ExpertsEnabled {
		t.Fatal("default launch enabled expert writer", e)
	}
	t.Setenv("YIJIE_EXPERTS_ENABLED", "true")
	if e := loadExpertProfile(&config); e == nil {
		t.Fatal("expert launch ignored required Native profile")
	}
}
func TestFEAT158ExpertLaunchConsumesOnlyReviewedCandidateManifest(t *testing.T) {
	bundle := os.Getenv("YIJIE_EXPERT_TEST_BUNDLE")
	if bundle == "" {
		t.Skip("actual candidate not supplied")
	}
	slot := filepath.Join(t.TempDir(), "skill-packages-experts")
	if e := os.Mkdir(slot, 0700); e != nil {
		t.Fatal(e)
	}
	manifest, e := os.ReadFile(filepath.Join(bundle, "bundle-manifest.json"))
	if e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(filepath.Join(slot, "bundle-manifest.json"), manifest, 0600); e != nil {
		t.Fatal(e)
	}
	instance := uuid.NewString()
	t.Setenv("YIJIE_EXPERTS_ENABLED", "true")
	t.Setenv("YIJIE_ENV", "local")
	t.Setenv("YIJIE_LOCAL_PROFILE", "demo_fast")
	t.Setenv("YIJIE_AGENT_HOST_INSTANCE_NONCE", instance)
	config := Config{Environment: "local", ParentPID: os.Getppid(), InstanceNonce: instance, Runtime: codex.Config{RuntimePermissionsEnabled: true, ChatModelsEnabled: true, Market: codex.MarketConfig{Manifest: "/reviewed/manifest"}}, Skills: SkillFeatureConfig{BundleRoot: slot, InstallRoot: filepath.Join(t.TempDir(), "installed"), Read: true, Manage: true}}
	if e = loadExpertProfile(&config); e != nil || !config.ExpertsEnabled {
		t.Fatal("reviewed local candidate gate", e)
	}
}
