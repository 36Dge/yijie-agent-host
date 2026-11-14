package skills

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	bolt "go.etcd.io/bbolt"
)

const nativeLegacyDirectory = "native-v3/legacy"

type nativeRoute struct {
	SchemaVersion int    `json:"schema_version"`
	StateDatabase string `json:"state_database"`
	Backend       string `json:"backend"`
}
type LegacySkillSource struct {
	ID, RuntimeName, Version, Root, Entrypoint string
	DisplayName, Description, Category         string
	Retired                                    bool
	Enabled                                    bool
	Files                                      map[string][]byte
	ToggleEvidence                             bool
}

// Validate the same immutable asset authority before opening the session writer,
// without running legacy filesystem recovery before acquiring the Host store.
func ValidateResourceAuthority(config Config) error {
	bundle, err := canonicalDirectory(config.BundleRoot, false)
	if err != nil {
		return err
	}
	if err = validateDirectoryAuthority(bundle, false); err != nil {
		return err
	}
	current, err := loadCatalog(bundle)
	if err != nil {
		return err
	}
	return validateExpertCatalogAuthority(current, config.ExpertCatalogDigest)
}

// Capture the pre-recovery bytes while the caller owns the Host session-store
// lock. This precedes NewService's normal legacy recovery and never opens an old
// writer or changes the original journal.
func SnapshotNativeMigrationInput(managedRoot, hostHome string) error {
	state := filepath.Join(managedRoot, stateDirectoryName)
	raw, err := os.ReadFile(filepath.Join(state, operationsFileName))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var marker operationStore
	if strictJSON(raw, &marker) != nil {
		return errors.New("legacy migration input is invalid")
	}
	if marker.SchemaVersion == 3 {
		return nil
	}
	if marker.SchemaVersion != 1 && marker.SchemaVersion != 2 {
		return errors.New("legacy migration input schema unsupported")
	}
	snapshots := filepath.Join(hostHome, "skill-market-v2/legacy-input")
	if err = os.MkdirAll(snapshots, 0700); err != nil {
		return err
	}
	for _, name := range []string{operationsFileName, operationsDatabaseName} {
		source := filepath.Join(state, name)
		info, e := os.Lstat(source)
		if errors.Is(e, os.ErrNotExist) {
			continue
		}
		if e != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
			return errors.New("legacy migration input authority unavailable")
		}
		file, e := os.Open(source)
		if e != nil {
			return e
		}
		hash := sha256.New()
		_, e = io.Copy(hash, file)
		file.Close()
		if e != nil {
			return e
		}
		sum := hex.EncodeToString(hash.Sum(nil))
		if e = copyJournalFile(source, filepath.Join(snapshots, sum+"-"+name), sum); e != nil {
			return e
		}
	}
	folder, err := os.Open(snapshots)
	if err != nil {
		return err
	}
	defer folder.Close()
	return folder.Sync()
}

// Normal legacy recovery has already restored any unconfirmed filesystem
// change. An unacknowledged ordinary request has no complete replay result and
// its full request cannot be reconstructed from a fingerprint. Finish it in the
// existing error codec, preserving ID/fingerprint, rather than guessing intent
// or executing it again. Completed results and expert requests are untouched.
func (s *Service) FinalizeRecoveredOrdinaryOperations(report ...func([]string) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.opMu.Lock()
	defer s.opMu.Unlock()
	if s.nativeReader {
		return nil
	}
	for _, record := range s.operations {
		if record.Status == "complete" {
			continue
		}
		if record.Phase != "reserved" || !channelClosed(record.done) {
			return errors.New("legacy operation is still active")
		}
		if skill, ok := s.operationCatalog.byID[record.SkillID]; ok && isExpertResource(skill) {
			return errors.New("expert operation requires its original recovery before Skill migration")
		}
	}
	ids := []string{}
	for _, record := range s.operations {
		if record.Status != "complete" {
			ids = append(ids, record.ID)
		}
	}
	if len(ids) > 0 && len(report) == 1 {
		if err := report[0](ids); err != nil {
			return err
		}
	}
	changed := false
	for _, record := range s.operations {
		if record.Status == "complete" {
			continue
		}
		record.Status = "complete"
		record.Phase = "complete"
		record.ErrorCode = CodeRuntimeSyncFailed
		if record.Kind == "scan" {
			record.ErrorCode = CodeScanFailed
		}
		record.CompletedAt = s.now().UTC()
		changed = true
	}
	if !changed {
		return nil
	}
	return s.persistOperationsLocked()
}

// Called by every new loader, independently of the marketplace feature flag.
// A v3 marker never falls back to the public legacy writer.
func (s *Service) openNativeLegacyBackend() error {
	raw, err := s.state.ReadFile("native-v3/route.json")
	if err != nil {
		return errors.New("native Skill compatibility route unavailable")
	}
	var route nativeRoute
	if strictJSON(raw, &route) != nil || route.SchemaVersion != 1 || route.Backend != nativeLegacyDirectory || !filepath.IsAbs(route.StateDatabase) || filepath.Base(route.StateDatabase) != "state.db" {
		return errors.New("native Skill compatibility route invalid")
	}
	info, err := os.Lstat(route.StateDatabase)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
		return errors.New("native Skill state unavailable")
	}
	db, err := bolt.Open(route.StateDatabase, 0600, &bolt.Options{ReadOnly: true, Timeout: time.Second})
	if err != nil {
		return err
	}
	s.nativeOwned = map[string]bool{}
	err = db.View(func(tx *bolt.Tx) error {
		meta := tx.Bucket([]byte("meta"))
		entries := tx.Bucket([]byte("entries"))
		if meta == nil || entries == nil || (string(meta.Get([]byte("schema"))) != "1" && string(meta.Get([]byte("schema"))) != "2") {
			return errors.New("native Skill state schema unsupported")
		}
		return entries.ForEach(func(id, raw []byte) error {
			var e struct {
				Authority string `json:"authority"`
			}
			if err := json.Unmarshal(raw, &e); err != nil {
				return err
			}
			if e.Authority != "native" && e.Authority != "legacy" {
				return errors.New("native Skill authority schema unsupported")
			}
			s.nativeOwned[string(id)] = e.Authority == "native"
			return nil
		})
	})
	closeErr := db.Close()
	if err != nil || closeErr != nil {
		return errors.Join(err, closeErr)
	}
	backend, err := s.state.OpenRoot(route.Backend)
	if err != nil {
		return err
	}
	s.publicState = s.state
	s.state = backend
	s.statePath = filepath.Join(s.managedRoot, stateDirectoryName, route.Backend)
	s.nativeReader = true
	s.ordinaryReadOnly = true
	return s.loadOperations()
}
func (s *Service) nativeOwns(id string) bool {
	if id == "" {
		return false
	}
	if skill, ok := s.operationCatalog.byID[id]; ok && isExpertResource(skill) {
		return false
	}
	if s.nativeAuthority != nil {
		return s.nativeAuthority(id)
	}
	return s.nativeOwned[id]
}
func (s *Service) SetNativeAuthority(check func(string) bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nativeAuthority = check
	s.ordinaryReadOnly = true
}

// Export only receipt-validated ordinary files. Expert resources and old
// journals remain in their original codecs and directories.
func (s *Service) ExportOrdinarySources() ([]LegacySkillSource, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	current, err := s.loadCurrentCatalog()
	if err != nil {
		return nil, err
	}
	current, err = s.includeRetiredOrdinary(current)
	if err != nil {
		return nil, err
	}
	result := []LegacySkillSource{}
	for _, skill := range current.manifest.Skills {
		if isExpertResource(skill) {
			continue
		}
		installed := inspectInstallation(s.managed, skill)
		if !installed.exists {
			continue
		}
		if !installed.valid {
			return nil, errorWithCode(installed.failure, errors.New("legacy Skill needs review before migration"))
		}
		root, err := s.managed.OpenRoot(skill.ID)
		if err != nil {
			return nil, err
		}
		files := map[string][]byte{}
		for _, f := range installed.receipt.Files {
			raw, e := root.ReadFile(f.Path)
			if e != nil {
				root.Close()
				return nil, e
			}
			files[f.Path] = raw
		}
		root.Close()
		result = append(result, LegacySkillSource{ID: skill.ID, RuntimeName: installed.receipt.RuntimeName, Version: installed.receipt.Version, Root: filepath.Join(s.managedRoot, skill.ID), Entrypoint: filepath.Join(s.managedRoot, skill.ID, skill.Entrypoint), Enabled: installed.enabled, Files: files, ToggleEvidence: s.legacyToggleEvidence(skill.ID, installed.enabled), DisplayName: skill.DisplayName, Description: skill.Description, Category: skill.Category, Retired: skill.legacyRetired})
	}
	return result, nil
}
func (s *Service) legacyToggleEvidence(id string, enabled bool) bool {
	// Completed Host output plus the verified receipt and current native rule are
	// required by the caller. A path prefix alone never proves rule ownership.
	matches := false
	var latest time.Time
	inspect := func(p persistedOperation) {
		if p.Status == "complete" && p.ErrorCode == "" && p.State != nil && p.SkillID == id && p.CompletedAt != nil && (p.Kind == "enabled" || p.Kind == "install") && p.CompletedAt.After(latest) {
			latest = *p.CompletedAt
			matches = p.State.Enabled == enabled
		}
	}
	s.opMu.Lock()
	defer s.opMu.Unlock()
	for _, r := range s.operations {
		inspect(persistedOperation{Status: r.Status, ErrorCode: r.ErrorCode, State: stateToPersistedV1(r.State), SkillID: r.SkillID, CompletedAt: timePointer(r.CompletedAt), Kind: r.Kind})
	}
	if s.operationDB != nil {
		_ = s.operationDB.View(func(tx *bolt.Tx) error {
			return tx.Bucket(operationsBucket).ForEach(func(_, raw []byte) error {
				var p persistedOperation
				if json.Unmarshal(raw, &p) == nil {
					inspect(p)
				}
				return nil
			})
		})
	}
	return matches
}

// Freeze closes the old database before byte snapshots, prepares the compatible
// backend and locator, then publishes the public v3 marker LAST. No native
// installation is allowed before this method succeeds.
func (s *Service) FreezeNativeJournal(stateDatabase string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.opMu.Lock()
	defer s.opMu.Unlock()
	if s.nativeReader {
		return nil
	}
	for _, op := range s.operations {
		if op.Status != "complete" {
			return errors.New("unfinished legacy Skill operation must converge before migration")
		}
	}
	if s.operationDB != nil {
		if err := s.operationDB.Close(); err != nil {
			return err
		}
		s.operationDB = nil
	}
	statePath := filepath.Join(s.managedRoot, stateDirectoryName)
	backend := filepath.Join(statePath, nativeLegacyDirectory)
	snapshots := filepath.Join(statePath, "native-v3/snapshots")
	for _, dir := range []string{backend, snapshots} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			return err
		}
	}
	for _, name := range []string{operationsFileName, operationsDatabaseName} {
		original := filepath.Join(statePath, name)
		info, err := os.Lstat(original)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil || !info.Mode().IsRegular() {
			return errors.New("legacy Skill journal snapshot unavailable")
		}
		file, err := os.Open(original)
		if err != nil {
			return err
		}
		hash := sha256.New()
		_, err = io.Copy(hash, file)
		file.Close()
		if err != nil {
			return err
		}
		checksum := hex.EncodeToString(hash.Sum(nil))
		if err = copyJournalFile(original, filepath.Join(snapshots, checksum+"-"+name), checksum); err != nil {
			return err
		}
		// A pre-marker retry may see a previous compatible copy. It must match the
		// frozen source exactly; never overwrite an accepted compatible operation.
		if err = copyJournalFile(original, filepath.Join(backend, name), checksum); err != nil {
			return err
		}
	}
	if _, err := os.Stat(filepath.Join(backend, operationsFileName)); errors.Is(err, os.ErrNotExist) {
		if err = writeNewJournalFile(filepath.Join(backend, operationsFileName), []byte("{\"schema_version\":1,\"operations\":[]}\n")); err != nil {
			return err
		}
	}
	route := nativeRoute{SchemaVersion: 1, StateDatabase: stateDatabase, Backend: nativeLegacyDirectory}
	raw, _ := json.Marshal(route)
	routeName := filepath.Join(statePath, "native-v3/route.json")
	if existing, err := os.ReadFile(routeName); err == nil {
		if string(existing) != string(raw) {
			return errors.New("native Skill route differs")
		}
	} else if errors.Is(err, os.ErrNotExist) {
		if err = writeNewJournalFile(routeName, raw); err != nil {
			return err
		}
	} else {
		return err
	}
	for _, dir := range []string{snapshots, backend, filepath.Join(statePath, "native-v3")} {
		f, err := os.Open(dir)
		if err != nil {
			return err
		}
		err = f.Sync()
		f.Close()
		if err != nil {
			return err
		}
	}
	const temporary = "operations.native-v3.tmp"
	if _, err := s.state.Lstat(temporary); err == nil {
		if err = s.state.Remove(temporary); err != nil {
			return err
		}
	}
	if err := writeOwnerOnlyFile(s.state, temporary, []byte("{\"schema_version\":3,\"operations\":[]}\n")); err != nil {
		return err
	}
	if err := s.state.Rename(temporary, operationsFileName); err != nil {
		return err
	}
	if err := syncRoot(s.state); err != nil {
		return err
	}
	next, err := s.state.OpenRoot(nativeLegacyDirectory)
	if err != nil {
		return err
	}
	s.publicState = s.state
	s.state = next
	s.statePath = backend
	s.nativeReader = true
	raw, err = s.state.ReadFile(operationsFileName)
	if err != nil {
		return err
	}
	var marker operationStore
	if strictJSON(raw, &marker) != nil {
		return errors.New("legacy backend is invalid")
	}
	if marker.SchemaVersion == 2 {
		db, err := s.openOperationDatabase(false)
		if err != nil {
			return err
		}
		s.operationDB = db
	}
	return nil
}
func writeNewJournalFile(name string, data []byte) error {
	f, err := os.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	_, err = f.Write(data)
	if err == nil {
		err = f.Sync()
	}
	return errors.Join(err, f.Close())
}
func copyJournalFile(source, target, expected string) error {
	if f, err := os.Open(target); err == nil {
		h := sha256.New()
		_, err = io.Copy(h, f)
		f.Close()
		if err != nil || hex.EncodeToString(h.Sum(nil)) != expected {
			return errors.New("legacy snapshot differs")
		}
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	_, err = io.Copy(out, in)
	if err == nil {
		err = out.Sync()
	}
	return errors.Join(err, out.Close())
}
func (s *Service) SyncLegacyRoots(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	current, err := s.loadCurrentCatalog()
	if err != nil {
		return err
	}
	_, installed := inspectCatalog(s.managed, current)
	roots, err := s.runtimeRootsLocked(current, installed)
	if err != nil {
		return err
	}
	return s.setRuntimeRootsLocked(ctx, roots)
}

func (s *Service) InspectOrdinary(ctx context.Context, id string) (State, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	catalog, err := s.loadCurrentCatalog()
	if err != nil {
		return State{}, err
	}
	skill, err := manifestSkill(catalog, id)
	if err != nil || isExpertResource(skill) {
		return State{}, errorWithCode(CodeNotFound, err)
	}
	installed := inspectInstallation(s.managed, skill)
	result := State{ID: id, RuntimeName: skill.RuntimeName, Version: skill.Version, InstallationStatus: "not_installed"}
	if !installed.exists {
		return result, nil
	}
	if !installed.valid {
		return State{}, errorWithCode(installed.failure, errors.New("legacy installation unavailable"))
	}
	result.Version = installed.receipt.Version
	result.InstallationStatus = "installed"
	result.Enabled = installed.enabled
	entries, err := s.runtime.ListSkills(ctx, []string{s.managedRoot}, true)
	if err != nil {
		return State{}, err
	}
	for _, entry := range entries {
		for _, visible := range entry.Skills {
			if visible.Path == filepath.Join(s.managedRoot, id, skill.Entrypoint) && visible.Name == skill.RuntimeName {
				result.RuntimeVisible = visible.Enabled
			}
		}
	}
	return result, nil
}

func (s *Service) nativePathOwned(p string) bool {
	relative, err := filepath.Rel(s.managedRoot, p)
	if err != nil {
		return false
	}
	parts := strings.Split(relative, string(os.PathSeparator))
	return len(parts) > 1 && s.nativeOwns(parts[0])
}

func (s *Service) SetNativeProjection(project func(context.Context, string) (State, error)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nativeProjection = project
}

func (s *Service) withResourceMaintenance(ctx context.Context, action func(context.Context) (State, error)) (State, error) {
	if !s.nativeReader {
		return action(ctx)
	}
	runtime, ok := s.runtime.(interface {
		WithSkillMaintenance(context.Context, func(context.Context) error) error
	})
	if !ok {
		return State{}, errorWithCode(CodeRuntimeUnavailable, errors.New("native resource admission unavailable"))
	}
	var state State
	err := runtime.WithSkillMaintenance(ctx, func(guarded context.Context) error { var err error; state, err = action(guarded); return err })
	return state, err
}
