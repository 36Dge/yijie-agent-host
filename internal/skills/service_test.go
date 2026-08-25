package skills

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/36Dge/yijie-agent-host/internal/codex"
)

const testSkillID = "yijie.fixture.model-only"

type testRuntime struct {
	mu                      sync.Mutex
	roots                   []string
	enabled                 map[string]bool
	runtimeName             string
	failListCount           int
	failListAt              int
	listCalls               int
	setRootsEntered         chan struct{}
	blockSetRoots           chan struct{}
	calls                   int
	setRootsCalls           int
	onSetRoots              func()
	applyRootsThenFailCount int
	onWriteConfig           func(bool)
}

// These types are copied from the previous Host's schema_version 1 journal
// decoder. A journal written after Manifest v2 operations must remain strict-
// JSON readable by this shape so rolling the application back is safe.
type preManifestV2OperationStore struct {
	SchemaVersion int                               `json:"schema_version"`
	Operations    []preManifestV2PersistedOperation `json:"operations"`
}

type preManifestV2PersistedOperation struct {
	ID              string                 `json:"id"`
	Fingerprint     string                 `json:"fingerprint"`
	Kind            string                 `json:"kind"`
	SkillID         string                 `json:"skill_id"`
	Status          string                 `json:"status"`
	Phase           string                 `json:"phase"`
	HadPrevious     bool                   `json:"had_previous"`
	PreviousEnabled bool                   `json:"previous_enabled"`
	State           *preManifestV2State    `json:"state,omitempty"`
	Snapshot        *preManifestV2Snapshot `json:"snapshot,omitempty"`
	ErrorCode       ErrorCode              `json:"error_code"`
	CompletedAt     *time.Time             `json:"completed_at,omitempty"`
}

type preManifestV2State struct {
	ID                  string
	RuntimeName         string
	Version             string
	CatalogStatus       string
	MaintenanceStatus   string
	CapabilityReadiness string
	InstallationStatus  string
	Enabled             bool
	RuntimeVisible      bool
	FailureCode         string
}

type preManifestV2Snapshot struct {
	CatalogRevision string
	ScannedAt       time.Time
	Skills          []preManifestV2State
}

func newTestRuntime(runtimeName string) *testRuntime {
	return &testRuntime{enabled: make(map[string]bool), runtimeName: runtimeName}
}

func (runtime *testRuntime) SetSkillsExtraRoots(ctx context.Context, roots []string) error {
	runtime.mu.Lock()
	runtime.calls++
	runtime.setRootsCalls++
	entered := runtime.setRootsEntered
	block := runtime.blockSetRoots
	runtime.mu.Unlock()
	if entered != nil {
		select {
		case entered <- struct{}{}:
		default:
		}
	}
	if block != nil {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-block:
		}
	}
	runtime.mu.Lock()
	runtime.roots = append([]string(nil), roots...)
	callback := runtime.onSetRoots
	failAfterApply := runtime.applyRootsThenFailCount > 0
	if failAfterApply {
		runtime.applyRootsThenFailCount--
	}
	runtime.mu.Unlock()
	if callback != nil {
		callback()
	}
	if failAfterApply {
		return errors.New("synthetic roots response lost after apply")
	}
	return nil
}

func (runtime *testRuntime) WriteSkillConfig(_ context.Context, skillPath string, enabled bool) (bool, error) {
	runtime.mu.Lock()
	runtime.calls++
	info, err := os.Lstat(skillPath)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		runtime.mu.Unlock()
		return false, errors.New("cannot configure missing Skill")
	}
	runtime.enabled[skillPath] = enabled
	callback := runtime.onWriteConfig
	runtime.mu.Unlock()
	if callback != nil {
		callback(enabled)
	}
	return enabled, nil
}

func (runtime *testRuntime) ListSkills(ctx context.Context, cwds []string, _ bool) ([]codex.SkillsListEntry, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	runtime.calls++
	runtime.listCalls++
	if runtime.failListCount > 0 {
		runtime.failListCount--
		return nil, errors.New("synthetic Runtime list failure")
	}
	if runtime.failListAt > 0 && runtime.listCalls == runtime.failListAt {
		return nil, errors.New("synthetic Runtime list failure at exact call")
	}
	if len(cwds) != 1 {
		return nil, errors.New("unexpected Runtime roots")
	}
	projection := codex.SkillsListEntry{CWD: cwds[0], Skills: []codex.SkillMetadata{}, Errors: []codex.SkillErrorInfo{}}
	for _, root := range runtime.roots {
		err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if path != root && entry.IsDir() && entry.Name()[0] == '.' {
				return filepath.SkipDir
			}
			if entry.IsDir() || entry.Name() != "SKILL.md" {
				return nil
			}
			info, err := entry.Info()
			if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
				return nil
			}
			projection.Skills = append(projection.Skills, codex.SkillMetadata{
				Name: runtime.runtimeName, Description: "fixture", Path: path,
				Scope: codex.SkillScopeUser, Enabled: runtime.enabled[path],
			})
			return nil
		})
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
	}
	sort.Slice(projection.Skills, func(i, j int) bool { return projection.Skills[i].Path < projection.Skills[j].Path })
	return []codex.SkillsListEntry{projection}, nil
}

func TestManifestRevisionUsesExactValidatedBytesAndAssertsFormats(t *testing.T) {
	bundleRoot, raw := copyPinnedTestBundle(t, "manifest-valid.json", "fixture-model-only-0.1.0.zip")
	catalog, err := loadCatalog(bundleRoot)
	if err != nil {
		t.Fatalf("load valid catalog: %v", err)
	}
	digest := sha256.Sum256(raw)
	if catalog.revision != hex.EncodeToString(digest[:]) {
		t.Fatalf("revision=%s, want exact raw digest", catalog.revision)
	}

	for _, mutation := range []struct {
		name  string
		apply func(map[string]any)
	}{
		{
			name: "invalid uri",
			apply: func(document map[string]any) {
				document["source"].(map[string]any)["repository"] = ":// not a uri"
			},
		},
		{
			name: "invalid date-time",
			apply: func(document map[string]any) {
				document["skills"].([]any)[0].(map[string]any)["provenance"].(map[string]any)["reviewed_at"] = "2026-99-99"
			},
		},
	} {
		t.Run(mutation.name, func(t *testing.T) {
			var document map[string]any
			if err := json.Unmarshal(raw, &document); err != nil {
				t.Fatal(err)
			}
			mutation.apply(document)
			invalid, err := json.Marshal(document)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(bundleRoot, manifestFileName), invalid, 0o600); err != nil {
				t.Fatal(err)
			}
			_, err = loadCatalog(bundleRoot)
			assertCode(t, err, CodeManifestInvalid)
		})
	}
}

func TestManifestV2CatalogProjectsAllThirtyEightSkillsAndRejectsBlockedInstallFirst(t *testing.T) {
	bundleRoot, raw := copyPinnedV2CatalogBundle(t)
	loaded, err := loadCatalog(bundleRoot)
	if err != nil {
		t.Fatalf("load Manifest v2 catalog: %v", err)
	}
	if loaded.manifest.SchemaVersion != 2 || len(loaded.manifest.Skills) != 38 {
		t.Fatalf("v2 catalog shape: schema=%d skills=%d", loaded.manifest.SchemaVersion, len(loaded.manifest.Skills))
	}
	digest := sha256.Sum256(raw)
	if loaded.revision != hex.EncodeToString(digest[:]) {
		t.Fatalf("v2 catalog revision=%s", loaded.revision)
	}
	wantCategories := map[string]int{
		"sourcing-selection":  5,
		"market-research":     9,
		"content-marketing":   7,
		"traffic-advertising": 9,
		"store-operations":    8,
	}
	gotCategories := make(map[string]int, len(wantCategories))
	installable := 0
	blocked := 0
	for _, skill := range loaded.manifest.Skills {
		gotCategories[skill.Category]++
		if skill.Icon.Registry == "" || skill.Icon.Key == "" || skill.Risk.Level == "" || len(skill.Risk.Reasons) == 0 ||
			skill.Provenance.SourceReference == "" || skill.Provenance.SourceSHA256 == "" ||
			skill.License.Expression == "" || skill.Capabilities.ExecutionMode == "" {
			t.Fatalf("v2 metadata was not consumed for %s: %#v", skill.ID, skill)
		}
		if installableSkill(skill) {
			installable++
		} else if skill.Release.CatalogStatus == "blocked" && catalogEntryMode(skill) == "catalog-only" {
			blocked++
		} else {
			t.Fatalf("unexpected v2 entry classification for %s", skill.ID)
		}
	}
	if fmt.Sprint(gotCategories) != fmt.Sprint(wantCategories) || installable != 1 || blocked != 37 {
		t.Fatalf("v2 catalog counts: categories=%v installable=%d blocked=%d", gotCategories, installable, blocked)
	}

	runtime := newTestRuntime("copywriting")
	managedRoot := filepath.Join(t.TempDir(), "managed")
	service, err := NewService(Config{BundleRoot: bundleRoot, ManagedRoot: managedRoot, Runtime: runtime})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := service.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Skills) != 38 {
		t.Fatalf("query projected %d Skills, want 38", len(snapshot.Skills))
	}
	ready := 0
	for index, state := range snapshot.Skills {
		if state.ID != loaded.manifest.Skills[index].ID {
			t.Fatalf("query order[%d]=%s, want %s", index, state.ID, loaded.manifest.Skills[index].ID)
		}
		if state.CapabilityReadiness == "ready" {
			ready++
		}
		if state.CatalogStatus == "blocked" && state.CatalogBlockedReason == "" {
			t.Fatalf("blocked query projection omitted reason for %s", state.ID)
		}
		if state.CatalogStatus == "installable" && state.CatalogBlockedReason != "" {
			t.Fatalf("installable query projection leaked blocked reason for %s", state.ID)
		}
	}
	if ready != 8 {
		t.Fatalf("v2 capability projection ready=%d, want 8", ready)
	}

	blockedSkill := loaded.manifest.Skills[0]
	runtime.mu.Lock()
	runtime.failListCount = 1
	runtime.mu.Unlock()
	_, err = service.Install(context.Background(), InstallInput{
		OperationID:           "019fbd88-cbc3-7bf1-934d-7b05cd693fb0",
		SkillID:               blockedSkill.ID,
		ExpectedVersion:       blockedSkill.Version,
		ExpectedArchiveSHA256: strings.Repeat("f", 64),
		CatalogRevision:       loaded.revision,
	})
	assertCode(t, err, CodeNotInstallable)
	runtime.mu.Lock()
	runtime.failListCount = 0
	runtime.mu.Unlock()
	assertNoSkillTransactionResidue(t, service.managedRoot, blockedSkill.ID)
	scanned, err := service.Scan(context.Background(), ScanInput{
		OperationID: "019fbd88-cbc3-7bf1-934d-7b05cd693fb3", Reason: "page_open",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Close(); err != nil {
		t.Fatal(err)
	}
	journalPath := filepath.Join(managedRoot, stateDirectoryName, operationsFileName)
	journal, err := os.ReadFile(journalPath)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(journal, []byte(`"CatalogBlockedReason"`)) {
		t.Fatal("schema_version 1 journal emitted a Manifest v2-only field")
	}
	var previousHostJournal preManifestV2OperationStore
	if err := strictJSON(journal, &previousHostJournal); err != nil {
		t.Fatalf("previous Host cannot strictly decode v2 operation journal: %v", err)
	}

	// Development builds briefly wrote CatalogBlockedReason directly into the
	// v1 journal. Accept that transitional shape, rewrite it to the downgrade-
	// safe v1 DTO, and derive reasons again from the current signed catalog.
	blockedStatus := []byte(`"CatalogStatus":"blocked",`)
	transitionalField := []byte(`"CatalogStatus":"blocked","CatalogBlockedReason":"license_unverified",`)
	transitionalJournal := bytes.ReplaceAll(journal, blockedStatus, transitionalField)
	if bytes.Equal(journal, transitionalJournal) {
		t.Fatal("synthetic transitional journal contained no blocked state")
	}
	if err := os.WriteFile(journalPath, transitionalJournal, 0o600); err != nil {
		t.Fatal(err)
	}
	restarted, err := NewService(Config{BundleRoot: bundleRoot, ManagedRoot: managedRoot, Runtime: newTestRuntime("copywriting")})
	if err != nil {
		t.Fatalf("restart with persisted v2 snapshot: %v", err)
	}
	defer restarted.Close()
	replayed, err := restarted.Scan(context.Background(), ScanInput{
		OperationID: "019fbd88-cbc3-7bf1-934d-7b05cd693fb3", Reason: "page_open",
	})
	if err != nil || !replayed.ScannedAt.Equal(scanned.ScannedAt) ||
		stateByID(replayed, blockedSkill.ID).CatalogBlockedReason != "license_unverified" {
		t.Fatalf("v2 blocked snapshot did not replay exactly: snapshot=%#v err=%v", replayed, err)
	}
	migratedJournal, err := os.ReadFile(journalPath)
	if err != nil || bytes.Contains(migratedJournal, []byte(`"CatalogBlockedReason"`)) {
		t.Fatalf("transitional blocked journal was not rewritten to v1: err=%v", err)
	}
	previousHostJournal = preManifestV2OperationStore{}
	if err := strictJSON(migratedJournal, &previousHostJournal); err != nil {
		t.Fatalf("previous Host cannot decode migrated journal: %v", err)
	}
}

func TestInstallFailsAtomicallyWhenDeclaredArchiveDisappears(t *testing.T) {
	bundleRoot, manifest := makeTestBundle(t, []testZipEntry{
		{name: "LICENSE", body: "fixture"}, {name: "SKILL.md", body: "valid"},
	}, "0.1.0")
	service, err := NewService(Config{
		BundleRoot: bundleRoot, ManagedRoot: filepath.Join(t.TempDir(), "managed"),
		Runtime: newTestRuntime(manifest.Skills[0].RuntimeName),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	if err := os.Remove(filepath.Join(bundleRoot, manifest.Skills[0].Archive.Path)); err != nil {
		t.Fatal(err)
	}
	_, err = service.Install(context.Background(), installInput(manifest, "019fbd88-cbc3-7bf1-934d-7b05cd693fb1"))
	assertCode(t, err, CodeBundleMissing)
	assertNoSkillTransactionResidue(t, service.managedRoot, manifest.Skills[0].ID)
}

func TestManifestV2ArchiveDigestMismatchIsAtomic(t *testing.T) {
	bundleRoot, manifest := makeTestBundle(t, []testZipEntry{
		{name: "LICENSE", body: "fixture"}, {name: "SKILL.md", body: "valid"},
	}, "0.1.0")
	service, err := NewService(Config{
		BundleRoot: bundleRoot, ManagedRoot: filepath.Join(t.TempDir(), "managed"),
		Runtime: newTestRuntime(manifest.Skills[0].RuntimeName),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	archivePath := filepath.Join(bundleRoot, manifest.Skills[0].Archive.Path)
	archive, err := os.ReadFile(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	archive[len(archive)-1] ^= 0xff
	if err := os.Chmod(archivePath, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(archivePath, archive, 0o400); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(archivePath, 0o400); err != nil {
		t.Fatal(err)
	}
	_, err = service.Install(context.Background(), installInput(manifest, "019fbd88-cbc3-7bf1-934d-7b05cd693fb2"))
	assertCode(t, err, CodeArchiveChecksum)
	assertNoSkillTransactionResidue(t, service.managedRoot, manifest.Skills[0].ID)
}

func TestArchivePreflightRejectsUnsafeEntrySets(t *testing.T) {
	tests := []struct {
		name    string
		entries []testZipEntry
	}{
		{name: "reserved receipt", entries: []testZipEntry{{name: receiptFileName, body: "x"}, {name: "SKILL.md", body: "ok"}}},
		{name: "reserved marker", entries: []testZipEntry{{name: disabledFileName, body: ""}, {name: "SKILL.md", body: "ok"}}},
		{name: "exact duplicate", entries: []testZipEntry{{name: "SKILL.md", body: "one"}, {name: "SKILL.md", body: "two"}}},
		{name: "case fold collision", entries: []testZipEntry{{name: "SKILL.md", body: "ok"}, {name: "Readme", body: "one"}, {name: "README", body: "two"}}},
		{name: "unicode normalization collision", entries: []testZipEntry{{name: "SKILL.md", body: "ok"}, {name: "caf\u00e9.txt", body: "one"}, {name: "cafe\u0301.txt", body: "two"}}},
		{name: "directory normalization collision", entries: []testZipEntry{{name: "SKILL.md", body: "ok"}, {name: "Assets/one", body: "one"}, {name: "assets/two", body: "two"}}},
		{name: "file parent conflict", entries: []testZipEntry{{name: "SKILL.md", body: "ok"}, {name: "parent", body: "one"}, {name: "parent/child", body: "two"}}},
		{name: "backslash", entries: []testZipEntry{{name: "SKILL.md", body: "ok"}, {name: `dir\escape`, body: "two"}}},
		{name: "absolute", entries: []testZipEntry{{name: "SKILL.md", body: "ok"}, {name: "/escape", body: "two"}}},
		{name: "zip slip", entries: []testZipEntry{{name: "SKILL.md", body: "ok"}, {name: "../escape.txt", body: "two"}}},
		{name: "nested Skill entrypoint", entries: []testZipEntry{{name: "SKILL.md", body: "ok"}, {name: "nested/SKILL.md", body: "hidden second Skill"}}},
		{name: "symlink", entries: []testZipEntry{{name: "SKILL.md", body: "ok"}, {name: "link", body: "target", mode: os.ModeSymlink | 0o777}}},
		{name: "special", entries: []testZipEntry{{name: "SKILL.md", body: "ok"}, {name: "pipe", mode: os.ModeNamedPipe | 0o600}}},
	}
	for index, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			bundleRoot, manifest := makeTestBundle(t, test.entries, "0.1.0")
			runtime := newTestRuntime(manifest.Skills[0].RuntimeName)
			service, err := NewService(Config{BundleRoot: bundleRoot, ManagedRoot: filepath.Join(t.TempDir(), "managed"), Runtime: runtime})
			if err != nil {
				t.Fatalf("NewService: %v", err)
			}
			defer service.Close()
			_, err = service.Install(context.Background(), installInput(manifest, fmt.Sprintf("019fbd88-cbc3-7bf1-934d-%012d", 100+index)))
			assertCode(t, err, CodeArchiveUnsafe)
			if _, statErr := os.Lstat(filepath.Join(service.managedRoot, testSkillID)); !errors.Is(statErr, os.ErrNotExist) {
				t.Fatalf("unsafe archive left managed target: %v", statErr)
			}
		})
	}
}

func TestScanFailsClosedForMovedMissingAndCorruptFiles(t *testing.T) {
	bundleRoot, _ := copyPinnedTestBundle(t, "manifest-valid.json", "fixture-model-only-0.1.0.zip")
	catalog, err := loadCatalog(bundleRoot)
	if err != nil {
		t.Fatal(err)
	}
	runtime := newTestRuntime(catalog.manifest.Skills[0].RuntimeName)
	service, err := NewService(Config{BundleRoot: bundleRoot, ManagedRoot: filepath.Join(t.TempDir(), "managed"), Runtime: runtime})
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	if _, err := service.Install(context.Background(), InstallInput{
		OperationID: "019fbd88-cbc3-7bf1-934d-7b05cd693f80", SkillID: testSkillID,
		ExpectedVersion: catalog.manifest.Skills[0].Version, ExpectedArchiveSHA256: catalog.manifest.Skills[0].Archive.SHA256,
		CatalogRevision: catalog.revision,
	}); err != nil {
		t.Fatal(err)
	}
	entrypoint := filepath.Join(service.managedRoot, testSkillID, "SKILL.md")
	if err := os.WriteFile(entrypoint, []byte("tampered"), 0o600); err != nil {
		t.Fatal(err)
	}
	snapshot, err := service.List(context.Background())
	if err != nil {
		t.Fatalf("scan corrupt install: %v", err)
	}
	if state := stateByID(snapshot, testSkillID); state.InstallationStatus != "error" || state.FailureCode != string(CodeInstalledFilesCorrupt) || state.RuntimeVisible {
		t.Fatalf("corrupt projection=%#v", state)
	}
	if err := os.RemoveAll(filepath.Join(service.managedRoot, testSkillID)); err != nil {
		t.Fatal(err)
	}
	snapshot, err = service.List(context.Background())
	if err != nil {
		t.Fatalf("scan missing install: %v", err)
	}
	if state := stateByID(snapshot, testSkillID); state.InstallationStatus != "not_installed" || state.Enabled || state.RuntimeVisible {
		t.Fatalf("missing projection=%#v", state)
	}
}

func TestUpgradeRuntimeFailureAtomicallyRestoresUsableVersion(t *testing.T) {
	bundleRoot, _ := copyPinnedTestBundle(t, "manifest-valid.json", "fixture-model-only-0.1.0.zip")
	initialCatalog, err := loadCatalog(bundleRoot)
	if err != nil {
		t.Fatal(err)
	}
	runtime := newTestRuntime(initialCatalog.manifest.Skills[0].RuntimeName)
	service, err := NewService(Config{BundleRoot: bundleRoot, ManagedRoot: filepath.Join(t.TempDir(), "managed"), Runtime: runtime})
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	initialInput := InstallInput{
		OperationID: "019fbd88-cbc3-7bf1-934d-7b05cd693f81", SkillID: testSkillID,
		ExpectedVersion:       initialCatalog.manifest.Skills[0].Version,
		ExpectedArchiveSHA256: initialCatalog.manifest.Skills[0].Archive.SHA256,
		CatalogRevision:       initialCatalog.revision,
	}
	if _, err := service.Install(context.Background(), initialInput); err != nil {
		t.Fatal(err)
	}

	upgradedRoot, upgraded := makeTestBundle(t, []testZipEntry{
		{name: "LICENSE", body: "fixture"}, {name: "SKILL.md", body: "version two"},
	}, "0.2.0")
	copyBundleContents(t, upgradedRoot, bundleRoot)
	runtime.mu.Lock()
	runtime.failListCount = 1
	runtime.mu.Unlock()
	_, err = service.Install(context.Background(), InstallInput{
		OperationID: "019fbd88-cbc3-7bf1-934d-7b05cd693f82", SkillID: testSkillID,
		ExpectedVersion: upgraded.Skills[0].Version, ExpectedArchiveSHA256: upgraded.Skills[0].Archive.SHA256,
		CatalogRevision: upgraded.revision,
	})
	assertCode(t, err, CodeRuntimeSyncFailed)
	snapshot, err := service.List(context.Background())
	if err != nil {
		t.Fatalf("scan restored version: %v", err)
	}
	state := stateByID(snapshot, testSkillID)
	if state.InstallationStatus != "installed" || state.Version != "0.1.0" || !state.RuntimeVisible {
		t.Fatalf("rollback did not preserve usable version: %#v", state)
	}
}

func TestOperationBusyConcurrentReplayAndRestartConflict(t *testing.T) {
	bundleRoot, _ := copyPinnedTestBundle(t, "manifest-valid.json", "fixture-model-only-0.1.0.zip")
	catalog, err := loadCatalog(bundleRoot)
	if err != nil {
		t.Fatal(err)
	}
	runtime := newTestRuntime(catalog.manifest.Skills[0].RuntimeName)
	runtime.setRootsEntered = make(chan struct{}, 1)
	runtime.blockSetRoots = make(chan struct{})
	managedRoot := filepath.Join(t.TempDir(), "managed")
	service, err := NewService(Config{BundleRoot: bundleRoot, ManagedRoot: managedRoot, Runtime: runtime})
	if err != nil {
		t.Fatal(err)
	}
	input := InstallInput{
		OperationID: "019fbd88-cbc3-7bf1-934d-7b05cd693f83", SkillID: testSkillID,
		ExpectedVersion:       catalog.manifest.Skills[0].Version,
		ExpectedArchiveSHA256: catalog.manifest.Skills[0].Archive.SHA256,
		CatalogRevision:       catalog.revision,
	}
	type result struct {
		state State
		err   error
	}
	first := make(chan result, 1)
	second := make(chan result, 1)
	go func() { state, err := service.Install(context.Background(), input); first <- result{state, err} }()
	select {
	case <-runtime.setRootsEntered:
	case <-time.After(3 * time.Second):
		t.Fatal("first mutation did not reach Runtime")
	}
	go func() { state, err := service.Install(context.Background(), input); second <- result{state, err} }()
	_, busyErr := service.Scan(context.Background(), ScanInput{OperationID: "019fbd88-cbc3-7bf1-934d-7b05cd693f84", Reason: "user_retry"})
	assertCode(t, busyErr, CodeBusy)
	select {
	case replay := <-second:
		t.Fatalf("same operation returned before owner completed: %#v", replay)
	case <-time.After(50 * time.Millisecond):
	}
	close(runtime.blockSetRoots)
	ownerResult := <-first
	replayResult := <-second
	if ownerResult.err != nil || replayResult.err != nil || ownerResult.state != replayResult.state {
		t.Fatalf("concurrent replay mismatch: owner=%#v replay=%#v", ownerResult, replayResult)
	}
	if err := service.Close(); err != nil {
		t.Fatal(err)
	}

	restarted, err := NewService(Config{BundleRoot: bundleRoot, ManagedRoot: managedRoot, Runtime: newTestRuntime(catalog.manifest.Skills[0].RuntimeName)})
	if err != nil {
		t.Fatal(err)
	}
	defer restarted.Close()
	conflicting := input
	conflicting.ExpectedVersion = "0.1.1"
	_, err = restarted.Install(context.Background(), conflicting)
	assertCode(t, err, CodeOperationConflict)
}

func TestUnconfirmedFilesystemCommitRollsBackAndResumesAfterRestart(t *testing.T) {
	bundleRoot, _ := copyPinnedTestBundle(t, "manifest-valid.json", "fixture-model-only-0.1.0.zip")
	catalog, err := loadCatalog(bundleRoot)
	if err != nil {
		t.Fatal(err)
	}
	managedRoot := filepath.Join(t.TempDir(), "managed")
	service, err := NewService(Config{
		BundleRoot: bundleRoot, ManagedRoot: managedRoot, Runtime: newTestRuntime(catalog.manifest.Skills[0].RuntimeName),
	})
	if err != nil {
		t.Fatal(err)
	}
	input := InstallInput{
		OperationID: "019fbd88-cbc3-7bf1-934d-7b05cd693f87", SkillID: testSkillID,
		ExpectedVersion:       catalog.manifest.Skills[0].Version,
		ExpectedArchiveSHA256: catalog.manifest.Skills[0].Archive.SHA256,
		CatalogRevision:       catalog.revision,
	}
	fingerprint := operationFingerprint(
		"install", input.OperationID, input.SkillID, input.ExpectedVersion,
		input.ExpectedArchiveSHA256, input.CatalogRevision,
	)
	record, owner, err := service.reserveOperation(input.OperationID, fingerprint, "install", input.SkillID)
	if err != nil || !owner {
		t.Fatalf("reserve synthetic interrupted operation: owner=%v err=%v", owner, err)
	}
	stageName := transactionName(".yijie-staging", "", input.OperationID)
	if _, err := extractArchive(service.bundle, service.managed, stageName, catalog.manifest.Skills[0], catalog.revision); err != nil {
		t.Fatal(err)
	}
	if err := service.updateOperationPhase(record, "committing", false, false); err != nil {
		t.Fatal(err)
	}
	if err := service.managed.Rename(stageName, input.SkillID); err != nil {
		t.Fatal(err)
	}
	if err := service.updateOperationPhase(record, "filesystem_committed", false, false); err != nil {
		t.Fatal(err)
	}
	if err := service.Close(); err != nil {
		t.Fatal(err)
	}

	restarted, err := NewService(Config{
		BundleRoot: bundleRoot, ManagedRoot: managedRoot, Runtime: newTestRuntime(catalog.manifest.Skills[0].RuntimeName),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer restarted.Close()
	if _, err := os.Lstat(filepath.Join(managedRoot, input.SkillID)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("unconfirmed first install survived restart recovery: %v", err)
	}
	state, err := restarted.Install(context.Background(), input)
	if err != nil || state.InstallationStatus != "installed" || !state.RuntimeVisible {
		t.Fatalf("matching operation did not safely resume: state=%#v err=%v", state, err)
	}
}

func TestRuntimeChangedNotificationOnlySignalsAndCoalesces(t *testing.T) {
	bundleRoot, _ := copyPinnedTestBundle(t, "manifest-valid.json", "fixture-model-only-0.1.0.zip")
	catalog, err := loadCatalog(bundleRoot)
	if err != nil {
		t.Fatal(err)
	}
	runtime := newTestRuntime(catalog.manifest.Skills[0].RuntimeName)
	service, err := NewService(Config{BundleRoot: bundleRoot, ManagedRoot: filepath.Join(t.TempDir(), "managed"), Runtime: runtime})
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	for index := 0; index < 100; index++ {
		if err := service.HandleRuntimeNotification(json.RawMessage(`{}`)); err != nil {
			t.Fatal(err)
		}
	}
	runtime.mu.Lock()
	calls := runtime.calls
	runtime.mu.Unlock()
	if calls != 0 || len(service.changed) != 1 {
		t.Fatalf("notification callback performed work or failed to coalesce: calls=%d queued=%d", calls, len(service.changed))
	}
	if err := service.HandleRuntimeNotification(json.RawMessage(`{"unexpected":true}`)); err == nil {
		t.Fatal("invalid skills/changed payload was accepted")
	}
}

func TestRuntimeRegistersOnlyValidatedCatalogSkillRoots(t *testing.T) {
	bundleRoot, _ := copyPinnedTestBundle(t, "manifest-valid.json", "fixture-model-only-0.1.0.zip")
	catalog, err := loadCatalog(bundleRoot)
	if err != nil {
		t.Fatal(err)
	}
	managedRoot := filepath.Join(t.TempDir(), "managed")
	runtime := newTestRuntime(catalog.manifest.Skills[0].RuntimeName)
	service, err := NewService(Config{BundleRoot: bundleRoot, ManagedRoot: managedRoot, Runtime: runtime})
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()

	orphanRoot := filepath.Join(managedRoot, "orphan")
	if err := os.MkdirAll(orphanRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(orphanRoot, "SKILL.md"), []byte("orphan"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Install(context.Background(), InstallInput{
		OperationID: "019fbd88-cbc3-7bf1-934d-7b05cd693f90", SkillID: testSkillID,
		ExpectedVersion: catalog.manifest.Skills[0].Version, ExpectedArchiveSHA256: catalog.manifest.Skills[0].Archive.SHA256,
		CatalogRevision: catalog.revision,
	}); err != nil {
		t.Fatal(err)
	}
	runtime.mu.Lock()
	roots := append([]string(nil), runtime.roots...)
	runtime.mu.Unlock()
	wantRoot := filepath.Join(service.managedRoot, testSkillID)
	if len(roots) != 1 || roots[0] != wantRoot {
		t.Fatalf("Runtime roots=%v, want only %s", roots, wantRoot)
	}

	nestedRoot := filepath.Join(wantRoot, "nested")
	if err := os.Mkdir(nestedRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nestedRoot, "SKILL.md"), []byte("injected"), 0o600); err != nil {
		t.Fatal(err)
	}
	snapshot, err := service.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	state := stateByID(snapshot, testSkillID)
	if state.InstallationStatus != "error" || state.RuntimeVisible {
		t.Fatalf("injected nested Skill did not fail closed: %#v", state)
	}
	runtime.mu.Lock()
	roots = append([]string(nil), runtime.roots...)
	runtime.mu.Unlock()
	if len(roots) != 0 {
		t.Fatalf("invalid Skill root remained registered: %v", roots)
	}
}

func TestRuntimeChangedFromExtraRootsSetDoesNotSelfLoop(t *testing.T) {
	bundleRoot, _ := copyPinnedTestBundle(t, "manifest-valid.json", "fixture-model-only-0.1.0.zip")
	catalog, err := loadCatalog(bundleRoot)
	if err != nil {
		t.Fatal(err)
	}
	managedRoot := filepath.Join(t.TempDir(), "managed")
	initial, err := NewService(Config{BundleRoot: bundleRoot, ManagedRoot: managedRoot, Runtime: newTestRuntime(catalog.manifest.Skills[0].RuntimeName)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := initial.Install(context.Background(), InstallInput{
		OperationID: "019fbd88-cbc3-7bf1-934d-7b05cd693f91", SkillID: testSkillID,
		ExpectedVersion: catalog.manifest.Skills[0].Version, ExpectedArchiveSHA256: catalog.manifest.Skills[0].Archive.SHA256,
		CatalogRevision: catalog.revision,
	}); err != nil {
		t.Fatal(err)
	}
	if err := initial.Close(); err != nil {
		t.Fatal(err)
	}

	runtime := newTestRuntime(catalog.manifest.Skills[0].RuntimeName)
	restarted, err := NewService(Config{BundleRoot: bundleRoot, ManagedRoot: managedRoot, Runtime: runtime})
	if err != nil {
		t.Fatal(err)
	}
	defer restarted.Close()
	runtime.mu.Lock()
	runtime.onSetRoots = func() {
		if err := restarted.HandleRuntimeNotification(json.RawMessage(`{}`)); err != nil {
			t.Errorf("handle pre-response notification: %v", err)
		}
	}
	runtime.mu.Unlock()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		restarted.Run(ctx)
		close(done)
	}()
	deadline := time.Now().Add(2 * time.Second)
	for {
		runtime.mu.Lock()
		setCalls := runtime.setRootsCalls
		runtime.mu.Unlock()
		if setCalls > 0 {
			break
		}
		if time.Now().After(deadline) {
			cancel()
			<-done
			t.Fatal("Runtime roots were never registered")
		}
		time.Sleep(5 * time.Millisecond)
	}
	time.Sleep(100 * time.Millisecond)
	runtime.mu.Lock()
	setCalls := runtime.setRootsCalls
	totalCalls := runtime.calls
	runtime.mu.Unlock()
	cancel()
	<-done
	if setCalls != 1 || totalCalls > 8 {
		t.Fatalf("skills/changed feedback loop: set calls=%d total calls=%d", setCalls, totalCalls)
	}
}

func TestOperationIDsAreNotEvictedAfterSixtyFourCompletions(t *testing.T) {
	bundleRoot, _ := copyPinnedTestBundle(t, "manifest-valid.json", "fixture-model-only-0.1.0.zip")
	catalog, err := loadCatalog(bundleRoot)
	if err != nil {
		t.Fatal(err)
	}
	managedRoot := filepath.Join(t.TempDir(), "managed")
	service, err := NewService(Config{BundleRoot: bundleRoot, ManagedRoot: managedRoot, Runtime: newTestRuntime(catalog.manifest.Skills[0].RuntimeName)})
	if err != nil {
		t.Fatal(err)
	}
	firstID := "019fbd88-cbc3-7bf1-934e-000000000000"
	var first Snapshot
	for index := 0; index < 70; index++ {
		operationID := fmt.Sprintf("019fbd88-cbc3-7bf1-934e-%012d", index)
		snapshot, err := service.Scan(context.Background(), ScanInput{OperationID: operationID, Reason: "page_open"})
		if err != nil {
			t.Fatalf("scan %d: %v", index, err)
		}
		if index == 0 {
			first = snapshot
		}
	}
	if err := service.Close(); err != nil {
		t.Fatal(err)
	}
	restarted, err := NewService(Config{BundleRoot: bundleRoot, ManagedRoot: managedRoot, Runtime: newTestRuntime(catalog.manifest.Skills[0].RuntimeName)})
	if err != nil {
		t.Fatal(err)
	}
	defer restarted.Close()
	replayed, err := restarted.Scan(context.Background(), ScanInput{OperationID: firstID, Reason: "page_open"})
	if err != nil || !replayed.ScannedAt.Equal(first.ScannedAt) {
		t.Fatalf("old operation was not replayed: snapshot=%#v err=%v", replayed, err)
	}
	_, err = restarted.Scan(context.Background(), ScanInput{OperationID: firstID, Reason: "window_resume"})
	assertCode(t, err, CodeOperationConflict)
}

func TestManagedRootSyncFailureRollsBackInstallAndUninstall(t *testing.T) {
	newService := func(t *testing.T) (*Service, catalog) {
		t.Helper()
		bundleRoot, _ := copyPinnedTestBundle(t, "manifest-valid.json", "fixture-model-only-0.1.0.zip")
		loaded, err := loadCatalog(bundleRoot)
		if err != nil {
			t.Fatal(err)
		}
		service, err := NewService(Config{BundleRoot: bundleRoot, ManagedRoot: filepath.Join(t.TempDir(), "managed"), Runtime: newTestRuntime(loaded.manifest.Skills[0].RuntimeName)})
		if err != nil {
			t.Fatal(err)
		}
		return service, loaded
	}
	t.Run("install", func(t *testing.T) {
		service, loaded := newService(t)
		defer service.Close()
		calls := 0
		service.syncManaged = func(root *os.Root) error {
			calls++
			if calls == 1 {
				return errors.New("synthetic directory sync failure")
			}
			return syncRoot(root)
		}
		_, err := service.Install(context.Background(), installInput(testManifestProjection{revision: loaded.revision, Skills: loaded.manifest.Skills}, "019fbd88-cbc3-7bf1-934d-7b05cd693f92"))
		assertCode(t, err, CodeInstallFailed)
		if _, statErr := os.Lstat(filepath.Join(service.managedRoot, testSkillID)); !errors.Is(statErr, os.ErrNotExist) {
			t.Fatalf("failed install survived sync rollback: %v", statErr)
		}
	})
	t.Run("uninstall", func(t *testing.T) {
		service, loaded := newService(t)
		defer service.Close()
		manifest := testManifestProjection{revision: loaded.revision, Skills: loaded.manifest.Skills}
		if _, err := service.Install(context.Background(), installInput(manifest, "019fbd88-cbc3-7bf1-934d-7b05cd693f93")); err != nil {
			t.Fatal(err)
		}
		calls := 0
		service.syncManaged = func(root *os.Root) error {
			calls++
			if calls == 1 {
				return errors.New("synthetic directory sync failure")
			}
			return syncRoot(root)
		}
		_, err := service.Uninstall(context.Background(), UninstallInput{OperationID: "019fbd88-cbc3-7bf1-934d-7b05cd693f94", SkillID: testSkillID})
		assertCode(t, err, CodeUninstallFailed)
		snapshot, listErr := service.List(context.Background())
		if listErr != nil {
			t.Fatal(listErr)
		}
		state := stateByID(snapshot, testSkillID)
		if state.InstallationStatus != "installed" || !state.Enabled || !state.RuntimeVisible {
			t.Fatalf("failed uninstall did not restore prior state: %#v", state)
		}
	})
}

func TestEnabledMarkerSyncFailureRestoresFilesystemAndRuntime(t *testing.T) {
	bundleRoot, _ := copyPinnedTestBundle(t, "manifest-valid.json", "fixture-model-only-0.1.0.zip")
	loaded, err := loadCatalog(bundleRoot)
	if err != nil {
		t.Fatal(err)
	}
	runtime := newTestRuntime(loaded.manifest.Skills[0].RuntimeName)
	service, err := NewService(Config{
		BundleRoot: bundleRoot, ManagedRoot: filepath.Join(t.TempDir(), "managed"), Runtime: runtime,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	manifest := testManifestProjection{revision: loaded.revision, Skills: loaded.manifest.Skills}
	if _, err := service.Install(context.Background(), installInput(manifest, "019fbd88-cbc3-7bf1-934d-7b05cd693fd0")); err != nil {
		t.Fatal(err)
	}

	failFirstSkillSync := func() {
		calls := 0
		service.syncSkill = func(root *os.Root) error {
			calls++
			if calls == 1 {
				return errors.New("synthetic Skill directory sync failure")
			}
			return syncRoot(root)
		}
	}
	assertState := func(wantEnabled bool) {
		t.Helper()
		snapshot, listErr := service.List(context.Background())
		if listErr != nil {
			t.Fatal(listErr)
		}
		state := stateByID(snapshot, testSkillID)
		if state.Enabled != wantEnabled || state.RuntimeVisible != wantEnabled || state.InstallationStatus != "installed" {
			t.Fatalf("enabled rollback mismatch: state=%#v wantEnabled=%v", state, wantEnabled)
		}
	}

	failFirstSkillSync()
	_, err = service.SetEnabled(context.Background(), EnabledInput{
		OperationID: "019fbd88-cbc3-7bf1-934d-7b05cd693fd1", SkillID: testSkillID, Enabled: false,
	})
	assertCode(t, err, CodeInstallFailed)
	assertState(true)

	service.syncSkill = syncRoot
	if _, err := service.SetEnabled(context.Background(), EnabledInput{
		OperationID: "019fbd88-cbc3-7bf1-934d-7b05cd693fd2", SkillID: testSkillID, Enabled: false,
	}); err != nil {
		t.Fatal(err)
	}
	assertState(false)

	failFirstSkillSync()
	_, err = service.SetEnabled(context.Background(), EnabledInput{
		OperationID: "019fbd88-cbc3-7bf1-934d-7b05cd693fd3", SkillID: testSkillID, Enabled: true,
	})
	assertCode(t, err, CodeInstallFailed)
	assertState(false)
}

func TestEnabledRollbackFailureRemainsRecoverable(t *testing.T) {
	bundleRoot, _ := copyPinnedTestBundle(t, "manifest-valid.json", "fixture-model-only-0.1.0.zip")
	loaded, err := loadCatalog(bundleRoot)
	if err != nil {
		t.Fatal(err)
	}
	service, err := NewService(Config{
		BundleRoot: bundleRoot, ManagedRoot: filepath.Join(t.TempDir(), "managed"),
		Runtime: newTestRuntime(loaded.manifest.Skills[0].RuntimeName),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	manifest := testManifestProjection{revision: loaded.revision, Skills: loaded.manifest.Skills}
	if _, err := service.Install(context.Background(), installInput(manifest, "019fbd88-cbc3-7bf1-934d-7b05cd693fd4")); err != nil {
		t.Fatal(err)
	}

	operationID := "019fbd88-cbc3-7bf1-934d-7b05cd693fd5"
	calls := 0
	service.syncSkill = func(root *os.Root) error {
		calls++
		if calls <= 2 {
			return errors.New("synthetic forward and rollback sync failure")
		}
		return syncRoot(root)
	}
	input := EnabledInput{OperationID: operationID, SkillID: testSkillID, Enabled: false}
	_, err = service.SetEnabled(context.Background(), input)
	assertCode(t, err, CodeInstallFailed)
	service.opMu.Lock()
	record := service.operations[operationID]
	if record == nil || record.Status != "in_progress" || record.Phase != "committing" {
		service.opMu.Unlock()
		t.Fatalf("ambiguous rollback was marked complete: %#v", record)
	}
	service.opMu.Unlock()

	service.syncSkill = syncRoot
	snapshot, err := service.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	state := stateByID(snapshot, testSkillID)
	if !state.Enabled || !state.RuntimeVisible {
		t.Fatalf("recovery did not restore previous enabled state: %#v", state)
	}
	_, err = service.SetEnabled(context.Background(), input)
	assertCode(t, err, CodeInstallFailed)
	state, err = service.SetEnabled(context.Background(), EnabledInput{
		OperationID: "019fbd88-cbc3-7bf1-934d-7b05cd693fdb", SkillID: testSkillID, Enabled: false,
	})
	if err != nil || state.Enabled || state.RuntimeVisible {
		t.Fatalf("new operation did not proceed after recovery: state=%#v err=%v", state, err)
	}
}

func TestAmbiguousEnabledFailureReplaysAfterPreviousHostRecovery(t *testing.T) {
	bundleRoot, _ := copyPinnedTestBundle(t, "manifest-valid.json", "fixture-model-only-0.1.0.zip")
	loaded, err := loadCatalog(bundleRoot)
	if err != nil {
		t.Fatal(err)
	}
	managedRoot := filepath.Join(t.TempDir(), "managed")
	newRuntime := func() *testRuntime {
		return newTestRuntime(loaded.manifest.Skills[0].RuntimeName)
	}
	service, err := NewService(Config{BundleRoot: bundleRoot, ManagedRoot: managedRoot, Runtime: newRuntime()})
	if err != nil {
		t.Fatal(err)
	}
	manifest := testManifestProjection{revision: loaded.revision, Skills: loaded.manifest.Skills}
	if _, err := service.Install(context.Background(), installInput(manifest, "019fbd88-cbc3-7bf1-934d-7b05cd693feb")); err != nil {
		service.Close()
		t.Fatal(err)
	}

	operationID := "019fbd88-cbc3-7bf1-934d-7b05cd693fec"
	input := EnabledInput{OperationID: operationID, SkillID: testSkillID, Enabled: false}
	calls := 0
	service.syncSkill = func(*os.Root) error {
		calls++
		if calls <= 2 {
			return errors.New("synthetic forward and rollback sync failure")
		}
		return nil
	}
	_, err = service.SetEnabled(context.Background(), input)
	assertCode(t, err, CodeInstallFailed)
	journal, readErr := os.ReadFile(filepath.Join(managedRoot, stateDirectoryName, operationsFileName))
	if readErr != nil || !bytes.Contains(journal, []byte(`"id":"`+operationID+`"`)) ||
		!bytes.Contains(journal, []byte(`"status":"in_progress"`)) ||
		!bytes.Contains(journal, []byte(`"phase":"committing"`)) ||
		!bytes.Contains(journal, []byte(`"error_code":"install_failed"`)) {
		service.Close()
		t.Fatalf("ambiguous error was not durable before restart: err=%v journal=%s", readErr, journal)
	}
	// Simulate a downgrade to the previous Host: its strict v1 DTO accepts the
	// new ErrorCode field and its recovery resets every in-progress phase to
	// reserved while preserving that field. Re-upgrading must accept and finish
	// this transitional journal instead of making the Skill service unstartable.
	var previousHostJournal preManifestV2OperationStore
	if err := strictJSON(journal, &previousHostJournal); err != nil {
		service.Close()
		t.Fatalf("previous Host could not decode ambiguous v2 journal: %v", err)
	}
	found := false
	for index := range previousHostJournal.Operations {
		operation := &previousHostJournal.Operations[index]
		if operation.ID != operationID {
			continue
		}
		found = true
		operation.Phase = "reserved"
		operation.HadPrevious = false
		operation.PreviousEnabled = false
	}
	if !found {
		service.Close()
		t.Fatal("previous Host journal omitted ambiguous operation")
	}
	downgradedJournal, err := json.Marshal(previousHostJournal)
	if err != nil {
		service.Close()
		t.Fatal(err)
	}
	downgradedJournal = append(downgradedJournal, '\n')
	if err := os.WriteFile(filepath.Join(managedRoot, stateDirectoryName, operationsFileName), downgradedJournal, 0o600); err != nil {
		service.Close()
		t.Fatal(err)
	}
	if err := service.Close(); err != nil {
		t.Fatal(err)
	}

	restarted, err := NewService(Config{BundleRoot: bundleRoot, ManagedRoot: managedRoot, Runtime: newRuntime()})
	if err != nil {
		t.Fatal(err)
	}
	defer restarted.Close()
	_, err = restarted.SetEnabled(context.Background(), input)
	assertCode(t, err, CodeInstallFailed)
	snapshot, err := restarted.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	state := stateByID(snapshot, testSkillID)
	if !state.Enabled || !state.RuntimeVisible {
		t.Fatalf("restart replay changed the restored enabled state: %#v", state)
	}
}

func TestExistingDisabledMarkerRecoveryRetriesSkillFsync(t *testing.T) {
	bundleRoot, _ := copyPinnedTestBundle(t, "manifest-valid.json", "fixture-model-only-0.1.0.zip")
	loaded, err := loadCatalog(bundleRoot)
	if err != nil {
		t.Fatal(err)
	}
	service, err := NewService(Config{
		BundleRoot: bundleRoot, ManagedRoot: filepath.Join(t.TempDir(), "managed"),
		Runtime: newTestRuntime(loaded.manifest.Skills[0].RuntimeName),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	manifest := testManifestProjection{revision: loaded.revision, Skills: loaded.manifest.Skills}
	if _, err := service.Install(context.Background(), installInput(manifest, "019fbd88-cbc3-7bf1-934d-7b05cd693fed")); err != nil {
		t.Fatal(err)
	}
	if _, err := service.SetEnabled(context.Background(), EnabledInput{
		OperationID: "019fbd88-cbc3-7bf1-934d-7b05cd693fee", SkillID: testSkillID, Enabled: false,
	}); err != nil {
		t.Fatal(err)
	}

	operationID := "019fbd88-cbc3-7bf1-934d-7b05cd693fef"
	done := make(chan struct{})
	close(done)
	service.opMu.Lock()
	service.operations[operationID] = &operationRecord{
		ID: operationID, Fingerprint: operationFingerprint("enabled", operationID, testSkillID, "true"),
		Kind: "enabled", SkillID: testSkillID, Status: "in_progress", Phase: "committing",
		HadPrevious: true, PreviousEnabled: false, ErrorCode: CodeInstallFailed, done: done,
	}
	if err := service.persistOperationsLocked(); err != nil {
		service.opMu.Unlock()
		t.Fatal(err)
	}
	service.opMu.Unlock()

	calls := 0
	service.syncSkill = func(*os.Root) error {
		calls++
		if calls <= 2 {
			return errors.New("synthetic existing marker sync failure")
		}
		return nil
	}
	for attempt := 1; attempt <= 2; attempt++ {
		_, listErr := service.List(context.Background())
		assertCode(t, listErr, CodeScanFailed)
		service.opMu.Lock()
		phase := service.operations[operationID].Phase
		service.opMu.Unlock()
		if phase != "committing" || calls != attempt {
			t.Fatalf("recovery attempt %d cleared phase or skipped marker fsync: phase=%s calls=%d", attempt, phase, calls)
		}
	}
	service.syncSkill = syncRoot
	snapshot, err := service.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	service.opMu.Lock()
	record := service.operations[operationID]
	service.opMu.Unlock()
	if record.Status != "complete" || record.Phase != "complete" || record.ErrorCode != CodeInstallFailed {
		t.Fatalf("successful recovery did not finalize the original error: %#v", record)
	}
	state := stateByID(snapshot, testSkillID)
	if state.Enabled || state.RuntimeVisible {
		t.Fatalf("marker recovery changed the previous disabled state: %#v", state)
	}
}

func TestFailedCompletedTransactionResidueIsRolledBack(t *testing.T) {
	newInstalledService := func(t *testing.T) (*Service, catalog, string) {
		t.Helper()
		bundleRoot, _ := copyPinnedTestBundle(t, "manifest-valid.json", "fixture-model-only-0.1.0.zip")
		loaded, err := loadCatalog(bundleRoot)
		if err != nil {
			t.Fatal(err)
		}
		service, err := NewService(Config{
			BundleRoot: bundleRoot, ManagedRoot: filepath.Join(t.TempDir(), "managed"),
			Runtime: newTestRuntime(loaded.manifest.Skills[0].RuntimeName),
		})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := service.Install(context.Background(), installInput(
			testManifestProjection{revision: loaded.revision, Skills: loaded.manifest.Skills},
			"019fbd88-cbc3-7bf1-934d-7b05cd693fd6",
		)); err != nil {
			service.Close()
			t.Fatal(err)
		}
		entrypoint := filepath.Join(service.managedRoot, testSkillID, "SKILL.md")
		original, err := os.ReadFile(entrypoint)
		if err != nil {
			service.Close()
			t.Fatal(err)
		}
		return service, loaded, string(original)
	}
	addFailedRecord := func(t *testing.T, service *Service, operationID, kind string, code ErrorCode) {
		t.Helper()
		done := make(chan struct{})
		close(done)
		record := &operationRecord{
			ID: operationID, Fingerprint: operationFingerprint(kind, operationID, testSkillID),
			Kind: kind, SkillID: testSkillID, Status: "complete", Phase: "complete",
			HadPrevious: true, PreviousEnabled: true, ErrorCode: code,
			CompletedAt: time.Now().UTC(), done: done,
		}
		service.opMu.Lock()
		service.operations[operationID] = record
		err := service.persistOperationsLocked()
		service.opMu.Unlock()
		if err != nil {
			t.Fatal(err)
		}
	}

	t.Run("failed install backup", func(t *testing.T) {
		service, _, original := newInstalledService(t)
		defer service.Close()
		operationID := "019fbd88-cbc3-7bf1-934d-7b05cd693fd7"
		backupName := transactionName(".yijie-backup", testSkillID, operationID)
		if err := service.managed.Rename(testSkillID, backupName); err != nil {
			t.Fatal(err)
		}
		if err := service.managed.Mkdir(testSkillID, 0o700); err != nil {
			t.Fatal(err)
		}
		newRoot, err := service.managed.OpenRoot(testSkillID)
		if err != nil {
			t.Fatal(err)
		}
		if err := writeOwnerOnlyFile(newRoot, "SKILL.md", []byte("unconfirmed replacement")); err != nil {
			newRoot.Close()
			t.Fatal(err)
		}
		newRoot.Close()
		addFailedRecord(t, service, operationID, "install", CodeInstallFailed)
		snapshot, err := service.List(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		body, readErr := os.ReadFile(filepath.Join(service.managedRoot, testSkillID, "SKILL.md"))
		if readErr != nil || string(body) != original || !stateByID(snapshot, testSkillID).RuntimeVisible {
			t.Fatalf("failed install residue was treated as committed: body=%q state=%#v err=%v", body, stateByID(snapshot, testSkillID), readErr)
		}
	})

	t.Run("failed uninstall trash", func(t *testing.T) {
		service, _, _ := newInstalledService(t)
		defer service.Close()
		operationID := "019fbd88-cbc3-7bf1-934d-7b05cd693fd8"
		trashName := transactionName(".yijie-trash", testSkillID, operationID)
		if err := service.managed.Rename(testSkillID, trashName); err != nil {
			t.Fatal(err)
		}
		addFailedRecord(t, service, operationID, "uninstall", CodeUninstallFailed)
		snapshot, err := service.List(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		state := stateByID(snapshot, testSkillID)
		if state.InstallationStatus != "installed" || !state.Enabled || !state.RuntimeVisible {
			t.Fatalf("failed uninstall trash was discarded: %#v", state)
		}
	})
}

func TestUninstallRuntimeConfirmationFailureRestoresVisibilityBeforeReturn(t *testing.T) {
	bundleRoot, _ := copyPinnedTestBundle(t, "manifest-valid.json", "fixture-model-only-0.1.0.zip")
	loaded, err := loadCatalog(bundleRoot)
	if err != nil {
		t.Fatal(err)
	}
	runtime := newTestRuntime(loaded.manifest.Skills[0].RuntimeName)
	service, err := NewService(Config{
		BundleRoot: bundleRoot, ManagedRoot: filepath.Join(t.TempDir(), "managed"), Runtime: runtime,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	if _, err := service.Install(context.Background(), installInput(
		testManifestProjection{revision: loaded.revision, Skills: loaded.manifest.Skills},
		"019fbd88-cbc3-7bf1-934d-7b05cd693fd9",
	)); err != nil {
		t.Fatal(err)
	}
	runtime.mu.Lock()
	runtime.failListAt = runtime.listCalls + 2 // mutation preflight succeeds; disable confirmation fails
	runtime.mu.Unlock()
	_, err = service.Uninstall(context.Background(), UninstallInput{
		OperationID: "019fbd88-cbc3-7bf1-934d-7b05cd693fda", SkillID: testSkillID,
	})
	assertCode(t, err, CodeRuntimeSyncFailed)
	entrypoint := filepath.Join(service.managedRoot, testSkillID, "SKILL.md")
	runtime.mu.Lock()
	enabled := runtime.enabled[entrypoint]
	runtime.mu.Unlock()
	if !enabled {
		t.Fatal("failed uninstall returned while Runtime still had the previous Skill disabled")
	}
	snapshot, listErr := service.List(context.Background())
	if listErr != nil {
		t.Fatal(listErr)
	}
	state := stateByID(snapshot, testSkillID)
	if state.InstallationStatus != "installed" || !state.Enabled || !state.RuntimeVisible {
		t.Fatalf("failed uninstall did not preserve the old usable state: %#v", state)
	}
}

func TestRuntimeRootsApplyThenErrorIsCompensated(t *testing.T) {
	bundleRoot, _ := copyPinnedTestBundle(t, "manifest-valid.json", "fixture-model-only-0.1.0.zip")
	loaded, err := loadCatalog(bundleRoot)
	if err != nil {
		t.Fatal(err)
	}
	runtime := newTestRuntime(loaded.manifest.Skills[0].RuntimeName)
	service, err := NewService(Config{
		BundleRoot: bundleRoot, ManagedRoot: filepath.Join(t.TempDir(), "managed"), Runtime: runtime,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	if _, err := service.Install(context.Background(), installInput(
		testManifestProjection{revision: loaded.revision, Skills: loaded.manifest.Skills},
		"019fbd88-cbc3-7bf1-934d-7b05cd693fdc",
	)); err != nil {
		t.Fatal(err)
	}
	runtime.mu.Lock()
	runtime.applyRootsThenFailCount = 1
	runtime.mu.Unlock()
	_, err = service.Uninstall(context.Background(), UninstallInput{
		OperationID: "019fbd88-cbc3-7bf1-934d-7b05cd693fdd", SkillID: testSkillID,
	})
	assertCode(t, err, CodeRuntimeSyncFailed)
	runtime.mu.Lock()
	roots := append([]string(nil), runtime.roots...)
	runtime.mu.Unlock()
	wantRoot := filepath.Join(service.managedRoot, testSkillID)
	if len(roots) != 1 || roots[0] != wantRoot {
		t.Fatalf("ambiguous roots response was not compensated: roots=%v want=%s", roots, wantRoot)
	}
	snapshot, err := service.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	state := stateByID(snapshot, testSkillID)
	if !state.Enabled || !state.RuntimeVisible || state.InstallationStatus != "installed" {
		t.Fatalf("roots compensation did not preserve previous state: %#v", state)
	}
}

func TestCanceledRequestUsesIndependentRollbackContext(t *testing.T) {
	bundleRoot, _ := copyPinnedTestBundle(t, "manifest-valid.json", "fixture-model-only-0.1.0.zip")
	loaded, err := loadCatalog(bundleRoot)
	if err != nil {
		t.Fatal(err)
	}
	runtime := newTestRuntime(loaded.manifest.Skills[0].RuntimeName)
	service, err := NewService(Config{
		BundleRoot: bundleRoot, ManagedRoot: filepath.Join(t.TempDir(), "managed"), Runtime: runtime,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	if _, err := service.Install(context.Background(), installInput(
		testManifestProjection{revision: loaded.revision, Skills: loaded.manifest.Skills},
		"019fbd88-cbc3-7bf1-934d-7b05cd693fde",
	)); err != nil {
		t.Fatal(err)
	}
	requestContext, cancel := context.WithCancel(context.Background())
	runtime.mu.Lock()
	runtime.onWriteConfig = func(enabled bool) {
		if !enabled {
			cancel()
		}
	}
	runtime.mu.Unlock()
	_, err = service.SetEnabled(requestContext, EnabledInput{
		OperationID: "019fbd88-cbc3-7bf1-934d-7b05cd693fdf", SkillID: testSkillID, Enabled: false,
	})
	assertCode(t, err, CodeRuntimeSyncFailed)
	runtime.mu.Lock()
	runtime.onWriteConfig = nil
	enabled := runtime.enabled[filepath.Join(service.managedRoot, testSkillID, "SKILL.md")]
	runtime.mu.Unlock()
	if !enabled {
		t.Fatal("rollback reused canceled request context and left Runtime disabled")
	}
	snapshot, listErr := service.List(context.Background())
	if listErr != nil {
		t.Fatal(listErr)
	}
	state := stateByID(snapshot, testSkillID)
	if !state.Enabled || !state.RuntimeVisible {
		t.Fatalf("canceled request changed durable enabled state: %#v", state)
	}
}

func TestRecoveryFsyncPrecedesJournalPhaseClear(t *testing.T) {
	bundleRoot, _ := copyPinnedTestBundle(t, "manifest-valid.json", "fixture-model-only-0.1.0.zip")
	loaded, err := loadCatalog(bundleRoot)
	if err != nil {
		t.Fatal(err)
	}
	managedRoot := filepath.Join(t.TempDir(), "managed")
	service, err := NewService(Config{
		BundleRoot: bundleRoot, ManagedRoot: managedRoot, Runtime: newTestRuntime(loaded.manifest.Skills[0].RuntimeName),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Install(context.Background(), installInput(
		testManifestProjection{revision: loaded.revision, Skills: loaded.manifest.Skills},
		"019fbd88-cbc3-7bf1-934d-7b05cd693fe0",
	)); err != nil {
		service.Close()
		t.Fatal(err)
	}
	operationID := "019fbd88-cbc3-7bf1-934d-7b05cd693fe1"
	trashName := transactionName(".yijie-trash", testSkillID, operationID)
	if err := service.managed.Rename(testSkillID, trashName); err != nil {
		service.Close()
		t.Fatal(err)
	}
	done := make(chan struct{})
	close(done)
	service.opMu.Lock()
	service.operations[operationID] = &operationRecord{
		ID: operationID, Fingerprint: operationFingerprint("uninstall", operationID, testSkillID),
		Kind: "uninstall", SkillID: testSkillID, Status: "in_progress", Phase: "filesystem_committed",
		HadPrevious: true, PreviousEnabled: true, done: done,
	}
	if err := service.persistOperationsLocked(); err != nil {
		service.opMu.Unlock()
		service.Close()
		t.Fatal(err)
	}
	service.opMu.Unlock()
	service.syncManaged = func(*os.Root) error { return errors.New("synthetic recovery directory sync failure") }
	_, err = service.List(context.Background())
	assertCode(t, err, CodeScanFailed)
	service.opMu.Lock()
	phase := service.operations[operationID].Phase
	service.opMu.Unlock()
	if phase != "filesystem_committed" {
		service.Close()
		t.Fatalf("journal phase cleared before recovery fsync: %s", phase)
	}
	journal, readErr := os.ReadFile(filepath.Join(managedRoot, stateDirectoryName, operationsFileName))
	if readErr != nil || !bytes.Contains(journal, []byte(`"phase":"filesystem_committed"`)) {
		service.Close()
		t.Fatalf("durable journal lost recovery phase: err=%v journal=%s", readErr, journal)
	}
	service.syncManaged = syncRoot
	if err := service.Close(); err != nil {
		t.Fatal(err)
	}
	// Simulate the rename disappearing after the failed directory fsync.
	if err := os.Rename(filepath.Join(managedRoot, testSkillID), filepath.Join(managedRoot, trashName)); err != nil {
		t.Fatal(err)
	}
	restarted, err := NewService(Config{
		BundleRoot: bundleRoot, ManagedRoot: managedRoot, Runtime: newTestRuntime(loaded.manifest.Skills[0].RuntimeName),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer restarted.Close()
	snapshot, err := restarted.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	state := stateByID(snapshot, testSkillID)
	if state.InstallationStatus != "installed" || !state.RuntimeVisible {
		t.Fatalf("second recovery did not restore unconfirmed uninstall: %#v", state)
	}
}

func TestRecoveryRetriesManagedFsyncWithoutTransactionResidue(t *testing.T) {
	for _, kind := range []string{"install", "uninstall"} {
		t.Run(kind, func(t *testing.T) {
			bundleRoot, _ := copyPinnedTestBundle(t, "manifest-valid.json", "fixture-model-only-0.1.0.zip")
			loaded, err := loadCatalog(bundleRoot)
			if err != nil {
				t.Fatal(err)
			}
			service, err := NewService(Config{
				BundleRoot: bundleRoot, ManagedRoot: filepath.Join(t.TempDir(), "managed"),
				Runtime: newTestRuntime(loaded.manifest.Skills[0].RuntimeName),
			})
			if err != nil {
				t.Fatal(err)
			}
			defer service.Close()
			if _, err := service.Install(context.Background(), installInput(
				testManifestProjection{revision: loaded.revision, Skills: loaded.manifest.Skills},
				"019fbd88-cbc3-7bf1-934d-7b05cd693ff0",
			)); err != nil {
				t.Fatal(err)
			}

			operationID := "019fbd88-cbc3-7bf1-934d-7b05cd693ff1"
			if kind == "uninstall" {
				operationID = "019fbd88-cbc3-7bf1-934d-7b05cd693ff2"
			}
			done := make(chan struct{})
			close(done)
			service.opMu.Lock()
			service.operations[operationID] = &operationRecord{
				ID: operationID, Fingerprint: operationFingerprint(kind, operationID, testSkillID),
				Kind: kind, SkillID: testSkillID, Status: "in_progress", Phase: "filesystem_committed",
				HadPrevious: true, PreviousEnabled: true, ErrorCode: CodeInstallFailed, done: done,
			}
			if err := service.persistOperationsLocked(); err != nil {
				service.opMu.Unlock()
				t.Fatal(err)
			}
			service.opMu.Unlock()

			calls := 0
			service.syncManaged = func(*os.Root) error {
				calls++
				if calls <= 2 {
					return errors.New("synthetic residue-free recovery sync failure")
				}
				return nil
			}
			for attempt := 1; attempt <= 2; attempt++ {
				_, listErr := service.List(context.Background())
				assertCode(t, listErr, CodeScanFailed)
				service.opMu.Lock()
				phase := service.operations[operationID].Phase
				service.opMu.Unlock()
				if phase != "filesystem_committed" || calls != attempt {
					t.Fatalf("recovery attempt %d cleared phase or skipped managed fsync: phase=%s calls=%d", attempt, phase, calls)
				}
			}
			service.syncManaged = syncRoot
			if _, err := service.List(context.Background()); err != nil {
				t.Fatal(err)
			}
			service.opMu.Lock()
			record := service.operations[operationID]
			service.opMu.Unlock()
			if record.Status != "complete" || record.Phase != "complete" || record.ErrorCode != CodeInstallFailed {
				t.Fatalf("successful recovery did not finalize original error: %#v", record)
			}
		})
	}
}

func TestConfirmedBackupNeverResurrectsAfterExternalMove(t *testing.T) {
	bundleRoot, _ := copyPinnedTestBundle(t, "manifest-valid.json", "fixture-model-only-0.1.0.zip")
	loaded, err := loadCatalog(bundleRoot)
	if err != nil {
		t.Fatal(err)
	}
	managedRoot := filepath.Join(t.TempDir(), "managed")
	service, err := NewService(Config{
		BundleRoot: bundleRoot, ManagedRoot: managedRoot, Runtime: newTestRuntime(loaded.manifest.Skills[0].RuntimeName),
	})
	if err != nil {
		t.Fatal(err)
	}
	operationID := "019fbd88-cbc3-7bf1-934d-7b05cd693fe2"
	if _, err := service.Install(context.Background(), installInput(
		testManifestProjection{revision: loaded.revision, Skills: loaded.manifest.Skills}, operationID,
	)); err != nil {
		service.Close()
		t.Fatal(err)
	}
	backupName := transactionName(".yijie-backup", testSkillID, operationID)
	if err := service.managed.Rename(testSkillID, backupName); err != nil {
		service.Close()
		t.Fatal(err)
	}
	stageName := transactionName(".yijie-staging", "", "019fbd88-cbc3-7bf1-934d-7b05cd693fe3")
	if _, err := extractArchive(service.bundle, service.managed, stageName, loaded.manifest.Skills[0], loaded.revision); err != nil {
		service.Close()
		t.Fatal(err)
	}
	if err := service.managed.Rename(stageName, testSkillID); err != nil {
		service.Close()
		t.Fatal(err)
	}
	if err := service.Close(); err != nil {
		t.Fatal(err)
	}
	movedRoot := filepath.Join(t.TempDir(), "externally-moved-skill")
	if err := os.Rename(filepath.Join(managedRoot, testSkillID), movedRoot); err != nil {
		t.Fatal(err)
	}
	restarted, err := NewService(Config{
		BundleRoot: bundleRoot, ManagedRoot: managedRoot, Runtime: newTestRuntime(loaded.manifest.Skills[0].RuntimeName),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer restarted.Close()
	snapshot, err := restarted.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	state := stateByID(snapshot, testSkillID)
	if state.InstallationStatus != "not_installed" || state.RuntimeVisible {
		t.Fatalf("confirmed backup resurrected after external target move: %#v", state)
	}
	if _, err := os.Lstat(filepath.Join(managedRoot, backupName)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("confirmed backup was not garbage-collected: %v", err)
	}
}

func TestSuccessfulCommitJournalFailureRollsBackLifecycle(t *testing.T) {
	newService := func(t *testing.T) (*Service, catalog) {
		t.Helper()
		bundleRoot, _ := copyPinnedTestBundle(t, "manifest-valid.json", "fixture-model-only-0.1.0.zip")
		loaded, err := loadCatalog(bundleRoot)
		if err != nil {
			t.Fatal(err)
		}
		service, err := NewService(Config{
			BundleRoot: bundleRoot, ManagedRoot: filepath.Join(t.TempDir(), "managed"),
			Runtime: newTestRuntime(loaded.manifest.Skills[0].RuntimeName),
		})
		if err != nil {
			t.Fatal(err)
		}
		return service, loaded
	}
	failSuccessfulCommit := func(service *Service, operationID string) {
		service.persistOperationsFault = func(stored operationStore) error {
			for _, operation := range stored.Operations {
				if operation.ID == operationID && operation.Status == "complete" && operation.ErrorCode == "" {
					return errors.New("synthetic successful-result journal failure")
				}
			}
			return nil
		}
	}
	t.Run("install", func(t *testing.T) {
		service, loaded := newService(t)
		defer service.Close()
		operationID := "019fbd88-cbc3-7bf1-934d-7b05cd693fe4"
		failSuccessfulCommit(service, operationID)
		_, err := service.Install(context.Background(), installInput(
			testManifestProjection{revision: loaded.revision, Skills: loaded.manifest.Skills}, operationID,
		))
		assertCode(t, err, CodeScanFailed)
		snapshot, listErr := service.List(context.Background())
		if listErr != nil {
			t.Fatal(listErr)
		}
		state := stateByID(snapshot, testSkillID)
		if state.InstallationStatus != "not_installed" || state.RuntimeVisible {
			t.Fatalf("failed success-journal install remained committed: %#v", state)
		}
	})
	t.Run("enabled", func(t *testing.T) {
		service, loaded := newService(t)
		defer service.Close()
		if _, err := service.Install(context.Background(), installInput(
			testManifestProjection{revision: loaded.revision, Skills: loaded.manifest.Skills},
			"019fbd88-cbc3-7bf1-934d-7b05cd693fe5",
		)); err != nil {
			t.Fatal(err)
		}
		operationID := "019fbd88-cbc3-7bf1-934d-7b05cd693fe6"
		failSuccessfulCommit(service, operationID)
		_, err := service.SetEnabled(context.Background(), EnabledInput{OperationID: operationID, SkillID: testSkillID, Enabled: false})
		assertCode(t, err, CodeScanFailed)
		snapshot, listErr := service.List(context.Background())
		if listErr != nil {
			t.Fatal(listErr)
		}
		state := stateByID(snapshot, testSkillID)
		if !state.Enabled || !state.RuntimeVisible {
			t.Fatalf("failed success-journal enable remained committed: %#v", state)
		}
	})
	t.Run("uninstall", func(t *testing.T) {
		service, loaded := newService(t)
		defer service.Close()
		if _, err := service.Install(context.Background(), installInput(
			testManifestProjection{revision: loaded.revision, Skills: loaded.manifest.Skills},
			"019fbd88-cbc3-7bf1-934d-7b05cd693fe7",
		)); err != nil {
			t.Fatal(err)
		}
		operationID := "019fbd88-cbc3-7bf1-934d-7b05cd693fe8"
		failSuccessfulCommit(service, operationID)
		_, err := service.Uninstall(context.Background(), UninstallInput{OperationID: operationID, SkillID: testSkillID})
		assertCode(t, err, CodeScanFailed)
		snapshot, listErr := service.List(context.Background())
		if listErr != nil {
			t.Fatal(listErr)
		}
		state := stateByID(snapshot, testSkillID)
		if state.InstallationStatus != "installed" || !state.Enabled || !state.RuntimeVisible {
			t.Fatalf("failed success-journal uninstall remained committed: %#v", state)
		}
	})
}

func TestFinalizationFailureMatchesImmediateErrorReplay(t *testing.T) {
	failErrorFinalization := func(service *Service, operationID string) {
		service.persistOperationsFault = func(stored operationStore) error {
			for _, operation := range stored.Operations {
				if operation.ID == operationID && operation.Status == "complete" && operation.ErrorCode != "" {
					return errors.New("synthetic error-result journal failure")
				}
			}
			return nil
		}
	}
	assertOwnerAndReplay := func(t *testing.T, call func() error) {
		t.Helper()
		assertCode(t, call(), CodeScanFailed)
		assertCode(t, call(), CodeScanFailed)
	}

	t.Run("scan", func(t *testing.T) {
		bundleRoot, _ := copyPinnedTestBundle(t, "manifest-valid.json", "fixture-model-only-0.1.0.zip")
		loaded, err := loadCatalog(bundleRoot)
		if err != nil {
			t.Fatal(err)
		}
		runtime := newTestRuntime(loaded.manifest.Skills[0].RuntimeName)
		service, err := NewService(Config{
			BundleRoot: bundleRoot, ManagedRoot: filepath.Join(t.TempDir(), "managed"), Runtime: runtime,
		})
		if err != nil {
			t.Fatal(err)
		}
		defer service.Close()
		operationID := "019fbd88-cbc3-7bf1-934d-7b05cd693ff3"
		failErrorFinalization(service, operationID)
		runtime.failListCount = 1
		assertOwnerAndReplay(t, func() error {
			_, callErr := service.Scan(context.Background(), ScanInput{OperationID: operationID, Reason: "user_retry"})
			return callErr
		})
	})

	t.Run("blocked install", func(t *testing.T) {
		bundleRoot, _ := copyPinnedV2CatalogBundle(t)
		loaded, err := loadCatalog(bundleRoot)
		if err != nil {
			t.Fatal(err)
		}
		blockedSkill := loaded.manifest.Skills[0]
		service, err := NewService(Config{
			BundleRoot: bundleRoot, ManagedRoot: filepath.Join(t.TempDir(), "managed"),
			Runtime: newTestRuntime(blockedSkill.RuntimeName),
		})
		if err != nil {
			t.Fatal(err)
		}
		defer service.Close()
		operationID := "019fbd88-cbc3-7bf1-934d-7b05cd693ff4"
		failErrorFinalization(service, operationID)
		input := InstallInput{
			OperationID: operationID, SkillID: blockedSkill.ID, ExpectedVersion: blockedSkill.Version,
			ExpectedArchiveSHA256: strings.Repeat("f", 64), CatalogRevision: loaded.revision,
		}
		assertOwnerAndReplay(t, func() error {
			_, callErr := service.Install(context.Background(), input)
			return callErr
		})
	})

	t.Run("enabled", func(t *testing.T) {
		bundleRoot, _ := copyPinnedTestBundle(t, "manifest-valid.json", "fixture-model-only-0.1.0.zip")
		loaded, err := loadCatalog(bundleRoot)
		if err != nil {
			t.Fatal(err)
		}
		service, err := NewService(Config{
			BundleRoot: bundleRoot, ManagedRoot: filepath.Join(t.TempDir(), "managed"),
			Runtime: newTestRuntime(loaded.manifest.Skills[0].RuntimeName),
		})
		if err != nil {
			t.Fatal(err)
		}
		defer service.Close()
		operationID := "019fbd88-cbc3-7bf1-934d-7b05cd693ff5"
		failErrorFinalization(service, operationID)
		input := EnabledInput{OperationID: operationID, SkillID: testSkillID, Enabled: true}
		assertOwnerAndReplay(t, func() error {
			_, callErr := service.SetEnabled(context.Background(), input)
			return callErr
		})
	})

	t.Run("uninstall", func(t *testing.T) {
		bundleRoot, _ := copyPinnedTestBundle(t, "manifest-valid.json", "fixture-model-only-0.1.0.zip")
		loaded, err := loadCatalog(bundleRoot)
		if err != nil {
			t.Fatal(err)
		}
		service, err := NewService(Config{
			BundleRoot: bundleRoot, ManagedRoot: filepath.Join(t.TempDir(), "managed"),
			Runtime: newTestRuntime(loaded.manifest.Skills[0].RuntimeName),
		})
		if err != nil {
			t.Fatal(err)
		}
		defer service.Close()
		operationID := "019fbd88-cbc3-7bf1-934d-7b05cd693ff6"
		failErrorFinalization(service, operationID)
		if err := os.WriteFile(filepath.Join(bundleRoot, manifestFileName), []byte("{"), 0o600); err != nil {
			t.Fatal(err)
		}
		input := UninstallInput{OperationID: operationID, SkillID: testSkillID}
		assertOwnerAndReplay(t, func() error {
			_, callErr := service.Uninstall(context.Background(), input)
			return callErr
		})
	})
}

func TestAmbiguousFailureJoinersObserveImmutableAttempt(t *testing.T) {
	bundleRoot, _ := copyPinnedTestBundle(t, "manifest-valid.json", "fixture-model-only-0.1.0.zip")
	loaded, err := loadCatalog(bundleRoot)
	if err != nil {
		t.Fatal(err)
	}
	service, err := NewService(Config{
		BundleRoot: bundleRoot, ManagedRoot: filepath.Join(t.TempDir(), "managed"),
		Runtime: newTestRuntime(loaded.manifest.Skills[0].RuntimeName),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	if _, err := service.Install(context.Background(), installInput(
		testManifestProjection{revision: loaded.revision, Skills: loaded.manifest.Skills},
		"019fbd88-cbc3-7bf1-934d-7b05cd693fe9",
	)); err != nil {
		t.Fatal(err)
	}
	entered := make(chan struct{})
	release := make(chan struct{})
	calls := 0
	service.syncSkill = func(root *os.Root) error {
		calls++
		if calls == 1 {
			close(entered)
			<-release
		}
		if calls <= 2 {
			return errors.New("synthetic ambiguous marker sync failure")
		}
		return syncRoot(root)
	}
	input := EnabledInput{
		OperationID: "019fbd88-cbc3-7bf1-934d-7b05cd693fea", SkillID: testSkillID, Enabled: false,
	}
	owner := make(chan error, 1)
	go func() { _, operationErr := service.SetEnabled(context.Background(), input); owner <- operationErr }()
	<-entered
	const joinerCount = 32
	joiners := make(chan error, joinerCount)
	for index := 0; index < joinerCount; index++ {
		go func() { _, operationErr := service.SetEnabled(context.Background(), input); joiners <- operationErr }()
	}
	time.Sleep(20 * time.Millisecond)
	close(release)
	assertCode(t, <-owner, CodeInstallFailed)
	_, immediateRetryErr := service.SetEnabled(context.Background(), input)
	assertCode(t, immediateRetryErr, CodeInstallFailed)
	for index := 0; index < joinerCount; index++ {
		assertCode(t, <-joiners, CodeInstallFailed)
	}
	service.syncSkill = syncRoot
	if _, err := service.List(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestPostCommitCleanupSyncFailureKeepsLifecycleOutcomeSuccessful(t *testing.T) {
	t.Run("upgrade", func(t *testing.T) {
		bundleRoot, _ := copyPinnedTestBundle(t, "manifest-valid.json", "fixture-model-only-0.1.0.zip")
		initial, err := loadCatalog(bundleRoot)
		if err != nil {
			t.Fatal(err)
		}
		service, err := NewService(Config{
			BundleRoot: bundleRoot, ManagedRoot: filepath.Join(t.TempDir(), "managed"),
			Runtime: newTestRuntime(initial.manifest.Skills[0].RuntimeName),
		})
		if err != nil {
			t.Fatal(err)
		}
		defer service.Close()
		if _, err := service.Install(context.Background(), installInput(
			testManifestProjection{revision: initial.revision, Skills: initial.manifest.Skills},
			"019fbd88-cbc3-7bf1-934d-7b05cd693fc0",
		)); err != nil {
			t.Fatal(err)
		}
		upgradedRoot, upgraded := makeTestBundle(t, []testZipEntry{
			{name: "LICENSE", body: "fixture"}, {name: "SKILL.md", body: "upgraded"},
		}, "0.2.0")
		copyBundleContents(t, upgradedRoot, bundleRoot)
		calls := 0
		service.syncManaged = func(root *os.Root) error {
			calls++
			if calls == 2 {
				return errors.New("synthetic post-commit directory sync failure")
			}
			return syncRoot(root)
		}
		input := installInput(upgraded, "019fbd88-cbc3-7bf1-934d-7b05cd693fc1")
		state, err := service.Install(context.Background(), input)
		if err != nil || state.Version != "0.2.0" || !state.RuntimeVisible || calls != 2 {
			t.Fatalf("committed upgrade outcome changed by cleanup sync: state=%#v calls=%d err=%v", state, calls, err)
		}
		replayed, err := service.Install(context.Background(), input)
		if err != nil || replayed != state {
			t.Fatalf("committed upgrade did not replay success: state=%#v err=%v", replayed, err)
		}
	})

	t.Run("uninstall", func(t *testing.T) {
		bundleRoot, _ := copyPinnedTestBundle(t, "manifest-valid.json", "fixture-model-only-0.1.0.zip")
		loaded, err := loadCatalog(bundleRoot)
		if err != nil {
			t.Fatal(err)
		}
		service, err := NewService(Config{
			BundleRoot: bundleRoot, ManagedRoot: filepath.Join(t.TempDir(), "managed"),
			Runtime: newTestRuntime(loaded.manifest.Skills[0].RuntimeName),
		})
		if err != nil {
			t.Fatal(err)
		}
		defer service.Close()
		if _, err := service.Install(context.Background(), installInput(
			testManifestProjection{revision: loaded.revision, Skills: loaded.manifest.Skills},
			"019fbd88-cbc3-7bf1-934d-7b05cd693fc2",
		)); err != nil {
			t.Fatal(err)
		}
		calls := 0
		service.syncManaged = func(root *os.Root) error {
			calls++
			if calls == 2 {
				return errors.New("synthetic post-commit directory sync failure")
			}
			return syncRoot(root)
		}
		input := UninstallInput{OperationID: "019fbd88-cbc3-7bf1-934d-7b05cd693fc3", SkillID: testSkillID}
		state, err := service.Uninstall(context.Background(), input)
		if err != nil || state.InstallationStatus != "not_installed" || state.RuntimeVisible || calls != 2 {
			t.Fatalf("committed uninstall outcome changed by cleanup sync: state=%#v calls=%d err=%v", state, calls, err)
		}
		replayed, err := service.Uninstall(context.Background(), input)
		if err != nil || replayed != state {
			t.Fatalf("committed uninstall did not replay success: state=%#v err=%v", replayed, err)
		}
	})
}

func TestRootPermissionsAndInvalidEnableAreFailClosed(t *testing.T) {
	bundleRoot, _ := copyPinnedTestBundle(t, "manifest-valid.json", "fixture-model-only-0.1.0.zip")
	if err := os.Chmod(bundleRoot, 0o777); err != nil {
		t.Fatal(err)
	}
	_, err := NewService(Config{BundleRoot: bundleRoot, ManagedRoot: filepath.Join(t.TempDir(), "managed"), Runtime: newTestRuntime("fixture")})
	assertCode(t, err, CodeBundleMissing)
	if err := os.Chmod(bundleRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	catalog, err := loadCatalog(bundleRoot)
	if err != nil {
		t.Fatal(err)
	}
	service, err := NewService(Config{BundleRoot: bundleRoot, ManagedRoot: filepath.Join(t.TempDir(), "managed"), Runtime: newTestRuntime(catalog.manifest.Skills[0].RuntimeName)})
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	_, err = service.SetEnabled(context.Background(), EnabledInput{
		OperationID: "019fbd88-cbc3-7bf1-934d-7b05cd693f85", SkillID: testSkillID, Enabled: true,
	})
	assertCode(t, err, CodeInvalidRequest)
}

func TestManagedRootRejectsReplaceableAncestorAndFirstUseStaleInstall(t *testing.T) {
	bundleRoot, _ := copyPinnedTestBundle(t, "manifest-valid.json", "fixture-model-only-0.1.0.zip")
	catalog, err := loadCatalog(bundleRoot)
	if err != nil {
		t.Fatal(err)
	}
	unsafeParent := filepath.Join(t.TempDir(), "replaceable")
	if err := os.Mkdir(unsafeParent, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(unsafeParent, 0o777); err != nil {
		t.Fatal(err)
	}
	_, err = NewService(Config{
		BundleRoot: bundleRoot, ManagedRoot: filepath.Join(unsafeParent, "managed"),
		Runtime: newTestRuntime(catalog.manifest.Skills[0].RuntimeName),
	})
	assertCode(t, err, CodeScanFailed)

	service, err := NewService(Config{
		BundleRoot: bundleRoot, ManagedRoot: filepath.Join(t.TempDir(), "managed"),
		Runtime: newTestRuntime(catalog.manifest.Skills[0].RuntimeName),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	input := InstallInput{
		OperationID: "019fbd88-cbc3-7bf1-934d-7b05cd693f95", SkillID: testSkillID,
		ExpectedVersion: "0.1.1", ExpectedArchiveSHA256: catalog.manifest.Skills[0].Archive.SHA256,
		CatalogRevision: catalog.revision,
	}
	_, err = service.Install(context.Background(), input)
	assertCode(t, err, CodeInvalidRequest)
}

func TestBundleManifestAndArchiveRejectGroupWritableFiles(t *testing.T) {
	bundleRoot, _ := copyPinnedTestBundle(t, "manifest-valid.json", "fixture-model-only-0.1.0.zip")
	manifestPath := filepath.Join(bundleRoot, manifestFileName)
	archivePath := filepath.Join(bundleRoot, "packages", "fixture-model-only-0.1.0.zip")
	if err := os.Chmod(manifestPath, 0o664); err != nil {
		t.Fatal(err)
	}
	_, err := NewService(Config{
		BundleRoot: bundleRoot, ManagedRoot: filepath.Join(t.TempDir(), "managed"), Runtime: newTestRuntime("fixture"),
	})
	assertCode(t, err, CodeManifestInvalid)
	if err := os.Chmod(manifestPath, 0o600); err != nil {
		t.Fatal(err)
	}
	catalog, err := loadCatalog(bundleRoot)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(archivePath, 0o664); err != nil {
		t.Fatal(err)
	}
	service, err := NewService(Config{
		BundleRoot: bundleRoot, ManagedRoot: filepath.Join(t.TempDir(), "managed"),
		Runtime: newTestRuntime(catalog.manifest.Skills[0].RuntimeName),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	_, err = service.Install(context.Background(), InstallInput{
		OperationID: "019fbd88-cbc3-7bf1-934d-7b05cd693f86", SkillID: testSkillID,
		ExpectedVersion:       catalog.manifest.Skills[0].Version,
		ExpectedArchiveSHA256: catalog.manifest.Skills[0].Archive.SHA256,
		CatalogRevision:       catalog.revision,
	})
	assertCode(t, err, CodeArchiveUnsafe)
}

type testZipEntry struct {
	name string
	body string
	mode os.FileMode
}

type testManifestProjection struct {
	revision string
	Skills   []ManifestSkill
}

func makeTestBundle(t *testing.T, entries []testZipEntry, version string) (string, testManifestProjection) {
	t.Helper()
	_, baseRaw := copyPinnedTestBundle(t, "manifest-valid.json", "fixture-model-only-0.1.0.zip")
	var document map[string]any
	if err := json.Unmarshal(baseRaw, &document); err != nil {
		t.Fatal(err)
	}
	var archive bytes.Buffer
	writer := zip.NewWriter(&archive)
	var uncompressed int64
	for _, entry := range entries {
		header := &zip.FileHeader{Name: entry.name, Method: zip.Deflate}
		if entry.mode != 0 {
			header.SetMode(entry.mode)
		} else {
			header.SetMode(0o600)
		}
		file, err := writer.CreateHeader(header)
		if err != nil {
			t.Fatalf("create ZIP entry %q: %v", entry.name, err)
		}
		if _, err := io.WriteString(file, entry.body); err != nil {
			t.Fatal(err)
		}
		uncompressed += int64(len(entry.body))
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(archive.Bytes())
	skill := document["skills"].([]any)[0].(map[string]any)
	document["schema_version"] = float64(2)
	skill["version"] = version
	skill["catalog_entry_mode"] = "bundled"
	document["bundle_version"] = version
	skill["archive"] = map[string]any{
		"path": "packages/test.zip", "sha256": hex.EncodeToString(digest[:]),
		"compressed_size_bytes": archive.Len(), "uncompressed_size_bytes": uncompressed, "file_count": len(entries),
	}
	raw, err := json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	bundleRoot := filepath.Join(t.TempDir(), "bundle")
	if err := os.MkdirAll(filepath.Join(bundleRoot, "packages"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bundleRoot, manifestFileName), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bundleRoot, "packages", "test.zip"), archive.Bytes(), 0o400); err != nil {
		t.Fatal(err)
	}
	catalog, err := loadCatalog(bundleRoot)
	if err != nil {
		t.Fatalf("load generated catalog: %v", err)
	}
	return bundleRoot, testManifestProjection{revision: catalog.revision, Skills: catalog.manifest.Skills}
}

func installInput(manifest testManifestProjection, operationID string) InstallInput {
	return InstallInput{
		OperationID: operationID, SkillID: manifest.Skills[0].ID,
		ExpectedVersion: manifest.Skills[0].Version, ExpectedArchiveSHA256: manifest.Skills[0].Archive.SHA256,
		CatalogRevision: manifest.revision,
	}
}

func copyPinnedTestBundle(t *testing.T, manifestName, archiveName string) (string, []byte) {
	t.Helper()
	_, sourceFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate test source")
	}
	sourceRoot := filepath.Clean(filepath.Join(filepath.Dir(sourceFile), "..", "..", "api", "fixtures", "skills", "bundle-v1"))
	bundleRoot := filepath.Join(t.TempDir(), "bundle")
	if err := os.MkdirAll(filepath.Join(bundleRoot, "packages"), 0o755); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(sourceRoot, manifestName))
	if err != nil {
		t.Fatal(err)
	}
	archive, err := os.ReadFile(filepath.Join(sourceRoot, "packages", archiveName))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bundleRoot, manifestFileName), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bundleRoot, "packages", archiveName), archive, 0o400); err != nil {
		t.Fatal(err)
	}
	return bundleRoot, raw
}

func copyPinnedV2CatalogBundle(t *testing.T) (string, []byte) {
	t.Helper()
	_, sourceFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate test source")
	}
	sourceRoot := filepath.Clean(filepath.Join(filepath.Dir(sourceFile), "..", "..", "api", "fixtures", "skills", "bundle-v2"))
	bundleRoot := filepath.Join(t.TempDir(), "bundle")
	if err := os.MkdirAll(filepath.Join(bundleRoot, "packages"), 0o755); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(sourceRoot, "manifest-catalog-38.json"))
	if err != nil {
		t.Fatal(err)
	}
	archive, err := os.ReadFile(filepath.Join(sourceRoot, "packages", "fixture-copywriting-0.1.0.zip"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bundleRoot, manifestFileName), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bundleRoot, "packages", "fixture-copywriting-0.1.0.zip"), archive, 0o400); err != nil {
		t.Fatal(err)
	}
	return bundleRoot, raw
}

func assertNoSkillTransactionResidue(t *testing.T, managedRoot, skillID string) {
	t.Helper()
	entries, err := os.ReadDir(managedRoot)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if name == skillID || strings.HasPrefix(name, ".yijie-staging-") ||
			strings.HasPrefix(name, ".yijie-backup-") || strings.HasPrefix(name, ".yijie-failed-") {
			t.Fatalf("failed installation left transaction residue %q", name)
		}
	}
}

func copyBundleContents(t *testing.T, source, destination string) {
	t.Helper()
	manifest, err := os.ReadFile(filepath.Join(source, manifestFileName))
	if err != nil {
		t.Fatal(err)
	}
	archive, err := os.ReadFile(filepath.Join(source, "packages", "test.zip"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(destination, manifestFileName), manifest, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(destination, "packages", "test.zip"), archive, 0o400); err != nil {
		t.Fatal(err)
	}
}

func assertCode(t *testing.T, err error, code ErrorCode) {
	t.Helper()
	var serviceError *ServiceError
	if !errors.As(err, &serviceError) || serviceError.Code != code {
		t.Fatalf("error=%v, want code %s", err, code)
	}
}
