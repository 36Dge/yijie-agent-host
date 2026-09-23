package session

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/google/uuid"
	bolt "go.etcd.io/bbolt"
)

func TestFEAT155RecoveryStoreReadOnlyReopenAndSessionAssociation(t *testing.T) {
	home := t.TempDir()
	store, err := OpenStore(home)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()
	operationID := uuid.NewString()
	type expected struct {
		record    Record
		operation TurnOperation
	}
	var records []expected
	for _, state := range []string{TurnOperationStatePending, TurnOperationStateAccepted, TurnOperationStateUncertain} {
		taskID, sessionID, threadID := uuid.NewString(), uuid.NewString(), uuid.NewString()
		if err := store.Reserve(Record{TaskID: taskID, AgentSessionID: sessionID, Cwd: home}); err != nil {
			t.Fatal(err)
		}
		reserved, err := store.GetByTask(taskID)
		if err != nil || reserved.CodexThreadID != "" {
			t.Fatalf("reserved mapping: %+v %v", reserved, err)
		}
		if _, err := store.BindThread(sessionID, threadID, "", "", ""); err != nil {
			t.Fatal(err)
		}
		digest := strings.Repeat("a", 64)
		if _, _, err := store.PrepareTurnOperation(sessionID, operationID, digest, TraceContext{TenantID: "synthetic-trace"}); err != nil {
			t.Fatal(err)
		}
		if state == TurnOperationStateAccepted {
			if _, err := store.AcceptTurnOperation(sessionID, operationID, digest, uuid.NewString()); err != nil {
				t.Fatal(err)
			}
		}
		if state == TurnOperationStateUncertain {
			// Ordinary lifecycle method on synthetic data; no transport fault is injected.
			if _, err := store.MarkTurnOperationUncertain(sessionID, operationID, digest, "runtime_request_failed"); err != nil {
				t.Fatal(err)
			}
		}
		record, err := store.GetByTask(taskID)
		if err != nil {
			t.Fatal(err)
		}
		operation, err := store.TurnOperation(sessionID, operationID)
		if err != nil {
			t.Fatal(err)
		}
		records = append(records, expected{record, operation})
	}
	check := func() {
		// No Runtime object is supplied: even an accidental Runtime invocation fails.
		service := NewService(nil, store, NewEventHub(8, 8), nil)
		txID := func() int {
			var id int
			if err := store.db.View(func(tx *bolt.Tx) error { id = tx.ID(); return nil }); err != nil {
				t.Fatal(err)
			}
			return id
		}
		before := txID()
		for repeat := 0; repeat < 3; repeat++ {
			for _, expected := range records {
				record, err := service.LookupSessionMapping(expected.record.TaskID)
				if err != nil || !reflect.DeepEqual(record, expected.record) {
					t.Fatalf("mapping changed: %v", err)
				}
				operation, err := service.LookupTurnOperation(record.AgentSessionID, operationID)
				if err != nil || !reflect.DeepEqual(operation, expected.operation) {
					t.Fatalf("operation changed or crossed sessions: %v", err)
				}
			}
		}
		if _, err := service.LookupTurnOperation(uuid.NewString(), operationID); !errors.Is(err, ErrNotFound) {
			t.Fatalf("unrelated session: %v", err)
		}
		if _, err := service.LookupSessionMapping(uuid.NewString()); !errors.Is(err, ErrNotFound) {
			t.Fatalf("unknown task: %v", err)
		}
		if _, err := service.LookupTurnOperation("ordinary-invalid-id", operationID); !errors.Is(err, ErrInvalidArgument) {
			t.Fatal(err)
		}
		if _, err := service.LookupSessionMapping(uuid.Nil.String()); !errors.Is(err, ErrInvalidArgument) {
			t.Fatal(err)
		}
		if before != txID() {
			t.Fatal("read queries committed a write transaction")
		}
	}
	check()
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = OpenStore(home)
	if err != nil {
		t.Fatal(err)
	}
	check()
	accepted := records[1]
	if _, err := store.CompleteTurn(accepted.record.AgentSessionID, accepted.operation.TurnID, "completed"); err != nil {
		t.Fatal(err)
	}
	cleanupID := uuid.NewString()
	if _, err := store.BeginCleanup(cleanupID, accepted.record.AgentSessionID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.MarkCleanupRuntimeDeleted(cleanupID, accepted.record.AgentSessionID); err != nil {
		t.Fatal(err)
	}
	if err := store.DeleteSessionWithReceipt(cleanupID, accepted.record.AgentSessionID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.GetByTask(accepted.record.TaskID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cleaned mapping: %v", err)
	}
	if _, err := store.TurnOperation(accepted.record.AgentSessionID, operationID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cleaned operation: %v", err)
	}
}
