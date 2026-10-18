package session

import (
	"encoding/json"
	"sort"

	expert "github.com/36Dge/yijie-agent-host/internal/contracts/experts"
	market "github.com/36Dge/yijie-agent-host/internal/contracts/markethost"
	bolt "go.etcd.io/bbolt"
)

var expertResourceOperationsBucket = []byte("expert_resource_operations_v1")

type expertResourceOperation struct {
	Version        int                      `json:"version"`
	Sequence       uint64                   `json:"sequence"`
	ScopeHash      string                   `json:"scope_hash"`
	IntentDigest   string                   `json:"intent_digest"`
	InstallationID string                   `json:"installation_id"`
	Reference      expert.ExpertRef         `json:"reference"`
	Receipt        expert.ResourceOperation `json:"receipt"`
}

func validResourceOperation(op expertResourceOperation) bool {
	return op.Version == 1 && op.Sequence > 0 && op.Receipt.Validate() == nil && op.Reference.Validate() == nil && op.Reference.ExpertId == op.Receipt.Expert.ExpertId && op.Reference.Version == op.Receipt.Expert.Version
}
func (s *Store) readExpertResourceOperation(id string) (expertResourceOperation, error) {
	var out expertResourceOperation
	e := s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(expertResourceOperationsBucket)
		if b == nil || b.Get([]byte(id)) == nil {
			return ErrNotFound
		}
		if json.Unmarshal(b.Get([]byte(id)), &out) != nil || !validResourceOperation(out) {
			return ErrExpertContext
		}
		return nil
	})
	return out, e
}
func (s *Store) beginExpertResourceOperation(op *expertResourceOperation) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(expertResourceOperationsBucket)
		if !s.expertWriter || b == nil {
			return ErrExpertContext
		}
		for _, name := range [][]byte{expertResourceOperationsBucket, expertOperationsBucket, marketOperationsBucket, cleanupOperationsBucket} {
			if bucket := tx.Bucket(name); bucket == nil || bucket.Get([]byte(op.Receipt.OperationId)) != nil {
				return ErrTurnOperationConflict
			}
		}
		if e := pendingExpertResource(tx, string(op.Reference.ExpertId)); e != nil {
			return e
		}
		var e error
		op.Sequence, e = b.NextSequence()
		if e != nil {
			return e
		}
		if !validResourceOperation(*op) {
			return ErrExpertContext
		}
		raw, e := json.Marshal(op)
		if e != nil {
			return e
		}
		return b.Put([]byte(op.Receipt.OperationId), raw)
	})
}
func (s *Store) finishExpertResourceOperation(op expertResourceOperation) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(expertResourceOperationsBucket)
		if b == nil || !validResourceOperation(op) {
			return ErrExpertContext
		}
		var old expertResourceOperation
		if json.Unmarshal(b.Get([]byte(op.Receipt.OperationId)), &old) != nil || old.ScopeHash != op.ScopeHash || old.IntentDigest != op.IntentDigest || old.Reference != op.Reference || old.Sequence != op.Sequence {
			return ErrTurnOperationConflict
		}
		raw, e := json.Marshal(op)
		if e != nil {
			return e
		}
		return b.Put([]byte(op.Receipt.OperationId), raw)
	})
}
func pendingExpertResource(tx *bolt.Tx, expertID string) error {
	b := tx.Bucket(expertResourceOperationsBucket)
	if b == nil {
		return nil
	}
	return b.ForEach(func(_, raw []byte) error {
		var op expertResourceOperation
		if json.Unmarshal(raw, &op) != nil || !validResourceOperation(op) {
			return ErrExpertContext
		}
		if string(op.Reference.ExpertId) == expertID && (op.Receipt.State == expert.OperationStatePending || op.Receipt.State == expert.OperationStateUnknown) {
			return ErrTurnOperationPending
		}
		return nil
	})
}
func (s *Store) noPendingExpertResource(expertID string) error {
	return s.db.View(func(tx *bolt.Tx) error { return pendingExpertResource(tx, expertID) })
}
func (s *Store) latestExpertResourceOperations(scopeHash string) ([]expertResourceOperation, error) {
	latest := map[string]expertResourceOperation{}
	e := s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(expertResourceOperationsBucket)
		if b == nil {
			return ErrExpertContext
		}
		return b.ForEach(func(_, raw []byte) error {
			var op expertResourceOperation
			if json.Unmarshal(raw, &op) != nil || !validResourceOperation(op) {
				return ErrExpertContext
			}
			if op.ScopeHash == scopeHash {
				key := string(op.Reference.ExpertId)
				if previous, exists := latest[key]; !exists || previous.Sequence < op.Sequence {
					latest[key] = op
				}
			}
			return nil
		})
	})
	if e != nil {
		return nil, e
	}
	if len(latest) > 64 {
		return nil, ErrExpertContext
	}
	out := make([]expertResourceOperation, 0, len(latest))
	for _, op := range latest {
		out = append(out, op)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Sequence < out[j].Sequence })
	return out, nil
}

// Unknown versions/digests fail closed. Only the reviewed catalog can establish
// that a retained exact reference lives in another installation directory.
func protectedExpertReference(ref *expert.ExpertRef, expertID string, coexisting []expert.ExpertRef) bool {
	if ref == nil || string(ref.ExpertId) != expertID {
		return false
	}
	for _, other := range coexisting {
		if *ref == other {
			return false
		}
	}
	return true
}

func (s *Store) expertResourceReferenced(expertID string, coexisting []expert.ExpertRef) (bool, error) {
	referenced := false
	e := s.db.View(func(tx *bolt.Tx) error {
		if e := pendingExpertResource(tx, expertID); e != nil {
			return e
		}
		if b := tx.Bucket(expertBindingsBucket); b != nil {
			if e := b.ForEach(func(_, raw []byte) error {
				var binding expertBinding
				if json.Unmarshal(raw, &binding) != nil {
					return ErrExpertContext
				}
				if protectedExpertReference(binding.Expert, expertID, coexisting) {
					referenced = true
				}
				return nil
			}); e != nil {
				return e
			}
		}
		if b := tx.Bucket(expertOperationsBucket); b != nil {
			if e := b.ForEach(func(_, raw []byte) error {
				var op expertOperation
				if json.Unmarshal(raw, &op) != nil {
					return ErrExpertContext
				}
				if protectedExpertReference(op.Receipt.Expert, expertID, coexisting) && (op.Receipt.State == expert.OperationStatePending || op.Receipt.State == expert.OperationStateUnknown) {
					referenced = true
				}
				return nil
			}); e != nil {
				return e
			}
		}
		if b := tx.Bucket(marketOperationsBucket); b != nil {
			if e := b.ForEach(func(_, raw []byte) error {
				var op marketOperation
				if json.Unmarshal(raw, &op) != nil {
					return ErrExpertContext
				}
				if op.ExpertSnapshot != nil && protectedExpertReference(op.ExpertSnapshot.Expert, expertID, coexisting) && op.Receipt.State != market.SubmissionStateAccepted {
					referenced = true
				}
				return nil
			}); e != nil {
				return e
			}
		}
		return nil
	})
	return referenced, e
}
