package session

import (
	"encoding/json"

	wire "github.com/36Dge/yijie-agent-host/internal/contracts/markethost"
	rejection "github.com/36Dge/yijie-agent-host/internal/contracts/submissionrejection"
	bolt "go.etcd.io/bbolt"
)

var rejectionBaseVersionKey = []byte("submission_rejection_base_schema")

func storageEnvelopeVersion(tx *bolt.Tx) string {
	m := tx.Bucket(metadataBucket)
	if string(m.Get(storeSchemaVersionKey)) == "14" {
		return string(m.Get(rejectionBaseVersionKey))
	}
	return string(m.Get(storeSchemaVersionKey))
}
func protectRejectionStore(tx *bolt.Tx) error {
	m := tx.Bucket(metadataBucket)
	if string(m.Get(storeSchemaVersionKey)) == "14" {
		return nil
	}
	if err := m.Put(rejectionBaseVersionKey, m.Get(storeSchemaVersionKey)); err != nil {
		return err
	}
	return m.Put(storeSchemaVersionKey, []byte("14"))
}

var marketRejectionsBucket = []byte("market_submission_rejections_v1")

// This journal fences both operation identities before any native mutation.
// An HTTP error or absence from the accepted ledger is never such a proof.
type marketRejection struct {
	Version      int             `json:"version"`
	ScopeHash    string          `json:"scope_hash"`
	IntentDigest string          `json:"intent_digest"`
	Proof        rejection.Proof `json:"proof"`
}

func (s *Store) readMarketRejection(id string) (marketRejection, error) {
	var out marketRejection
	err := s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(marketRejectionsBucket)
		if b == nil || b.Get([]byte(id)) == nil {
			return ErrNotFound
		}
		if json.Unmarshal(b.Get([]byte(id)), &out) != nil || out.Version != 1 || out.Proof.Validate() != nil {
			return ErrMarketPurpose
		}
		return nil
	})
	return out, err
}

func (s *Service) rejectMarketBeforeDispatch(input wire.SubmitPayload, grant *marketGrant, reason string) error {
	if grant.expertSnapshot != nil {
		return marketFail(wire.ErrorCodeBusy)
	}
	v := grant.body
	if err := s.consumeMarketGrant(string(input.GrantRef), grant); err != nil {
		return err
	}
	defer s.finishMarketGrant(string(input.GrantRef))
	p := rejection.Proof{SchemaVersion: 1, HostInstanceId: rejection.CanonicalId(s.market.instance), TaskId: rejection.CanonicalId(v.TaskId), LocalSessionId: rejection.CanonicalId(v.LocalSessionId), SubmissionOperationId: rejection.CanonicalId(v.SubmissionOperationId), TurnOperationId: rejection.CanonicalId(v.Snapshot.TurnOperationId), SelectionDigest: rejection.SelectionDigest(v.Snapshot.SelectionDigest), Reason: reason, Outcome: "not_dispatched"}
	if p.Validate() != nil {
		return marketFail(wire.ErrorCodeInvalidRequest)
	}
	entry := marketRejection{Version: 1, ScopeHash: grant.scopeHash, IntentDigest: grant.digest, Proof: p}
	err := s.store.db.Update(func(tx *bolt.Tx) error {
		if !s.store.marketWriter {
			return ErrMarketPurpose
		}
		for _, id := range []rejection.CanonicalId{p.SubmissionOperationId, p.TurnOperationId} {
			for _, bucket := range [][]byte{marketOperationsBucket, expertOperationsBucket, marketRejectionsBucket, cleanupOperationsBucket} {
				if b := tx.Bucket(bucket); b != nil && b.Get([]byte(id)) != nil {
					return ErrTurnOperationConflict
				}
			}
			if v.AgentSessionId != nil {
				for _, bucket := range [][]byte{turnOperationsBucket, modelOperationsBucket} {
					if b := tx.Bucket(bucket); b != nil && b.Get(turnOperationKey(string(*v.AgentSessionId), string(id))) != nil {
						return ErrTurnOperationConflict
					}
				}
			}
		}
		b, err := tx.CreateBucketIfNotExists(marketRejectionsBucket)
		if err != nil {
			return err
		}
		raw, err := json.Marshal(entry)
		if err != nil {
			return err
		}
		if err = b.Put([]byte(p.SubmissionOperationId), raw); err != nil {
			return err
		}
		if err = b.Put([]byte(p.TurnOperationId), raw); err != nil {
			return err
		}
		return protectRejectionStore(tx)
	})
	if err != nil {
		return marketFail(wire.ErrorCodeRequestConflict)
	}
	if reason == "skill_unavailable" {
		return marketFail(wire.ErrorCodeInvalidRequest)
	}
	return marketFail(wire.ErrorCodeBusy)
}

func (s *Service) ReadMarketSubmissionRejection(task, operation string) (rejection.Proof, error) {
	if wire.CanonicalId(task).Validate() != nil || wire.CanonicalId(operation).Validate() != nil {
		return rejection.Proof{}, marketFail(wire.ErrorCodeInvalidRequest)
	}
	owner, err := s.marketReadableOwner()
	if err != nil {
		return rejection.Proof{}, err
	}
	entry, err := s.store.readMarketRejection(operation)
	if err != nil {
		return rejection.Proof{}, marketFail(wire.ErrorCodeNotFound)
	}
	if entry.ScopeHash != owner || string(entry.Proof.TaskId) != task || string(entry.Proof.TurnOperationId) != operation {
		return rejection.Proof{}, marketFail(wire.ErrorCodeAuthorityMismatch)
	}
	return entry.Proof, nil
}
