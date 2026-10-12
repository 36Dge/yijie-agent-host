package session

import (
	"encoding/json"
	"errors"
	"reflect"

	expert "github.com/36Dge/yijie-agent-host/internal/contracts/experts"
	market "github.com/36Dge/yijie-agent-host/internal/contracts/markethost"
	bolt "go.etcd.io/bbolt"
)

var expertBindingsBucket = []byte("expert_bindings_v1")
var expertOperationsBucket = []byte("expert_operations_v1")
var ErrExpertContext = errors.New("expert context requires versioned Native delivery")

// Opt-in candidate writer. Old Host binaries reject schema 10 rather than silently
// dropping expert context. This stores adapter receipts, not product selection.
func WithExpertStorage() StoreOption {
	return func(s *Store) error {
		s.expertReader, s.expertWriter, s.marketWriter, s.modelWriter, s.draftWriter = true, true, true, true, true
		return nil
	}
}

// WithExpertStorageReader preserves mappings and original receipts after the
// feature is disabled. It never creates/upgrades expert buckets or enables any
// expert writer; ordinary sessions retain the existing market writer semantics.
func WithExpertStorageReader() StoreOption {
	return func(s *Store) error { s.expertReader = true; return nil }
}
func (s *Store) prepareExpertFormat(tx *bolt.Tx) error {
	v := string(tx.Bucket(metadataBucket).Get(storeSchemaVersionKey))
	if v == "10" {
		if (!s.expertWriter && !s.expertReader) || tx.Bucket(expertBindingsBucket) == nil || tx.Bucket(expertOperationsBucket) == nil || tx.Bucket(expertResourceOperationsBucket) == nil {
			return ErrExpertContext
		}
		return nil
	}
	if v == "9" {
		if (!s.expertWriter && !s.expertReader) || tx.Bucket(expertBindingsBucket) == nil || tx.Bucket(expertOperationsBucket) == nil {
			return ErrExpertContext
		}
		if !s.expertWriter {
			return nil
		}
	} else {
		if !s.expertWriter {
			return nil
		}
		if v != "8" || s.feat126ProjectDirectory != "" {
			return ErrExpertContext
		}
		for _, b := range [][]byte{expertBindingsBucket, expertOperationsBucket} {
			if _, e := tx.CreateBucket(b); e != nil {
				return e
			}
		}
	}
	if _, e := tx.CreateBucket(expertResourceOperationsBucket); e != nil {
		return e
	}
	return tx.Bucket(metadataBucket).Put(storeSchemaVersionKey, []byte("10"))
}

type expertBinding struct {
	Version          int               `json:"version"`
	ScopeHash        string            `json:"scope_hash"`
	LocalSessionID   string            `json:"local_session_id"`
	AgentSessionID   string            `json:"agent_session_id"`
	Revision         expert.Revision   `json:"revision"`
	Expert           *expert.ExpertRef `json:"expert"`
	PendingOperation string            `json:"pending_operation,omitempty"`
}
type expertOperation struct {
	Version      int                 `json:"version"`
	ScopeHash    string              `json:"scope_hash"`
	IntentDigest string              `json:"intent_digest"`
	Receipt      expert.ApplyReceipt `json:"receipt"`
}

func readExpertBinding(tx *bolt.Tx, sessionID string) (expertBinding, error) {
	var out expertBinding
	b := tx.Bucket(expertBindingsBucket)
	if b == nil || b.Get([]byte(sessionID)) == nil {
		return out, ErrNotFound
	}
	if json.Unmarshal(b.Get([]byte(sessionID)), &out) != nil || out.Version != 1 || out.AgentSessionID != sessionID || out.Revision.Validate() != nil || (out.Expert != nil && out.Expert.Validate() != nil) {
		return out, ErrExpertContext
	}
	return out, nil
}
func putExpertBinding(tx *bolt.Tx, b expertBinding) error {
	if tx.Bucket(expertBindingsBucket) == nil {
		return ErrExpertContext
	}
	raw, e := json.Marshal(b)
	if e != nil {
		return e
	}
	return tx.Bucket(expertBindingsBucket).Put([]byte(b.AgentSessionID), raw)
}
func (s *Store) expertBinding(sessionID string) (expertBinding, error) {
	var out expertBinding
	e := s.db.View(func(tx *bolt.Tx) error { var e error; out, e = readExpertBinding(tx, sessionID); return e })
	return out, e
}

// Called inside the existing market-operation transaction. The accepted send
// freezes one expert identity atomically with its original market intent.
func (s *Store) bindExpertSubmission(tx *bolt.Tx, record Record, op marketOperation, isNew bool) error {
	binding, e := readExpertBinding(tx, record.AgentSessionID)
	if op.ExpertSnapshot == nil {
		if e == nil {
			return ErrExpertContext
		}
		if !errors.Is(e, ErrNotFound) {
			return e
		}
		return nil
	}
	if !s.expertWriter || op.ExpertSnapshot.Validate() != nil || op.ExpertSnapshot.TurnOperationId != op.Receipt.TurnOperationId {
		return ErrExpertContext
	}
	if e == nil {
		if binding.PendingOperation != "" || binding.ScopeHash != op.ScopeHash || binding.LocalSessionID != record.MarketLocalSessionID || binding.Revision != op.ExpertSnapshot.BindingRevision || !reflect.DeepEqual(binding.Expert, op.ExpertSnapshot.Expert) {
			return ErrExpertContext
		}
		return nil
	}
	if !errors.Is(e, ErrNotFound) {
		return e
	}
	if !isNew && (op.ExpertSnapshot.Expert != nil || op.ExpertSnapshot.BindingRevision != 0) {
		return ErrExpertContext
	}
	return putExpertBinding(tx, expertBinding{Version: 1, ScopeHash: op.ScopeHash, LocalSessionID: record.MarketLocalSessionID, AgentSessionID: record.AgentSessionID, Revision: op.ExpertSnapshot.BindingRevision, Expert: op.ExpertSnapshot.Expert})
}
func (s *Store) checkExpertSubmission(record Record, snapshot *expert.TurnExpertSnapshot, isNew bool) error {
	return s.db.View(func(tx *bolt.Tx) error {
		b, e := readExpertBinding(tx, record.AgentSessionID)
		if errors.Is(e, ErrNotFound) {
			if snapshot == nil || isNew || (snapshot.Expert == nil && snapshot.BindingRevision == 0) {
				return nil
			}
			return ErrExpertContext
		}
		if e != nil {
			return e
		}
		if snapshot == nil || b.PendingOperation != "" || b.ScopeHash != record.MarketScopeHash || b.LocalSessionID != record.MarketLocalSessionID || b.Revision != snapshot.BindingRevision || !reflect.DeepEqual(b.Expert, snapshot.Expert) {
			return ErrExpertContext
		}
		return nil
	})
}
func noPendingExpertOrMarket(tx *bolt.Tx, sessionID string) error {
	return noPendingExpertOrMarketExcept(tx, sessionID, "")
}
func noPendingExpertOrMarketExcept(tx *bolt.Tx, sessionID, recoveringOperation string) error {
	b, e := readExpertBinding(tx, sessionID)
	if e == nil && b.PendingOperation != "" && b.PendingOperation != recoveringOperation {
		return ErrTurnOperationPending
	}
	if e != nil && !errors.Is(e, ErrNotFound) {
		return e
	}
	if bucket := tx.Bucket(marketOperationsBucket); bucket != nil {
		return bucket.ForEach(func(_, raw []byte) error {
			var op marketOperation
			if json.Unmarshal(raw, &op) != nil {
				return ErrExpertContext
			}
			if op.Receipt.AgentSessionId != nil && string(*op.Receipt.AgentSessionId) == sessionID && op.Receipt.State != market.SubmissionStateAccepted {
				return ErrTurnOperationPending
			}
			return nil
		})
	}
	return nil
}

func (s *Store) validateExpertRecovery(record Record, operation expertOperation) error {
	return s.db.View(func(tx *bolt.Tx) error {
		binding, e := readExpertBinding(tx, record.AgentSessionID)
		if e != nil {
			return e
		}
		if binding.ScopeHash != operation.ScopeHash || binding.LocalSessionID != string(operation.Receipt.LocalSessionId) || binding.PendingOperation != string(operation.Receipt.OperationId) || binding.Revision != operation.Receipt.ExpectedBindingRevision {
			return ErrExpertContext
		}
		return nil
	})
}
func (s *Store) readExpertOperation(id string) (expertOperation, error) {
	var out expertOperation
	e := s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(expertOperationsBucket)
		if b == nil || b.Get([]byte(id)) == nil {
			return ErrNotFound
		}
		if json.Unmarshal(b.Get([]byte(id)), &out) != nil || out.Version != 1 || out.Receipt.Validate() != nil {
			return ErrExpertContext
		}
		return nil
	})
	return out, e
}
func (s *Store) beginExpertOperation(record Record, operation expertOperation) error {
	if operation.Receipt.Validate() != nil || operation.Receipt.State != expert.OperationStatePending {
		return ErrExpertContext
	}
	return s.db.Update(func(tx *bolt.Tx) error {
		if !s.expertWriter || tx.Bucket(expertOperationsBucket) == nil {
			return ErrExpertContext
		}
		for _, name := range [][]byte{marketOperationsBucket, cleanupOperationsBucket, turnOperationsBucket, modelOperationsBucket} {
			if tx.Bucket(name) == nil {
				return ErrExpertContext
			}
		}
		id := []byte(operation.Receipt.OperationId)
		for _, bucket := range [][]byte{expertOperationsBucket, marketOperationsBucket, cleanupOperationsBucket} {
			if tx.Bucket(bucket).Get(id) != nil {
				return ErrTurnOperationConflict
			}
		}
		for _, bucket := range [][]byte{turnOperationsBucket, modelOperationsBucket} {
			if tx.Bucket(bucket).Get(turnOperationKey(record.AgentSessionID, string(id))) != nil {
				return ErrTurnOperationConflict
			}
		}
		current, e := s.loadRecord(tx, record.AgentSessionID)
		if e != nil {
			return e
		}
		if current.CodexThreadID != record.CodexThreadID || current.ModelRevision != record.ModelRevision || current.State != StateIdle || current.ActiveTurnID != "" || current.CleanupOperationID != "" {
			return ErrSessionNotUsable
		}
		if e = noPendingExpertOrMarket(tx, record.AgentSessionID); e != nil {
			return e
		}
		if current.Purpose == purposeOrdinary {
			current.Purpose = purposeMarket
			current.MarketScopeHash = operation.ScopeHash
			current.MarketLocalSessionID = string(operation.Receipt.LocalSessionId)
			if validateMarketRecord(current, "9") != nil {
				return ErrExpertContext
			}
			raw, e := json.Marshal(current)
			if e != nil {
				return e
			}
			if e = tx.Bucket(sessionsBucket).Put([]byte(current.AgentSessionID), raw); e != nil {
				return e
			}
		}
		binding, e := readExpertBinding(tx, record.AgentSessionID)
		if errors.Is(e, ErrNotFound) {
			binding = expertBinding{Version: 1, ScopeHash: operation.ScopeHash, LocalSessionID: string(operation.Receipt.LocalSessionId), AgentSessionID: record.AgentSessionID}
		}
		if e != nil && !errors.Is(e, ErrNotFound) {
			return e
		}
		if binding.ScopeHash != operation.ScopeHash || binding.LocalSessionID != string(operation.Receipt.LocalSessionId) || binding.Revision != operation.Receipt.ExpectedBindingRevision {
			return ErrExpertContext
		}
		binding.PendingOperation = string(operation.Receipt.OperationId)
		if e = putExpertBinding(tx, binding); e != nil {
			return e
		}
		raw, e := json.Marshal(operation)
		if e != nil {
			return e
		}
		return tx.Bucket(expertOperationsBucket).Put(id, raw)
	})
}
func (s *Store) finishExpertOperation(operation expertOperation) error {
	if operation.Receipt.Validate() != nil {
		return ErrExpertContext
	}
	return s.db.Update(func(tx *bolt.Tx) error {
		bucket := tx.Bucket(expertOperationsBucket)
		if bucket == nil {
			return ErrExpertContext
		}
		var old expertOperation
		if json.Unmarshal(bucket.Get([]byte(operation.Receipt.OperationId)), &old) != nil || old.IntentDigest != operation.IntentDigest || old.ScopeHash != operation.ScopeHash || old.Receipt.AgentSessionId != operation.Receipt.AgentSessionId || old.Receipt.LocalSessionId != operation.Receipt.LocalSessionId || old.Receipt.NativeThreadId != operation.Receipt.NativeThreadId || old.Receipt.HostInstanceId != operation.Receipt.HostInstanceId || old.Receipt.ExpectedBindingRevision != operation.Receipt.ExpectedBindingRevision || !reflect.DeepEqual(old.Receipt.Expert, operation.Receipt.Expert) {
			return ErrTurnOperationConflict
		}
		b, e := readExpertBinding(tx, string(operation.Receipt.AgentSessionId))
		if e != nil {
			return e
		}
		if b.PendingOperation != string(operation.Receipt.OperationId) {
			return ErrTurnOperationConflict
		}
		if operation.Receipt.State == expert.OperationStateSucceeded {
			current, err := s.loadRecord(tx, string(operation.Receipt.AgentSessionId))
			if err != nil {
				return err
			}
			if current.CodexThreadID != string(operation.Receipt.NativeThreadId) || current.State != StateIdle || current.ActiveTurnID != "" || current.CleanupOperationID != "" {
				return ErrSessionNotUsable
			}
			b.Expert = operation.Receipt.Expert
			b.Revision = operation.Receipt.BindingRevision
			b.PendingOperation = ""
		} else if operation.Receipt.State == expert.OperationStateRejected {
			b.PendingOperation = ""
		}
		if e = putExpertBinding(tx, b); e != nil {
			return e
		}
		raw, e := json.Marshal(operation)
		if e != nil {
			return e
		}
		return bucket.Put([]byte(operation.Receipt.OperationId), raw)
	})
}
