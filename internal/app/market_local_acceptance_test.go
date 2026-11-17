package app

import (
	"github.com/36Dge/yijie-agent-host/internal/codex"
	"os"
	"path/filepath"
	"testing"
)

func TestMarketLocalAcceptanceKeepsAssemblyFlagsConsistent(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	home := filepath.Join(root, ".local", "market-local-acceptance", "normal-run", "host-home")
	manifest := filepath.Join(root, "worker-manifest.json")
	if err := os.WriteFile(manifest, []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	nonce := "15700000-0000-4000-8000-000000000001"
	for key, value := range map[string]string{"YIJIE_ENV": "local", "YIJIE_LOCAL_PROFILE": "demo_fast", "YIJIE_DEVELOPMENT_CLIENT": "true", "YIJIE_MARKET_CONNECTORS_ENABLED": "true", "YIJIE_MARKET_PROVIDER_ONLY": "false", "YIJIE_MARKET_WORKER_MANIFEST": manifest, "YIJIE_MARKET_LOCAL_ACCEPTANCE": "normal-run", "YIJIE_AGENT_HOST_INSTANCE_NONCE": nonce} {
		t.Setenv(key, value)
	}
	runtime := codex.DefaultConfig()
	runtime.RuntimePermissionsEnabled = true
	runtime.ChatModelsEnabled = true
	runtime.KimiK31Candidate = true
	runtime.NativeToolsEnabled = true
	runtime.InteractionToolsEnabled = true
	runtime.ToolSearchEnabled = true
	runtime.BuiltinMediaEnabled = true
	runtime.BuiltinVideoEnabled = true
	runtime.CodexHome = filepath.Join(filepath.Dir(home), "codex-home")
	config := Config{Environment: "local", HostHome: home, ParentPID: os.Getppid(), InstanceNonce: nonce, Runtime: runtime, ImageGenerationEnabled: true}
	if err := loadMarketProfile(&config); err != nil {
		t.Fatal(err)
	}
	if config.Runtime.Market.LocalAcceptance != "normal-run" || config.Runtime.BuiltinMediaEnabled || config.Runtime.BuiltinVideoEnabled || config.ImageGenerationEnabled {
		t.Fatal("media assembly diverged from isolated Runtime configuration")
	}
}
