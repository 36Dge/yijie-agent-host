package skills

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	bolt "go.etcd.io/bbolt"
)

// Ordinary completed-history fixture: no process interruption, fault injection,
// malformed archive, or permission manipulation is used in these tests.
func fullLegacyJournal(t *testing.T) (*Service, Config, Snapshot, []byte) {
	t.Helper()
	bundle, _ := copyPinnedTestBundle(t, "manifest-valid.json", "fixture-model-only-0.1.0.zip")
	current, err := loadCatalog(bundle)
	if err != nil {
		t.Fatal(err)
	}
	config := Config{BundleRoot: bundle, ManagedRoot: filepath.Join(t.TempDir(), "managed"), Runtime: newTestRuntime(current.manifest.Skills[0].RuntimeName)}
	s, err := NewService(config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	first, err := s.Scan(context.Background(), ScanInput{OperationID: historyID(0), Reason: "page_open"})
	if err != nil {
		t.Fatal(err)
	}
	for i := 1; i < maxOperations; i++ {
		r := *s.operations[historyID(0)]
		r.ID = historyID(i)
		r.Fingerprint = operationFingerprint("scan", r.ID, "page_open")
		s.operations[r.ID] = &r
	}
	if err := s.persistOperationsLocked(); err != nil {
		t.Fatal(err)
	}
	legacy, err := s.state.ReadFile(operationsFileName)
	if err != nil {
		t.Fatal(err)
	}
	return s, config, first, legacy
}

func historyID(i int) string { return fmt.Sprintf("019fbd88-cbc3-7bf1-934e-%012d", i) }

func databaseCount(t *testing.T, s *Service) int {
	t.Helper()
	count := 0
	if err := s.operationDB.View(func(tx *bolt.Tx) error {
		count = tx.Bucket(operationsBucket).Stats().KeyN
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return count
}

func TestFullSkillJournalMigratesWithoutLosingReplayOrBlockingLifecycle(t *testing.T) {
	s, config, first, legacy := fullLegacyJournal(t)
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err := NewService(config)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	for i := 0; i < 5; i++ {
		if _, err := s.List(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	afterReads, err := s.state.ReadFile(operationsFileName)
	if err != nil || !bytes.Equal(legacy, afterReads) || s.operationDB != nil {
		t.Fatalf("live reads changed the full legacy journal: %v", err)
	}
	if _, err := s.Scan(context.Background(), ScanInput{OperationID: historyID(maxOperations), Reason: "user_retry"}); err != nil {
		t.Fatal(err)
	}
	if count := databaseCount(t, s); count != maxOperations+1 {
		t.Fatalf("lost history: %d", count)
	}
	backup, err := s.state.ReadFile("operations.v1-" + operationFingerprint(string(legacy)) + ".json")
	if err != nil || !bytes.Equal(backup, legacy) {
		t.Fatalf("legacy backup is not exact: %v", err)
	}
	marker, err := s.state.ReadFile(operationsFileName)
	if err != nil {
		t.Fatal(err)
	}
	var document operationStore
	if json.Unmarshal(marker, &document) != nil || document.SchemaVersion != 2 {
		t.Fatal("old Hosts are not prevented from reopening partial history")
	}
	current, err := loadCatalog(config.BundleRoot)
	if err != nil {
		t.Fatal(err)
	}
	skill := current.manifest.Skills[0]
	install := InstallInput{OperationID: historyID(maxOperations + 1), SkillID: skill.ID, ExpectedVersion: skill.Version,
		ExpectedArchiveSHA256: skill.Archive.SHA256, CatalogRevision: current.revision}
	installed, err := s.Install(context.Background(), install)
	if err != nil || !installed.Enabled || !installed.RuntimeVisible {
		t.Fatalf("install after full history: %#v, %v", installed, err)
	}
	if _, err := s.SetEnabled(context.Background(), EnabledInput{OperationID: historyID(maxOperations + 2), SkillID: skill.ID, Enabled: false}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Uninstall(context.Background(), UninstallInput{OperationID: historyID(maxOperations + 3), SkillID: skill.ID}); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = NewService(config)
	if err != nil {
		t.Fatal(err)
	}
	if len(s.operations) != 0 {
		t.Fatal("completed disk history was loaded into the active cache")
	}
	for _, i := range []int{0, 1000, maxOperations - 1} {
		replayed, err := s.Scan(context.Background(), ScanInput{OperationID: historyID(i), Reason: "page_open"})
		if err != nil || !reflect.DeepEqual(first, replayed) {
			t.Fatalf("old scan %d changed after restart: %v", i, err)
		}
	}
	_, err = s.Scan(context.Background(), ScanInput{OperationID: historyID(0), Reason: "window_resume"})
	assertCode(t, err, CodeOperationConflict)
	replayedInstall, err := s.Install(context.Background(), install)
	if err != nil || !reflect.DeepEqual(installed, replayedInstall) {
		t.Fatalf("install replay changed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(config.ManagedRoot, skill.ID)); !os.IsNotExist(err) {
		t.Fatal("replaying a past install reinstalled an already uninstalled Skill")
	}
	if count := databaseCount(t, s); count != maxOperations+4 {
		t.Fatalf("replay changed history: %d", count)
	}
}

func TestSkillJournalV2ResumesOrdinaryReservedRequestAfterClose(t *testing.T) {
	s, config, _, _ := fullLegacyJournal(t)
	id := historyID(maxOperations)
	record, owner, err := s.reserveOperation(id, operationFingerprint("scan", id, "page_open"), "scan", "")
	if err != nil || !owner || record.Status != "in_progress" {
		t.Fatalf("reserve: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := NewService(config)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if len(reopened.operations) != 1 {
		t.Fatalf("pending request not loaded: %d", len(reopened.operations))
	}
	if _, err := reopened.Scan(context.Background(), ScanInput{OperationID: id, Reason: "page_open"}); err != nil {
		t.Fatal(err)
	}
	if count := databaseCount(t, reopened); count != maxOperations+1 {
		t.Fatalf("resuming duplicated history: %d", count)
	}
}

func TestSkillJournalV2LiveReadsDoNotWriteOrForgetCommittedCleanup(t *testing.T) {
	s, config, _, _ := fullLegacyJournal(t)
	if _, err := s.Scan(context.Background(), ScanInput{OperationID: historyID(maxOperations), Reason: "user_retry"}); err != nil {
		t.Fatal(err)
	}
	transactionID := func() int {
		var id int
		if err := s.operationDB.View(func(tx *bolt.Tx) error { id = tx.ID(); return nil }); err != nil {
			t.Fatal(err)
		}
		return id
	}
	before := transactionID()
	for i := 0; i < 5; i++ {
		if _, err := s.List(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if transactionID() != before {
		t.Fatal("live catalog reads wrote unchanged journal transactions")
	}
	current, err := loadCatalog(config.BundleRoot)
	if err != nil {
		t.Fatal(err)
	}
	skill := current.manifest.Skills[0]
	id := historyID(maxOperations + 1)
	if _, err := s.Install(context.Background(), InstallInput{OperationID: id, SkillID: skill.ID, ExpectedVersion: skill.Version,
		ExpectedArchiveSHA256: skill.Archive.SHA256, CatalogRevision: current.revision}); err != nil {
		t.Fatal(err)
	}
	// A valid committed-cleanup layout, built in a temporary managed directory.
	// No syscall failure, process interruption, or permission fault is introduced.
	backup := transactionName(".yijie-backup", skill.ID, id)
	if err := s.managed.Mkdir(backup, 0o700); err != nil {
		t.Fatal(err)
	}
	s.opMu.Lock()
	s.releaseCompletedOperationCache()
	s.opMu.Unlock()
	snapshot, err := s.List(context.Background())
	if err != nil || snapshot.Skills[0].InstallationStatus != "installed" || !snapshot.Skills[0].RuntimeVisible {
		t.Fatalf("evicting a cached commit reverted an installed Skill: %#v, %v", snapshot, err)
	}
	if _, err := s.managed.Lstat(backup); !os.IsNotExist(err) {
		t.Fatal("committed cleanup did not complete")
	}
}
