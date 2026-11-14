package skillmarket

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/36Dge/yijie-agent-host/internal/codex"
	"github.com/36Dge/yijie-agent-host/internal/session"
	legacy "github.com/36Dge/yijie-agent-host/internal/skills"
	bolt "go.etcd.io/bbolt"
)

// Executes the standard-built desktop-host maintenance command, then opens the
// same data via the normal cold-start sequence. No replacement executable,
// process fault, supplier endpoint or hand-edited database is involved.
func TestFEAT174MaintenanceCommandAndColdCompatibility(t *testing.T) {
	binary := os.Getenv("YIJIE_FEAT174_HOST_BINARY")
	if binary == "" {
		t.Skip("requires canonical desktop-host build")
	}
	s, r, options := nativeService(t)
	old, legacyConfig, items := ordinaryLegacy(t, s, r, options, 2)
	migrateLegacy(t, s, old)
	data := ordinaryMarkdown("ordinary-personal-cold-start")
	op, err := s.Import(context.Background(), importInput("SKILL.md", data), data)
	if err != nil {
		t.Fatal(err)
	}
	runOperation(t, s, op.OperationId)
	store, err := session.OpenStore(options.HostHome)
	if err != nil {
		t.Fatal(err)
	}
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	if err = old.Close(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err = r.Shutdown(ctx); err != nil {
		t.Fatal("normal runtime exit", err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	// Restore a normally opened private DB for nativeService's existing cleanup.
	defer func() {
		var err error
		s.db, err = bolt.Open(filepath.Join(options.HostHome, "skill-market-v2/state.db"), 0600, &bolt.Options{Timeout: time.Second})
		if err != nil {
			t.Error(err)
		}
	}()
	configValue, _ := nativeTestConfigurations.Load(r)
	runtimeConfig := configValue.(codex.Config)
	env := []string{"PATH=" + os.Getenv("PATH"), "HOME=" + os.Getenv("HOME"), "YIJIE_ENV=local", "YIJIE_LOCAL_PROFILE=demo_fast",
		"YIJIE_CODEX_BINARY=" + runtimeConfig.BinaryPath, "YIJIE_CODEX_MANIFEST=" + runtimeConfig.ManifestPath, "YIJIE_CODEX_HOME=" + runtimeConfig.CodexHome,
		"YIJIE_AGENT_HOST_HOME=" + options.HostHome, "YIJIE_MODEL_PROVIDER=minimax", "YIJIE_MINIMAX_API_KEY=ordinary-local-minimax", "YIJIE_KIMI_API_KEY=ordinary-local-kimi",
		"YIJIE_CHAT_MODELS_ENABLED=true", "YIJIE_KIMI_K31_PRO_CANDIDATE=true", "YIJIE_RUNTIME_PERMISSIONS_ENABLED=true", "YIJIE_FEAT169_NATIVE_TOOLS=true", "YIJIE_FEAT169_INTERACTION_TOOLS=true",
		"YIJIE_FEAT174_NATIVE_SKILLS=true", "YIJIE_SKILL_BUNDLE_ROOT=" + legacyConfig.BundleRoot, "YIJIE_SKILL_INSTALL_ROOT=" + legacyConfig.ManagedRoot,
		"YIJIE_NATIVE_SKILL_BUNDLE_ROOT=" + options.BundleRoot, "YIJIE_NATIVE_SKILL_BUNDLE_SHA256=" + options.BundleSHA256, "YIJIE_SKILL_MARKET_ROOT=" + options.DataRoot,
		"YIJIE_PERMISSION_VERIFICATION_BASE_URL=http://127.0.0.1:18083/v1"}
	command := func(action string) {
		t.Helper()
		cmd := exec.Command(binary, "--skill-market-maintenance="+action)
		cmd.Env = env
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatal(action, string(out), err)
		}
		t.Log("normal maintenance command completed:", action)
	}
	cold := func(compatible bool) {
		t.Helper()
		next := codex.NewManager(runtimeConfig, nil)
		legacyConfig.Runtime = next
		reader, err := legacy.NewService(legacyConfig)
		if err != nil {
			t.Fatal("cold v3 reader", err)
		}
		defer reader.Close()
		nextOptions := options
		nextOptions.Runtime = next
		market, err := Open(nextOptions)
		if err != nil {
			t.Fatal(err)
		}
		defer market.Close()
		if err = market.PrepareMigration(reader); err != nil {
			t.Fatal(err)
		}
		if err = next.Start(context.Background()); err != nil {
			t.Fatal("cold runtime", err)
		}
		defer func() {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			if err := next.Shutdown(ctx); err != nil {
				t.Error("normal cold shutdown", err)
			}
		}()
		if err = market.Initialize(context.Background()); err != nil {
			t.Fatal(err)
		}
		if err = market.Migrate(context.Background(), reader); err != nil {
			t.Fatal("cold migration", err)
		}
		if err = market.PrepareOfferUpdates(context.Background()); err != nil {
			t.Fatal(err)
		}
		market.MarkReady()
		market.updateSubmissionReadiness()
		if market.CompatibleMode() != compatible {
			t.Fatal("cold mode differs")
		}
		_, release, err := next.HoldSkillResources(context.Background())
		if err != nil {
			t.Fatal("cold task admission remains blocked", err)
		}
		release()
		catalog, err := market.Catalog(context.Background())
		if err != nil {
			t.Fatal("cold catalog", err)
		}
		found := 0
		for _, skill := range catalog.Skills {
			if skill.Id == items[0].ID || skill.Id == items[1].ID {
				if !skill.Enabled || !skill.RuntimeVisible {
					t.Fatal("legacy installation unavailable after cold start", skill)
				}
				found++
			}
			if skill.Id == op.SkillId {
				if skill.Enabled == compatible {
					t.Fatal("personal enabled intent not preserved")
				}
				found++
			}
		}
		if found != 3 {
			t.Fatal("cold inventory incomplete", found)
		}
		t.Log("cold start verified; compatible =", compatible, "; two old identities and one personal source retained; task admission open")
	}
	command("rollback")
	beforeCold, err := os.ReadFile(filepath.Join(runtimeConfig.CodexHome, "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	cold(true)
	afterCold, err := os.ReadFile(filepath.Join(runtimeConfig.CodexHome, "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(beforeCold, afterCold) {
		t.Fatal("normal cold startup rewrote native user config")
	}
	t.Log("cold startup retained native user config bytes and its CAS version")
	command("resume")
	cold(false)
}
