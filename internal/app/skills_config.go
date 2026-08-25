package app

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

const (
	skillBundleRootEnv  = "YIJIE_SKILL_BUNDLE_ROOT"
	skillInstallRootEnv = "YIJIE_SKILL_INSTALL_ROOT"
)

// SkillFeatureConfig is immutable launch authority for the local Skill
// consumer. Capabilities are derived only from the exact native Desktop
// profile; neither a request nor possession of the Host bearer can grant them.
type SkillFeatureConfig struct {
	BundleRoot  string
	InstallRoot string
	Read        bool
	Manage      bool
}

func (c SkillFeatureConfig) Configured() bool {
	return c.BundleRoot != "" && c.InstallRoot != ""
}

func (c SkillFeatureConfig) Enabled() bool {
	return c.Configured() && c.Read && c.Manage
}

func loadSkillFeatureConfig() (SkillFeatureConfig, error) {
	environment, environmentSet := os.LookupEnv("YIJIE_ENV")
	profile, profileSet := os.LookupEnv("YIJIE_LOCAL_PROFILE")
	exactLocalProfile := environmentSet && profileSet && environment == "local" && profile == "demo_fast"

	bundleRoot := os.Getenv(skillBundleRootEnv)
	installRoot := os.Getenv(skillInstallRootEnv)
	if (bundleRoot == "") != (installRoot == "") {
		return SkillFeatureConfig{}, errors.New("Skill bundle and install roots must be configured together")
	}
	if bundleRoot != "" {
		if !canonicalAbsolutePath(bundleRoot) || !canonicalAbsolutePath(installRoot) {
			return SkillFeatureConfig{}, errors.New("Skill bundle and install roots must be canonical absolute paths")
		}
		if pathsOverlap(bundleRoot, installRoot) {
			return SkillFeatureConfig{}, errors.New("Skill bundle and install roots must not overlap")
		}
	}

	return SkillFeatureConfig{
		BundleRoot:  bundleRoot,
		InstallRoot: installRoot,
		Read:        exactLocalProfile,
		Manage:      exactLocalProfile,
	}, nil
}

func validateSkillHostAuthority(config SkillFeatureConfig, hostHome string) error {
	if config.Enabled() && !canonicalAbsolutePath(hostHome) {
		return errors.New("enabled Skill management requires a canonical absolute Host home")
	}
	return nil
}

func canonicalAbsolutePath(value string) bool {
	return filepath.IsAbs(value) && filepath.Clean(value) == value
}

func pathsOverlap(first, second string) bool {
	return pathContains(first, second) || pathContains(second, first)
}

func pathContains(parent, child string) bool {
	relative, err := filepath.Rel(parent, child)
	if err != nil {
		return false
	}
	return relative == "." || (relative != ".." && !filepath.IsAbs(relative) && !strings.HasPrefix(relative, ".."+string(filepath.Separator)))
}
