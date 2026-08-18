package session

import (
	"bytes"
	"context"
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

func TestDefaultStoreCompatibilityMatrixKeepsAbsoluteCWDAndNoFEAT126Marker(t *testing.T) {
	const (
		secondTaskID    = "019c0123-4567-7abc-8123-456789abcdfa"
		secondSessionID = "019c0123-4567-7abc-8123-456789abcdfb"
	)
	for _, schema := range []string{"1", "2", "3", "new-write"} {
		t.Run("schema-"+schema, func(t *testing.T) {
			home := filepath.Join(t.TempDir(), "host-home")
			if err := os.Mkdir(home, 0o700); err != nil {
				t.Fatal(err)
			}
			canonicalCWD, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			if schema != "new-write" {
				seedDefaultStoreSchema(t, filepath.Join(home, "sessions.db"), schema, Record{
					TaskID: testTaskID, AgentSessionID: testSessionID, Cwd: canonicalCWD,
				})
			}

			store, err := OpenStore(home)
			if err != nil {
				t.Fatalf("open default schema %s: %v", schema, err)
			}
			if schema == "new-write" {
				if err := store.Reserve(Record{TaskID: testTaskID, AgentSessionID: testSessionID, Cwd: canonicalCWD}); err != nil {
					t.Fatal(err)
				}
			} else if err := store.Reserve(Record{TaskID: secondTaskID, AgentSessionID: secondSessionID, Cwd: canonicalCWD}); err != nil {
				t.Fatal(err)
			}
			persisted, err := store.Get(testSessionID)
			if err != nil || persisted.Cwd != canonicalCWD || !filepath.IsAbs(persisted.Cwd) || filepath.Clean(persisted.Cwd) != persisted.Cwd {
				t.Fatalf("default schema %s changed canonical cwd: record=%+v err=%v", schema, persisted, err)
			}
			if err := store.db.View(func(tx *bolt.Tx) error {
				metadata := tx.Bucket(metadataBucket)
				if got := string(metadata.Get(storeSchemaVersionKey)); got != storeSchemaVersion {
					t.Fatalf("default schema %s did not converge to %s: %q", schema, storeSchemaVersion, got)
				}
				if metadata.Get(feat126CwdEncodingKey) != nil || metadata.Get(feat126RunIDKey) != nil {
					t.Fatalf("default schema %s gained FEAT-126 authority metadata", schema)
				}
				if tx.Bucket(turnOperationsBucket) == nil {
					t.Fatalf("default schema %s omitted the v4 turn operation bucket", schema)
				}
				for _, sessionID := range []string{testSessionID, secondSessionID} {
					encoded := tx.Bucket(sessionsBucket).Get([]byte(sessionID))
					if encoded == nil {
						if schema == "new-write" && sessionID == secondSessionID {
							continue
						}
						t.Fatalf("default schema %s omitted session %s", schema, sessionID)
					}
					if !bytes.Contains(encoded, []byte(canonicalCWD)) || bytes.Contains(encoded, []byte(feat126OpaqueProjectCwd)) {
						t.Fatalf("default schema %s did not persist canonical absolute cwd: %q", schema, encoded)
					}
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			if err := store.Close(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func seedDefaultStoreSchema(t *testing.T, path, schema string, record Record) {
	t.Helper()
	database, err := bolt.Open(path, 0o600, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := database.Update(func(tx *bolt.Tx) error {
		metadata, err := tx.CreateBucketIfNotExists(metadataBucket)
		if err != nil {
			return err
		}
		if err := metadata.Put(storeSchemaVersionKey, []byte(schema)); err != nil {
			return err
		}
		sessions, err := tx.CreateBucketIfNotExists(sessionsBucket)
		if err != nil {
			return err
		}
		encoded, err := json.Marshal(record)
		if err != nil {
			return err
		}
		return sessions.Put([]byte(record.AgentSessionID), encoded)
	}); err != nil {
		_ = database.Close()
		t.Fatal(err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestCleanupLeaseIsAtomicWithTurnStart(t *testing.T) {
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
	start := make(chan struct{})
	results := make(chan error, 2)
	go func() {
		<-start
		_, err := store.BeginCleanup(uuid.NewString(), testSessionID)
		results <- err
	}()
	go func() {
		<-start
		_, err := store.PrepareTurn(testSessionID, TraceContext{})
		results <- err
	}()
	close(start)
	first, second := <-results, <-results
	if (first == nil) == (second == nil) {
		t.Fatalf("cleanup and turn lease were not exclusive: first=%v second=%v", first, second)
	}
	loser := first
	if loser == nil {
		loser = second
	}
	if !errors.Is(loser, ErrTurnActive) && !errors.Is(loser, ErrSessionNotUsable) {
		t.Fatalf("unexpected lease loser: %v", loser)
	}
}

func TestCleanupConfirmedStateSurvivesRestartAndSkipsRuntimeReplay(t *testing.T) {
	home := filepath.Join(t.TempDir(), "host-home")
	store, err := OpenStore(home)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Reserve(Record{TaskID: testTaskID, AgentSessionID: testSessionID, Cwd: t.TempDir()}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.BindThread(testSessionID, testThreadID, "runtime-session", "MiniMax-M3", "minimax"); err != nil {
		t.Fatal(err)
	}
	operationID := uuid.NewString()
	if _, err := store.BeginCleanup(operationID, testSessionID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.MarkCleanupRuntimeDeleted(operationID, testSessionID); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := OpenStore(home)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	runtimeCalls := 0
	service := NewService(&fakeRuntime{deleteThread: func(string) error {
		runtimeCalls++
		return errors.New("must not be called")
	}}, reopened, NewEventHub(8, 2), nil)
	result, err := service.CleanupSession(context.Background(), testSessionID, operationID)
	if err != nil || result.Outcome != "complete" || runtimeCalls != 0 {
		t.Fatalf("restart cleanup did not resume confirmed phase: result=%+v calls=%d err=%v", result, runtimeCalls, err)
	}
}

func TestCleanupReceiptLookupPurgesExpiredRecord(t *testing.T) {
	store, err := OpenStore(filepath.Join(t.TempDir(), "host-home"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	operationID := uuid.NewString()
	expired, err := json.Marshal(CleanupReceipt{
		SchemaVersion: 1, OperationID: operationID, SessionHash: store.keyedSessionHash(testSessionID),
		ExpiresAt: time.Now().Add(-time.Second),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.db.Update(func(tx *bolt.Tx) error {
		return tx.Bucket(cleanupReceiptsBucket).Put([]byte(operationID), expired)
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CleanupReceipt(operationID, testSessionID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expired receipt lookup: %v", err)
	}
	if err := store.db.View(func(tx *bolt.Tx) error {
		if tx.Bucket(cleanupReceiptsBucket).Get([]byte(operationID)) != nil {
			t.Fatal("expired receipt remained in long-lived store")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

const (
	testTaskID           = "019c0123-4567-7abc-8123-456789abcdea"
	testSessionID        = "019c0123-4567-7abc-8123-456789abcdeb"
	testThreadID         = "019c0123-4567-7abc-8123-456789abcdec"
	testTurnID           = "019c0123-4567-7abc-8123-456789abcded"
	testTurnOperationID  = "019c0123-4567-7abc-8123-456789abcdee"
	testTurnOperationID2 = "019c0123-4567-7abc-8123-456789abcdef"
)

func TestTurnOperationAcceptedReplayAndConflictSurviveRestart(t *testing.T) {
	home := filepath.Join(t.TempDir(), "host-home")
	store, err := OpenStore(home)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Reserve(Record{TaskID: testTaskID, AgentSessionID: testSessionID, Cwd: t.TempDir()}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.BindThread(testSessionID, testThreadID, "runtime-session", "MiniMax-M3", "minimax"); err != nil {
		t.Fatal(err)
	}
	digest := strings.Repeat("a", 64)
	operation, _, err := store.PrepareTurnOperation(testSessionID, testTurnOperationID, digest, TraceContext{TraceID: "initial"})
	if err != nil || operation.State != TurnOperationStatePending {
		t.Fatalf("prepare turn operation: operation=%+v err=%v", operation, err)
	}
	if _, err := store.AcceptTurnOperation(testSessionID, testTurnOperationID, digest, testTurnID); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := OpenStore(home)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	replayed, _, err := reopened.PrepareTurnOperation(
		testSessionID, testTurnOperationID, digest, TraceContext{TraceID: "replay"},
	)
	if err != nil || replayed.State != TurnOperationStateAccepted || replayed.TurnID != testTurnID {
		t.Fatalf("accepted operation did not survive restart: operation=%+v err=%v", replayed, err)
	}
	if _, _, err := reopened.PrepareTurnOperation(
		testSessionID, testTurnOperationID, strings.Repeat("b", 64), TraceContext{},
	); !errors.Is(err, ErrTurnOperationConflict) {
		t.Fatalf("changed operation input did not conflict after restart: %v", err)
	}
}

func TestTurnOperationPendingAndUncertainRemainFailClosedAfterRestart(t *testing.T) {
	for _, state := range []string{TurnOperationStatePending, TurnOperationStateUncertain} {
		t.Run(state, func(t *testing.T) {
			home := filepath.Join(t.TempDir(), "host-home")
			store, err := OpenStore(home)
			if err != nil {
				t.Fatal(err)
			}
			if err := store.Reserve(Record{TaskID: testTaskID, AgentSessionID: testSessionID, Cwd: t.TempDir()}); err != nil {
				t.Fatal(err)
			}
			if _, err := store.BindThread(testSessionID, testThreadID, "runtime-session", "MiniMax-M3", "minimax"); err != nil {
				t.Fatal(err)
			}
			digest := strings.Repeat("c", 64)
			if _, _, err := store.PrepareTurnOperation(testSessionID, testTurnOperationID, digest, TraceContext{}); err != nil {
				t.Fatal(err)
			}
			if state == TurnOperationStateUncertain {
				if _, err := store.MarkTurnOperationUncertain(
					testSessionID, testTurnOperationID, digest, "turn_start_failed",
				); err != nil {
					t.Fatal(err)
				}
			}
			if err := store.Close(); err != nil {
				t.Fatal(err)
			}
			reopened, err := OpenStore(home)
			if err != nil {
				t.Fatal(err)
			}
			defer reopened.Close()
			persisted, err := reopened.TurnOperation(testSessionID, testTurnOperationID)
			if err != nil || persisted.State != state {
				t.Fatalf("turn operation state did not survive restart: operation=%+v err=%v", persisted, err)
			}
			if state == TurnOperationStateUncertain {
				if _, err := reopened.Resume(testSessionID, TraceContext{}, "runtime-session-resumed", "", "", ""); err != nil {
					t.Fatalf("resume session after unknown result: %v", err)
				}
			}
			if _, _, err := reopened.PrepareTurnOperation(
				testSessionID, testTurnOperationID, digest, TraceContext{},
			); !errors.Is(err, ErrTurnOperationPending) {
				t.Fatalf("pending or uncertain operation re-entered Runtime path: %v", err)
			}
			if _, _, err := reopened.PrepareTurnOperation(
				testSessionID, testTurnOperationID, strings.Repeat("d", 64), TraceContext{},
			); !errors.Is(err, ErrTurnOperationConflict) {
				t.Fatalf("changed pending or uncertain operation did not conflict: %v", err)
			}
		})
	}
}

func TestTurnOperationScopeIsPerSessionAndCleanupRemovesOperations(t *testing.T) {
	const (
		secondTaskID    = "019c0123-4567-7abc-8123-456789abcdf1"
		secondSessionID = "019c0123-4567-7abc-8123-456789abcdf2"
		secondThreadID  = "019c0123-4567-7abc-8123-456789abcdf3"
	)
	store, err := OpenStore(filepath.Join(t.TempDir(), "host-home"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	for _, record := range []Record{
		{TaskID: testTaskID, AgentSessionID: testSessionID, Cwd: t.TempDir()},
		{TaskID: secondTaskID, AgentSessionID: secondSessionID, Cwd: t.TempDir()},
	} {
		if err := store.Reserve(record); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := store.BindThread(testSessionID, testThreadID, "runtime-session-one", "MiniMax-M3", "minimax"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.BindThread(secondSessionID, secondThreadID, "runtime-session-two", "MiniMax-M3", "minimax"); err != nil {
		t.Fatal(err)
	}
	firstDigest := strings.Repeat("e", 64)
	secondDigest := strings.Repeat("f", 64)
	if _, _, err := store.PrepareTurnOperation(testSessionID, testTurnOperationID, firstDigest, TraceContext{}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.PrepareTurnOperation(secondSessionID, testTurnOperationID, secondDigest, TraceContext{}); err != nil {
		t.Fatalf("same operation id was not independent in another session: %v", err)
	}
	if _, err := store.MarkTurnOperationUncertain(
		testSessionID, testTurnOperationID, firstDigest, "turn_start_failed",
	); err != nil {
		t.Fatal(err)
	}
	cleanupID := uuid.NewString()
	if _, err := store.BeginCleanup(cleanupID, testSessionID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.MarkCleanupRuntimeDeleted(cleanupID, testSessionID); err != nil {
		t.Fatal(err)
	}
	if err := store.DeleteSessionWithReceipt(cleanupID, testSessionID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.TurnOperation(testSessionID, testTurnOperationID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cleanup retained a deleted session's turn operation: %v", err)
	}
	second, err := store.TurnOperation(secondSessionID, testTurnOperationID)
	if err != nil || second.InputDigest != secondDigest {
		t.Fatalf("cleanup crossed session scope: operation=%+v err=%v", second, err)
	}
}

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
	if persisted.AgentSessionID != testSessionID || persisted.Trace.TraceID != "trace-2" || persisted.Cwd != record.Cwd {
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

func TestFEAT126StorePersistsOpaqueProjectAndReconstructsAfterRestart(t *testing.T) {
	const runID = "123e4567-e89b-42d3-a456-426614174000"
	_, hostHome, projectDirectory := newFEAT126StoreAuthority(t, runID)
	option := WithFEAT126Authority(projectDirectory, runID)
	store, err := OpenStore(hostHome, option)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Reserve(Record{
		TaskID: testTaskID, AgentSessionID: testSessionID, Cwd: projectDirectory,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.BindThread(testSessionID, testThreadID, "runtime-session", "MiniMax-M3", "minimax"); err != nil {
		t.Fatal(err)
	}
	if err := store.db.View(func(tx *bolt.Tx) error {
		metadata := tx.Bucket(metadataBucket)
		if string(metadata.Get(feat126CwdEncodingKey)) != feat126CwdEncodingVersion ||
			string(metadata.Get(feat126RunIDKey)) != runID {
			t.Fatalf("unexpected FEAT-126 durable authority metadata")
		}
		encoded := tx.Bucket(sessionsBucket).Get([]byte(testSessionID))
		if bytes.Contains(encoded, []byte(projectDirectory)) || !bytes.Contains(encoded, []byte(feat126OpaqueProjectCwd)) {
			t.Fatalf("unexpected FEAT-126 persisted project representation: %q", encoded)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	databasePath := filepath.Join(hostHome, "sessions.db")
	assertFileExcludesBytes(t, databasePath, []byte(projectDirectory))
	reopened, err := OpenStore(hostHome, option)
	if err != nil {
		t.Fatalf("reopen exact FEAT-126 store: %v", err)
	}
	reconstructed, err := reopened.GetByThread(testThreadID)
	if err != nil {
		t.Fatal(err)
	}
	if reconstructed.Cwd != projectDirectory {
		t.Fatalf("reconstructed cwd = %q, want %q", reconstructed.Cwd, projectDirectory)
	}
	if _, err := reopened.UpdateTrace(testSessionID, TraceContext{TraceID: "restart-trace"}); err != nil {
		t.Fatal(err)
	}
	if err := reopened.Close(); err != nil {
		t.Fatal(err)
	}
	assertFileExcludesBytes(t, databasePath, []byte(projectDirectory))
	beforeDefaultOpen, err := os.ReadFile(databasePath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := OpenStore(hostHome); err == nil {
		t.Fatal("default store mode accepted a FEAT-126 encoded database")
	}
	afterDefaultOpen, err := os.ReadFile(databasePath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(beforeDefaultOpen, afterDefaultOpen) {
		t.Fatal("default reader modified a FEAT-126 encoded database before rejecting it")
	}
}

func TestFEAT126StoreMigratesSchemaV3ToV4(t *testing.T) {
	const runID = "123e4567-e89b-42d3-a456-426614174000"
	_, hostHome, projectDirectory := newFEAT126StoreAuthority(t, runID)
	option := WithFEAT126Authority(projectDirectory, runID)
	store, err := OpenStore(hostHome, option)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	database, err := bolt.Open(filepath.Join(hostHome, "sessions.db"), 0o600, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := database.Update(func(tx *bolt.Tx) error {
		if err := tx.DeleteBucket(turnOperationsBucket); err != nil {
			return err
		}
		return tx.Bucket(metadataBucket).Put(storeSchemaVersionKey, []byte("3"))
	}); err != nil {
		_ = database.Close()
		t.Fatal(err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := OpenStore(hostHome, option)
	if err != nil {
		t.Fatalf("migrate FEAT-126 schema v3: %v", err)
	}
	defer reopened.Close()
	if err := reopened.db.View(func(tx *bolt.Tx) error {
		if got := string(tx.Bucket(metadataBucket).Get(storeSchemaVersionKey)); got != storeSchemaVersion {
			t.Fatalf("migrated FEAT-126 schema version = %q", got)
		}
		if tx.Bucket(turnOperationsBucket) == nil {
			t.Fatal("migrated FEAT-126 store omitted turn operation bucket")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestFEAT126StoreAuthorityRequiresCanonicalRFC4122UUIDv4BeforeDatabaseCreation(t *testing.T) {
	invalidRunIDs := []struct {
		name  string
		value string
	}{
		{name: "malformed", value: "not-a-uuid"},
		{name: "uppercase", value: "123E4567-E89B-42D3-A456-426614174003"},
		{name: "uuid-v1", value: "6ba7b810-9dad-11d1-80b4-00c04fd430c8"},
		{name: "uuid-v7", value: "019fbd88-cbc3-7bf1-934d-7b05cd693f80"},
		{name: "non-rfc4122-variant", value: "123e4567-e89b-42d3-4456-426614174003"},
		{name: "surrounding-whitespace", value: " 123e4567-e89b-42d3-a456-426614174003 "},
	}
	for _, test := range invalidRunIDs {
		t.Run(test.name, func(t *testing.T) {
			_, hostHome, projectDirectory := newFEAT126StoreAuthority(t, test.value)
			databasePath := filepath.Join(hostHome, "sessions.db")
			store, err := OpenStore(hostHome, WithFEAT126Authority(projectDirectory, test.value))
			if store != nil {
				_ = store.Close()
			}
			if err == nil {
				t.Fatalf("accepted non-UUIDv4 FEAT-126 run id %q", test.value)
			}
			if _, statErr := os.Lstat(databasePath); !errors.Is(statErr, os.ErrNotExist) {
				t.Fatalf("invalid run authority reached database creation: %v", statErr)
			}
		})
	}
}

func TestFEAT126StoreDirectoryAuthorityMatrixRejectsBeforeWrite(t *testing.T) {
	const runID = "123e4567-e89b-42d3-a456-426614174000"
	roles := []string{"run-root", "host-home", "project"}
	conditions := []string{"missing", "symlink", "wrong-owner", "0755", "noncanonical"}

	for _, role := range roles {
		for _, condition := range conditions {
			t.Run(role+"/"+condition, func(t *testing.T) {
				runRoot, hostHome, projectDirectory := newFEAT126StoreAuthority(t, runID)
				tempRoot := filepath.Dir(runRoot)
				path := map[string]string{
					"run-root":  runRoot,
					"host-home": hostHome,
					"project":   projectDirectory,
				}[role]

				directValidation := false
				switch condition {
				case "missing":
					if err := os.Rename(path, path+".missing"); err != nil {
						t.Fatal(err)
					}
				case "symlink":
					target := path + ".target"
					if err := os.Rename(path, target); err != nil {
						t.Fatal(err)
					}
					if err := os.Symlink(target, path); err != nil {
						t.Fatal(err)
					}
				case "wrong-owner":
					directValidation = true
					if _, err := validateExactOwnerDirectoryForUID(
						path,
						"FEAT-126 "+role,
						uint32(os.Geteuid()+1),
					); err == nil {
						t.Fatal("accepted a directory owned by a different authority")
					}
				case "0755":
					if err := os.Chmod(path, 0o755); err != nil {
						t.Fatal(err)
					}
				case "noncanonical":
					switch role {
					case "host-home":
						hostHome += string(os.PathSeparator) + "."
					default:
						projectDirectory = path + string(os.PathSeparator) + "." +
							string(os.PathSeparator) + map[string]string{
							"run-root": "project",
							"project":  "",
						}[role]
					}
				}

				if !directValidation {
					store, err := OpenStore(
						hostHome,
						WithFEAT126Authority(projectDirectory, runID),
					)
					if store != nil {
						_ = store.Close()
					}
					if err == nil {
						t.Fatalf("accepted invalid %s %s authority", role, condition)
					}
				}

				if err := filepath.Walk(tempRoot, func(path string, info os.FileInfo, err error) error {
					if err != nil {
						return err
					}
					if info.Mode().IsRegular() && (info.Name() == "sessions.db" || info.Name() == "cleanup-receipt.key") {
						t.Fatalf("invalid %s %s authority reached persistent write", role, condition)
					}
					return nil
				}); err != nil {
					t.Fatal(err)
				}
			})
		}
	}
}

func TestFEAT126StoreRejectsCwdOutsideExactRunAuthority(t *testing.T) {
	const runID = "123e4567-e89b-42d3-a456-426614174000"
	runRoot, hostHome, projectDirectory := newFEAT126StoreAuthority(t, runID)
	otherDirectory := filepath.Join(runRoot, "other")
	if err := os.Mkdir(otherDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	store, err := OpenStore(hostHome, WithFEAT126Authority(projectDirectory, runID))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Reserve(Record{
		TaskID: testTaskID, AgentSessionID: testSessionID, Cwd: otherDirectory,
	}); err == nil {
		t.Fatal("FEAT-126 store accepted a cwd outside the exact project authority")
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	const foreignRunID = "123e4567-e89b-42d3-a456-426614174002"
	_, _, foreignProject := newFEAT126StoreAuthority(t, foreignRunID)
	if _, err := OpenStore(hostHome, WithFEAT126Authority(foreignProject, foreignRunID)); err == nil {
		t.Fatal("FEAT-126 store accepted a project outside the Host run root")
	}
}

func TestFEAT126StoreDoesNotAdoptDefaultPersistedCwd(t *testing.T) {
	const runID = "123e4567-e89b-42d3-a456-426614174000"
	_, hostHome, projectDirectory := newFEAT126StoreAuthority(t, runID)
	store, err := OpenStore(hostHome)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Reserve(Record{
		TaskID: testTaskID, AgentSessionID: testSessionID, Cwd: projectDirectory,
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenStore(hostHome, WithFEAT126Authority(projectDirectory, runID)); err == nil {
		t.Fatal("FEAT-126 encoding adopted a store that already contains an absolute cwd")
	}
}

func TestFEAT126StoreRejectsCopiedDatabaseBoundToAnotherRun(t *testing.T) {
	const (
		firstRunID  = "123e4567-e89b-42d3-a456-426614174000"
		secondRunID = "123e4567-e89b-42d3-a456-426614174002"
	)
	_, firstHostHome, firstProject := newFEAT126StoreAuthority(t, firstRunID)
	first, err := OpenStore(firstHostHome, WithFEAT126Authority(firstProject, firstRunID))
	if err != nil {
		t.Fatal(err)
	}
	if err := first.Reserve(Record{TaskID: testTaskID, AgentSessionID: testSessionID, Cwd: firstProject}); err != nil {
		t.Fatal(err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}

	_, secondHostHome, secondProject := newFEAT126StoreAuthority(t, secondRunID)
	source, err := os.ReadFile(filepath.Join(firstHostHome, "sessions.db"))
	if err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(secondHostHome, "sessions.db")
	if err := os.WriteFile(destination, source, 0o600); err != nil {
		t.Fatal(err)
	}
	assertRejectedOpenPreservesBytes(t, destination, func() error {
		store, openErr := OpenStore(secondHostHome, WithFEAT126Authority(secondProject, secondRunID))
		if store != nil {
			_ = store.Close()
		}
		return openErr
	})
}

func TestFEAT126StoreRejectsEveryPreExistingUnmarkedDatabaseWithoutMutation(t *testing.T) {
	const runID = "123e4567-e89b-42d3-a456-426614174000"
	tests := []struct {
		name    string
		prepare func(*testing.T, string, string)
	}{
		{
			name: "zero byte",
			prepare: func(t *testing.T, hostHome, _ string) {
				if err := os.WriteFile(filepath.Join(hostHome, "sessions.db"), nil, 0o600); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "default empty",
			prepare: func(t *testing.T, hostHome, _ string) {
				store, err := OpenStore(hostHome)
				if err != nil {
					t.Fatal(err)
				}
				if err := store.Close(); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "default delete to empty",
			prepare: func(t *testing.T, hostHome, projectDirectory string) {
				store, err := OpenStore(hostHome)
				if err != nil {
					t.Fatal(err)
				}
				if err := store.Reserve(Record{TaskID: testTaskID, AgentSessionID: testSessionID, Cwd: projectDirectory}); err != nil {
					t.Fatal(err)
				}
				if err := store.db.Update(func(tx *bolt.Tx) error {
					if err := tx.Bucket(tasksBucket).Delete([]byte(testTaskID)); err != nil {
						return err
					}
					return tx.Bucket(sessionsBucket).Delete([]byte(testSessionID))
				}); err != nil {
					t.Fatal(err)
				}
				if err := store.Close(); err != nil {
					t.Fatal(err)
				}
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, hostHome, projectDirectory := newFEAT126StoreAuthority(t, runID)
			test.prepare(t, hostHome, projectDirectory)
			databasePath := filepath.Join(hostHome, "sessions.db")
			assertRejectedOpenPreservesBytes(t, databasePath, func() error {
				store, err := OpenStore(hostHome, WithFEAT126Authority(projectDirectory, runID))
				if store != nil {
					_ = store.Close()
				}
				return err
			})
		})
	}
}

func TestFEAT126StoreRejectsAuthorityMetadataAndSentinelDriftWithoutMutation(t *testing.T) {
	const runID = "123e4567-e89b-42d3-a456-426614174000"
	tests := []struct {
		name   string
		mutate func(*bolt.Tx) error
	}{
		{name: "missing marker", mutate: func(tx *bolt.Tx) error {
			return tx.Bucket(metadataBucket).Delete(feat126CwdEncodingKey)
		}},
		{name: "wrong marker", mutate: func(tx *bolt.Tx) error {
			return tx.Bucket(metadataBucket).Put(feat126CwdEncodingKey, []byte("opaque-project-v2"))
		}},
		{name: "missing run id", mutate: func(tx *bolt.Tx) error {
			return tx.Bucket(metadataBucket).Delete(feat126RunIDKey)
		}},
		{name: "wrong run id", mutate: func(tx *bolt.Tx) error {
			return tx.Bucket(metadataBucket).Put(feat126RunIDKey, []byte("123e4567-e89b-42d3-a456-426614174002"))
		}},
		{name: "absolute row cwd", mutate: func(tx *bolt.Tx) error {
			bucket := tx.Bucket(sessionsBucket)
			encoded := bucket.Get([]byte(testSessionID))
			var record Record
			if err := json.Unmarshal(encoded, &record); err != nil {
				return err
			}
			record.Cwd = "/private/forbidden/project"
			encoded, err := json.Marshal(record)
			if err != nil {
				return err
			}
			return bucket.Put([]byte(testSessionID), encoded)
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, hostHome, projectDirectory := newFEAT126StoreAuthority(t, runID)
			store, err := OpenStore(hostHome, WithFEAT126Authority(projectDirectory, runID))
			if err != nil {
				t.Fatal(err)
			}
			if err := store.Reserve(Record{TaskID: testTaskID, AgentSessionID: testSessionID, Cwd: projectDirectory}); err != nil {
				t.Fatal(err)
			}
			if err := store.db.Update(test.mutate); err != nil {
				t.Fatal(err)
			}
			if err := store.Close(); err != nil {
				t.Fatal(err)
			}
			databasePath := filepath.Join(hostHome, "sessions.db")
			assertRejectedOpenPreservesBytes(t, databasePath, func() error {
				reopened, openErr := OpenStore(hostHome, WithFEAT126Authority(projectDirectory, runID))
				if reopened != nil {
					_ = reopened.Close()
				}
				return openErr
			})
		})
	}
}

func TestFEAT126StoreDoesNotRepairDatabasePermissions(t *testing.T) {
	const runID = "123e4567-e89b-42d3-a456-426614174000"
	_, hostHome, projectDirectory := newFEAT126StoreAuthority(t, runID)
	store, err := OpenStore(hostHome, WithFEAT126Authority(projectDirectory, runID))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	databasePath := filepath.Join(hostHome, "sessions.db")
	if err := os.Chmod(databasePath, 0o640); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenStore(hostHome, WithFEAT126Authority(projectDirectory, runID)); err == nil {
		t.Fatal("FEAT-126 store repaired and accepted non-canonical permissions")
	}
	info, err := os.Lstat(databasePath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o640 {
		t.Fatalf("FEAT-126 store permissions changed to %o", info.Mode().Perm())
	}
}

func assertRejectedOpenPreservesBytes(t *testing.T, path string, open func() error) {
	t.Helper()
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := open(); err == nil {
		t.Fatal("invalid FEAT-126 store was accepted")
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("rejected FEAT-126 store was modified")
	}
}

func newFEAT126StoreAuthority(t *testing.T, runID string) (string, string, string) {
	t.Helper()
	tempRoot, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	runRoot := filepath.Join(tempRoot, runID)
	hostHome := filepath.Join(runRoot, "host-home")
	projectDirectory := filepath.Join(runRoot, "project")
	for _, directory := range []string{runRoot, hostHome, projectDirectory} {
		if err := os.Mkdir(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	return runRoot, hostHome, projectDirectory
}

func assertFileExcludesBytes(t *testing.T, path string, forbidden []byte) {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(content, forbidden) {
		t.Fatalf("%s contains forbidden project bytes", filepath.Base(path))
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
	operation, err := store.BeginCleanup(operationID, testSessionID)
	if err != nil || operation.State != CleanupStateRuntimeDeletePending {
		t.Fatalf("begin cleanup: operation=%+v err=%v", operation, err)
	}
	if _, err := store.MarkCleanupRuntimeDeleted(operationID, testSessionID); err != nil {
		t.Fatalf("confirm runtime deletion: %v", err)
	}
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

func TestAcceptTurnOperationDoesNotReactivateCompletedShortTurn(t *testing.T) {
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
	digest := strings.Repeat("a", 64)
	if _, _, err := store.PrepareTurnOperation(testSessionID, testTurnOperationID, digest, TraceContext{}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CompleteTurn(testSessionID, testTurnID, "completed"); err != nil {
		t.Fatal(err)
	}
	record, err := store.AcceptTurnOperation(testSessionID, testTurnOperationID, digest, testTurnID)
	if err != nil {
		t.Fatal(err)
	}
	if record.State != StateIdle || record.ActiveTurnID != "" || record.LastTurnID != testTurnID {
		t.Fatalf("late accepted response reactivated a terminal turn: %+v", record)
	}
	operation, err := store.TurnOperation(testSessionID, testTurnOperationID)
	if err != nil || operation.State != TurnOperationStateAccepted || operation.TurnID != testTurnID {
		t.Fatalf("short turn acceptance was not persisted: operation=%+v err=%v", operation, err)
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
