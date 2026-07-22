package session

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

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
