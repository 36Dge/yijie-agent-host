package app

import (
	"errors"
	"os"
	"path/filepath"

	"github.com/36Dge/yijie-agent-host/internal/skills"
)

// This gate only assembles the reviewed local candidate. It cannot be used as
// request authority: every private command still checks the original Native pipe
// hello, current instance/epoch/scope, and the appropriate admission permission.
func loadExpertProfile(config *Config) error {
	enabled, e := exactBoolEnv("YIJIE_EXPERTS_ENABLED")
	if e != nil {
		return e
	}
	if !enabled {
		return nil
	}
	if os.Getenv("YIJIE_ENV") != "local" || config.Environment != "local" || os.Getenv("YIJIE_LOCAL_PROFILE") != "demo_fast" || config.ParentPID <= 0 || config.ParentPID != os.Getppid() || !isCanonicalUUID(config.InstanceNonce) || os.Getenv("YIJIE_AGENT_HOST_INSTANCE_NONCE") != config.InstanceNonce || config.MarketProviderOnly || config.Runtime.Market.Manifest == "" || !config.Runtime.RuntimePermissionsEnabled || !config.Runtime.ChatModelsEnabled || config.Runtime.FakeResponses.Enabled || config.Runtime.DynamicToolsEnabled || !config.Skills.Enabled() {
		return errors.New("experts require the exact Native-owned local model, market and managed resources profile")
	}
	if filepath.Base(config.Skills.BundleRoot) != "skill-packages-experts" {
		return errors.New("experts require the separate reviewed candidate resource slot")
	}
	if e := skills.VerifyExpertCandidate(config.Skills.BundleRoot); e != nil {
		return errors.New("reviewed expert candidate resources are unavailable")
	}
	config.ExpertsEnabled = true
	return nil
}
