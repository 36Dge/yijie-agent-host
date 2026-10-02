package session

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/36Dge/yijie-agent-host/internal/codex"
	wire "github.com/36Dge/yijie-agent-host/internal/contracts/chatmodels"
	bolt "go.etcd.io/bbolt"
)

var (
	ErrModelUnavailable   = errors.New("model unavailable")
	ErrModelRevision      = errors.New("model revision conflict")
	ErrModelUnknown       = errors.New("model selection unknown")
	modelOperationsBucket = []byte("model_operations_v1")
)

type modelRuntime interface {
	ChatModelsEnabled() bool
	ModelAvailable(string) bool
	SwitchThread(context.Context, string) (codex.ThreadInfo, error)
}

func WithChatModelStorage() StoreOption {
	return func(s *Store) error { s.modelWriter = true; s.draftWriter = true; return nil }
}
func (s *Store) prepareModelFormat(tx *bolt.Tx) error {
	if !s.modelWriter {
		return nil
	}
	if s.feat126ProjectDirectory != "" {
		return ErrModelUnavailable
	}
	v := string(tx.Bucket(metadataBucket).Get(storeSchemaVersionKey))
	if v != "6" && v != "7" {
		return ErrModelUnavailable
	}
	if _, e := tx.CreateBucketIfNotExists(modelOperationsBucket); e != nil {
		return e
	}
	return tx.Bucket(metadataBucket).Put(storeSchemaVersionKey, []byte("7"))
}
func (s *Service) ChatModelsEnabled() bool {
	r, ok := s.runtime.(modelRuntime)
	return ok && r.ChatModelsEnabled() && s.store.modelWriter
}
func (s *Service) ChatModelCatalog() wire.Catalog {
	list := make([]wire.ModelAvailability, 0, 2)
	runtime, ok := s.runtime.(modelRuntime)
	for _, p := range wire.Definitions() {
		available := ok && s.ChatModelsEnabled() && runtime.ModelAvailable(string(p.ProfileId))
		reason := wire.ModelAvailabilityReasonNotConfigured
		if available {
			reason = wire.ModelAvailabilityReasonReady
		}
		list = append(list, wire.ModelAvailability{Profile: p, Available: available, Reason: reason})
	}
	return wire.Catalog{SchemaVersion: 1, DefaultProfile: wire.ProfileIdKimiK3MaxV1, Models: list}
}
func recordProfile(r Record) string {
	if r.ModelProfile != "" {
		return r.ModelProfile
	}
	if r.Model == codex.MiniMaxModel && r.ModelProvider == codex.MiniMaxProviderID {
		return "minimax-m3-high-v1"
	}
	return ""
}
func modelSelection(r Record) wire.Selection {
	state := wire.SelectionStateReady
	if r.ModelSelectionState != "" {
		state = wire.SelectionState(r.ModelSelectionState)
	}
	id := recordProfile(r)
	if id == "" {
		state = wire.SelectionStateUnknown
	}
	out := wire.Selection{SchemaVersion: 1, AgentSessionId: r.AgentSessionID, Revision: r.ModelRevision, State: state}
	if id != "" {
		v := wire.ProfileId(id)
		out.ProfileId = &v
	}
	if r.ModelSelectionOperation != "" {
		out.OperationId = &r.ModelSelectionOperation
	}
	return out
}
func (s *Service) ReadChatModel(sessionID string) (wire.Selection, error) {
	if !s.ChatModelsEnabled() {
		return wire.Selection{}, ErrModelUnavailable
	}
	if !isCanonicalNonZeroUUID(sessionID) {
		return wire.Selection{}, ErrInvalidArgument
	}
	r, e := s.store.Get(sessionID)
	if e != nil {
		return wire.Selection{}, e
	}
	return modelSelection(r), nil
}
func (s *Service) checkModelRequest(ctx context.Context, r Record) error {
	id := codex.ModelProfileID(ctx)
	if id == "" {
		if r.ModelProfile != "" || r.ModelSelectionState != "" {
			return ErrModelUnavailable
		}
		return nil
	}
	if !s.ChatModelsEnabled() {
		return ErrModelUnavailable
	}
	rt := s.runtime.(modelRuntime)
	if !rt.ModelAvailable(id) {
		return ErrModelUnavailable
	}
	if r.AgentSessionID != "" && (recordProfile(r) != id || (r.ModelSelectionState != "" && r.ModelSelectionState != "ready")) {
		return ErrModelRevision
	}
	return nil
}

type modelOperation struct {
	Request  wire.SelectRequest `json:"request"`
	Result   wire.Selection     `json:"result"`
	Complete bool               `json:"complete"`
}

func (s *Service) SelectChatModel(ctx context.Context, sessionID string, request wire.SelectRequest) (wire.Selection, error) {
	if !s.ChatModelsEnabled() {
		return wire.Selection{}, ErrModelUnavailable
	}
	if wire.Validate("SelectRequest", request) != nil || !isCanonicalNonZeroUUID(request.OperationId) || !isCanonicalNonZeroUUID(sessionID) {
		return wire.Selection{}, ErrInvalidArgument
	}
	runtime := s.runtime.(modelRuntime)
	if !runtime.ModelAvailable(string(request.ProfileId)) {
		return wire.Selection{}, ErrModelUnavailable
	}
	s.modelOperationMu.Lock()
	defer s.modelOperationMu.Unlock()
	var record Record
	var operation modelOperation
	key := turnOperationKey(sessionID, request.OperationId)
	err := s.store.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(modelOperationsBucket)
		if b == nil {
			return ErrModelUnavailable
		}
		if data := b.Get(key); data != nil {
			if json.Unmarshal(data, &operation) != nil {
				return ErrModelUnknown
			}
			if operation.Request != request {
				return ErrTurnOperationConflict
			}
			if operation.Complete {
				return nil
			}
			var e error
			record, e = s.store.loadRecord(tx, sessionID)
			return e
		}
		var e error
		record, e = s.store.loadRecord(tx, sessionID)
		if e != nil {
			return e
		}
		if record.ModelRevision != request.ExpectedRevision {
			return ErrModelRevision
		}
		if record.ModelSelectionState != "" && record.ModelSelectionState != "ready" {
			return ErrModelUnknown
		}
		if record.ActiveTurnID != "" || record.State == StateActive || record.State == StateStarting || record.State == StateCleaning || record.CleanupOperationID != "" {
			return ErrTurnActive
		}
		if record.CodexThreadID == "" || recordProfile(record) == "" {
			return ErrModelUnknown
		}
		if e = s.requireNoRuntimeApprovalForThread(record.CodexThreadID); e != nil {
			return e
		}
		if e = tx.Bucket(turnOperationsBucket).ForEach(func(_, raw []byte) error {
			var op TurnOperation
			if json.Unmarshal(raw, &op) != nil {
				return ErrModelUnknown
			}
			if op.SessionID == sessionID && (op.State == TurnOperationStatePending || op.State == TurnOperationStateUncertain) {
				return ErrTurnActive
			}
			return nil
		}); e != nil {
			return e
		}
		operation.Request = request
		operation.Result = modelSelection(record)
		operation.Result.OperationId = &request.OperationId
		if recordProfile(record) == string(request.ProfileId) {
			operation.Complete = true
		} else {
			record.ModelSelectionState = "switching"
			record.ModelSelectionOperation = request.OperationId
			if e = s.store.saveRecord(tx, record); e != nil {
				return e
			}
		}
		raw, e := json.Marshal(operation)
		if e != nil {
			return e
		}
		return b.Put(key, raw)
	})
	if err != nil {
		return wire.Selection{}, err
	}
	if operation.Complete {
		return operation.Result, nil
	}
	ctx = codex.WithModelProfile(ctx, string(request.ProfileId))
	if record.Purpose == purposeDraft {
		if e := s.draftCwdMatches(record); e != nil {
			return wire.Selection{}, e
		}
		ctx = codex.WithScheduledDraft(ctx, record.DraftWorkspaceID)
	}
	thread, err := runtime.SwitchThread(ctx, record.CodexThreadID)
	p, _ := codex.ModelProfile(string(request.ProfileId))
	if err != nil || thread.ID != record.CodexThreadID || thread.Model != p.Model || thread.ModelProvider != p.Provider || thread.ReasoningEffort != p.Effort {
		_, _ = s.store.update(sessionID, func(r *Record) error { r.ModelSelectionState = "unknown"; return nil })
		return wire.Selection{}, ErrModelUnknown
	}
	err = s.store.db.Update(func(tx *bolt.Tx) error {
		r, e := s.store.loadRecord(tx, sessionID)
		if e != nil {
			return e
		}
		if r.ModelSelectionOperation != request.OperationId || r.ModelRevision != request.ExpectedRevision {
			return ErrModelUnknown
		}
		r.ModelProfile = string(request.ProfileId)
		r.Model = p.Model
		r.ModelProvider = p.Provider
		r.ModelRevision++
		r.ModelSelectionState = "ready"
		if e = s.store.saveRecord(tx, r); e != nil {
			return e
		}
		operation.Complete = true
		operation.Result = modelSelection(r)
		raw, e := json.Marshal(operation)
		if e != nil {
			return e
		}
		return tx.Bucket(modelOperationsBucket).Put(key, raw)
	})
	if err != nil {
		return wire.Selection{}, ErrModelUnknown
	}
	return operation.Result, nil
}
