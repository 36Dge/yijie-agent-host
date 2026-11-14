package skillmarket

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/36Dge/yijie-agent-host/internal/codex"
	contract "github.com/36Dge/yijie-agent-host/internal/contracts"
	wire "github.com/36Dge/yijie-agent-host/internal/contracts/skillmarketv2"
	legacy "github.com/36Dge/yijie-agent-host/internal/skills"
	"github.com/google/uuid"
	bolt "go.etcd.io/bbolt"
)

// Safe normal lifecycle integration: actual pinned executable, ordinary Skill
// sources and orderly Shutdown. No model turn, attack archive or fault fixture.
var nativeTestConfigurations sync.Map

func nativeService(t *testing.T, provider ...http.HandlerFunc) (*Service, *codex.Manager, Config) {
	t.Helper()
	binary, manifest, bundle := os.Getenv("YIJIE_CODEX_INTEGRATION_BINARY"), os.Getenv("YIJIE_CODEX_INTEGRATION_MANIFEST"), os.Getenv("YIJIE_FEAT174_TEST_BUNDLE")
	if binary == "" || manifest == "" || bundle == "" {
		t.Skip("requires pinned Runtime and bundled native sources")
	}
	root, err := os.MkdirTemp("", "feat174-safe-")
	if err != nil {
		t.Fatal(err)
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{"runtime", "host", "market/builtin", "market/user", "workspace"} {
		if err = os.MkdirAll(filepath.Join(root, dir), 0700); err != nil {
			t.Fatal(err)
		}
	}
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("unexpected provider call in no-model integration")
		w.WriteHeader(400)
	})
	if len(provider) == 1 {
		handler = provider[0]
	}
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	cfg := codex.DefaultConfig()
	cfg.BinaryPath = binary
	cfg.ManifestPath = manifest
	cfg.CodexHome = filepath.Join(root, "runtime")
	cfg.StartupTimeout = 30 * time.Second
	cfg.RequestTimeout = 30 * time.Second
	cfg.MiniMax = codex.MiniMaxConfig{Enabled: true, APIKey: "ordinary-local-minimax"}
	cfg.KimiAPIKey = "ordinary-local-kimi"
	cfg.ChatModelsEnabled = true
	cfg.KimiK31Candidate = true
	cfg.RuntimePermissionsEnabled = true
	cfg.NativeToolsEnabled = true
	cfg.InteractionToolsEnabled = true
	cfg.NativeSkillMarketplaceEnabled = true
	cfg.SkillMarketplaceRoots = []string{filepath.Join(root, "market/builtin"), filepath.Join(root, "market/user")}
	cfg.PermissionVerificationBaseURL = server.URL + "/v1"
	manager := codex.NewManager(cfg, nil)
	nativeTestConfigurations.Store(manager, cfg)
	t.Cleanup(func() { nativeTestConfigurations.Delete(manager) })
	raw, err := os.ReadFile(filepath.Join(bundle, "presentation.json"))
	if err != nil {
		t.Fatal(err)
	}
	options := Config{BundleRoot: bundle, BundleSHA256: digest(raw), DataRoot: filepath.Join(root, "market"), HostHome: filepath.Join(root, "host"), Runtime: manager}
	service, err := Open(options)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := manager.Shutdown(ctx); err != nil {
			t.Error("normal shutdown incomplete", err)
			return
		}
		if err := service.Close(); err != nil {
			t.Error(err)
		}
		_ = os.RemoveAll(root)
	})
	if err = manager.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err = service.Initialize(context.Background()); err != nil {
		t.Fatal(err)
	}
	service.MarkReady()
	service.updateSubmissionReadiness()
	return service, manager, options
}
func ordinaryMarkdown(name string) []byte {
	return []byte("---\nname: " + name + "\ndescription: A normal local skill for deterministic lifecycle verification.\n---\n\nRead the ordinary reference when asked.\n")
}
func ordinaryZip(t *testing.T, prefix string, files sourceFiles) []byte {
	t.Helper()
	var b bytes.Buffer
	z := zip.NewWriter(&b)
	if prefix != "" {
		_, e := z.Create(prefix)
		if e != nil {
			t.Fatal(e)
		}
	}
	names := []string{}
	for n := range files {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		value := files[n]
		w, e := z.Create(prefix + n)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = w.Write(value); e != nil {
			t.Fatal(e)
		}
	}
	if e := z.Close(); e != nil {
		t.Fatal(e)
	}
	return b.Bytes()
}
func importInput(filename string, data []byte) wire.ImportRequest {
	format := "markdown"
	if strings.EqualFold(filepath.Ext(filename), ".zip") {
		format = "zip"
	}
	return wire.ImportRequest{OperationId: uuid.NewString(), FileName: filename, Format: format, Sha256: digest(data), Bytes: int64(len(data))}
}
func runOperation(t *testing.T, s *Service, id string) wire.Operation {
	t.Helper()
	if err := s.execute(context.Background(), id); err != nil {
		t.Fatal("operation", err)
	}
	out, err := s.Operation(context.Background(), id)
	if err != nil || out.State != wire.OperationStateComplete {
		t.Fatalf("operation not complete: %+v %v", out, err)
	}
	s.updateSubmissionReadiness()
	return out
}
func mutate(t *testing.T, s *Service, id string, kind wire.MutationKind) wire.Operation {
	t.Helper()
	var e entry
	if err := s.get("entries", id, &e); err != nil {
		t.Fatal(err)
	}
	op, err := s.Mutate(context.Background(), wire.Mutation{OperationId: uuid.NewString(), SkillId: id, Kind: kind, ExpectedEpoch: e.Epoch})
	if err != nil {
		t.Fatal(err)
	}
	return runOperation(t, s, op.OperationId)
}

func TestFEAT174NativeMarketplaceNormalLifecycle(t *testing.T) {
	s, r, _ := nativeService(t)
	ctx := context.Background()
	catalog, err := s.Catalog(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(catalog.Skills) != 38 {
		t.Fatal("fresh catalog", len(catalog.Skills))
	}
	for _, skill := range catalog.Skills {
		if skill.Enabled || skill.InstallationState != wire.InstallStateNotInstalled {
			t.Fatal("fresh installation", skill.Id)
		}
	}
	entries, err := s.entries()
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		detail, err := r.ReadSkillPlugin(ctx, s.root(e.market()), e.Selected.PluginName)
		if err != nil || len(detail.Skills) != 1 || len(detail.McpServers) != 0 || len(detail.Hooks) != 0 {
			t.Fatal("bundled native preflight", e.ID, err)
		}
	}
	inventory, err := r.ReadSkillPluginStates(ctx, nil, []string{"yijie-builtin", "yijie-user"}, "")
	if err != nil || len(inventory) != 0 {
		t.Fatal("preflight installed a Skill", err)
	}
	builtin := catalog.Skills[0].Id
	mutate(t, s, builtin, wire.MutationKindInstall)
	mutate(t, s, builtin, wire.MutationKindDisable)
	mutate(t, s, builtin, wire.MutationKindRefresh)
	var builtinEntry entry
	_ = s.get("entries", builtin, &builtinEntry)
	state, err := s.readState(ctx, builtinEntry)
	if err != nil || state.EffectiveEnabled {
		t.Fatal("native refresh enabled a disabled installation", err)
	}
	mutate(t, s, builtin, wire.MutationKindUninstall)
	files := sourceFiles{"SKILL.md": ordinaryMarkdown("ordinary-import"), "references/note.md": []byte("An ordinary reference.\n"), "scripts/read.py": []byte("print('ordinary source only')\n"), "assets/note.txt": []byte("ordinary asset\n")}
	request := importInput("ordinary.zip", ordinaryZip(t, "ordinary/", files))
	op, err := s.Import(ctx, request, ordinaryZip(t, "ordinary/", files))
	if err != nil {
		t.Fatal(err)
	}
	// Unpublished sources are not selectable while native preflight can read them.
	before, err := s.Catalog(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range before.Skills {
		if v.Id == op.SkillId {
			t.Fatal("prepared import published early")
		}
	}
	runOperation(t, s, op.OperationId)
	var imported entry
	_ = s.get("entries", op.SkillId, &imported)
	state, err = s.readState(ctx, imported)
	if err != nil || !state.EffectiveEnabled || len(state.Skills) != 1 {
		t.Fatal("import not enabled", err)
	}
	nativePath := filepath.Dir(string(*state.Skills[0].Path))
	for name, value := range files {
		actual, e := os.ReadFile(filepath.Join(nativePath, name))
		if e != nil || !bytes.Equal(actual, value) {
			t.Fatal("resource bytes changed", name, e)
		}
	}
	disabled := mutate(t, s, op.SkillId, wire.MutationKindDisable)
	same := request
	same.OperationId = uuid.NewString()
	duplicate, err := s.Import(ctx, same, ordinaryZip(t, "ordinary/", files))
	if err != nil || duplicate.SkillId != op.SkillId || duplicate.Epoch != disabled.Epoch || duplicate.State != wire.OperationStateComplete {
		t.Fatal("duplicate identity", duplicate, err)
	}
	_ = s.get("entries", op.SkillId, &imported)
	state, err = s.readState(ctx, imported)
	if err != nil || state.EffectiveEnabled {
		t.Fatal("duplicate import re-enabled skill")
	}
	mutate(t, s, op.SkillId, wire.MutationKindUninstall)
	same.OperationId = uuid.NewString()
	again, err := s.Import(ctx, same, ordinaryZip(t, "ordinary/", files))
	if err != nil || again.SkillId != op.SkillId || again.Epoch <= duplicate.Epoch {
		t.Fatal("reimport epoch", again, err)
	}
	runOperation(t, s, again.OperationId)
	if _, err = s.Cancel(ctx, op.OperationId); err != nil {
		t.Fatal(err)
	}
	_ = s.get("entries", op.SkillId, &imported)
	state, err = s.readState(ctx, imported)
	if err != nil || !state.EffectiveEnabled {
		t.Fatal("old cancellation changed new epoch")
	}
	for _, test := range []struct {
		name string
		data []byte
	}{{"skill.md", ordinaryMarkdown("single-file")}, {"root.zip", ordinaryZip(t, "", sourceFiles{"skill.md": ordinaryMarkdown("root-file"), "__MACOSX/._note": []byte("ordinary ignored metadata"), ".DS_Store": []byte("ordinary metadata")})}} {
		request := importInput(test.name, test.data)
		op, err := s.Import(ctx, request, test.data)
		if err != nil {
			t.Fatal(err)
		}
		runOperation(t, s, op.OperationId)
	}
	data := ordinaryMarkdown("cancelled-file")
	request = importInput("SKILL.md", data)
	pending, err := s.Import(ctx, request, data)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Cancel(ctx, pending.OperationId); err != nil {
		t.Fatal(err)
	}
	if err = s.execute(ctx, pending.OperationId); err != nil {
		t.Fatal(err)
	}
	cancelled, _ := s.Operation(ctx, pending.OperationId)
	if cancelled.State != wire.OperationStateCancelled {
		t.Fatal("normal cancel", cancelled)
	}
	if err = r.WithSkillMaintenance(ctx, func(ctx context.Context) error { return nil }); err != nil {
		t.Fatal("idle maintenance", err)
	}
	final, err := s.Catalog(ctx)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(final)
	if bytes.Contains(raw, []byte(s.config.DataRoot)) || bytes.Contains(raw, []byte("/plugins/cache/")) {
		t.Fatal("public path disclosure")
	}
}

func TestFEAT174RuleMappingPreservesUnrelatedAndNameScope(t *testing.T) {
	original := []any{map[string]any{"path": "/ordinary/old/SKILL.md", "enabled": false}, map[string]any{"name": "shared-name", "enabled": false}, map[string]any{"path": "/unrelated/SKILL.md", "enabled": true}}
	mapped, err := mapRules(original, "/ordinary/old/SKILL.md", "shared-name", "/ordinary/new/SKILL.md", "native:shared-name", true, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(mapped) != 3 || mapped[0].(map[string]any)["name"] != "shared-name" || mapped[1].(map[string]any)["name"] != "native:shared-name" || mapped[2].(map[string]any)["path"] != "/unrelated/SKILL.md" {
		t.Fatalf("rule mapping changed scope/order: %+v", mapped)
	}
	if original[0].(map[string]any)["path"] != "/ordinary/old/SKILL.md" {
		t.Fatal("mutated source rules")
	}
}

func TestFEAT174LegacyMigrationRollbackAndReplay(t *testing.T) {
	s, runtime, options := nativeService(t)
	ctx := context.Background()
	bundle := os.Getenv("YIJIE_FEAT174_LEGACY_BUNDLE")
	if bundle == "" {
		t.Skip("requires ordinary legacy bundle")
	}
	bundle, canonicalErr := filepath.EvalSymlinks(bundle)
	if canonicalErr != nil {
		t.Fatal(canonicalErr)
	}
	raw, err := os.ReadFile(filepath.Join(bundle, "bundle-manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var manifest legacy.Manifest
	if err = json.Unmarshal(raw, &manifest); err != nil {
		t.Fatal(err)
	}
	managed := filepath.Join(options.HostHome, "legacy-skills")
	if err = os.Mkdir(managed, 0700); err != nil {
		t.Fatal(err)
	}
	old, err := legacy.NewService(legacy.Config{BundleRoot: bundle, ManagedRoot: managed, Runtime: runtime})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if old != nil {
			_ = old.Close()
		}
	}()
	initial, err := old.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	chosen := manifest.Skills[0]
	installRequest := legacy.InstallInput{OperationID: uuid.NewString(), SkillID: chosen.ID, ExpectedVersion: chosen.Version, ExpectedArchiveSHA256: chosen.Archive.SHA256, CatalogRevision: initial.CatalogRevision}
	original, err := old.Install(ctx, installRequest)
	if err != nil {
		t.Fatal(err)
	}
	disabled, err := old.SetEnabled(ctx, legacy.EnabledInput{OperationID: uuid.NewString(), SkillID: chosen.ID, Enabled: false})
	if err != nil || disabled.Enabled {
		t.Fatal("legacy disable", err)
	}
	before, err := os.ReadFile(filepath.Join(managed, ".yijie-state/operations.json"))
	if err != nil {
		t.Fatal(err)
	}
	s.ready.Store(false)
	runtime.SetSkillSubmissionReady(false)
	if err = s.PrepareMigration(old); err != nil {
		t.Fatal(err)
	}
	marker, err := os.ReadFile(filepath.Join(managed, ".yijie-state/operations.json"))
	if err != nil || !bytes.Contains(marker, []byte("\"schema_version\":3")) {
		t.Fatal("downgrade guard not published", err)
	}
	snapshot, err := os.ReadFile(filepath.Join(managed, ".yijie-state/native-v3/snapshots", digest(before)+"-operations.json"))
	if err != nil || !bytes.Equal(before, snapshot) {
		t.Fatal("legacy byte snapshot changed", err)
	}
	if err = s.Migrate(ctx, old); err != nil {
		t.Fatal("migrate", err)
	}
	if !s.NativeRoute(chosen.ID) {
		t.Fatal("native routing was not committed")
	}
	var migrated entry
	_ = s.get("entries", chosen.ID, &migrated)
	state, err := s.readState(ctx, migrated)
	if err != nil || state.EffectiveEnabled || !state.UserConfigPresent {
		t.Fatal("migration lost disabled intent", err)
	}
	// Old completed operation remains a read-only exact replay after routing.
	replayed, err := old.Install(ctx, installRequest)
	if err != nil || replayed != original {
		t.Fatal("legacy replay changed", replayed, err)
	}
	if _, err = old.List(ctx); err != nil {
		t.Fatal("legacy scan", err)
	}
	nativeEntries, err := runtime.ListSkills(ctx, []string{managed}, true)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range nativeEntries {
		for _, skill := range entry.Skills {
			if skill.Path == filepath.Join(managed, chosen.ID, "SKILL.md") {
				t.Fatal("legacy scan revived migrated root")
			}
		}
	}
	s.MarkReady()
	s.updateSubmissionReadiness()
	personalBytes := ordinaryMarkdown("preserved-personal-source")
	personalRequest := importInput("SKILL.md", personalBytes)
	personal, err := s.Import(ctx, personalRequest, personalBytes)
	if err != nil {
		t.Fatal(err)
	}
	runOperation(t, s, personal.OperationId)
	if err = s.RollbackMigration(ctx, old); err != nil {
		t.Fatal("compatible rollback", err)
	}
	if s.NativeRoute(chosen.ID) {
		t.Fatal("legacy authority not restored")
	}
	state, err = s.readState(ctx, migrated)
	if err != nil || !absent(state) {
		t.Fatal("native rollback unconfirmed", err)
	}
	after, err := os.ReadFile(filepath.Join(managed, ".yijie-state/operations.json"))
	if err != nil || !bytes.Equal(marker, after) {
		t.Fatal("compatibility writer replaced public guard", err)
	}
	replayed, err = old.Install(ctx, installRequest)
	if err != nil || replayed != original {
		t.Fatal("rollback altered historical result", err)
	}
	var personalEntry entry
	_ = s.get("entries", personal.SkillId, &personalEntry)
	personalState, err := s.readState(ctx, personalEntry)
	if err != nil || personalState.CacheState != "valid" || personalState.EffectiveEnabled {
		t.Fatal("compatible rollback discarded or enabled personal source", err)
	}
	if err = s.ResumeMigration(ctx, old); err != nil {
		t.Fatal("compatible resume", err)
	}
	if s.CompatibleMode() || !s.NativeRoute(chosen.ID) {
		t.Fatal("native migration did not resume")
	}
	personalState, err = s.readState(ctx, personalEntry)
	if err != nil || !personalState.EffectiveEnabled {
		t.Fatal("personal intent was not restored after compatible resume", err)
	}
	if err = old.Close(); err != nil {
		t.Fatal(err)
	}
	old = nil
	// Universal v3 reader must work without the feature switch. Close the private
	// store normally before opening a second compatible legacy reader.
	if err = s.db.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := legacy.NewService(legacy.Config{BundleRoot: bundle, ManagedRoot: managed, Runtime: runtime})
	if err != nil {
		t.Fatal("new reader without feature switch", err)
	}
	if _, err = reopened.Install(ctx, installRequest); err != nil {
		t.Fatal(err)
	}
	if err = reopened.Close(); err != nil {
		t.Fatal(err)
	}
	// Reopen the same private DB for the harness's normal cleanup.
	s.db, err = bolt.Open(filepath.Join(options.HostHome, "skill-market-v2/state.db"), 0600, &bolt.Options{Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
}

func TestFEAT174NativeRefreshRollbackKeepsRulesAndDownlistedInventory(t *testing.T) {
	s, r, _ := nativeService(t)
	ctx := context.Background()
	catalog, err := s.Catalog(ctx)
	if err != nil {
		t.Fatal(err)
	}
	id := catalog.Skills[0].Id
	mutate(t, s, id, wire.MutationKindInstall)
	var e entry
	_ = s.get("entries", id, &e)
	original := e.Selected
	before, err := s.readState(ctx, e)
	if err != nil {
		t.Fatal(err)
	}
	// An ordinary explicit per-skill opt-out remains in its native user layer.
	if _, err = r.WriteSkillConfig(ctx, string(*before.Skills[0].Path), false); err != nil {
		t.Fatal(err)
	}
	files, err := readVerifiedFiles(filepath.Join(s.root(e.market()), e.Selected.SourcePath, "skills/main"), e.Selected)
	if err != nil {
		t.Fatal(err)
	}
	files["SKILL.md"] = ordinaryMarkdown("ordinary-new-version")
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
	mutate(t, s, id, wire.MutationKindRefresh)
	_ = s.get("entries", id, &e)
	updated, err := s.readState(ctx, e)
	if err != nil || updated.CacheVersion == nil || *updated.CacheVersion != target.NativePackageVersion || len(updated.Skills) != 1 || updated.Skills[0].Enabled {
		t.Fatal("refresh lost the opt-out", err)
	}
	config, err := r.ReadSkillPluginConfig(ctx, s.config.DataRoot)
	if err != nil {
		t.Fatal(err)
	}
	layers, err := readRuleLayers(config)
	if err != nil {
		t.Fatal(err)
	}
	mapped := false
	for _, layer := range layers {
		if layer.Name["type"] == "user" && rulesReference(layer.Rules, string(*updated.Skills[0].Path), "") {
			mapped = true
		}
	}
	if !mapped {
		t.Fatal("path rule was not rebound")
	}
	mutate(t, s, id, wire.MutationKindDisable)
	mutate(t, s, id, wire.MutationKindRollback)
	_ = s.get("entries", id, &e)
	rolled, err := s.readState(ctx, e)
	if err != nil || rolled.EffectiveEnabled || rolled.CacheVersion == nil || *rolled.CacheVersion != original.NativePackageVersion {
		t.Fatal("native content rollback changed disabled intent", err)
	}
	// Normal next publication withdraws the offer, retaining its installed stock.
	e.Offer = nil
	if err = s.saveEntry(e); err != nil {
		t.Fatal(err)
	}
	downlisted, err := s.Catalog(ctx)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, skill := range downlisted.Skills {
		if skill.Id == id {
			found = true
			if !skill.Retired || skill.InstallationState != wire.InstallStateInstalled {
				t.Fatal("retired inventory lost controls")
			}
		}
	}
	if !found {
		t.Fatal("retired installed Skill hidden")
	}
	mutate(t, s, id, wire.MutationKindUninstall)
	downlisted, err = s.Catalog(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, skill := range downlisted.Skills {
		if skill.Id == id {
			t.Fatal("retired uninstalled Skill was offered again")
		}
	}
}

type nativeInstallBoundary struct {
	Runtime
	reached chan struct{}
	release chan struct{}
	once    sync.Once
}

func (r *nativeInstallBoundary) InstallSkillPlugin(ctx context.Context, root, name string) error {
	if err := r.Runtime.InstallSkillPlugin(ctx, root, name); err != nil {
		return err
	}
	r.once.Do(func() { close(r.reached) })
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-r.release:
		return nil
	}
}
func TestFEAT174NormalCancelAfterNativeInstallAndReopen(t *testing.T) {
	s, r, options := nativeService(t)
	ctx := context.Background()
	data := ordinaryMarkdown("cancel-after-native")
	request := importInput("SKILL.md", data)
	op, err := s.Import(ctx, request, data)
	if err != nil {
		t.Fatal(err)
	}
	boundary := &nativeInstallBoundary{Runtime: r, reached: make(chan struct{}), release: make(chan struct{})}
	s.config.Runtime = boundary
	finished := make(chan error, 1)
	go func() { finished <- s.execute(ctx, op.OperationId) }()
	select {
	case <-boundary.reached:
	case <-time.After(10 * time.Second):
		t.Fatal("native install boundary not reached")
	}
	if _, err = s.Cancel(ctx, op.OperationId); err != nil {
		t.Fatal(err)
	}
	close(boundary.release)
	if err = <-finished; err != nil {
		t.Fatal(err)
	}
	result, err := s.Operation(ctx, op.OperationId)
	if err != nil || result.State != wire.OperationStateCancelled {
		t.Fatal("post-install cancel", result, err)
	}
	var e entry
	_ = s.get("entries", op.SkillId, &e)
	state, err := s.readState(ctx, e)
	if err != nil || !absent(state) {
		t.Fatal("cancel did not confirm both native facts", err)
	}
	s.config.Runtime = r
	request = importInput("SKILL.md", ordinaryMarkdown("normal-reopen"))
	pending, err := s.Import(ctx, request, ordinaryMarkdown("normal-reopen"))
	if err != nil {
		t.Fatal(err)
	}
	// Normal close/reopen at a durable source boundary, without process faults.
	if err = s.db.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(options)
	if err != nil {
		t.Fatal(err)
	}
	s.db = reopened.db
	s.ready.Store(false)
	if err = s.Initialize(ctx); err != nil {
		t.Fatal(err)
	}
	s.MarkReady()
	runOperation(t, s, pending.OperationId)
	same, err := s.Operation(ctx, pending.OperationId)
	if err != nil || same.State != wire.OperationStateComplete {
		t.Fatal(err)
	}
	replay, err := s.Import(ctx, request, ordinaryMarkdown("normal-reopen"))
	if err != nil || replay != same {
		t.Fatal("completed receipt replay differs", err)
	}
}

func TestFEAT174ConcurrentImportsUseOneCatalogWriter(t *testing.T) {
	s, _, _ := nativeService(t)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); s.Run(ctx) }()
	defer func() { cancel(); <-done }()
	type outcome struct {
		op  wire.Operation
		err error
	}
	results := make(chan outcome, 2)
	for _, name := range []string{"ordinary-a", "ordinary-b"} {
		go func(name string) {
			data := ordinaryMarkdown(name)
			request := importInput("SKILL.md", data)
			op, err := s.Import(ctx, request, data)
			results <- outcome{op, err}
		}(name)
	}
	ids := []string{}
	for range 2 {
		out := <-results
		if out.err != nil {
			t.Fatal(out.err)
		}
		ids = append(ids, out.op.OperationId)
	}
	deadline := time.Now().Add(10 * time.Second)
	for _, id := range ids {
		for {
			op, err := s.Operation(ctx, id)
			if err != nil {
				t.Fatal(err)
			}
			if op.State == wire.OperationStateComplete {
				break
			}
			if op.State == wire.OperationStateNeedsAttention || time.Now().After(deadline) {
				t.Fatal("concurrent operation incomplete", op)
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
	catalog, err := s.Catalog(ctx)
	if err != nil {
		t.Fatal(err)
	}
	users := 0
	for _, skill := range catalog.Skills {
		if skill.Origin == wire.OriginUser {
			users++
			if !skill.Enabled || !skill.RuntimeVisible {
				t.Fatal("import missing runtime confirmation")
			}
		}
	}
	if users != 2 {
		t.Fatal("lost catalog entry", users)
	}
}

func TestFEAT174OrdinaryImportValidationAndReselection(t *testing.T) {
	s, _, _ := nativeService(t)
	ctx := context.Background()
	data := []byte("---\nname: ordinary-description-missing\n---\nAn ordinary draft skill.\n")
	request := importInput("SKILL.md", data)
	op, err := s.Import(ctx, request, data)
	if err != nil {
		t.Fatal(err)
	}
	_ = s.execute(ctx, op.OperationId)
	status, err := s.Operation(ctx, op.OperationId)
	if err != nil || status.State != wire.OperationStateNeedsAttention || status.ErrorCode != "native_preflight_failed" {
		t.Fatal("missing description did not remain reviewable", status, err)
	}
	if _, err = s.Cancel(ctx, op.OperationId); err != nil {
		t.Fatal(err)
	}
	if err = s.execute(ctx, op.OperationId); err != nil {
		t.Fatal(err)
	}
	valid := ordinaryMarkdown("普通技能名称")
	request = importInput("SKILL.md", valid)
	complete, err := s.Import(ctx, request, valid)
	if err != nil {
		t.Fatal(err)
	}
	runOperation(t, s, complete.OperationId)
	var e entry
	_ = s.get("entries", complete.SkillId, &e)
	if e.DisplayName != "普通技能名称" {
		t.Fatal("Native parsed name was not retained", e.DisplayName)
	}
}

func TestFEAT174NativeExplicitSkillAndColdResume(t *testing.T) {
	requests := make(chan []byte, 6)
	calls := 0
	provider := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/v1/responses" {
			t.Error("unexpected local route")
			w.WriteHeader(400)
			return
		}
		body, err := io.ReadAll(io.LimitReader(r.Body, 8<<20))
		if err != nil {
			t.Error(err)
			return
		}
		calls++
		if calls > 4 {
			t.Error("unexpected local continuation")
			w.WriteHeader(429)
			return
		}
		requests <- body
		item := map[string]any{"type": "message", "id": fmt.Sprintf("skill-item-%d", calls), "role": "assistant", "phase": "final_answer", "content": []any{map[string]any{"type": "output_text", "text": "Ordinary local verification complete."}}}
		response := map[string]any{"id": fmt.Sprintf("skill-response-%d", calls), "status": "completed", "output": []any{item}, "usage": map[string]any{"input_tokens": 1, "output_tokens": 1, "total_tokens": 2}}
		w.Header().Set("Content-Type", "text/event-stream")
		for _, event := range []any{map[string]any{"type": "response.created", "response": map[string]any{"id": response["id"], "status": "in_progress", "output": []any{}}}, map[string]any{"type": "response.output_item.added", "output_index": 0, "item": item}, map[string]any{"type": "response.output_item.done", "output_index": 0, "item": item}, map[string]any{"type": "response.completed", "response": response}} {
			raw, _ := json.Marshal(event)
			fmt.Fprintf(w, "data: %s\n\n", raw)
			w.(http.Flusher).Flush()
		}
	})
	s, r, options := nativeService(t, provider)
	ctx := codex.WithPermissionMode(codex.WithModelProfile(context.Background(), codex.KimiK31ProfileID), codex.PermissionAsk)
	cwd := filepath.Join(filepath.Dir(options.HostHome), "workspace")
	thread, err := r.StartThread(ctx, cwd)
	if err != nil {
		t.Fatal(err)
	}
	turn := func(runtime *codex.Manager, text string, inputs []codex.UserInput) []byte {
		t.Helper()
		message, err := codex.TextUserInput(text)
		if err != nil {
			t.Fatal(err)
		}
		inputs = append([]codex.UserInput{message}, inputs...)
		if _, err = runtime.StartTurnV2(ctx, thread.ID, inputs, "max"); err != nil {
			t.Fatal(err)
		}
		var request []byte
		select {
		case request = <-requests:
		case <-time.After(15 * time.Second):
			t.Fatal("local provider did not receive native input")
		}
		deadline := time.Now().Add(15 * time.Second)
		for {
			state, err := runtime.ReadThreadStatus(ctx, thread.ID)
			if err != nil {
				t.Fatal(err)
			}
			if state == "idle" {
				break
			}
			if time.Now().After(deadline) {
				t.Fatal("normal local turn did not complete")
			}
			time.Sleep(10 * time.Millisecond)
		}
		return request
	}
	baseline := turn(r, "Ordinary original message.", nil)
	if bytes.Contains(baseline, []byte("ORDINARY_SKILL_BODY_174")) {
		t.Fatal("uninstalled body injected")
	}
	data := append(ordinaryMarkdown("普通显式技能"), []byte("\nORDINARY_SKILL_BODY_174\n")...)
	request := importInput("SKILL.md", data)
	op, err := s.Import(ctx, request, data)
	if err != nil {
		t.Fatal(err)
	}
	runOperation(t, s, op.OperationId)
	refs := []contract.SelectedSkillRef{{Id: op.SkillId, Version: "1.0.0"}}
	inputs, err := s.ResolveSelectedSkillsInDirectory(ctx, refs, cwd)
	if err != nil {
		t.Fatal(err)
	}
	explicit := turn(r, "Use the selected ordinary skill.", inputs)
	if !bytes.Contains(explicit, []byte("ORDINARY_SKILL_BODY_174")) {
		t.Fatal("native explicit input did not load skill body")
	}
	// A new native thread receives metadata for implicit discovery, not every
	// Skill body. The native policy opt-out remains excluded from that catalog.
	optoutFiles := sourceFiles{"SKILL.md": ordinaryMarkdown("ordinary-explicit-only"), "agents/openai.yaml": []byte("policy:\n  allow_implicit_invocation: false\n")}
	optoutData := ordinaryZip(t, "", optoutFiles)
	optoutRequest := importInput("optout.zip", optoutData)
	optout, err := s.Import(ctx, optoutRequest, optoutData)
	if err != nil {
		t.Fatal(err)
	}
	runOperation(t, s, optout.OperationId)
	var selectedEntry, optoutEntry entry
	_ = s.get("entries", op.SkillId, &selectedEntry)
	_ = s.get("entries", optout.SkillId, &optoutEntry)
	selectedState, err := s.readState(ctx, selectedEntry)
	if err != nil {
		t.Fatal(err)
	}
	optoutState, err := s.readState(ctx, optoutEntry)
	if err != nil {
		t.Fatal(err)
	}
	retained := thread
	thread, err = r.StartThread(ctx, cwd)
	if err != nil {
		t.Fatal(err)
	}
	catalogInput := turn(r, "Describe the current ordinary task briefly.", nil)
	thread = retained
	if !bytes.Contains(catalogInput, []byte(selectedState.Skills[0].Name)) {
		t.Fatal("native implicit discovery metadata missing")
	}
	if bytes.Contains(catalogInput, []byte("ORDINARY_SKILL_BODY_174")) {
		t.Fatal("unselected body was eagerly injected")
	}
	if bytes.Contains(catalogInput, []byte(optoutState.Skills[0].Name)) {
		t.Fatal("native implicit opt-out was ignored")
	}
	before, err := r.ReadThread(ctx, thread.ID)
	if err != nil {
		t.Fatal(err)
	}
	shutdown, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err = r.Shutdown(shutdown); err != nil {
		t.Fatal(err)
	}
	value, _ := nativeTestConfigurations.Load(r)
	next := codex.NewManager(value.(codex.Config), nil)
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := next.Shutdown(ctx); err != nil {
			t.Error("normal cold Runtime shutdown", err)
		}
	}()
	if err = next.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	s.config.Runtime = next
	if err = s.Initialize(ctx); err != nil {
		t.Fatal(err)
	}
	s.MarkReady()
	s.updateSubmissionReadiness()
	if _, err = next.ResumeThread(ctx, thread.ID); err != nil {
		t.Fatal("normal cold resume", err)
	}
	after, err := next.ReadThread(ctx, thread.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(before, []byte("Ordinary original message.")) || !bytes.Contains(after, []byte("Ordinary original message.")) {
		t.Fatal("original history missing after cold resume")
	}
	inputs, err = s.ResolveSelectedSkillsInDirectory(ctx, refs, cwd)
	if err != nil {
		t.Fatal(err)
	}
	cold := turn(next, "Continue with the same selected skill.", inputs)
	if !bytes.Contains(cold, []byte("ORDINARY_SKILL_BODY_174")) {
		t.Fatal("cold explicit selection body unavailable")
	}
	if calls != 4 {
		t.Fatal("unexpected local provider count", calls)
	}
}

func TestFEAT174NativeDependencyMetadataDoesNotInstallMCP(t *testing.T) {
	s, r, _ := nativeService(t)
	ctx := context.Background()
	files := sourceFiles{"SKILL.md": ordinaryMarkdown("ordinary-dependency"), "agents/openai.yaml": []byte("interface:\n  display_name: Ordinary Portable Skill\n  short_description: A normal dependency declaration.\ndependencies:\n  tools:\n    - type: mcp\n      value: ordinary-unconfigured-server\n")}
	data := ordinaryZip(t, "ordinary/", files)
	request := importInput("ordinary.zip", data)
	op, err := s.Import(ctx, request, data)
	if err != nil {
		t.Fatal(err)
	}
	runOperation(t, s, op.OperationId)
	catalog, err := s.Catalog(ctx)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, skill := range catalog.Skills {
		if skill.Id == op.SkillId {
			found = true
			if skill.DependencyState != wire.DependencyStateMissing || !skill.Enabled || skill.DisplayName != "Ordinary Portable Skill" {
				t.Fatal("dependency and installation facts were conflated", skill.DependencyState, skill.DisplayName)
			}
		}
	}
	if !found {
		t.Fatal("import missing")
	}
	inventory, err := r.ReadSkillMCPInventory(ctx)
	if err != nil || len(inventory) != 0 {
		t.Fatal("import installed an MCP dependency", err)
	}
}

func TestFEAT174ActiveTurnHoldsResourceReplacement(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	var finish sync.Once
	defer finish.Do(func() { close(release) })
	provider := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/v1/responses" {
			t.Error("unexpected local route")
			w.WriteHeader(400)
			return
		}
		select {
		case <-entered:
			t.Error("new turn crossed maintenance admission")
			w.WriteHeader(429)
			return
		default:
			close(entered)
		}
		select {
		case <-release:
		case <-r.Context().Done():
			return
		}
		item := map[string]any{"type": "message", "id": "ordinary-active-item", "role": "assistant", "phase": "final_answer", "content": []any{map[string]any{"type": "output_text", "text": "Ordinary local work complete."}}}
		response := map[string]any{"id": "ordinary-active-response", "status": "completed", "output": []any{item}, "usage": map[string]any{"input_tokens": 1, "output_tokens": 1, "total_tokens": 2}}
		w.Header().Set("Content-Type", "text/event-stream")
		for _, event := range []any{map[string]any{"type": "response.created", "response": map[string]any{"id": response["id"], "status": "in_progress", "output": []any{}}}, map[string]any{"type": "response.output_item.added", "output_index": 0, "item": item}, map[string]any{"type": "response.output_item.done", "output_index": 0, "item": item}, map[string]any{"type": "response.completed", "response": response}} {
			raw, _ := json.Marshal(event)
			fmt.Fprintf(w, "data: %s\n\n", raw)
			w.(http.Flusher).Flush()
		}
	})
	s, r, options := nativeService(t, provider)
	ctx := codex.WithPermissionMode(codex.WithModelProfile(context.Background(), codex.KimiK31ProfileID), codex.PermissionAsk)
	catalog, err := s.Catalog(ctx)
	if err != nil {
		t.Fatal(err)
	}
	id := catalog.Skills[0].Id
	mutate(t, s, id, wire.MutationKindInstall)
	cwd := filepath.Join(filepath.Dir(options.HostHome), "workspace")
	thread, err := r.StartThread(ctx, cwd)
	if err != nil {
		t.Fatal(err)
	}
	input, _ := codex.TextUserInput("Wait for ordinary local work.")
	if _, err = r.StartTurnV2(ctx, thread.ID, []codex.UserInput{input}, "max"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-entered:
	case <-time.After(15 * time.Second):
		t.Fatal("ordinary local task not active")
	}
	var e entry
	_ = s.get("entries", id, &e)
	op, err := s.Mutate(ctx, wire.Mutation{OperationId: uuid.NewString(), SkillId: id, Kind: wire.MutationKindUninstall, ExpectedEpoch: e.Epoch})
	if err != nil {
		t.Fatal(err)
	}
	if err = s.execute(ctx, op.OperationId); err == nil {
		t.Fatal("active resources were replaced")
	}
	state, err := s.readState(ctx, e)
	if err != nil || state.CacheState != "valid" {
		t.Fatal("active installation changed", err)
	}
	other, err := r.StartThread(ctx, cwd)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = r.StartTurnV2(ctx, other.ID, []codex.UserInput{input}, "max"); err == nil {
		t.Fatal("new turn admitted during pending maintenance")
	}
	finish.Do(func() { close(release) })
	deadline := time.Now().Add(15 * time.Second)
	for {
		state, err := r.ReadThreadStatus(ctx, thread.ID)
		if err != nil {
			t.Fatal(err)
		}
		if state == "idle" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("normal active task did not finish")
		}
		time.Sleep(10 * time.Millisecond)
	}
	runOperation(t, s, op.OperationId)
}
