package session

import (
	"encoding/json"
	"errors"

	"github.com/36Dge/yijie-agent-host/internal/codex"
	wire "github.com/36Dge/yijie-agent-host/internal/contracts/chatmodelsv2"
	bolt "go.etcd.io/bbolt"
)

var modelOperationsV2Bucket = []byte("model_operations_v2")
var modelBaseVersionKey = []byte("model_v2_base_schema")

// Schema 11 is an envelope over the existing domain schema. Old binaries reject
// it before reading or writing sessions; disabling execution does not downgrade it.
func WithChatModelV2Storage() StoreOption {
	return func(s *Store) error { s.modelV2Writer, s.modelWriter, s.draftWriter = true, true, true; return nil }
}

func domainStoreVersion(tx *bolt.Tx) string {
	m := tx.Bucket(metadataBucket)
	if string(m.Get(storeSchemaVersionKey)) == "11" {
		return string(m.Get(modelBaseVersionKey))
	}
	return string(m.Get(storeSchemaVersionKey))
}

func putDomainStoreVersion(tx *bolt.Tx, version string) error {
	key := storeSchemaVersionKey
	if string(tx.Bucket(metadataBucket).Get(key)) == "11" {
		key = modelBaseVersionKey
	}
	return tx.Bucket(metadataBucket).Put(key, []byte(version))
}

func (s *Store) prepareModelV2Format(tx *bolt.Tx) error {
	if !s.modelV2Writer {
		return nil
	}
	v := domainStoreVersion(tx)
	if (v != "7" && v != "8" && v != "9" && v != "10") || s.feat126ProjectDirectory != "" {
		return ErrModelUnavailable
	}
	if _, err := tx.CreateBucketIfNotExists(modelOperationsV2Bucket); err != nil {
		return err
	}
	if err := tx.Bucket(metadataBucket).Put(modelBaseVersionKey, []byte(v)); err != nil {
		return err
	}
	return tx.Bucket(metadataBucket).Put(storeSchemaVersionKey, []byte("11"))
}

type modelOperationV2 struct {
	Version         int                 `json:"version"`
	SessionID       string              `json:"session_id"`
	ThreadID        string              `json:"thread_id"`
	PreviousProfile string              `json:"previous_profile"`
	Request         wire.SelectRequest  `json:"request"`
	Stage           string              `json:"stage"`
	Result          *wire.Selection     `json:"result,omitempty"`
	Receipt         *modelSwitchReceipt `json:"receipt,omitempty"`
}

type modelSwitchReceipt struct {
	ThreadID string `json:"thread_id"`
	Model    string `json:"model"`
	Provider string `json:"provider"`
	Effort   string `json:"effort"`
}

func loadModelOperationV2(tx *bolt.Tx, sessionID, operationID string) (modelOperationV2, error) {
	var op modelOperationV2
	b := tx.Bucket(modelOperationsV2Bucket)
	if b == nil || b.Get(turnOperationKey(sessionID, operationID)) == nil {
		return op, ErrNotFound
	}
	if json.Unmarshal(b.Get(turnOperationKey(sessionID, operationID)), &op) != nil || op.Version != 2 || op.SessionID != sessionID || op.Request.OperationId != operationID || op.ThreadID == "" || wire.Validate("SelectRequest", op.Request) != nil || op.Request.ExpectedRevision >= 9007199254740991 || wire.Validate("ProfileId", op.PreviousProfile) != nil {
		return op, ErrModelUnknown
	}
	switch op.Stage {
	case "prepared", "apply_dispatched", "uncertain":
		if op.Result != nil || op.Receipt != nil {
			return op, ErrModelUnknown
		}
	case "receipt_validated", "committed":
		p, ok := codex.ModelProfile(string(op.Request.ProfileId))
		if !ok || op.Receipt == nil || *op.Receipt != (modelSwitchReceipt{ThreadID: op.ThreadID, Model: p.Model, Provider: p.Provider, Effort: p.Effort}) {
			return op, ErrModelUnknown
		}
		if op.Result == nil || wire.Validate("Selection", op.Result) != nil || op.Result.AgentSessionId != sessionID || op.Result.OperationId == nil || *op.Result.OperationId != operationID || op.Result.ProfileId == nil || string(*op.Result.ProfileId) != string(op.Request.ProfileId) || op.Result.Revision != op.Request.ExpectedRevision+1 || op.Result.State != wire.SelectionStateReady {
			return op, ErrModelUnknown
		}
	default:
		return op, ErrModelUnknown
	}
	return op, nil
}

func putModelOperationV2(tx *bolt.Tx, op modelOperationV2) error {
	b := tx.Bucket(modelOperationsV2Bucket)
	if b == nil {
		return ErrModelUnavailable
	}
	raw, err := json.Marshal(op)
	if err != nil {
		return err
	}
	return b.Put(turnOperationKey(op.SessionID, op.Request.OperationId), raw)
}

func legacyModelOperation(tx *bolt.Tx, sessionID, operationID string) (modelOperation, error) {
	var op modelOperation
	b := tx.Bucket(modelOperationsBucket)
	if b == nil || b.Get(turnOperationKey(sessionID, operationID)) == nil {
		return op, ErrNotFound
	}
	if json.Unmarshal(b.Get(turnOperationKey(sessionID, operationID)), &op) != nil || op.Request.OperationId != operationID {
		return op, ErrModelUnknown
	}
	return op, nil
}

func noOtherModelOperation(tx *bolt.Tx, sessionID, ownID string) error {
	if b := tx.Bucket(modelOperationsV2Bucket); b != nil {
		if err := b.ForEach(func(_, raw []byte) error {
			var op modelOperationV2
			if json.Unmarshal(raw, &op) != nil {
				return ErrModelUnknown
			}
			if op.SessionID == sessionID && op.Request.OperationId != ownID && op.Stage != "committed" {
				return ErrModelUnknown
			}
			return nil
		}); err != nil {
			return err
		}
	}
	if b := tx.Bucket(modelOperationsBucket); b != nil {
		return b.ForEach(func(key, raw []byte) error {
			var op modelOperation
			if json.Unmarshal(raw, &op) != nil {
				return ErrModelUnknown
			}
			if string(key) == string(turnOperationKey(sessionID, op.Request.OperationId)) && op.Request.OperationId != ownID && !op.Complete {
				return ErrModelUnknown
			}
			return nil
		})
	}
	return nil
}

func (s *Service) ReadChatModelOperationV2(sessionID, operationID string) (wire.ModelOperationReceipt, error) {
	var out wire.ModelOperationReceipt
	if !s.ChatModelsV2Enabled() {
		return out, ErrModelUnavailable
	}
	if !isCanonicalNonZeroUUID(sessionID) || !isCanonicalNonZeroUUID(operationID) {
		return out, ErrInvalidArgument
	}
	err := s.store.db.View(func(tx *bolt.Tx) error {
		if _, err := s.store.loadRecord(tx, sessionID); err != nil {
			return err
		}
		op, err := loadModelOperationV2(tx, sessionID, operationID)
		out = wire.ModelOperationReceipt{SchemaVersion: 2, AgentSessionId: sessionID, OperationId: operationID}
		if errors.Is(err, ErrNotFound) {
			old, e := legacyModelOperation(tx, sessionID, operationID)
			if e != nil {
				return e
			}
			// Convert through the source-generated snapshot type without decorating
			// the old result with today's execution availability or schema version.
			raw, _ := json.Marshal(old.Request)
			if json.Unmarshal(raw, &out.Request) != nil {
				return ErrModelUnknown
			}
			out.State = wire.ModelOperationStateUnknown
			if old.Complete {
				raw, _ = json.Marshal(old.Result)
				out.Result = &wire.SelectionResultSnapshot{}
				if json.Unmarshal(raw, out.Result) != nil {
					return ErrModelUnknown
				}
				out.State = wire.ModelOperationStateCommitted
			}
			return nil
		}
		if err != nil {
			return err
		}
		out.Request = wire.OperationRequestSnapshot{SchemaVersion: 2, OperationId: operationID, ExpectedRevision: op.Request.ExpectedRevision, ProfileId: wire.ProfileId(op.Request.ProfileId)}
		out.State = wire.ModelOperationStateUnknown
		if op.Stage == "prepared" {
			out.State = wire.ModelOperationStatePrepared
		}
		if op.Stage == "apply_dispatched" {
			out.State = wire.ModelOperationStateApplying
		}
		if op.Stage == "committed" {
			out.State = wire.ModelOperationStateCommitted
			raw, _ := json.Marshal(op.Result)
			out.Result = &wire.SelectionResultSnapshot{}
			if json.Unmarshal(raw, out.Result) != nil {
				return ErrModelUnknown
			}
		}
		return nil
	})
	if err == nil && (wire.Validate("ModelOperationReceipt", out) != nil || (out.Result != nil && (out.Result.AgentSessionId != sessionID || out.Result.OperationId == nil || *out.Result.OperationId != operationID))) {
		err = ErrModelUnknown
	}
	return out, err
}
