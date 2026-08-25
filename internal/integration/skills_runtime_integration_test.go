package integration

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/36Dge/yijie-agent-host/internal/codex"
	"github.com/36Dge/yijie-agent-host/internal/skills"
)

func TestPinnedRuntimeManagedSkillLifecycle(t *testing.T) {
	binaryPath := os.Getenv("YIJIE_CODEX_INTEGRATION_BINARY")
	manifestPath := os.Getenv("YIJIE_CODEX_INTEGRATION_MANIFEST")
	if binaryPath == "" || manifestPath == "" {
		t.Skip("set YIJIE_CODEX_INTEGRATION_BINARY and YIJIE_CODEX_INTEGRATION_MANIFEST")
	}
	bundleRoot, raw, manifest := copySkillFixtureBundle(t, "manifest-valid.json", "fixture-model-only-0.1.0.zip")
	managedRoot := filepath.Join(t.TempDir(), "managed")
	if err := os.Mkdir(managedRoot, 0o700); err != nil {
		t.Fatalf("create managed root: %v", err)
	}
	managedRoot, err := filepath.EvalSymlinks(managedRoot)
	if err != nil {
		t.Fatalf("canonicalize managed root: %v", err)
	}
	orphanRoot := filepath.Join(managedRoot, "orphan")
	if err := os.Mkdir(orphanRoot, 0o700); err != nil {
		t.Fatalf("create unmanaged Skill root: %v", err)
	}
	if err := os.WriteFile(filepath.Join(orphanRoot, "SKILL.md"), []byte("---\nname: orphan\ndescription: unmanaged fixture\n---\nunmanaged\n"), 0o600); err != nil {
		t.Fatalf("write unmanaged Skill: %v", err)
	}
	codexHome := t.TempDir()

	firstManager, firstService := startPinnedSkillsRuntime(t, binaryPath, manifestPath, codexHome, bundleRoot, managedRoot)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	initial, err := firstService.List(ctx)
	if err != nil {
		t.Fatalf("list with pinned Runtime: %v", err)
	}
	assertFixtureState(t, initial, "not_installed", false, false)
	assertPinnedRuntimeManagedPaths(t, ctx, firstManager, managedRoot, nil)
	installed, err := firstService.Install(ctx, installInputForFixture(raw, manifest, "019fbd88-cbc3-7bf1-934d-7b05cd693f81"))
	if err != nil || installed.InstallationStatus != "installed" || !installed.RuntimeVisible {
		t.Fatalf("install with pinned Runtime: state=%#v err=%v", installed, err)
	}
	assertPinnedRuntimeManagedPaths(t, ctx, firstManager, managedRoot, []string{filepath.Join(managedRoot, fixtureSkillID, "SKILL.md")})
	disabled, err := firstService.SetEnabled(ctx, skills.EnabledInput{
		OperationID: "019fbd88-cbc3-7bf1-934d-7b05cd693f82",
		SkillID:     fixtureSkillID,
		Enabled:     false,
	})
	if err != nil || !isInstalledButInvisible(disabled) {
		t.Fatalf("disable with pinned Runtime: state=%#v err=%v", disabled, err)
	}
	shutdownPinnedSkillsRuntime(t, firstManager, firstService)

	secondManager, secondService := startPinnedSkillsRuntime(t, binaryPath, manifestPath, codexHome, bundleRoot, managedRoot)
	defer shutdownPinnedSkillsRuntime(t, secondManager, secondService)
	replayed, err := secondService.List(ctx)
	if err != nil {
		t.Fatalf("replay disabled state with restarted Runtime: %v", err)
	}
	assertFixtureState(t, replayed, "installed", false, false)
	enabled, err := secondService.SetEnabled(ctx, skills.EnabledInput{
		OperationID: "019fbd88-cbc3-7bf1-934d-7b05cd693f83",
		SkillID:     fixtureSkillID,
		Enabled:     true,
	})
	if err != nil || !enabled.RuntimeVisible {
		t.Fatalf("re-enable with restarted Runtime: state=%#v err=%v", enabled, err)
	}
	uninstalled, err := secondService.Uninstall(ctx, skills.UninstallInput{
		OperationID: "019fbd88-cbc3-7bf1-934d-7b05cd693f84",
		SkillID:     fixtureSkillID,
	})
	if err != nil || uninstalled.InstallationStatus != "not_installed" || uninstalled.RuntimeVisible {
		t.Fatalf("uninstall with pinned Runtime: state=%#v err=%v", uninstalled, err)
	}
}

func assertPinnedRuntimeManagedPaths(
	t *testing.T,
	ctx context.Context,
	manager *codex.Manager,
	managedRoot string,
	want []string,
) {
	t.Helper()
	entries, err := manager.ListSkills(ctx, []string{managedRoot}, true)
	if err != nil {
		t.Fatalf("list pinned Runtime managed projection: %v", err)
	}
	paths := make([]string, 0, len(want))
	for _, entry := range entries {
		for _, skill := range entry.Skills {
			relative, relErr := filepath.Rel(managedRoot, skill.Path)
			if relErr == nil && relative != ".." && !filepath.IsAbs(relative) &&
				!strings.HasPrefix(relative, ".."+string(filepath.Separator)) && skill.Enabled {
				paths = append(paths, skill.Path)
			}
		}
	}
	slices.Sort(paths)
	want = append([]string(nil), want...)
	slices.Sort(want)
	if !slices.Equal(paths, want) {
		t.Fatalf("managed Runtime paths=%v, want %v", paths, want)
	}
}

func startPinnedSkillsRuntime(
	t *testing.T,
	binaryPath, manifestPath, codexHome, bundleRoot, managedRoot string,
) (*codex.Manager, *skills.Service) {
	t.Helper()
	config := codex.DefaultConfig()
	config.BinaryPath = binaryPath
	config.ManifestPath = manifestPath
	config.CodexHome = codexHome
	config.StartupTimeout = 20 * time.Second
	config.RequestTimeout = 10 * time.Second
	config.ShutdownTimeout = 10 * time.Second
	manager := codex.NewManager(config, nil)
	service, err := skills.NewService(skills.Config{BundleRoot: bundleRoot, ManagedRoot: managedRoot, Runtime: manager})
	if err != nil {
		t.Fatalf("open managed Skill service: %v", err)
	}
	if err := manager.SetNotificationHandler(func(method string, params json.RawMessage) {
		if method == codex.RuntimeNotificationSkillsChanged {
			_ = service.HandleRuntimeNotification(params)
		}
	}); err != nil {
		service.Close()
		t.Fatalf("set pinned Runtime notification handler: %v", err)
	}
	if err := manager.Start(context.Background()); err != nil {
		service.Close()
		t.Fatalf("start pinned Runtime: %v", err)
	}
	return manager, service
}

func shutdownPinnedSkillsRuntime(t *testing.T, manager *codex.Manager, service *skills.Service) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := manager.Shutdown(ctx); err != nil {
		t.Errorf("shutdown pinned Runtime: %v", err)
	}
	if err := service.Close(); err != nil {
		t.Errorf("close Skill service: %v", err)
	}
}

func installInputForFixture(raw []byte, manifest fixtureCatalog, operationID string) skills.InstallInput {
	digest := sha256.Sum256(raw)
	return skills.InstallInput{
		OperationID:           operationID,
		SkillID:               manifest.Skills[0].ID,
		ExpectedVersion:       manifest.Skills[0].Version,
		ExpectedArchiveSHA256: manifest.Skills[0].Archive.SHA256,
		CatalogRevision:       hex.EncodeToString(digest[:]),
	}
}
