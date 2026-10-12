package session

import (
	"encoding/json"
	"errors"
	"reflect"
	"time"

	expert "github.com/36Dge/yijie-agent-host/internal/contracts/experts"
	wire "github.com/36Dge/yijie-agent-host/internal/contracts/markethost"
	bolt "go.etcd.io/bbolt"
)

type marketOperation struct {
	ExpertSnapshot *expert.TurnExpertSnapshot `json:"expert_snapshot,omitempty"`
	Version        int                        `json:"version"`
	ScopeHash      string                     `json:"scope_hash"`
	IntentDigest   string                     `json:"intent_digest"`
	Receipt        wire.SubmissionReceipt     `json:"receipt"`
}

func (s *Store) readMarketOperation(operation string) (marketOperation, error) {
	var out marketOperation
	err := s.db.View(func(tx *bolt.Tx) error {
		bucket := tx.Bucket(marketOperationsBucket)
		if bucket == nil {
			return ErrNotFound
		}
		raw := bucket.Get([]byte(operation))
		if raw == nil {
			return ErrNotFound
		}
		if json.Unmarshal(raw, &out) != nil || !validMarketOperation(out) {
			return ErrMarketPurpose
		}
		return nil
	})
	return out, err
}

func (s *Store) beginMarketOperation(record Record, operation marketOperation, adopt bool, expectedRevision int64) error {
	if !s.marketWriter {
		return ErrMarketPurpose
	}
	return s.db.Update(func(tx *bolt.Tx) error {
		bucket := tx.Bucket(marketOperationsBucket)
		if bucket == nil {
			return ErrMarketPurpose
		}
		for _, key := range []wire.CanonicalId{operation.Receipt.SubmissionOperationId, operation.Receipt.TurnOperationId} {
			if bucket.Get([]byte(key)) != nil || (tx.Bucket(expertOperationsBucket) != nil && tx.Bucket(expertOperationsBucket).Get([]byte(key)) != nil) {
				return ErrTurnOperationConflict
			}
			if tx.Bucket(turnOperationsBucket).Get(turnOperationKey(record.AgentSessionID, string(key))) != nil || tx.Bucket(modelOperationsBucket).Get(turnOperationKey(record.AgentSessionID, string(key))) != nil || tx.Bucket(cleanupOperationsBucket).Get([]byte(key)) != nil {
				return ErrTurnOperationConflict
			}
		}
		if err := bucket.ForEach(func(_, raw []byte) error {
			var old marketOperation
			if json.Unmarshal(raw, &old) != nil {
				return ErrMarketPurpose
			}
			if old.Receipt.AgentSessionId != nil && string(*old.Receipt.AgentSessionId) == record.AgentSessionID && old.Receipt.State != wire.SubmissionStateAccepted {
				return ErrTurnOperationPending
			}
			return nil
		}); err != nil {
			return err
		}
		old, err := s.loadRecord(tx, record.AgentSessionID)
		isNew := errors.Is(err, ErrNotFound)
		if errors.Is(err, ErrNotFound) {
			if adopt || expectedRevision != 0 || tx.Bucket(tasksBucket).Get([]byte(record.TaskID)) != nil {
				return ErrTaskExists
			}
			now := time.Now().UTC()
			record.State, record.CreatedAt, record.UpdatedAt = StateStarting, now, now
			if err := s.saveRecord(tx, record); err != nil {
				return err
			}
			if err := tx.Bucket(tasksBucket).Put([]byte(record.TaskID), []byte(record.AgentSessionID)); err != nil {
				return err
			}
		} else if err != nil {
			return err
		} else {
			if old.TaskID != record.TaskID || old.Cwd != record.Cwd || old.ModelRevision != expectedRevision || old.ActiveTurnID != "" || old.State != StateIdle || old.CleanupOperationID != "" {
				return ErrSessionNotUsable
			}
			if old.Purpose == purposeOrdinary && adopt {
				// Explicit trusted Native adoption is the sole purpose change.
				// All mapping/ownership checks and the new operation receipt share
				// this transaction; generic writers still reject purpose changes.
				old.Purpose, old.MarketScopeHash, old.MarketLocalSessionID = purposeMarket, record.MarketScopeHash, record.MarketLocalSessionID
				if validateMarketRecord(old, "8") != nil {
					return ErrMarketPurpose
				}
				encoded, e := json.Marshal(old)
				if e != nil {
					return e
				}
				if e = tx.Bucket(sessionsBucket).Put([]byte(old.AgentSessionID), encoded); e != nil {
					return e
				}
			} else if old.Purpose != purposeMarket || old.MarketScopeHash != record.MarketScopeHash || old.MarketLocalSessionID != record.MarketLocalSessionID {
				return ErrMarketPurpose
			}
		}
		if err := s.bindExpertSubmission(tx, record, operation, isNew); err != nil {
			return err
		}
		raw, err := json.Marshal(operation)
		if err != nil {
			return err
		}
		if err := bucket.Put([]byte(operation.Receipt.TurnOperationId), raw); err != nil {
			return err
		}
		if operation.Receipt.SubmissionOperationId != operation.Receipt.TurnOperationId {
			return bucket.Put([]byte(operation.Receipt.SubmissionOperationId), raw)
		}
		return nil
	})
}

func (s *Store) finishMarketOperation(operation marketOperation) error {
	if !validMarketOperation(operation) {
		return ErrExpertContext
	}
	return s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(marketOperationsBucket)
		if b == nil {
			return ErrMarketPurpose
		}
		var old marketOperation
		if json.Unmarshal(b.Get([]byte(operation.Receipt.TurnOperationId)), &old) != nil || old.IntentDigest != operation.IntentDigest || old.ScopeHash != operation.ScopeHash || old.Version != operation.Version || !reflect.DeepEqual(old.ExpertSnapshot, operation.ExpertSnapshot) {
			return ErrTurnOperationConflict
		}
		encoded, err := json.Marshal(operation)
		if err != nil {
			return err
		}
		if err := b.Put([]byte(operation.Receipt.TurnOperationId), encoded); err != nil {
			return err
		}
		if operation.Receipt.SubmissionOperationId != operation.Receipt.TurnOperationId {
			return b.Put([]byte(operation.Receipt.SubmissionOperationId), encoded)
		}
		return nil
	})
}

func (s *Service) marketReadableOwner() (string, error) {
	if s.market == nil {
		return "", marketFail(wire.ErrorCodeNotReady)
	}
	a := s.market
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closed || a.hello == nil {
		return "", marketFail(wire.ErrorCodeAuthorityMismatch)
	}
	return s.marketScopeHash(wire.ScopeBinding{OwnerUserId: a.hello.OwnerUserId, TenantId: a.hello.TenantId}), nil
}

func (s *Service) ReadMarketOperation(task, operation string) (wire.SubmissionReceipt, error) {
	if wire.CanonicalId(task).Validate() != nil || wire.CanonicalId(operation).Validate() != nil {
		return wire.SubmissionReceipt{}, marketFail(wire.ErrorCodeInvalidRequest)
	}
	owner, err := s.marketReadableOwner()
	if err != nil {
		return wire.SubmissionReceipt{}, err
	}
	entry, err := s.store.readMarketOperation(operation)
	if err != nil {
		return wire.SubmissionReceipt{}, marketFail(wire.ErrorCodeSessionNotFound)
	}
	if entry.ExpertSnapshot != nil || entry.ScopeHash != owner || string(entry.Receipt.TaskId) != task || string(entry.Receipt.TurnOperationId) != operation {
		return wire.SubmissionReceipt{}, marketFail(wire.ErrorCodeAuthorityMismatch)
	}
	return s.reconcileMarketReceipt(entry), nil
}

func (s *Store) marketNoPendingTurns(sessionID string) error {
	return s.marketNoPendingTurnsExcept(sessionID, "")
}
func (s *Store) marketNoPendingTurnsExcept(sessionID, recoveringOperation string) error {
	return s.db.View(func(tx *bolt.Tx) error {
		if e := noPendingExpertOrMarketExcept(tx, sessionID, recoveringOperation); e != nil {
			return e
		}
		return tx.Bucket(turnOperationsBucket).ForEach(func(_, raw []byte) error {
			var op TurnOperation
			if json.Unmarshal(raw, &op) != nil {
				return ErrSessionNotUsable
			}
			if op.SessionID == sessionID && op.State != TurnOperationStateAccepted {
				return ErrTurnOperationPending
			}
			return nil
		})
	})
}
