package skillmarket

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/36Dge/yijie-agent-host/internal/codex"
	wire "github.com/36Dge/yijie-agent-host/internal/contracts/skillmarketv2"
	legacy "github.com/36Dge/yijie-agent-host/internal/skills"
	"github.com/google/uuid"
)

func ordinaryLegacy(t *testing.T, s *Service, r *codex.Manager, options Config, count int) (*legacy.Service, legacy.Config, []legacy.ManifestSkill) {
	t.Helper()
	bundle := os.Getenv("YIJIE_FEAT174_LEGACY_BUNDLE")
	if bundle == "" {
		t.Skip("requires the ordinary legacy source bundle")
	}
	bundle, err := filepath.EvalSymlinks(bundle)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(bundle, "bundle-manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var manifest legacy.Manifest
	if err = json.Unmarshal(raw, &manifest); err != nil {
		t.Fatal(err)
	}
	cfg := legacy.Config{BundleRoot: bundle, ManagedRoot: filepath.Join(options.HostHome, "legacy-skills"), Runtime: r}
	old, err := legacy.NewService(cfg)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range manifest.Skills[:count] {
		state, err := old.List(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		_, err = old.Install(context.Background(), legacy.InstallInput{OperationID: uuid.NewString(), SkillID: item.ID, ExpectedVersion: item.Version, ExpectedArchiveSHA256: item.Archive.SHA256, CatalogRevision: state.CatalogRevision})
		if err != nil {
			t.Fatal(err)
		}
	}
	return old, cfg, manifest.Skills[:count]
}

func migrateLegacy(t *testing.T, s *Service, old *legacy.Service) {
	t.Helper()
	s.ready.Store(false)
	s.config.Runtime.SetSkillSubmissionReady(false)
	if err := s.PrepareMigration(old); err != nil {
		t.Fatal(err)
	}
	if err := s.Migrate(context.Background(), old); err != nil {
		t.Fatal(err)
	}
	s.MarkReady()
	s.updateSubmissionReadiness()
}

func TestFEAT174RollbackPreservesLaterUserUninstall(t *testing.T) {
	s, r, options := nativeService(t)
	old, cfg, items := ordinaryLegacy(t, s, r, options, 1)
	defer old.Close()
	migrateLegacy(t, s, old)
	mutate(t, s, items[0].ID, wire.MutationKindUninstall)
	before, err := s.userRuleLayer(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err = s.RollbackMigration(context.Background(), old); ErrorCode(err) != "migration_user_intent_changed" {
		t.Fatal("uninstall was not preserved", err)
	}
	after, err := s.userRuleLayer(context.Background())
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatal("rejected rollback changed user rules", err)
	}
	if !s.NativeRoute(items[0].ID) || s.CompatibleMode() || !s.ready.Load() {
		t.Fatal("rejected rollback changed routing or availability")
	}
	var entry entry
	_ = s.get("entries", items[0].ID, &entry)
	state, err := s.readState(context.Background(), entry)
	if err != nil || !absent(state) {
		t.Fatal("uninstalled native resource revived", err)
	}
	listed, err := r.ListSkills(context.Background(), []string{cfg.ManagedRoot}, true)
	if err != nil {
		t.Fatal(err)
	}
	for _, group := range listed {
		for _, skill := range group.Skills {
			if skill.Path == filepath.Join(cfg.ManagedRoot, items[0].ID, "SKILL.md") {
				t.Fatal("legacy root revived")
			}
		}
	}
	if _, err = os.Stat(filepath.Join(cfg.ManagedRoot, items[0].ID, "SKILL.md")); err != nil {
		t.Fatal("retained rollback source was removed", err)
	}
	t.Log("completed user uninstall remains absent; failed rollback changes neither rules nor routing; old bytes retained")
}

func TestFEAT174RollbackReversesGlobalRuleHistory(t *testing.T) {
	s, r, options := nativeService(t)
	old, cfg, items := ordinaryLegacy(t, s, r, options, 2)
	defer old.Close()
	ctx := context.Background()
	for _, item := range items {
		if _, err := r.WriteSkillConfig(ctx, filepath.Join(cfg.ManagedRoot, item.ID, "SKILL.md"), false); err != nil {
			t.Fatal(err)
		}
	}
	before, err := s.userRuleLayer(ctx)
	if err != nil {
		t.Fatal(err)
	}
	migrateLegacy(t, s, old)
	for _, item := range items {
		var e entry
		_ = s.get("entries", item.ID, &e)
		files, err := readVerifiedFiles(filepath.Join(s.root(e.market()), e.Selected.SourcePath, "skills/main"), e.Selected)
		if err != nil {
			t.Fatal(err)
		}
		files["references/ordinary-release-note.md"] = []byte("Ordinary new release note.\n")
		target := e.Selected
		target.ContentVersion = "1.0.1"
		target = populateSource(target, files)
		if err = materializeSource(s.root(e.market()), target, files); err != nil {
			t.Fatal(err)
		}
		e.Offer = &target
		if err = s.saveEntry(e); err != nil {
			t.Fatal(err)
		}
		mutate(t, s, e.ID, wire.MutationKindRefresh)
	}
	if err = s.RollbackMigration(ctx, old); err != nil {
		t.Fatal("global reverse chain", err)
	}
	after, err := s.userRuleLayer(ctx)
	if err != nil || !reflect.DeepEqual(before.Rules, after.Rules) {
		t.Fatal("original user rules not restored", err)
	}
	for _, item := range items {
		if s.NativeRoute(item.ID) {
			t.Fatal("legacy route missing")
		}
	}
	if err = s.ResumeMigration(ctx, old); err != nil {
		t.Fatal("resume after global rollback", err)
	}
	for _, item := range items {
		var e entry
		_ = s.get("entries", item.ID, &e)
		state, err := s.readState(ctx, e)
		if err != nil || len(state.Skills) != 1 || state.Skills[0].Enabled {
			t.Fatal("explicit opt-out lost on resume", err)
		}
	}
	t.Log("two migrations + two native version refreshes + global rule reversal + compatible resume preserve original path opt-outs")
}

func TestFEAT174AttentionImportPreventsCompatibilityRollback(t *testing.T) {
	s, r, options := nativeService(t)
	old, _, _ := ordinaryLegacy(t, s, r, options, 0)
	defer old.Close()
	migrateLegacy(t, s, old)
	data := []byte("---\nname: ordinary-incomplete-draft\n---\nA normal unfinished note.\n")
	ctx := context.Background()
	op, err := s.Import(ctx, importInput("SKILL.md", data), data)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.execute(ctx, op.OperationId); err != nil {
		t.Fatal(err)
	}
	op, err = s.Operation(ctx, op.OperationId)
	if err != nil || op.State != wire.OperationStateNeedsAttention {
		t.Fatal("expected normal native parsing rejection", op, err)
	}
	if err = s.RollbackMigration(ctx, old); ErrorCode(err) != "skill_operations_pending" {
		t.Fatal("attention operation escaped maintenance admission", err)
	}
	if s.CompatibleMode() || !s.ready.Load() {
		t.Fatal("rejected rollback stranded normal service")
	}
	if _, err = s.Cancel(ctx, op.OperationId); err != nil {
		t.Fatal(err)
	}
	if err = s.execute(ctx, op.OperationId); err != nil {
		t.Fatal(err)
	}
	s.updateSubmissionReadiness()
	_, release, err := r.HoldSkillResources(ctx)
	if err != nil {
		t.Fatal("normal cancellation did not release task admission", err)
	}
	release()
	if err = s.RollbackMigration(ctx, old); err != nil {
		t.Fatal("resolved rollback", err)
	}
	if err = s.ResumeMigration(ctx, old); err != nil {
		t.Fatal("resolved resume", err)
	}
	t.Log("ordinary incomplete Markdown -> needs_attention -> rollback rejected without side effects -> normal cancel -> admission restored -> rollback/resume")
}

// Build a genuine next release through both source producers. No installed file,
// receipt, permission or executable is altered to manufacture a failure.
func removedRelease(t *testing.T, removeID string, catalogOnly bool) (string, string) {
	t.Helper()
	root, err := filepath.Abs("../../../yijie-skills")
	if err != nil {
		t.Fatal(err)
	}
	if err = os.MkdirAll(filepath.Join(root, ".local"), 0700); err != nil {
		t.Fatal(err)
	}
	dir, err := os.MkdirTemp(filepath.Join(root, ".local"), "feat174-next-release-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	raw, err := os.ReadFile(filepath.Join(root, "plugins/yijie-desktop-skills/bundle-source.json"))
	if err != nil {
		t.Fatal(err)
	}
	var source map[string]any
	if err = json.Unmarshal(raw, &source); err != nil {
		t.Fatal(err)
	}
	rows := []any{}
	for _, r := range source["skills"].([]any) {
		if r.(map[string]any)["id"] != removeID {
			rows = append(rows, r)
		} else if catalogOnly {
			entry := r.(map[string]any)
			// Catalog-only declarations carry the last producer's source digest;
			// the old packager no longer has an archive from which to derive it.
			previousRaw, readErr := os.ReadFile(filepath.Join(os.Getenv("YIJIE_FEAT174_LEGACY_BUNDLE"), "bundle-manifest.json"))
			if readErr != nil {
				t.Fatal(readErr)
			}
			var previous legacy.Manifest
			if err = json.Unmarshal(previousRaw, &previous); err != nil {
				t.Fatal(err)
			}
			for _, oldEntry := range previous.Skills {
				if oldEntry.ID == removeID {
					entry["provenance"].(map[string]any)["source_sha256"] = oldEntry.Provenance.SourceSHA256
				}
			}
			entry["catalog_entry_mode"] = "catalog-only"
			release := entry["release"].(map[string]any)
			release["catalog_status"] = "blocked"
			release["blocked_reason"] = "maintenance_ended"
			release["maintenance_status"] = "unmaintained"
			license := entry["license"].(map[string]any)
			license["redistribution_status"] = "unverified"
			license["authorization_scope"] = "none"
			rows = append(rows, entry)
		}
	}
	source["skills"] = rows
	input := filepath.Join(dir, "source.json")
	raw, _ = json.Marshal(source)
	if err = os.WriteFile(input, raw, 0600); err != nil {
		t.Fatal(err)
	}
	native, old := filepath.Join(dir, "native"), filepath.Join(dir, "legacy")
	for _, args := range [][]string{{"scripts/package-native-skill-marketplace.mjs", native, input}, {"scripts/package-desktop-skills.mjs", "--source", input, "--output", old}} {
		cmd := exec.Command("node", args...)
		cmd.Dir = root
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatal(string(out), err)
		}
	}
	return native, old
}

func TestFEAT174RetiredLegacyReceiptMigratesAndRollsBack(t *testing.T) {
	for _, catalogOnly := range []bool{false, true} {
		name := "removed"
		if catalogOnly {
			name = "catalog-only"
		}
		t.Run(name, func(t *testing.T) { retiredLegacyReceipt(t, catalogOnly) })
	}
}
func retiredLegacyReceipt(t *testing.T, catalogOnly bool) {
	if os.Getenv("YIJIE_FEAT174_LEGACY_BUNDLE") == "" || os.Getenv("YIJIE_CODEX_INTEGRATION_BINARY") == "" {
		t.Skip("requires pinned Runtime and legacy bundle")
	}
	raw, err := os.ReadFile(filepath.Join(os.Getenv("YIJIE_FEAT174_LEGACY_BUNDLE"), "bundle-manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var prior legacy.Manifest
	if err = json.Unmarshal(raw, &prior); err != nil {
		t.Fatal(err)
	}
	id := prior.Skills[0].ID
	nativeBundle, legacyBundle := removedRelease(t, id, catalogOnly)
	t.Setenv("YIJIE_FEAT174_TEST_BUNDLE", nativeBundle)
	s, r, options := nativeService(t)
	old, cfg, _ := ordinaryLegacy(t, s, r, options, 1)
	if err = old.Close(); err != nil {
		t.Fatal(err)
	}
	cfg.BundleRoot = legacyBundle
	old, err = legacy.NewService(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer old.Close()
	migrateLegacy(t, s, old)
	assertRetired := func() {
		t.Helper()
		catalog, err := s.Catalog(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, skill := range catalog.Skills {
			if skill.Id == id {
				found = true
				if !skill.Retired || skill.InstallationState != wire.InstallStateInstalled {
					t.Fatal("retired installation hidden", skill)
				}
			}
		}
		if !found {
			t.Fatal("receipt inventory omitted")
		}
	}
	assertRetired()
	if err = s.RollbackMigration(context.Background(), old); err != nil {
		t.Fatal(err)
	}
	s.MarkReady()
	s.updateSubmissionReadiness()
	assertRetired()
	if err = s.ResumeMigration(context.Background(), old); err != nil {
		t.Fatal(err)
	}
	s.MarkReady()
	s.updateSubmissionReadiness()
	mutate(t, s, id, wire.MutationKindDisable)
	mutate(t, s, id, wire.MutationKindUninstall)
	catalog, err := s.Catalog(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, skill := range catalog.Skills {
		if skill.Id == id {
			t.Fatal("retired source offered after uninstall")
		}
	}
	t.Log("next release built by canonical producers; retired receipt discovered without native registry; migrate/compatibility rollback/resume/manage/uninstall passed")
}
