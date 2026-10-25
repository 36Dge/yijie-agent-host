package session

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"testing"

	"github.com/36Dge/yijie-agent-host/internal/codex"
	legacy "github.com/36Dge/yijie-agent-host/internal/contracts/chatmodels"
	wire "github.com/36Dge/yijie-agent-host/internal/contracts/chatmodelsv2"
	bolt "go.etcd.io/bbolt"
)

func modelV2Service(t *testing.T) (*Service, *modelV2TestRuntime, string) {
	t.Helper()
	home := t.TempDir()
	store, err := OpenStore(home, WithChatModelV2Storage())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	reserveBoundSession(t, store)
	rt := &modelV2TestRuntime{ready: true}
	s := NewService(rt, store, NewEventHub(16, 8), nil, WithNativeReasoning())
	_, err = store.update(testSessionID, func(r *Record) error {
		r.ModelProfile, r.Model, r.ModelProvider, r.ModelSelectionState = "kimi-k3-max-v1", "kimi-k3", "kimi", "ready"
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return s, rt, home
}
func modelV2Request() wire.SelectRequest {
	return wire.SelectRequest{SchemaVersion: 2, OperationId: testTurnOperationID, ProfileId: wire.ActiveProfileIdKimiK31ProMaxV1}
}
func modelV2Recover(r wire.SelectRequest) wire.RecoverRequest {
	return wire.RecoverRequest{SchemaVersion: r.SchemaVersion, OperationId: r.OperationId, ExpectedRevision: r.ExpectedRevision, ProfileId: r.ProfileId}
}

func TestFEAT164V2SelectionAndTerminalReceipt(t *testing.T) {
	s, rt, _ := modelV2Service(t)
	r := modelV2Request()
	out, err := s.SelectChatModelV2(context.Background(), testSessionID, r)
	if err != nil || out.Revision != 1 || rt.switches != 1 {
		t.Fatal(out, err, rt.switches)
	}
	r2 := r
	r2.OperationId = "00000000-0000-4000-8000-000000000165"
	r2.ExpectedRevision = 1
	r2.ProfileId = wire.ActiveProfileIdMinimaxM3HighV1
	if _, err = s.SelectChatModelV2(context.Background(), testSessionID, r2); err != nil {
		t.Fatal(err)
	}
	rt.paused, rt.ready = true, false
	writesBeforeReplay := s.store.db.Stats().TxStats.Write
	old, err := s.RecoverChatModelV2(context.Background(), testSessionID, modelV2Recover(r))
	if err != nil || old.State != wire.ModelOperationStateCommitted || old.Result.Revision != 1 || rt.switches != 2 {
		t.Fatal(old, err, rt.switches)
	}
	current, err := s.ReadChatModelV2(testSessionID)
	if err != nil || current.Revision != 2 || *current.ProfileId != wire.ProfileIdMinimaxM3HighV1 {
		t.Fatal(current, err)
	}
	if _, err = s.SelectChatModelV2(context.Background(), testSessionID, r); err != nil || rt.switches != 2 {
		t.Fatal(err, rt.switches)
	}
	if s.store.db.Stats().TxStats.Write != writesBeforeReplay {
		t.Fatal("terminal replay opened a write transaction")
	}
}

type incompleteModelReceiptRuntime struct{ modelV2TestRuntime }

func (r *incompleteModelReceiptRuntime) SwitchThread(context.Context, string) (codex.ThreadInfo, error) {
	r.switches++
	return codex.ThreadInfo{ID: testThreadID}, nil
}
func TestFEAT164V2IncompleteReceiptRemainsUnknownUntilExplicitRecovery(t *testing.T) {
	s, _, home := modelV2Service(t)
	rt := &incompleteModelReceiptRuntime{modelV2TestRuntime: modelV2TestRuntime{ready: true}}
	s.runtime = rt
	if _, err := s.SelectChatModelV2(context.Background(), testSessionID, modelV2Request()); !errors.Is(err, ErrModelUnknown) {
		t.Fatal(err)
	}
	value, err := s.ReadChatModelV2(testSessionID)
	if err != nil || value.State != wire.SelectionStateUnknown || value.Revision != 0 || *value.ProfileId != wire.ProfileIdKimiK3MaxV1 {
		t.Fatal(value, err)
	}
	receipt, err := s.ReadChatModelOperationV2(testSessionID, testTurnOperationID)
	if err != nil || receipt.State != wire.ModelOperationStateUnknown || receipt.Result != nil {
		t.Fatal(receipt, err)
	}
	if err = s.store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := OpenStore(home, WithChatModelV2Storage())
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	s.store = reopened
	complete := &modelV2TestRuntime{ready: true}
	s.runtime = complete
	if _, err = s.SelectChatModelV2(context.Background(), testSessionID, modelV2Request()); !errors.Is(err, ErrModelUnknown) || complete.switches != 0 {
		t.Fatal(err)
	}
	receipt, err = s.RecoverChatModelV2(context.Background(), testSessionID, modelV2Recover(modelV2Request()))
	if err != nil || receipt.State != wire.ModelOperationStateCommitted || complete.switches != 1 {
		t.Fatal(receipt, err)
	}
}

func seedV2Window(t *testing.T, s *Service, stage string) {
	t.Helper()
	err := s.store.db.Update(func(tx *bolt.Tx) error {
		r, err := s.store.loadRecord(tx, testSessionID)
		if err != nil {
			return err
		}
		r.ModelSelectionState, r.ModelSelectionOperation = "unknown", testTurnOperationID
		if err = s.store.saveRecord(tx, r); err != nil {
			return err
		}
		op := modelOperationV2{Version: 2, SessionID: testSessionID, ThreadID: r.CodexThreadID, PreviousProfile: recordProfile(r), Request: modelV2Request(), Stage: stage}
		if stage == "receipt_validated" {
			op.Receipt = &modelSwitchReceipt{ThreadID: op.ThreadID, Model: "kimi-k3.1-pro", Provider: "kimi", Effort: "max"}
			p := wire.ProfileIdKimiK31ProMaxV1
			id := testTurnOperationID
			op.Result = &wire.Selection{SchemaVersion: 2, AgentSessionId: testSessionID, State: wire.SelectionStateReady, Revision: 1, ProfileId: &p, OperationId: &id, Availability: wire.SelectionAvailabilityReady}
		}
		return putModelOperationV2(tx, op)
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestFEAT164V2RecoveryWindowsAndUnrelatedWork(t *testing.T) {
	for _, stage := range []string{"prepared", "apply_dispatched", "uncertain", "receipt_validated"} {
		t.Run(stage, func(t *testing.T) {
			s, rt, home := modelV2Service(t)
			seedV2Window(t, s, stage)
			if err := s.store.Close(); err != nil {
				t.Fatal(err)
			}
			reopened, err := OpenStore(home, WithChatModelV2Storage())
			if err != nil {
				t.Fatal(err)
			}
			defer reopened.Close()
			s.store = reopened
			if _, err = s.ReadChatModelOperationV2(testSessionID, testTurnOperationID); err != nil || rt.switches != 0 {
				t.Fatal(err)
			}
			if _, err = s.SelectChatModelV2(context.Background(), testSessionID, modelV2Request()); !errors.Is(err, ErrModelUnknown) || rt.switches != 0 {
				t.Fatal("POST silently reapplied", err)
			}
			out, err := s.RecoverChatModelV2(context.Background(), testSessionID, modelV2Recover(modelV2Request()))
			if err != nil || out.State != wire.ModelOperationStateCommitted || rt.switches != 1 {
				t.Fatal(out, err, rt.switches)
			}
		})
	}
	for _, block := range []string{"active", "thread", "profile", "revision", "operation", "pending_turn", "other_model", "pending_title", "paused"} {
		t.Run(block, func(t *testing.T) {
			s, rt, _ := modelV2Service(t)
			seedV2Window(t, s, "uncertain")
			err := s.store.db.Update(func(tx *bolt.Tx) error {
				r, e := s.store.loadRecord(tx, testSessionID)
				if e != nil {
					return e
				}
				switch block {
				case "active":
					r.State = StateActive
					r.ActiveTurnID = testTurnID
				case "thread":
					r.CodexThreadID = "other-thread"
				case "profile":
					r.ModelProfile = "minimax-m3-high-v1"
				case "revision":
					r.ModelRevision = 2
				case "operation":
					r.ModelSelectionOperation = "00000000-0000-4000-8000-000000000166"
				case "pending_turn":
					raw, _ := json.Marshal(TurnOperation{SessionID: testSessionID, OperationID: "pending", State: TurnOperationStateUncertain})
					return tx.Bucket(turnOperationsBucket).Put([]byte("pending"), raw)
				case "other_model":
					op, _ := loadModelOperationV2(tx, testSessionID, testTurnOperationID)
					op.Request.OperationId = "00000000-0000-4000-8000-000000000167"
					return putModelOperationV2(tx, op)
				case "pending_title":
					s.titleOperations[titleOperationKey{sessionID: testSessionID, operationID: testTurnOperationID}] = &titleOperation{pending: true}
				case "paused":
					rt.paused = true
				}
				return s.store.saveRecord(tx, r)
			})
			if err != nil {
				t.Fatal(err)
			}
			if _, err = s.RecoverChatModelV2(context.Background(), testSessionID, modelV2Recover(modelV2Request())); err == nil || rt.switches != 0 {
				t.Fatal("unrelated work/binding admitted", err, rt.switches)
			}
		})
	}
}

func TestFEAT164V1ReceiptPreservedAndIncompleteNeverReapplied(t *testing.T) {
	for _, complete := range []bool{false, true} {
		t.Run(map[bool]string{false: "pending", true: "terminal"}[complete], func(t *testing.T) {
			s, rt, _ := modelV2Service(t)
			p := legacy.ProfileIdKimiK3MaxV1
			id := testTurnOperationID
			op := modelOperation{Request: legacy.SelectRequest{SchemaVersion: 1, OperationId: id, ProfileId: p}, Result: legacy.Selection{SchemaVersion: 1, AgentSessionId: testSessionID, State: legacy.SelectionStateReady, ProfileId: &p, OperationId: &id, Revision: 1}, Complete: complete}
			raw, _ := json.Marshal(op)
			if err := s.store.db.Update(func(tx *bolt.Tx) error {
				return tx.Bucket(modelOperationsBucket).Put(turnOperationKey(testSessionID, id), raw)
			}); err != nil {
				t.Fatal(err)
			}
			out, err := s.ReadChatModelOperationV2(testSessionID, id)
			if err != nil {
				t.Fatal(err)
			}
			if complete {
				want, _ := json.Marshal(op.Result)
				got, _ := json.Marshal(out.Result)
				if string(want) != string(got) {
					t.Fatal("legacy result changed", string(got))
				}
			} else if out.Result != nil || out.State != wire.ModelOperationStateUnknown {
				t.Fatal(out)
			}
			_, err = s.SelectChatModel(context.Background(), testSessionID, op.Request)
			if complete && err != nil || !complete && !errors.Is(err, ErrModelUnknown) || rt.switches != 0 {
				t.Fatal(err, rt.switches)
			}
			if _, err = s.RecoverChatModelV2(context.Background(), testSessionID, modelV2Recover(modelV2Request())); !errors.Is(err, ErrTurnOperationConflict) {
				t.Fatal(err)
			}
			if err = s.store.db.View(func(tx *bolt.Tx) error {
				if string(tx.Bucket(modelOperationsBucket).Get(turnOperationKey(testSessionID, id))) != string(raw) {
					t.Fatal("old bytes changed")
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestFEAT164Store11RejectsOldReaderAndPreservesDomainBase(t *testing.T) {
	for _, base := range []int{7, 8, 9, 10} {
		t.Run(strconv.Itoa(base), func(t *testing.T) {
			home := t.TempDir()
			options := []StoreOption{WithChatModelStorage()}
			if base >= 8 {
				options = append(options, WithMarketStorage())
			}
			if base == 10 {
				options = append(options, WithExpertStorage())
			}
			old, err := OpenStore(home, options...)
			if err != nil {
				t.Fatal(err)
			}
			reserveBoundSession(t, old)
			if base == 9 {
				// Ordinary historical v9 fixture: the original v8 -> v9 expand
				// creates binding/operation buckets before v10 resource operations.
				if err = old.db.Update(func(tx *bolt.Tx) error {
					for _, name := range [][]byte{expertBindingsBucket, expertOperationsBucket} {
						if _, e := tx.CreateBucket(name); e != nil {
							return e
						}
					}
					return putDomainStoreVersion(tx, "9")
				}); err != nil {
					t.Fatal(err)
				}
				options = append(options, WithExpertStorageReader())
			}
			before, _ := old.Get(testSessionID)
			if err = old.Close(); err != nil {
				t.Fatal(err)
			}
			options = append(options, WithChatModelV2Storage())
			current, err := OpenStore(home, options...)
			if err != nil {
				t.Fatal(err)
			}
			after, _ := current.Get(testSessionID)
			if before != after {
				t.Fatal("mapping changed")
			}
			if err = current.Close(); err != nil {
				t.Fatal(err)
			}
			if incompatible, e := OpenStore(home, WithChatModelStorage(), WithExpertStorageReader()); e == nil {
				incompatible.Close()
				t.Fatal("old reader accepted schema11")
			}
			current, err = OpenStore(home, options...)
			if err != nil {
				t.Fatal(err)
			}
			defer current.Close()
			after, _ = current.Get(testSessionID)
			if before != after {
				t.Fatal("repeat changed mapping")
			}
		})
	}
}
