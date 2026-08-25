package integration

import (
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
	"github.com/36Dge/yijie-agent-host/internal/skills"
)

const fixtureSkillID = "yijie.fixture.model-only"

type fixtureCatalog struct {
	SchemaVersion       int                   `json:"schema_version"`
	BundleVersion       string                `json:"bundle_version"`
	DistributionChannel string                `json:"distribution_channel"`
	Source              fixtureCatalogSource  `json:"source"`
	Skills              []fixtureCatalogSkill `json:"skills"`
}

type fixtureCatalogSource struct {
	Repository string `json:"repository"`
	Revision   string `json:"revision"`
	TreeSHA256 string `json:"tree_sha256"`
}

type fixtureCatalogSkill struct {
	ID               string `json:"id"`
	RuntimeName      string `json:"runtime_name"`
	Category         string `json:"category"`
	Version          string `json:"version"`
	CatalogEntryMode string `json:"catalog_entry_mode"`
	Icon             struct {
		Registry string `json:"registry"`
		Key      string `json:"key"`
	} `json:"icon"`
	Risk struct {
		Level   string   `json:"level"`
		Reasons []string `json:"reasons"`
	} `json:"risk"`
	Provenance struct {
		SourceReference string `json:"source_reference"`
		SourceSHA256    string `json:"source_sha256"`
		ReviewStatus    string `json:"review_status"`
	} `json:"provenance"`
	License struct {
		Expression           string `json:"expression"`
		RedistributionStatus string `json:"redistribution_status"`
		AuthorizationScope   string `json:"authorization_scope"`
		EvidenceReference    string `json:"evidence_reference"`
	} `json:"license"`
	Capabilities struct {
		ExecutionMode string   `json:"execution_mode"`
		Network       string   `json:"network"`
		Filesystem    string   `json:"filesystem"`
		RequiredTools []string `json:"required_tools"`
	} `json:"capabilities"`
	Archive struct {
		Path                  string `json:"path"`
		SHA256                string `json:"sha256"`
		CompressedSizeBytes   int64  `json:"compressed_size_bytes"`
		UncompressedSizeBytes int64  `json:"uncompressed_size_bytes"`
		FileCount             int    `json:"file_count"`
	} `json:"archive"`
	Release struct {
		CatalogStatus     string `json:"catalog_status"`
		MaintenanceStatus string `json:"maintenance_status"`
		BlockedReason     string `json:"blocked_reason"`
	} `json:"release"`
}

type filesystemSkillsRuntime struct {
	mu          sync.Mutex
	extraRoots  []string
	enabled     map[string]bool
	runtimeName string
}

type blockingSkillsRuntime struct {
	*filesystemSkillsRuntime
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

type notifyingSkillsRuntime struct {
	*filesystemSkillsRuntime
	onSet func()
}

type recoveringSkillsRuntime struct {
	*filesystemSkillsRuntime
	mu       sync.Mutex
	failures int
	ready    chan struct{}
	once     sync.Once
}

func (r *recoveringSkillsRuntime) SetSkillsExtraRoots(ctx context.Context, roots []string) error {
	r.mu.Lock()
	if r.failures > 0 {
		r.failures--
		r.mu.Unlock()
		return errors.New("Runtime is starting")
	}
	r.mu.Unlock()
	if err := r.filesystemSkillsRuntime.SetSkillsExtraRoots(ctx, roots); err != nil {
		return err
	}
	r.once.Do(func() { close(r.ready) })
	return nil
}

func (r *notifyingSkillsRuntime) SetSkillsExtraRoots(ctx context.Context, roots []string) error {
	if r.onSet != nil {
		r.onSet()
	}
	return r.filesystemSkillsRuntime.SetSkillsExtraRoots(ctx, roots)
}

func newBlockingSkillsRuntime(runtimeName string) *blockingSkillsRuntime {
	return &blockingSkillsRuntime{
		filesystemSkillsRuntime: newFilesystemSkillsRuntime(runtimeName),
		started:                 make(chan struct{}),
		release:                 make(chan struct{}),
	}
}

func (r *blockingSkillsRuntime) ListSkills(ctx context.Context, cwds []string, force bool) ([]codex.SkillsListEntry, error) {
	r.once.Do(func() {
		close(r.started)
		select {
		case <-ctx.Done():
		case <-r.release:
		}
	})
	return r.filesystemSkillsRuntime.ListSkills(ctx, cwds, force)
}

func newFilesystemSkillsRuntime(runtimeName string) *filesystemSkillsRuntime {
	return &filesystemSkillsRuntime{enabled: make(map[string]bool), runtimeName: runtimeName}
}

func (r *filesystemSkillsRuntime) SetSkillsExtraRoots(_ context.Context, roots []string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, root := range roots {
		info, err := os.Stat(root)
		if err != nil || !info.IsDir() {
			return errors.New("invalid extra root")
		}
	}
	r.extraRoots = append([]string(nil), roots...)
	return nil
}

func (r *filesystemSkillsRuntime) WriteSkillConfig(_ context.Context, skillPath string, enabled bool) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	info, err := os.Lstat(skillPath)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || filepath.Base(skillPath) != "SKILL.md" {
		return false, errors.New("Runtime cannot configure a missing Skill")
	}
	r.enabled[skillPath] = enabled
	return enabled, nil
}

func (r *filesystemSkillsRuntime) ListSkills(_ context.Context, cwds []string, _ bool) ([]codex.SkillsListEntry, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(cwds) != 1 {
		return nil, errors.New("Runtime root projection is inconsistent")
	}
	root := cwds[0]
	projection := codex.SkillsListEntry{CWD: root, Skills: []codex.SkillMetadata{}, Errors: []codex.SkillErrorInfo{}}
	for _, extraRoot := range r.extraRoots {
		err := filepath.WalkDir(extraRoot, func(skillPath string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if skillPath != extraRoot && entry.IsDir() && len(entry.Name()) > 0 && entry.Name()[0] == '.' {
				return filepath.SkipDir
			}
			if entry.IsDir() || entry.Name() != "SKILL.md" {
				return nil
			}
			info, statErr := entry.Info()
			if statErr != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
				return nil
			}
			projection.Skills = append(projection.Skills, codex.SkillMetadata{
				Name:        runtimeNameFromSkillFile(skillPath, r.runtimeName),
				Description: "fixture",
				Path:        skillPath,
				Scope:       codex.SkillScopeUser,
				Enabled:     r.enabled[skillPath],
			})
			return nil
		})
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, errors.New("Runtime root is unavailable")
		}
	}
	sort.Slice(projection.Skills, func(i, j int) bool { return projection.Skills[i].Path < projection.Skills[j].Path })
	return []codex.SkillsListEntry{projection}, nil
}

func runtimeNameFromSkillFile(skillPath, fallback string) string {
	raw, err := os.ReadFile(skillPath)
	if err != nil {
		return fallback
	}
	inFrontmatter := false
	for _, line := range strings.Split(string(raw), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "---" {
			if inFrontmatter {
				break
			}
			inFrontmatter = true
			continue
		}
		if inFrontmatter && strings.HasPrefix(trimmed, "name:") {
			name := strings.Trim(strings.TrimSpace(strings.TrimPrefix(trimmed, "name:")), `"'`)
			if name != "" {
				return name
			}
		}
	}
	return fallback
}

func TestManagedSkillFixtureLifecycleAndRestartReplay(t *testing.T) {
	bundleRoot, manifestRaw, manifest := copySkillFixtureBundle(
		t,
		"manifest-valid.json",
		"fixture-model-only-0.1.0.zip",
	)
	managedRoot := filepath.Join(t.TempDir(), "managed")
	if err := os.Mkdir(managedRoot, 0o700); err != nil {
		t.Fatalf("create managed root: %v", err)
	}
	clock := time.Date(2026, 8, 25, 1, 2, 3, 0, time.UTC)
	runtimeOne := newFilesystemSkillsRuntime(manifest.Skills[0].RuntimeName)
	service, err := skills.NewService(skills.Config{
		BundleRoot: bundleRoot, ManagedRoot: managedRoot, Runtime: runtimeOne,
		Now: func() time.Time { return clock },
	})
	if err != nil {
		t.Fatalf("open Skill service: %v (cause: %v)", err, errors.Unwrap(err))
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	initial, err := service.List(ctx)
	if err != nil {
		t.Fatalf("list empty managed root: %v", err)
	}
	assertFixtureState(t, initial, "not_installed", false, false)
	revision := sha256.Sum256(manifestRaw)
	if initial.CatalogRevision != hex.EncodeToString(revision[:]) {
		t.Fatalf("catalog revision %q does not hash exact manifest bytes", initial.CatalogRevision)
	}
	const scanOperationID = "019fbd88-cbc3-7bf1-934d-7b05cd693f70"
	scanned, err := service.Scan(ctx, skills.ScanInput{OperationID: scanOperationID, Reason: "page_open"})
	if err != nil || !scanned.ScannedAt.Equal(clock) {
		t.Fatalf("persist canonical scan result: snapshot=%#v err=%v", scanned, err)
	}

	installed, err := service.Install(ctx, skills.InstallInput{
		OperationID:           "019fbd88-cbc3-7bf1-934d-7b05cd693f71",
		SkillID:               manifest.Skills[0].ID,
		ExpectedVersion:       manifest.Skills[0].Version,
		ExpectedArchiveSHA256: manifest.Skills[0].Archive.SHA256,
		CatalogRevision:       initial.CatalogRevision,
	})
	if err != nil {
		t.Fatalf("install fixture Skill: %v", err)
	}
	if installed.InstallationStatus != "installed" || !installed.Enabled || !installed.RuntimeVisible {
		t.Fatalf("unexpected installed state: %#v tree=%v", installed, describeTree(managedRoot))
	}
	entrypoint := filepath.Join(managedRoot, fixtureSkillID, "SKILL.md")
	if info, err := os.Lstat(entrypoint); err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0 {
		t.Fatalf("installed entrypoint is not an owner-only regular file: info=%v err=%v", info, err)
	}

	disabled, err := service.SetEnabled(ctx, skills.EnabledInput{
		OperationID: "019fbd88-cbc3-7bf1-934d-7b05cd693f72",
		SkillID:     fixtureSkillID,
		Enabled:     false,
	})
	if err != nil {
		t.Fatalf("disable fixture Skill: %v", err)
	}
	if !isInstalledButInvisible(disabled) {
		t.Fatalf("unexpected disabled state: %#v", disabled)
	}
	if err := service.Close(); err != nil {
		t.Fatalf("close first Skill service: %v", err)
	}

	// A fresh Runtime has no process-local root/config state. Reopening Host must
	// replay the receipt and disabled marker without a user action.
	runtimeTwo := newFilesystemSkillsRuntime(manifest.Skills[0].RuntimeName)
	restarted, err := skills.NewService(skills.Config{
		BundleRoot: bundleRoot, ManagedRoot: managedRoot, Runtime: runtimeTwo,
		Now: func() time.Time { return clock.Add(time.Minute) },
	})
	if err != nil {
		t.Fatalf("restart Skill service: %v", err)
	}
	defer restarted.Close()
	replayed, err := restarted.List(ctx)
	if err != nil {
		t.Fatalf("replay Runtime projection after restart: %v", err)
	}
	assertFixtureState(t, replayed, "installed", false, false)
	replayedScan, err := restarted.Scan(ctx, skills.ScanInput{OperationID: scanOperationID, Reason: "page_open"})
	if err != nil || !replayedScan.ScannedAt.Equal(clock) || replayedScan.Skills[0].InstallationStatus != "not_installed" {
		t.Fatalf("completed scan operation was not replayed across restart: snapshot=%#v err=%v", replayedScan, err)
	}
	_, err = restarted.Scan(ctx, skills.ScanInput{OperationID: scanOperationID, Reason: "window_resume"})
	assertSkillServiceCode(t, err, skills.CodeOperationConflict)

	enabled, err := restarted.SetEnabled(ctx, skills.EnabledInput{
		OperationID: "019fbd88-cbc3-7bf1-934d-7b05cd693f74",
		SkillID:     fixtureSkillID,
		Enabled:     true,
	})
	if err != nil || !enabled.RuntimeVisible {
		t.Fatalf("re-enable fixture Skill: state=%#v err=%v", enabled, err)
	}
	uninstalled, err := restarted.Uninstall(ctx, skills.UninstallInput{
		OperationID: "019fbd88-cbc3-7bf1-934d-7b05cd693f73",
		SkillID:     fixtureSkillID,
	})
	if err != nil {
		t.Fatalf("uninstall fixture Skill: %v", err)
	}
	if uninstalled.InstallationStatus != "not_installed" || uninstalled.Enabled || uninstalled.RuntimeVisible {
		t.Fatalf("unexpected uninstalled state: %#v", uninstalled)
	}
	if _, err := os.Lstat(filepath.Join(managedRoot, fixtureSkillID)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("managed copy still exists after uninstall: %v", err)
	}
	if _, err := os.Stat(filepath.Join(bundleRoot, manifest.Skills[0].Archive.Path)); err != nil {
		t.Fatalf("uninstall removed bundled archive: %v", err)
	}
}

func TestManagedSkillRejectsPinnedChecksumAndZipSlipFixturesAtomically(t *testing.T) {
	for _, test := range []struct {
		name         string
		manifest     string
		archive      string
		expectedCode skills.ErrorCode
	}{
		{name: "checksum", manifest: "manifest-checksum-mismatch.json", archive: "fixture-model-only-checksum-mismatch.zip", expectedCode: skills.CodeArchiveChecksum},
		{name: "zip slip", manifest: "manifest-zip-slip.json", archive: "fixture-zip-slip.zip", expectedCode: skills.CodeArchiveUnsafe},
	} {
		t.Run(test.name, func(t *testing.T) {
			bundleRoot, raw, manifest := copySkillFixtureBundle(t, test.manifest, test.archive)
			managedRoot := filepath.Join(t.TempDir(), "managed")
			if err := os.Mkdir(managedRoot, 0o700); err != nil {
				t.Fatalf("create managed root: %v", err)
			}
			runtime := newFilesystemSkillsRuntime(manifest.Skills[0].RuntimeName)
			service, err := skills.NewService(skills.Config{BundleRoot: bundleRoot, ManagedRoot: managedRoot, Runtime: runtime})
			if err != nil {
				t.Fatalf("open Skill service: %v (cause: %v)", err, errors.Unwrap(err))
			}
			defer service.Close()
			revision := sha256.Sum256(raw)
			_, err = service.Install(context.Background(), skills.InstallInput{
				OperationID:           "019fbd88-cbc3-7bf1-934d-7b05cd693f75",
				SkillID:               manifest.Skills[0].ID,
				ExpectedVersion:       manifest.Skills[0].Version,
				ExpectedArchiveSHA256: manifest.Skills[0].Archive.SHA256,
				CatalogRevision:       hex.EncodeToString(revision[:]),
			})
			assertSkillServiceCode(t, err, test.expectedCode)
			entries, readErr := os.ReadDir(managedRoot)
			if readErr != nil {
				t.Fatalf("read managed root: %v", readErr)
			}
			for _, entry := range entries {
				if entry.Name() == manifest.Skills[0].ID || strings.HasPrefix(entry.Name(), ".yijie-staging--") ||
					strings.HasPrefix(entry.Name(), ".yijie-backup--") || strings.HasPrefix(entry.Name(), ".yijie-failed--") {
					t.Fatalf("failed installation left partial managed entry: %s", entry.Name())
				}
			}
			if _, outsideErr := os.Stat(filepath.Join(filepath.Dir(managedRoot), "escape.txt")); !errors.Is(outsideErr, os.ErrNotExist) {
				t.Fatalf("unsafe archive wrote outside managed root: %v", outsideErr)
			}
		})
	}
}

func TestManagedSkillMutationBusyAndSameOperationJoin(t *testing.T) {
	bundleRoot, raw, manifest := copySkillFixtureBundle(t, "manifest-valid.json", "fixture-model-only-0.1.0.zip")
	managedRoot := filepath.Join(t.TempDir(), "managed")
	if err := os.Mkdir(managedRoot, 0o700); err != nil {
		t.Fatalf("create managed root: %v", err)
	}
	runtime := newBlockingSkillsRuntime(manifest.Skills[0].RuntimeName)
	service, err := skills.NewService(skills.Config{BundleRoot: bundleRoot, ManagedRoot: managedRoot, Runtime: runtime})
	if err != nil {
		t.Fatalf("open Skill service: %v", err)
	}
	defer service.Close()
	revision := sha256.Sum256(raw)
	input := skills.InstallInput{
		OperationID:           "019fbd88-cbc3-7bf1-934d-7b05cd693f76",
		SkillID:               manifest.Skills[0].ID,
		ExpectedVersion:       manifest.Skills[0].Version,
		ExpectedArchiveSHA256: manifest.Skills[0].Archive.SHA256,
		CatalogRevision:       hex.EncodeToString(revision[:]),
	}
	type result struct {
		state skills.State
		err   error
	}
	first := make(chan result, 1)
	go func() {
		state, installErr := service.Install(context.Background(), input)
		first <- result{state: state, err: installErr}
	}()
	select {
	case <-runtime.started:
	case <-time.After(3 * time.Second):
		t.Fatal("first mutation did not reach Runtime")
	}

	joined := make(chan result, 1)
	go func() {
		state, installErr := service.Install(context.Background(), input)
		joined <- result{state: state, err: installErr}
	}()
	select {
	case result := <-joined:
		t.Fatalf("same in-flight operation did not join: %#v", result)
	case <-time.After(25 * time.Millisecond):
	}

	different := input
	different.OperationID = "019fbd88-cbc3-7bf1-934d-7b05cd693f77"
	startedAt := time.Now()
	_, err = service.Install(context.Background(), different)
	assertSkillServiceCode(t, err, skills.CodeBusy)
	if time.Since(startedAt) > time.Second {
		t.Fatal("different operation waited instead of returning skill_busy")
	}

	close(runtime.release)
	for name, resultChannel := range map[string]<-chan result{"owner": first, "joined": joined} {
		select {
		case result := <-resultChannel:
			if result.err != nil || result.state.InstallationStatus != "installed" || !result.state.RuntimeVisible {
				t.Fatalf("%s operation result: state=%#v err=%v", name, result.state, result.err)
			}
		case <-time.After(3 * time.Second):
			t.Fatalf("%s operation did not finish", name)
		}
	}
}

func TestRuntimeSkillsChangedCallbackIsSignalOnly(t *testing.T) {
	bundleRoot, _, manifest := copySkillFixtureBundle(t, "manifest-valid.json", "fixture-model-only-0.1.0.zip")
	managedRoot := filepath.Join(t.TempDir(), "managed")
	if err := os.Mkdir(managedRoot, 0o700); err != nil {
		t.Fatalf("create managed root: %v", err)
	}
	runtime := &notifyingSkillsRuntime{filesystemSkillsRuntime: newFilesystemSkillsRuntime(manifest.Skills[0].RuntimeName)}
	service, err := skills.NewService(skills.Config{BundleRoot: bundleRoot, ManagedRoot: managedRoot, Runtime: runtime})
	if err != nil {
		t.Fatalf("open Skill service: %v", err)
	}
	defer service.Close()
	runtime.onSet = func() {
		if notificationErr := service.HandleRuntimeNotification(json.RawMessage(`{}`)); notificationErr != nil {
			panic(notificationErr)
		}
	}

	done := make(chan error, 1)
	go func() {
		_, listErr := service.List(context.Background())
		done <- listErr
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("list with synchronous pre-response notification: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("skills/changed callback deadlocked the Runtime request")
	}

	startedAt := time.Now()
	for range 1000 {
		if err := service.HandleRuntimeNotification(json.RawMessage(`{}`)); err != nil {
			t.Fatalf("coalesce valid Runtime notification: %v", err)
		}
	}
	if time.Since(startedAt) > time.Second {
		t.Fatal("skills/changed storm blocked instead of coalescing")
	}
	if err := service.HandleRuntimeNotification(json.RawMessage(`{"path":"/private"}`)); err == nil {
		t.Fatal("accepted non-empty skills/changed payload")
	}
}

func TestSkillRunReplaysWhenRuntimeBecomesReadyWithoutUserAction(t *testing.T) {
	bundleRoot, _, manifest := copySkillFixtureBundle(t, "manifest-valid.json", "fixture-model-only-0.1.0.zip")
	managedRoot := filepath.Join(t.TempDir(), "managed")
	if err := os.Mkdir(managedRoot, 0o700); err != nil {
		t.Fatalf("create managed root: %v", err)
	}
	runtime := &recoveringSkillsRuntime{
		filesystemSkillsRuntime: newFilesystemSkillsRuntime(manifest.Skills[0].RuntimeName),
		failures:                2,
		ready:                   make(chan struct{}),
	}
	service, err := skills.NewService(skills.Config{BundleRoot: bundleRoot, ManagedRoot: managedRoot, Runtime: runtime})
	if err != nil {
		t.Fatalf("open Skill service: %v", err)
	}
	defer service.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go service.Run(ctx)
	select {
	case <-runtime.ready:
	case <-time.After(3 * time.Second):
		t.Fatal("Skill Runtime projection did not retry after startup failures")
	}
}

func copySkillFixtureBundle(t *testing.T, manifestName, archiveName string) (string, []byte, fixtureCatalog) {
	t.Helper()
	source := integrationContractPath(t, "fixtures", "skills", "bundle-v1")
	bundleRoot := filepath.Join(t.TempDir(), "bundle")
	packageRoot := filepath.Join(bundleRoot, "packages")
	if err := os.MkdirAll(packageRoot, 0o755); err != nil {
		t.Fatalf("create bundle root: %v", err)
	}
	manifestRaw := copyFile(t, filepath.Join(source, manifestName), filepath.Join(bundleRoot, "bundle-manifest.json"), 0o444)
	copyFile(t, filepath.Join(source, "packages", archiveName), filepath.Join(packageRoot, archiveName), 0o444)
	var manifest fixtureCatalog
	if err := json.Unmarshal(manifestRaw, &manifest); err != nil || len(manifest.Skills) != 1 {
		t.Fatalf("decode fixture manifest: skills=%d err=%v", len(manifest.Skills), err)
	}
	return bundleRoot, manifestRaw, manifest
}

func copyFile(t *testing.T, source, destination string, mode os.FileMode) []byte {
	t.Helper()
	input, err := os.Open(source)
	if err != nil {
		t.Fatalf("open fixture %s: %v", source, err)
	}
	defer input.Close()
	output, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		t.Fatalf("create fixture copy %s: %v", destination, err)
	}
	encoded, err := io.ReadAll(io.TeeReader(input, output))
	closeErr := output.Close()
	if err != nil || closeErr != nil {
		t.Fatalf("copy fixture: read=%v close=%v", err, closeErr)
	}
	return encoded
}

func integrationContractPath(t *testing.T, elements ...string) string {
	t.Helper()
	_, sourceFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate integration test source")
	}
	parts := append([]string{filepath.Dir(sourceFile), "..", "..", "api"}, elements...)
	return filepath.Clean(filepath.Join(parts...))
}

func assertFixtureState(t *testing.T, snapshot skills.Snapshot, status string, enabled, visible bool) {
	t.Helper()
	if len(snapshot.Skills) != 1 {
		t.Fatalf("snapshot contains %d Skills", len(snapshot.Skills))
	}
	state := snapshot.Skills[0]
	if state.ID != fixtureSkillID || state.InstallationStatus != status || state.Enabled != enabled || state.RuntimeVisible != visible {
		t.Fatalf("unexpected fixture state: %#v", state)
	}
}

func isInstalledButInvisible(state skills.State) bool {
	return state.InstallationStatus == "installed" && !state.Enabled && !state.RuntimeVisible
}

func assertSkillServiceCode(t *testing.T, err error, expected skills.ErrorCode) {
	t.Helper()
	var serviceError *skills.ServiceError
	if !errors.As(err, &serviceError) || serviceError.Code != expected {
		t.Fatalf("Skill error=%v, want code %s", err, expected)
	}
}

func describeTree(root string) []string {
	var result []string
	_ = filepath.WalkDir(root, func(name string, entry os.DirEntry, err error) error {
		if err != nil {
			result = append(result, fmt.Sprintf("ERR:%v", err))
			return nil
		}
		info, infoErr := entry.Info()
		if infoErr != nil {
			result = append(result, fmt.Sprintf("%s:ERR:%v", name, infoErr))
			return nil
		}
		relative, _ := filepath.Rel(root, name)
		result = append(result, fmt.Sprintf("%s:%s:%d", relative, info.Mode(), info.Size()))
		return nil
	})
	return result
}

func TestSkillFixtureCatalogShape(t *testing.T) {
	// Guards helper assumptions so a future fixture change fails with a useful
	// conformance error instead of silently weakening the black-box tests.
	_, _, manifest := copySkillFixtureBundle(t, "manifest-valid.json", "fixture-model-only-0.1.0.zip")
	if manifest.Skills[0].ID != fixtureSkillID || manifest.Skills[0].Archive.Path != "packages/fixture-model-only-0.1.0.zip" {
		t.Fatal(fmt.Sprintf("unexpected pinned fixture projection: %#v", manifest.Skills[0]))
	}
}

func TestYijieSkillsV030DualChannelConformance(t *testing.T) {
	if os.Getenv("YIJIE_SKILLS_CONFORMANCE") != "1" {
		t.Skip("set YIJIE_SKILLS_CONFORMANCE=1 and both 0.3.0 bundle roots")
	}
	localRoot := os.Getenv("YIJIE_SKILLS_LOCAL_BUNDLE_ROOT")
	if localRoot == "" {
		localRoot = os.Getenv("YIJIE_SKILLS_BUNDLE_ROOT")
	}
	releaseRoot := os.Getenv("YIJIE_SKILLS_DESKTOP_RELEASE_BUNDLE_ROOT")
	if localRoot == "" || releaseRoot == "" {
		t.Fatal("both YIJIE_SKILLS_LOCAL_BUNDLE_ROOT and YIJIE_SKILLS_DESKTOP_RELEASE_BUNDLE_ROOT are required")
	}

	localRaw, local := readProductBundle(t, localRoot, "local-development", "cc2b9be4d0e640e0888e97f6f7a09149a248386931786a7a089c8094304d94a5")
	releaseRaw, release := readProductBundle(t, releaseRoot, "desktop-release", "9f8459077615514183fdd4c81ff3b6b2ef1ea735257b04c040399d4c91c1daa2")
	assertProductChannelsEquivalent(t, localRoot, localRaw, local, releaseRoot, releaseRaw, release)

	for index, channel := range []struct {
		name     string
		root     string
		raw      []byte
		manifest fixtureCatalog
	}{
		{name: "local-development", root: localRoot, raw: localRaw, manifest: local},
		{name: "desktop-release", root: releaseRoot, raw: releaseRaw, manifest: release},
	} {
		t.Run(channel.name, func(t *testing.T) {
			runThirtyEightSkillLifecycle(t, index+1, channel.root, channel.raw, channel.manifest)
		})
	}
}

func readProductBundle(t *testing.T, root, channel, manifestSHA string) ([]byte, fixtureCatalog) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(root, "bundle-manifest.json"))
	if err != nil {
		t.Fatalf("read %s manifest: %v", channel, err)
	}
	digest := sha256.Sum256(raw)
	if hex.EncodeToString(digest[:]) != manifestSHA {
		t.Fatalf("%s manifest SHA-256=%s, want %s", channel, hex.EncodeToString(digest[:]), manifestSHA)
	}
	var manifest fixtureCatalog
	if err := json.Unmarshal(raw, &manifest); err != nil {
		t.Fatalf("decode %s manifest: %v", channel, err)
	}
	if manifest.SchemaVersion != 2 || manifest.BundleVersion != "0.3.0" ||
		manifest.DistributionChannel != channel || len(manifest.Skills) != 38 ||
		manifest.Source.Repository != "https://github.com/36Dge/yijie-skills.git" ||
		manifest.Source.Revision != "10c45bec29603b002e861e1499d5b4e684251af5" ||
		manifest.Source.TreeSHA256 != "3247a14004c76170cf41a2d854e2ceffa1fd43de6e0ca8bb596f1d61d9be1029" {
		t.Fatalf("unexpected %s producer identity: %#v", channel, manifest)
	}
	wantCategories := map[string]int{
		"sourcing-selection":  5,
		"market-research":     9,
		"content-marketing":   7,
		"traffic-advertising": 9,
		"store-operations":    8,
	}
	gotCategories := make(map[string]int, len(wantCategories))
	seenIDs := make(map[string]struct{}, len(manifest.Skills))
	seenRuntimeNames := make(map[string]struct{}, len(manifest.Skills))
	for _, skill := range manifest.Skills {
		gotCategories[skill.Category]++
		if _, exists := seenIDs[skill.ID]; exists {
			t.Fatalf("%s contains duplicate Skill id %s", channel, skill.ID)
		}
		if _, exists := seenRuntimeNames[skill.RuntimeName]; exists {
			t.Fatalf("%s contains duplicate Runtime name %s", channel, skill.RuntimeName)
		}
		seenIDs[skill.ID] = struct{}{}
		seenRuntimeNames[skill.RuntimeName] = struct{}{}
		if skill.CatalogEntryMode != "bundled" || skill.Release.CatalogStatus != "installable" ||
			skill.Release.BlockedReason != "" || skill.Release.MaintenanceStatus != "maintained" ||
			skill.Archive.Path == "" || skill.Archive.SHA256 == "" || skill.Archive.FileCount < 1 ||
			skill.Icon.Registry == "" || skill.Icon.Key == "" || skill.Risk.Level == "" || len(skill.Risk.Reasons) == 0 ||
			skill.Provenance.SourceReference == "" || skill.Provenance.SourceSHA256 == "" || skill.Provenance.ReviewStatus != "verified" ||
			skill.License.Expression == "" || skill.License.RedistributionStatus != "verified" ||
			skill.License.AuthorizationScope != "desktop-distribution" || skill.License.EvidenceReference == "" ||
			skill.Capabilities.ExecutionMode == "" {
			t.Fatalf("%s has incomplete or blocked metadata for %s: %#v", channel, skill.ID, skill)
		}
		archive, err := os.ReadFile(filepath.Join(root, skill.Archive.Path))
		if err != nil {
			t.Fatalf("read %s archive for %s: %v", channel, skill.ID, err)
		}
		archiveDigest := sha256.Sum256(archive)
		if hex.EncodeToString(archiveDigest[:]) != skill.Archive.SHA256 || int64(len(archive)) != skill.Archive.CompressedSizeBytes {
			t.Fatalf("%s archive metadata mismatch for %s", channel, skill.ID)
		}
	}
	for category, want := range wantCategories {
		if gotCategories[category] != want {
			t.Fatalf("%s category %s=%d, want %d (all=%v)", channel, category, gotCategories[category], want, gotCategories)
		}
	}
	return raw, manifest
}

func assertProductChannelsEquivalent(
	t *testing.T,
	localRoot string,
	localRaw []byte,
	local fixtureCatalog,
	releaseRoot string,
	releaseRaw []byte,
	release fixtureCatalog,
) {
	t.Helper()
	var localDocument, releaseDocument map[string]any
	if err := json.Unmarshal(localRaw, &localDocument); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(releaseRaw, &releaseDocument); err != nil {
		t.Fatal(err)
	}
	localDocument["distribution_channel"] = "normalized"
	releaseDocument["distribution_channel"] = "normalized"
	localCanonical, _ := json.Marshal(localDocument)
	releaseCanonical, _ := json.Marshal(releaseDocument)
	if !bytes.Equal(localCanonical, releaseCanonical) {
		t.Fatal("local-development and desktop-release manifests differ beyond distribution_channel")
	}
	if len(local.Skills) != len(release.Skills) {
		t.Fatal("producer channels have different archive inventories")
	}
	for index, localSkill := range local.Skills {
		releaseSkill := release.Skills[index]
		if localSkill.ID != releaseSkill.ID || localSkill.Archive.Path != releaseSkill.Archive.Path {
			t.Fatalf("producer channel order differs at %d", index)
		}
		localArchive, err := os.ReadFile(filepath.Join(localRoot, localSkill.Archive.Path))
		if err != nil {
			t.Fatal(err)
		}
		releaseArchive, err := os.ReadFile(filepath.Join(releaseRoot, releaseSkill.Archive.Path))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(localArchive, releaseArchive) {
			t.Fatalf("producer channel archive differs for %s", localSkill.ID)
		}
	}
}

func runThirtyEightSkillLifecycle(t *testing.T, namespace int, bundleRoot string, raw []byte, manifest fixtureCatalog) {
	t.Helper()
	managedRoot := filepath.Join(t.TempDir(), "managed")
	if err := os.Mkdir(managedRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	runtimeOne := newFilesystemSkillsRuntime("")
	service, err := skills.NewService(skills.Config{BundleRoot: bundleRoot, ManagedRoot: managedRoot, Runtime: runtimeOne})
	if err != nil {
		t.Fatalf("open %s 38-Skill service: %v (cause: %v)", manifest.DistributionChannel, err, errors.Unwrap(err))
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	initial, err := service.List(ctx)
	if err != nil || len(initial.Skills) != 38 {
		t.Fatalf("initial %s query: skills=%d err=%v", manifest.DistributionChannel, len(initial.Skills), err)
	}
	for _, state := range initial.Skills {
		if state.CatalogStatus != "installable" || state.CatalogBlockedReason != "" ||
			state.InstallationStatus != "not_installed" || state.CapabilityReadiness == "blocked" {
			t.Fatalf("unexpected initial product state: %#v", state)
		}
	}
	scanned, err := service.Scan(ctx, skills.ScanInput{
		OperationID: productOperationID(namespace, 1), Reason: "page_open",
	})
	if err != nil {
		t.Fatalf("scan %s catalog: %v", manifest.DistributionChannel, err)
	}
	assertThirtyEightStates(t, scanned, "not_installed", false, false)
	revision := sha256.Sum256(raw)
	for index, skill := range manifest.Skills {
		state, err := service.Install(ctx, skills.InstallInput{
			OperationID:           productOperationID(namespace, 10+index),
			SkillID:               skill.ID,
			ExpectedVersion:       skill.Version,
			ExpectedArchiveSHA256: skill.Archive.SHA256,
			CatalogRevision:       hex.EncodeToString(revision[:]),
		})
		if err != nil || state.InstallationStatus != "installed" || !state.Enabled || !state.RuntimeVisible {
			t.Fatalf("install %s[%d] %s: state=%#v err=%v", manifest.DistributionChannel, index, skill.ID, state, err)
		}
	}
	installed, err := service.List(ctx)
	assertThirtyEightStates(t, installed, "installed", true, true)
	for index, skill := range manifest.Skills {
		state, err := service.SetEnabled(ctx, skills.EnabledInput{
			OperationID: productOperationID(namespace, 100+index), SkillID: skill.ID, Enabled: false,
		})
		if err != nil || !isInstalledButInvisible(state) {
			t.Fatalf("disable %s: state=%#v err=%v", skill.ID, state, err)
		}
	}
	if err := service.Close(); err != nil {
		t.Fatal(err)
	}

	runtimeTwo := newFilesystemSkillsRuntime("")
	restarted, err := skills.NewService(skills.Config{BundleRoot: bundleRoot, ManagedRoot: managedRoot, Runtime: runtimeTwo})
	if err != nil {
		t.Fatalf("restart %s service: %v", manifest.DistributionChannel, err)
	}
	defer restarted.Close()
	replayed, err := restarted.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	assertThirtyEightStates(t, replayed, "installed", false, false)
	for index, skill := range manifest.Skills {
		state, err := restarted.SetEnabled(ctx, skills.EnabledInput{
			OperationID: productOperationID(namespace, 200+index), SkillID: skill.ID, Enabled: true,
		})
		if err != nil || !state.RuntimeVisible {
			t.Fatalf("re-enable %s after restart: state=%#v err=%v", skill.ID, state, err)
		}
	}

	movedSkill := manifest.Skills[len(manifest.Skills)/2]
	movedRoot := filepath.Join(filepath.Dir(managedRoot), "moved-"+manifest.DistributionChannel)
	if err := os.Rename(filepath.Join(managedRoot, movedSkill.ID), movedRoot); err != nil {
		t.Fatalf("move installed Skill outside managed root: %v", err)
	}
	movedSnapshot, err := restarted.Scan(ctx, skills.ScanInput{
		OperationID: productOperationID(namespace, 300), Reason: "directory_changed",
	})
	if err != nil {
		t.Fatal(err)
	}
	movedState := snapshotStateByID(t, movedSnapshot, movedSkill.ID)
	if movedState.InstallationStatus != "not_installed" || movedState.Enabled || movedState.RuntimeVisible {
		t.Fatalf("moved Skill remained active: %#v", movedState)
	}
	reinstalled, err := restarted.Install(ctx, skills.InstallInput{
		OperationID:           productOperationID(namespace, 301),
		SkillID:               movedSkill.ID,
		ExpectedVersion:       movedSkill.Version,
		ExpectedArchiveSHA256: movedSkill.Archive.SHA256,
		CatalogRevision:       hex.EncodeToString(revision[:]),
	})
	if err != nil || !reinstalled.RuntimeVisible {
		t.Fatalf("reinstall moved Skill: state=%#v err=%v", reinstalled, err)
	}

	for index, skill := range manifest.Skills {
		state, err := restarted.Uninstall(ctx, skills.UninstallInput{
			OperationID: productOperationID(namespace, 400+index), SkillID: skill.ID,
		})
		if err != nil || state.InstallationStatus != "not_installed" || state.Enabled || state.RuntimeVisible {
			t.Fatalf("uninstall %s: state=%#v err=%v", skill.ID, state, err)
		}
	}
	final, err := restarted.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	assertThirtyEightStates(t, final, "not_installed", false, false)
}

func productOperationID(namespace, sequence int) string {
	return fmt.Sprintf("019fbd88-cbc3-7bf1-934f-%012x", namespace*1000+sequence)
}

func assertThirtyEightStates(t *testing.T, snapshot skills.Snapshot, status string, enabled, visible bool) {
	t.Helper()
	if len(snapshot.Skills) != 38 {
		t.Fatalf("snapshot contains %d Skills, want 38", len(snapshot.Skills))
	}
	for _, state := range snapshot.Skills {
		if state.CatalogStatus != "installable" || state.CatalogBlockedReason != "" ||
			state.InstallationStatus != status || state.Enabled != enabled || state.RuntimeVisible != visible {
			t.Fatalf("unexpected 38-Skill lifecycle state: %#v", state)
		}
	}
}

func snapshotStateByID(t *testing.T, snapshot skills.Snapshot, id string) skills.State {
	t.Helper()
	for _, state := range snapshot.Skills {
		if state.ID == id {
			return state
		}
	}
	t.Fatalf("snapshot omitted %s", id)
	return skills.State{}
}
