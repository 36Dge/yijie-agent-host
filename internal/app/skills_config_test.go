package app

import (
	"path/filepath"
	"testing"
)

func TestSkillCapabilitiesRequireExactExplicitLocalDemoFastProfile(t *testing.T) {
	for _, test := range []struct {
		name        string
		environment string
		profile     string
		want        bool
	}{
		{name: "exact", environment: "local", profile: "demo_fast", want: true},
		{name: "missing environment does not inherit local default", environment: "", profile: "demo_fast"},
		{name: "missing profile", environment: "local", profile: ""},
		{name: "profile whitespace", environment: "local", profile: "demo_fast "},
		{name: "production", environment: "production", profile: "demo_fast"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("YIJIE_ENV", test.environment)
			t.Setenv("YIJIE_LOCAL_PROFILE", test.profile)
			t.Setenv(skillBundleRootEnv, "")
			t.Setenv(skillInstallRootEnv, "")
			config, err := loadSkillFeatureConfig()
			if err != nil {
				t.Fatalf("load Skill feature config: %v", err)
			}
			if config.Read != test.want || config.Manage != test.want {
				t.Fatalf("capabilities read=%v manage=%v, want both %v", config.Read, config.Manage, test.want)
			}
		})
	}
}

func TestSkillRootsArePairedCanonicalAndSeparated(t *testing.T) {
	base := t.TempDir()
	bundle := filepath.Join(base, "bundle")
	install := filepath.Join(base, "install")
	t.Setenv("YIJIE_ENV", "local")
	t.Setenv("YIJIE_LOCAL_PROFILE", "demo_fast")

	t.Setenv(skillBundleRootEnv, bundle)
	t.Setenv(skillInstallRootEnv, install)
	config, err := loadSkillFeatureConfig()
	if err != nil {
		t.Fatalf("load roots: %v", err)
	}
	if !config.Configured() || !config.Enabled() || config.BundleRoot != bundle || config.InstallRoot != install {
		t.Fatalf("unexpected roots: %#v", config)
	}
	t.Setenv("YIJIE_ENV", "production")
	config, err = loadSkillFeatureConfig()
	if err != nil || config.Enabled() {
		t.Fatalf("non-local profile enabled Skill Runtime registration: config=%#v err=%v", config, err)
	}
	t.Setenv("YIJIE_ENV", "local")

	t.Setenv(skillInstallRootEnv, "")
	if _, err := loadSkillFeatureConfig(); err == nil {
		t.Fatal("expected incomplete root pair to fail")
	}
	t.Setenv(skillInstallRootEnv, filepath.Join(bundle, "installed"))
	if _, err := loadSkillFeatureConfig(); err == nil {
		t.Fatal("expected overlapping roots to fail")
	}
	t.Setenv(skillInstallRootEnv, install+string(filepath.Separator)+".."+string(filepath.Separator)+"install")
	if _, err := loadSkillFeatureConfig(); err == nil {
		t.Fatal("expected non-canonical root to fail")
	}
}

func TestEnabledSkillsRequireHostBearerAuthority(t *testing.T) {
	config := SkillFeatureConfig{
		BundleRoot:  filepath.Join(t.TempDir(), "bundle"),
		InstallRoot: filepath.Join(t.TempDir(), "install"),
		Read:        true,
		Manage:      true,
	}
	if err := validateSkillHostAuthority(config, ""); err == nil {
		t.Fatal("expected missing Host home to fail")
	}
	if err := validateSkillHostAuthority(config, "relative"); err == nil {
		t.Fatal("expected relative Host home to fail")
	}
	if err := validateSkillHostAuthority(config, filepath.Join(t.TempDir(), "host")); err != nil {
		t.Fatalf("canonical Host home failed: %v", err)
	}
	config.Manage = false
	if err := validateSkillHostAuthority(config, ""); err != nil {
		t.Fatalf("disabled Skill consumer unexpectedly required Host home: %v", err)
	}
}
