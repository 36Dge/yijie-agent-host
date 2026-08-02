package session

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	bolt "go.etcd.io/bbolt"
)

func TestStoreMigratesSchemaV1AndPurgesExpiredContentFreeReceipts(t *testing.T) {
	home := t.TempDir()
	databasePath := filepath.Join(home, "sessions.db")
	database, err := bolt.Open(databasePath, 0o600, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := database.Update(func(tx *bolt.Tx) error {
		metadata, err := tx.CreateBucketIfNotExists(metadataBucket)
		if err != nil {
			return err
		}
		if err := metadata.Put(storeSchemaVersionKey, []byte("1")); err != nil {
			return err
		}
		receipts, err := tx.CreateBucketIfNotExists(cleanupReceiptsBucket)
		if err != nil {
			return err
		}
		expired, err := json.Marshal(CleanupReceipt{OperationID: uuid.NewString(), ExpiresAt: time.Now().Add(-time.Hour)})
		if err != nil {
			return err
		}
		return receipts.Put([]byte("expired"), expired)
	}); err != nil {
		t.Fatal(err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	store, err := OpenStore(home)
	if err != nil {
		t.Fatalf("migrate v1 store: %v", err)
	}
	defer store.Close()
	if err := store.db.View(func(tx *bolt.Tx) error {
		if got := string(tx.Bucket(metadataBucket).Get(storeSchemaVersionKey)); got != storeSchemaVersion {
			t.Fatalf("schema version = %q", got)
		}
		if tx.Bucket(cleanupReceiptsBucket).Get([]byte("expired")) != nil {
			t.Fatal("expired cleanup receipt was not purged")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

const (
	testTaskID    = "019c0123-4567-7abc-8123-456789abcdea"
	testSessionID = "019c0123-4567-7abc-8123-456789abcdeb"
	testThreadID  = "019c0123-4567-7abc-8123-456789abcdec"
	testTurnID    = "019c0123-4567-7abc-8123-456789abcded"
)

func TestStorePersistsMappingAndTurnLifecycle(t *testing.T) {
	home := filepath.Join(t.TempDir(), "host-home")
	store, err := OpenStore(home)
	if err != nil {
		t.Fatal(err)
	}
	record := Record{
		TaskID: testTaskID, AgentSessionID: testSessionID, Cwd: t.TempDir(),
		Trace: TraceContext{TraceID: "trace-1"},
	}
	if err := store.Reserve(record); err != nil {
		t.Fatal(err)
	}
	if err := store.Reserve(record); !errors.Is(err, ErrTaskExists) {
		t.Fatalf("expected duplicate task rejection, got %v", err)
	}
	bound, err := store.BindThread(testSessionID, testThreadID, "runtime-session", "MiniMax-M3", "minimax")
	if err != nil {
		t.Fatal(err)
	}
	if bound.State != StateIdle || bound.CodexThreadID != testThreadID {
		t.Fatalf("unexpected bound mapping: %+v", bound)
	}
	if _, err := store.PrepareTurn(testSessionID, TraceContext{TraceID: "trace-2"}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.BindTurn(testSessionID, testTurnID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.PrepareTurn(testSessionID, TraceContext{}); !errors.Is(err, ErrTurnActive) {
		t.Fatalf("expected active turn conflict, got %v", err)
	}
	completed, err := store.CompleteTurn(testSessionID, testTurnID, "completed")
	if err != nil {
		t.Fatal(err)
	}
	if completed.State != StateIdle || completed.ActiveTurnID != "" || completed.LastTurnID != testTurnID {
		t.Fatalf("unexpected completed mapping: %+v", completed)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := OpenStore(home)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	persisted, err := reopened.GetByThread(testThreadID)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.AgentSessionID != testSessionID || persisted.Trace.TraceID != "trace-2" {
		t.Fatalf("persisted mapping mismatch: %+v", persisted)
	}
	info, err := os.Stat(filepath.Join(home, "sessions.db"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o077 != 0 {
		t.Fatalf("session store is accessible to group/other: %o", info.Mode().Perm())
	}
	keyInfo, err := os.Stat(filepath.Join(home, "cleanup-receipt.key"))
	if err != nil {
		t.Fatal(err)
	}
	if keyInfo.Mode().Perm()&0o077 != 0 {
		t.Fatalf("cleanup receipt key is accessible to group/other: %o", keyInfo.Mode().Perm())
	}
}

func TestStorePhysicallyDeletesMappingsAndKeepsOnlyContentFreeReceipt(t *testing.T) {
	home := filepath.Join(t.TempDir(), "host-home")
	store, err := OpenStore(home)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.Reserve(Record{TaskID: testTaskID, AgentSessionID: testSessionID, Cwd: "/synthetic/private/project"}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.BindThread(testSessionID, testThreadID, "runtime-secret", "MiniMax-M3", "minimax"); err != nil {
		t.Fatal(err)
	}
	operationID := "019c0123-4567-7abc-8123-456789abcdee"
	if err := store.DeleteSessionWithReceipt(operationID, testSessionID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Get(testSessionID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("session mapping survived delete: %v", err)
	}
	if _, err := store.GetByThread(testThreadID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("thread mapping survived delete: %v", err)
	}
	receipt, err := store.CleanupReceipt(operationID, testSessionID)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(receipt)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{testSessionID, testThreadID, testTaskID, "runtime-secret", "/synthetic/private/project"} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("cleanup receipt leaked forbidden content %q: %s", forbidden, encoded)
		}
	}
	if receipt.SessionHash == "" || receipt.Outcome != "complete" {
		t.Fatalf("invalid cleanup receipt: %+v", receipt)
	}
	if _, err := store.CleanupReceipt(operationID, testTaskID); !errors.Is(err, ErrCleanupConflict) {
		t.Fatalf("operation reuse for another target must conflict: %v", err)
	}
}

func TestStoreRejectsWrongActiveTurn(t *testing.T) {
	store, err := OpenStore(filepath.Join(t.TempDir(), "host-home"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.Reserve(Record{TaskID: testTaskID, AgentSessionID: testSessionID, Cwd: t.TempDir()}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.BindThread(testSessionID, testThreadID, "runtime-session", "MiniMax-M3", "minimax"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.PrepareTurn(testSessionID, TraceContext{}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.BindTurn(testSessionID, testTurnID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CompleteTurn(testSessionID, testTaskID, "completed"); !errors.Is(err, ErrTurnNotActive) {
		t.Fatalf("expected wrong turn rejection, got %v", err)
	}
}

func TestStoreDoesNotReactivateTurnCompletedBeforeStartResponse(t *testing.T) {
	store, err := OpenStore(filepath.Join(t.TempDir(), "host-home"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.Reserve(Record{TaskID: testTaskID, AgentSessionID: testSessionID, Cwd: t.TempDir()}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.BindThread(testSessionID, testThreadID, "runtime-session", "MiniMax-M3", "minimax"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.PrepareTurn(testSessionID, TraceContext{}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CompleteTurn(testSessionID, testTurnID, "completed"); err != nil {
		t.Fatal(err)
	}
	record, err := store.BindTurn(testSessionID, testTurnID)
	if err != nil {
		t.Fatal(err)
	}
	if record.State != StateIdle || record.ActiveTurnID != "" || record.LastTurnID != testTurnID {
		t.Fatalf("late turn/start response reactivated a terminal turn: %+v", record)
	}
}

func TestOpenStoreRepairsExistingDatabasePermissions(t *testing.T) {
	home := filepath.Join(t.TempDir(), "host-home")
	store, err := OpenStore(home)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	dbPath := filepath.Join(home, "sessions.db")
	if err := os.Chmod(dbPath, 0o644); err != nil {
		t.Fatal(err)
	}
	store, err = OpenStore(home)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	info, err := os.Stat(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o077 != 0 {
		t.Fatalf("reopened session store permissions were not repaired: %o", info.Mode().Perm())
	}
}

func TestOpenStoreRejectsSymlinkedDatabaseAndReceiptKey(t *testing.T) {
	tests := []string{"sessions.db", "cleanup-receipt.key"}
	for _, name := range tests {
		t.Run(name, func(t *testing.T) {
			home := filepath.Join(t.TempDir(), "host-home")
			if err := os.MkdirAll(home, 0o700); err != nil {
				t.Fatal(err)
			}
			target := filepath.Join(t.TempDir(), "synthetic-target")
			if err := os.WriteFile(target, []byte("synthetic"), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(target, filepath.Join(home, name)); err != nil {
				t.Fatal(err)
			}
			if store, err := OpenStore(home); err == nil {
				_ = store.Close()
				t.Fatal("symlinked private Host file was accepted")
			}
		})
	}
}
