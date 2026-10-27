package session

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"

	"github.com/36Dge/yijie-agent-host/internal/codex"
	"github.com/google/uuid"
)

type cleanupRuntime struct {
	fakeRuntime
	status                        string
	readStatus                    func()
	statusErr                     error
	turnID                        string
	reads, interruptions, deletes int
}

func (r *cleanupRuntime) ReadThreadStatus(_ context.Context, _ string) (string, error) {
	r.reads++
	if r.readStatus != nil {
		r.readStatus()
	}
	return r.status, r.statusErr
}

func (r *cleanupRuntime) ReadThread(_ context.Context, id string) (json.RawMessage, error) {
	return json.Marshal(map[string]any{"id": id, "turns": []map[string]any{{"id": r.turnID, "status": "inProgress"}}})
}

func (r *cleanupRuntime) InterruptTurn(ctx context.Context, thread, turn string) error {
	r.interruptions++
	return r.fakeRuntime.InterruptTurn(ctx, thread, turn)
}

func (r *cleanupRuntime) DeleteThread(_ context.Context, _ string) error {
	r.deletes++
	return nil
}

func TestCleanupRecoveryStaleActiveAndStartingAfterNormalReopen(t *testing.T) {
	for _, state := range []string{StateActive, StateStarting} {
		for _, native := range []string{"idle", "notLoaded"} {
			t.Run(state+"/"+native, func(t *testing.T) {
				home := filepath.Join(t.TempDir(), "host-home")
				store, err := OpenStore(home)
				if err != nil {
					t.Fatal(err)
				}
				if err = store.Reserve(Record{TaskID: testTaskID, AgentSessionID: testSessionID, Cwd: t.TempDir()}); err != nil {
					t.Fatal(err)
				}
				if _, err = store.BindThread(testSessionID, testThreadID, "runtime", codex.MiniMaxModel, codex.MiniMaxProviderID); err != nil {
					t.Fatal(err)
				}
				if _, err = store.PrepareTurn(testSessionID, TraceContext{}); err != nil {
					t.Fatal(err)
				}
				if state == StateActive {
					if _, err = store.BindTurn(testSessionID, testTurnID); err != nil {
						t.Fatal(err)
					}
				}
				if err = store.Close(); err != nil {
					t.Fatal(err)
				}
				store, err = OpenStore(home)
				if err != nil {
					t.Fatal(err)
				}
				r := &cleanupRuntime{status: native}
				s := NewService(r, store, NewEventHub(8, 2), nil)
				operation := uuid.NewString()
				result, err := s.CleanupSession(context.Background(), testSessionID, operation)
				if err != nil || result.Outcome != "complete" || r.deletes != 1 || r.interruptions != 0 {
					t.Fatalf("cleanup: %+v %v runtime=%+v", result, err, r)
				}
				if err = store.Close(); err != nil {
					t.Fatal(err)
				}
				store, err = OpenStore(home)
				if err != nil {
					t.Fatal(err)
				}
				defer store.Close()
				if _, err = store.Get(testSessionID); !errors.Is(err, ErrNotFound) {
					t.Fatal("deleted mapping returned after reopen", err)
				}
				s = NewService(r, store, NewEventHub(8, 2), nil)
				result, err = s.CleanupSession(context.Background(), testSessionID, operation)
				if err != nil || result.Outcome != "complete" || r.deletes != 1 {
					t.Fatal("completed receipt was not idempotent", result, err)
				}
			})
		}
	}
}

func TestCleanupRecoveryInterruptRequiresCurrentNativeQuiescence(t *testing.T) {
	for _, stopped := range []bool{false, true} {
		t.Run(map[bool]string{false: "still_active", true: "stopped"}[stopped], func(t *testing.T) {
			store := openBoundFEAT134Store(t)
			if _, err := store.BindTurn(testSessionID, testTurnID); err != nil {
				t.Fatal(err)
			}
			r := &cleanupRuntime{status: "active", turnID: testTurnID}
			r.interrupt = func(thread, turn string) error {
				if thread != testThreadID || turn != testTurnID {
					t.Fatal("wrong native interrupt identity")
				}
				if stopped {
					r.status = "idle"
				}
				return nil
			}
			s := NewService(r, store, NewEventHub(8, 2), nil)
			result, err := s.CleanupSession(context.Background(), testSessionID, uuid.NewString())
			if err != nil || r.interruptions != 1 || r.reads != 2 {
				t.Fatal(result, err, r)
			}
			if stopped {
				if result.Outcome != "complete" || r.deletes != 1 {
					t.Fatal(result, r)
				}
			} else {
				if result.Outcome != "incomplete" || result.ReasonCode != "active_turn" || r.deletes != 0 {
					t.Fatal(result, r)
				}
				if _, err = store.Get(testSessionID); err != nil {
					t.Fatal("mapping removed before stop confirmation")
				}
			}
		})
	}
}

func TestCleanupRecoveryUnavailableObservationKeepsHistory(t *testing.T) {
	for _, status := range []string{"systemError", "", "futureStatus"} {
		t.Run(status, func(t *testing.T) {
			store := openBoundFEAT134Store(t)
			before, _ := store.Get(testSessionID)
			r := &cleanupRuntime{status: status}
			s := NewService(r, store, NewEventHub(8, 2), nil)
			result, err := s.CleanupSession(context.Background(), testSessionID, uuid.NewString())
			after, _ := store.Get(testSessionID)
			if err != nil || result.Outcome != "incomplete" || r.deletes != 0 || before != after {
				t.Fatal(result, err, "history changed")
			}
		})
	}
}

func TestCleanupRecoveryObservationCannotCrossNewSubmission(t *testing.T) {
	store := openBoundFEAT134Store(t)
	r := &cleanupRuntime{status: "idle"}
	r.readStatus = func() {
		if _, err := store.PrepareTurn(testSessionID, TraceContext{}); err != nil {
			t.Fatal(err)
		}
	}
	s := NewService(r, store, NewEventHub(8, 2), nil)
	result, err := s.CleanupSession(context.Background(), testSessionID, uuid.NewString())
	if err != nil || result.Outcome != "incomplete" || r.deletes != 0 {
		t.Fatal("stale observation granted cleanup", result, err)
	}
}

func TestCleanupRecoverySubmissionLockDefersWithoutRuntimeEffects(t *testing.T) {
	r := &cleanupRuntime{status: "idle"}
	s := NewService(r, openBoundFEAT134Store(t), NewEventHub(8, 2), nil)
	s.modelOperationMu.Lock()
	result, err := s.CleanupSession(context.Background(), testSessionID, uuid.NewString())
	s.modelOperationMu.Unlock()
	if err != nil || result.Outcome != "incomplete" || r.reads != 0 || r.deletes != 0 {
		t.Fatal(result, err)
	}
}

func TestCleanupRecoveryExactMissingThreadAndReadFailure(t *testing.T) {
	for _, missing := range []bool{true, false} {
		t.Run(map[bool]string{true: "exact_missing", false: "read_unavailable"}[missing], func(t *testing.T) {
			store := openBoundFEAT134Store(t)
			if _, err := store.BindTurn(testSessionID, testTurnID); err != nil {
				t.Fatal(err)
			}
			r := &cleanupRuntime{statusErr: errors.New("ordinary read unavailable")}
			if missing {
				r.statusErr = &codex.RPCError{Code: -32600, Message: "thread not found: " + testThreadID}
			}
			s := NewService(r, store, NewEventHub(8, 2), nil)
			result, err := s.CleanupSession(context.Background(), testSessionID, uuid.NewString())
			if err != nil {
				t.Fatal(err)
			}
			if missing {
				if result.Outcome != "complete" || r.deletes != 1 {
					t.Fatal(result, r)
				}
			} else if result.Outcome != "incomplete" || r.deletes != 0 {
				t.Fatal(result, r)
			}
		})
	}
}
