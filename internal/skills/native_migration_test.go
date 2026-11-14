package skills

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	bolt "go.etcd.io/bbolt"
)

func TestFEAT174V2JournalGuardRetainsBytesAndCompatibleCodec(t *testing.T) {
	old, config, first, _ := fullLegacyJournal(t)
	if _, err := old.Scan(context.Background(), ScanInput{OperationID: historyID(maxOperations), Reason: "user_retry"}); err != nil {
		t.Fatal(err)
	}
	originalMarker, err := os.ReadFile(filepath.Join(config.ManagedRoot, stateDirectoryName, operationsFileName))
	if err != nil {
		t.Fatal(err)
	}
	originalDB, err := os.ReadFile(filepath.Join(config.ManagedRoot, stateDirectoryName, operationsDatabaseName))
	if err != nil {
		t.Fatal(err)
	}
	private := filepath.Join(t.TempDir(), "state.db")
	db, err := bolt.Open(private, 0600, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = db.Update(func(tx *bolt.Tx) error {
		meta, e := tx.CreateBucket([]byte("meta"))
		if e != nil {
			return e
		}
		if e = meta.Put([]byte("schema"), []byte("1")); e != nil {
			return e
		}
		_, e = tx.CreateBucket([]byte("entries"))
		return e
	}); err != nil {
		t.Fatal(err)
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	if err = old.FreezeNativeJournal(private); err != nil {
		t.Fatal(err)
	}
	for name, original := range map[string][]byte{operationsFileName: originalMarker, operationsDatabaseName: originalDB} {
		sum := sha256.Sum256(original)
		saved, err := os.ReadFile(filepath.Join(config.ManagedRoot, stateDirectoryName, "native-v3/snapshots", hex.EncodeToString(sum[:])+"-"+name))
		if err != nil || !bytes.Equal(saved, original) {
			t.Fatal("journal snapshot differs", name, err)
		}
	}
	guard, err := os.ReadFile(filepath.Join(config.ManagedRoot, stateDirectoryName, operationsFileName))
	if err != nil || !bytes.Contains(guard, []byte("\"schema_version\":3")) {
		t.Fatal("v3 guard missing", err)
	}
	replayed, err := old.Scan(context.Background(), ScanInput{OperationID: historyID(0), Reason: "page_open"})
	if err != nil || !reflect.DeepEqual(replayed, first) {
		t.Fatal("completed result changed", err)
	}
	// A new compatibility-codec write must go to the redirected backend.
	if _, err = old.Scan(context.Background(), ScanInput{OperationID: historyID(maxOperations + 1), Reason: "user_retry"}); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(filepath.Join(config.ManagedRoot, stateDirectoryName, operationsFileName))
	if err != nil || !bytes.Equal(after, guard) {
		t.Fatal("compatible write replaced guard", err)
	}
	if err = old.Close(); err != nil {
		t.Fatal(err)
	}
	reader, err := NewService(config)
	if err != nil {
		t.Fatal("unconditional v3 reader", err)
	}
	defer reader.Close()
	result, err := reader.Scan(context.Background(), ScanInput{OperationID: historyID(0), Reason: "page_open"})
	if err != nil || !reflect.DeepEqual(result, first) {
		t.Fatal("reopened exact result differs", err)
	}
	originalAfter, err := os.ReadFile(filepath.Join(config.ManagedRoot, stateDirectoryName, operationsDatabaseName))
	if err != nil || !bytes.Equal(originalAfter, originalDB) {
		t.Fatal("original v2 bytes changed", err)
	}
}

func TestFEAT174RecoveredOrdinaryRequestKeepsFingerprintWithoutRedispatch(t *testing.T) {
	bundle, _ := copyPinnedTestBundle(t, "manifest-valid.json", "fixture-model-only-0.1.0.zip")
	current, err := loadCatalog(bundle)
	if err != nil {
		t.Fatal(err)
	}
	runtime := newTestRuntime(current.manifest.Skills[0].RuntimeName)
	config := Config{BundleRoot: bundle, ManagedRoot: filepath.Join(t.TempDir(), "managed"), Runtime: runtime}
	old, err := NewService(config)
	if err != nil {
		t.Fatal(err)
	}
	completed, err := old.Scan(context.Background(), ScanInput{OperationID: historyID(0), Reason: "page_open"})
	if err != nil {
		t.Fatal(err)
	}
	id := historyID(1)
	fingerprint := operationFingerprint("enabled", id, testSkillID, "true")
	if _, owner, err := old.reserveOperation(id, fingerprint, "enabled", testSkillID); err != nil || !owner {
		t.Fatal("normal received intent", err)
	}
	if err = old.Close(); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(filepath.Join(config.ManagedRoot, stateDirectoryName, operationsFileName))
	if err != nil {
		t.Fatal(err)
	}
	home := t.TempDir()
	if err = SnapshotNativeMigrationInput(config.ManagedRoot, home); err != nil {
		t.Fatal(err)
	}
	reopened, err := NewService(config)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	calls := runtime.calls
	reported := []string{}
	if err = reopened.FinalizeRecoveredOrdinaryOperations(func(ids []string) error { reported = append(reported, ids...); return nil }); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(reported, []string{id}) || runtime.calls != calls {
		t.Fatal("recovery redispatched or changed identity")
	}
	if record := reopened.operations[id]; record.Fingerprint != fingerprint || record.Status != "complete" || record.ErrorCode != CodeRuntimeSyncFailed {
		t.Fatal("unconfirmed intent was turned into success or lost")
	}
	again, err := reopened.Scan(context.Background(), ScanInput{OperationID: historyID(0), Reason: "page_open"})
	if err != nil || !reflect.DeepEqual(again, completed) {
		t.Fatal("completed history changed", err)
	}
	sum := sha256.Sum256(before)
	snapshot, err := os.ReadFile(filepath.Join(home, "skill-market-v2/legacy-input", hex.EncodeToString(sum[:])+"-"+operationsFileName))
	if err != nil || !bytes.Equal(snapshot, before) {
		t.Fatal("pre-recovery bytes lost", err)
	}
}
