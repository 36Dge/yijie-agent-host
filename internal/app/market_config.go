package app

import (
	"errors"
	"os"
	"path/filepath"

	"github.com/36Dge/yijie-agent-host/internal/codex"
)

// Market authority is only assembled for the parent-owned local Native pipe.
// Provider-only mode does not consume model configuration or launch Runtime.
func loadMarketProfile(config *Config) error {
	enabled, err := exactBoolEnv("YIJIE_MARKET_CONNECTORS_ENABLED")
	if err != nil {
		return err
	}
	providerOnly, err := exactBoolEnv("YIJIE_MARKET_PROVIDER_ONLY")
	if err != nil {
		return err
	}
	manifest := os.Getenv("YIJIE_MARKET_WORKER_MANIFEST")
	if !enabled {
		if providerOnly || manifest != "" {
			return errors.New("market configuration requires explicit activation")
		}
		return nil
	}
	if os.Getenv("YIJIE_ENV") != "local" || config.Environment != "local" || os.Getenv("YIJIE_LOCAL_PROFILE") != "demo_fast" || config.ParentPID <= 0 || config.ParentPID != os.Getppid() || !isCanonicalUUID(config.InstanceNonce) || os.Getenv("YIJIE_AGENT_HOST_INSTANCE_NONCE") != config.InstanceNonce {
		return errors.New("market control requires the exact Native-owned local profile")
	}
	if !canonicalAbsolutePath(config.HostHome) || !canonicalAbsolutePath(manifest) {
		return errors.New("market authority paths must be canonical and absolute")
	}
	info, err := os.Lstat(manifest)
	if err != nil || !info.Mode().IsRegular() {
		return errors.New("market worker manifest is unavailable")
	}
	if providerOnly {
		if config.Runtime.MiniMax.Enabled || config.Runtime.ChatModelsEnabled || config.Runtime.RuntimePermissionsEnabled || config.Runtime.FakeResponses.Enabled || config.Runtime.DynamicToolsEnabled || config.Runtime.BinaryPath != "" || config.Runtime.ManifestPath != "" || config.Runtime.CodexHome != "" || config.ScheduledCandidate != nil || config.Skills.Enabled() || config.NativeReasoningEnabled || config.FEAT134StreamingEnabled || config.FEAT136CommandToolItemsEnabled || config.ArtifactV3Enabled {
			return errors.New("market provider-only mode cannot assemble Runtime features")
		}
	} else if !config.Runtime.RuntimePermissionsEnabled || !config.Runtime.ChatModelsEnabled || config.Runtime.FakeResponses.Enabled || config.Runtime.DynamicToolsEnabled {
		return errors.New("market execution requires the existing local model and permission authority")
	}
	config.MarketProviderOnly = providerOnly
	config.Runtime.Market = codex.MarketConfig{Manifest: manifest, LibraryHome: filepath.Join(config.HostHome, "market-worker", "library-home"), HostInstanceID: config.InstanceNonce}
	return nil
}
