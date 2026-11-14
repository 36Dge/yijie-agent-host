package skillmarket

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/36Dge/yijie-agent-host/internal/codex"
	contract "github.com/36Dge/yijie-agent-host/internal/contracts"
	native "github.com/36Dge/yijie-agent-host/internal/contracts/runtimeskillmarket"
	wire "github.com/36Dge/yijie-agent-host/internal/contracts/skillmarketv2"
	legacy "github.com/36Dge/yijie-agent-host/internal/skills"
)

func personalSkill(t *testing.T, s *Service, name string) string {
	t.Helper()
	data := ordinaryMarkdown(name)
	op, err := s.Import(context.Background(), importInput("SKILL.md", data), data)
	if err != nil {
		t.Fatal(err)
	}
	runOperation(t, s, op.OperationId)
	return op.SkillId
}
func readyMarket(s *Service) { s.MarkReady(); s.updateSubmissionReadiness() }
func enabledSkill(t *testing.T, s *Service, id string) bool {
	t.Helper()
	var e entry
	if err := s.get("entries", id, &e); err != nil {
		t.Fatal(err)
	}
	state, err := s.readState(context.Background(), e)
	if err != nil || state.UserEnabled == nil {
		t.Fatal(state, err)
	}
	return *state.UserEnabled
}
func TestFEAT174MissingOldSourceRejectsBeforeNativeUninstall(t *testing.T) {
	s, r, options := nativeService(t)
	old, cfg, _ := ordinaryLegacy(t, s, r, options, 0)
	defer old.Close()
	entries, _ := s.entries()
	e := entries[0]
	before, err := s.readState(context.Background(), e)
	if err != nil {
		t.Fatal(err)
	}
	mutate(t, s, e.ID, wire.MutationKindInstall)
	// Legal initial history fixture: the retained old installation is absent.
	// No source file, permission or existing resource is deleted or damaged.
	m := migration{ID: e.ID, Phase: "complete", OldRoot: filepath.Join(cfg.ManagedRoot, e.ID), OldPath: filepath.Join(cfg.ManagedRoot, e.ID, "SKILL.md"), OldName: "ordinary-historical-skill", Source: e.Selected, DesiredEnabled: true, EffectiveEnabled: true, NativeBefore: &before}
	if err = s.saveMigration(m); err != nil {
		t.Fatal(err)
	}
	config, err := s.userRuleLayer(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err = s.RollbackMigration(context.Background(), old); ErrorCode(err) != "migration_old_source_unavailable" {
		t.Fatal(err)
	}
	after, err := s.readState(context.Background(), e)
	if err != nil || after.CacheState != "valid" || !after.EffectiveEnabled {
		t.Fatal("good native copy removed", err)
	}
	current, _ := s.userRuleLayer(context.Background())
	if !reflect.DeepEqual(config, current) || !s.ready.Load() || s.CompatibleMode() {
		t.Fatal("preflight had side effects")
	}
	if active, err := s.activeMaintenance(); err != nil || active != nil {
		t.Fatal("rejected preflight created maintenance", err)
	}
	t.Log("legal absent legacy source: rollback refused; native installation, rules, mode and readiness unchanged")
}
func TestFEAT174RepeatedRollbackUsesCurrentCycleToggleSnapshot(t *testing.T) {
	s, r, options := nativeService(t)
	old, _, _ := ordinaryLegacy(t, s, r, options, 2)
	defer old.Close()
	migrateLegacy(t, s, old)
	a, b := personalSkill(t, s, "ordinary-cycle-a"), personalSkill(t, s, "ordinary-cycle-b")
	mutate(t, s, a, wire.MutationKindDisable)
	ctx := context.Background()
	if err := s.RollbackMigration(ctx, old); err != nil {
		t.Fatal(err)
	}
	first, err := s.compatibilityCycle()
	if err != nil {
		t.Fatal(err)
	}
	if enabledSkill(t, s, a) || enabledSkill(t, s, b) {
		t.Fatal("compatibility retained enabled personal source")
	}
	if err = s.ResumeMigration(ctx, old); err != nil {
		t.Fatal(err)
	}
	readyMarket(s)
	if enabledSkill(t, s, a) || !enabledSkill(t, s, b) {
		t.Fatal("first intent changed")
	}
	mutate(t, s, a, wire.MutationKindEnable)
	mutate(t, s, b, wire.MutationKindDisable)
	mutate(t, s, b, wire.MutationKindEnable)
	if err = s.RollbackMigration(ctx, old); err != nil {
		t.Fatal("second rollback", err)
	}
	second, err := s.compatibilityCycle()
	if err != nil || first.ID == second.ID {
		t.Fatal("cycle reused", err)
	}
	if enabledSkill(t, s, a) || enabledSkill(t, s, b) {
		t.Fatal("stale false or epoch allowed enabled resource")
	}
	if err = s.ResumeMigration(ctx, old); err != nil {
		t.Fatal("second resume", err)
	}
	readyMarket(s)
	if !enabledSkill(t, s, a) || !enabledSkill(t, s, b) {
		t.Fatal("latest intent lost")
	}
	var retained maintenance
	if err = s.get("maintenance", first.ID, &retained); err != nil || !reflect.DeepEqual(first, retained) {
		t.Fatal("old cycle rewritten", err)
	}
	t.Log("two legacy + two personal; false->true and advanced epoch both preserved across two independent rollback/resume cycles")
}
func TestFEAT174ResumeCheckpointColdStartFinishesPersonalRestore(t *testing.T) {
	s, r, options := nativeService(t)
	old, legacyCfg, _ := ordinaryLegacy(t, s, r, options, 1)
	migrateLegacy(t, s, old)
	personal := personalSkill(t, s, "ordinary-resume-personal")
	ctx := context.Background()
	if err := s.RollbackMigration(ctx, old); err != nil {
		t.Fatal(err)
	}
	// Stop normally at the persisted checkpoint after migration, before toggles.
	err := s.config.Runtime.WithSkillMaintenance(ctx, func(ctx context.Context) error {
		s.serial.Lock()
		defer s.serial.Unlock()
		m, err := s.prepareMaintenance(ctx, old, "resume")
		if err != nil {
			return err
		}
		for m.Phase != "resume_restore" {
			if err = s.advanceMaintenance(ctx, old, m); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !s.CompatibleMode() || s.ready.Load() || enabledSkill(t, s, personal) {
		t.Fatal("mode or admission committed before restore")
	}
	if err = old.Close(); err != nil {
		t.Fatal(err)
	}
	stop, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if err = r.Shutdown(stop); err != nil {
		t.Fatal(err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	value, _ := nativeTestConfigurations.Load(r)
	next := codex.NewManager(value.(codex.Config), nil)
	legacyCfg.Runtime = next
	reader, err := legacy.NewService(legacyCfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		reader.Close()
		stop, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := next.Shutdown(stop); err != nil {
			t.Error(err)
		}
	})
	options.Runtime = next
	reopened, err := Open(options)
	if err != nil {
		t.Fatal(err)
	}
	// Keep the original harness cleanup owning this normally reopened database.
	s.db, s.config.Runtime = reopened.db, next
	s.compatible.Store(reopened.compatible.Load())
	if err = s.PrepareMigration(reader); err != nil {
		t.Fatal(err)
	}
	if err = next.Start(ctx); err != nil {
		t.Fatal(err)
	}
	if err = s.Initialize(ctx); err != nil {
		t.Fatal(err)
	}
	if err = s.Migrate(ctx, reader); err != nil {
		t.Fatal("startup did not continue maintenance", err)
	}
	readyMarket(s)
	if s.CompatibleMode() || !enabledSkill(t, s, personal) {
		t.Fatal("personal restore omitted")
	}
	if active, err := s.activeMaintenance(); err != nil || active != nil {
		t.Fatal("maintenance incomplete", err)
	}
	_, release, err := next.HoldSkillResources(ctx)
	if err != nil {
		t.Fatal(err)
	}
	release()
	t.Log("normal Host-store and Runtime close at resume_restore; standard startup continues journal, restores personal enablement, then opens admission")
}

type cwdRecordingRuntime struct {
	*codex.Manager
	seen []string
}

func (r *cwdRecordingRuntime) ListSkills(ctx context.Context, cwds []string, reload bool) ([]codex.SkillsListEntry, error) {
	r.seen = append(r.seen, cwds...)
	return r.Manager.ListSkills(ctx, cwds, reload)
}
func TestFEAT174CompatibleSelectionUsesNativeWorkingDirectory(t *testing.T) {
	s, r, options := nativeService(t)
	old, cfg, items := ordinaryLegacy(t, s, r, options, 1)
	if err := old.Close(); err != nil {
		t.Fatal(err)
	}
	recording := &cwdRecordingRuntime{Manager: r}
	cfg.Runtime = recording
	reader, err := legacy.NewService(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	migrateLegacy(t, s, reader)
	ctx := context.Background()
	if err = s.RollbackMigration(ctx, reader); err != nil {
		t.Fatal(err)
	}
	readyMarket(s)
	project := filepath.Join(options.HostHome, "ordinary-project")
	projectRoot := filepath.Join(project, ".agents", "skills", "ordinary-project-context")
	if err = os.MkdirAll(projectRoot, 0700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(projectRoot, "SKILL.md"), ordinaryMarkdown("ordinary-project-context"), 0600); err != nil {
		t.Fatal(err)
	}
	actual, err := r.ListSkills(ctx, []string{project}, true)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, group := range actual {
		for _, skill := range group.Skills {
			if skill.Name == "ordinary-project-context" {
				found = true
			}
		}
	}
	if !found {
		t.Fatal("native working-directory discovery missing")
	}
	refs := []contract.SelectedSkillRef{{Id: items[0].ID, Version: items[0].Version}}
	if _, err = s.ResolveSelectedSkillsInDirectory(ctx, refs, project); err != nil {
		t.Fatal(err)
	}
	if len(recording.seen) == 0 || recording.seen[len(recording.seen)-1] != project {
		t.Fatal("legacy resolver discarded actual CWD")
	}
	oldPath := filepath.Join(cfg.ManagedRoot, items[0].ID, "SKILL.md")
	if _, err = r.WriteSkillConfig(ctx, oldPath, false); err != nil {
		t.Fatal(err)
	}
	if _, err = s.ResolveSelectedSkillsInDirectory(ctx, refs, project); err == nil {
		t.Fatal("native user-layer opt-out ignored")
	}
	t.Log("real project-local discovery; legacy selection queries actual CWD; native user-layer opt-out rejected before dispatch; native project rules are not reinterpreted by Host")
}

func TestFEAT174CapacityCountsNativeAndReservationsNotHistory(t *testing.T) {
	states := make([]native.PluginState, 512)
	for i := range states {
		states[i] = native.PluginState{PluginId: fmt.Sprintf("ordinary-%d@yijie-user", i), CacheState: "valid"}
	}
	if n := len(installationSlots(states, []string{states[0].PluginId})); n != 512 {
		t.Fatal(n)
	}
	if n := len(installationSlots(states, []string{"ordinary-new@yijie-user"})); n != 513 {
		t.Fatal(n)
	}
	states[0].CacheState = "absent"
	if n := len(installationSlots(states, []string{"ordinary-new@yijie-user"})); n != 512 {
		t.Fatal(n)
	}
}
func TestFEAT174CatalogCapacityUpgradeRetainsPersonalInstallations(t *testing.T) {
	s, _, _ := nativeService(t)
	ids := []string{personalSkill(t, s, "ordinary-capacity-a"), personalSkill(t, s, "ordinary-capacity-b")}
	// A normal expanded release, generated with the same source materializer and
	// Bundle schema. No installed source or native cache is modified.
	bundleRoot := t.TempDir()
	sources := []wire.CatalogSource{}
	for i := 0; i < 512; i++ {
		id := fmt.Sprintf("yijie.capacity.ordinary-%03d", i)
		source := populateSource(wire.CatalogSource{Id: id, PluginName: "yj-builtin-" + digest([]byte(id))[:24], ContentVersion: "1.0.0", DisplayName: fmt.Sprintf("Ordinary offer %d", i), Description: "Ordinary release capacity validation", Category: "sourcing-selection", IconKey: "skill", RequiredTools: []string{}}, sourceFiles{"SKILL.md": ordinaryMarkdown(fmt.Sprintf("ordinary-offer-%03d", i))})
		if err := materializeSource(bundleRoot, source, sourceFiles{"SKILL.md": ordinaryMarkdown(fmt.Sprintf("ordinary-offer-%03d", i))}); err != nil {
			t.Fatal(err)
		}
		// Producer layout used by prepareBundle is plugins/<name>/skills/main.
		if err := os.MkdirAll(filepath.Join(bundleRoot, "plugins"), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(filepath.Join(bundleRoot, source.SourcePath), filepath.Join(bundleRoot, "plugins", source.PluginName)); err != nil {
			t.Fatal(err)
		}
		source.SourcePath = "plugins/" + source.PluginName
		sources = append(sources, source)
	}
	bundle := wire.Bundle{SchemaVersion: 2, MarketplaceName: "yijie-builtin", Skills: sources}
	raw, err := json.Marshal(bundle)
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(raw, new(wire.Bundle)); err != nil {
		t.Fatal("source schema", err)
	}
	if err = os.WriteFile(filepath.Join(bundleRoot, "presentation.json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	s.config.BundleRoot, s.config.BundleSHA256 = bundleRoot, digest(raw)
	if err = s.prepareBundle(); err != nil {
		t.Fatal(err)
	}
	if err = s.publishCatalog("yijie-builtin"); err != nil {
		t.Fatal(err)
	}
	catalog, err := s.Catalog(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(catalog.Skills) != 514 {
		t.Fatal("expanded release truncated", len(catalog.Skills))
	}
	if err = catalog.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, id := range ids {
		if !enabledSkill(t, s, id) {
			t.Fatal("personal intent changed")
		}
	}
	mutate(t, s, ids[0], wire.MutationKindUninstall)
	extra := personalSkill(t, s, "ordinary-capacity-after-upgrade")
	if !enabledSkill(t, s, extra) {
		t.Fatal("new import rejected by historical entry count")
	}
	var retained entry
	if err = s.get("entries", ids[0], &retained); err != nil {
		t.Fatal("historical identity deleted", err)
	}
	t.Log("512 offered sources + two prior native personal installs => 514 validated visible identities; user installations untouched")
}

func TestFEAT174RollbackPreparedSourcesBeforeNativeSnapshot(t *testing.T) {
	s, r, options := nativeService(t)
	old, _, items := ordinaryLegacy(t, s, r, options, 1)
	defer old.Close()
	if err := s.PrepareMigration(old); err != nil {
		t.Fatal(err)
	}
	if err := s.RollbackMigration(context.Background(), old); err != nil {
		t.Fatal("prepared source rollback", err)
	}
	if !s.CompatibleMode() || s.NativeRoute(items[0].ID) {
		t.Fatal("legacy route not retained")
	}
	listed, err := old.InspectOrdinary(context.Background(), items[0].ID)
	if err != nil || !listed.Enabled || !listed.RuntimeVisible {
		t.Fatal("zero-value snapshot changed legacy intent", err)
	}
	if err = s.ResumeMigration(context.Background(), old); err != nil {
		t.Fatal(err)
	}
	if !enabledSkill(t, s, items[0].ID) {
		t.Fatal("prepared source resume lost enablement")
	}
	t.Log("prepare-only journal rolls back and resumes normally; no unrecorded effective bool is treated as user intent")
}
