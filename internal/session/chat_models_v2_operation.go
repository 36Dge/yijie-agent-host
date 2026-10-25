package session

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/36Dge/yijie-agent-host/internal/codex"
	wire "github.com/36Dge/yijie-agent-host/internal/contracts/chatmodelsv2"
	bolt "go.etcd.io/bbolt"
)

func (s *Service) requireModelIdle(tx *bolt.Tx, record Record) error {
	if record.State != StateIdle || record.ActiveTurnID != "" || record.CleanupOperationID != "" {
		return ErrTurnActive
	}
	if record.CodexThreadID == "" || recordProfile(record) == "" {
		return ErrModelUnknown
	}
	if err := s.requireNoRuntimeApprovalForThread(record.CodexThreadID); err != nil {
		return err
	}
	s.titleMu.Lock()
	defer s.titleMu.Unlock()
	for key, op := range s.titleOperations {
		if key.sessionID == record.AgentSessionID && op.pending {
			return ErrTurnActive
		}
	}
	if err := tx.Bucket(turnOperationsBucket).ForEach(func(_, raw []byte) error {
		var op TurnOperation
		if json.Unmarshal(raw, &op) != nil {
			return ErrModelUnknown
		}
		if op.SessionID == record.AgentSessionID && (op.State == TurnOperationStatePending || op.State == TurnOperationStateUncertain) {
			return ErrTurnActive
		}
		return nil
	}); err != nil {
		return err
	}
	return noPendingExpertOrMarket(tx, record.AgentSessionID)
}

func (s *Service) validateModelRecovery(tx *bolt.Tx, op modelOperationV2, r Record) error {
	if r.AgentSessionID != op.SessionID || r.CodexThreadID != op.ThreadID || recordProfile(r) != op.PreviousProfile || r.ModelRevision != op.Request.ExpectedRevision || r.ModelSelectionOperation != op.Request.OperationId || (r.ModelSelectionState != "switching" && r.ModelSelectionState != "unknown") {
		return ErrModelUnknown
	}
	if err := noOtherModelOperation(tx, op.SessionID, op.Request.OperationId); err != nil {
		return err
	}
	return s.requireModelIdle(tx, r)
}

func (s *Service) SelectChatModelV2(ctx context.Context, sessionID string, request wire.SelectRequest) (wire.Selection, error) {
	return s.applyChatModelV2(ctx, sessionID, request, false)
}

func (s *Service) RecoverChatModelV2(ctx context.Context, sessionID string, request wire.RecoverRequest) (wire.ModelOperationReceipt, error) {
	if wire.Validate("RecoverRequest", request) != nil {
		return wire.ModelOperationReceipt{}, ErrInvalidArgument
	}
	_, err := s.applyChatModelV2(ctx, sessionID, wire.SelectRequest{SchemaVersion: request.SchemaVersion, OperationId: request.OperationId, ExpectedRevision: request.ExpectedRevision, ProfileId: request.ProfileId}, true)
	if err != nil {
		return wire.ModelOperationReceipt{}, err
	}
	return s.ReadChatModelOperationV2(sessionID, request.OperationId)
}

func (s *Service) applyChatModelV2(ctx context.Context, sessionID string, request wire.SelectRequest, recovery bool) (wire.Selection, error) {
	if !s.ChatModelsV2Enabled() || !s.store.modelV2Writer {
		return wire.Selection{}, ErrModelUnavailable
	}
	if wire.Validate("SelectRequest", request) != nil || !isCanonicalNonZeroUUID(sessionID) || !isCanonicalNonZeroUUID(request.OperationId) {
		return wire.Selection{}, ErrInvalidArgument
	}
	s.modelOperationMu.Lock()
	defer s.modelOperationMu.Unlock()
	var op modelOperationV2
	var record Record
	err := s.store.db.Update(func(tx *bolt.Tx) error {
		var err error
		op, err = loadModelOperationV2(tx, sessionID, request.OperationId)
		if err == nil {
			if op.Request != request {
				return ErrTurnOperationConflict
			}
			// Terminal evidence is immutable and independent of today's readiness.
			if op.Stage == "committed" {
				return nil
			}
			if !recovery {
				return ErrModelUnknown
			}
		} else if !errors.Is(err, ErrNotFound) {
			return err
		} else {
			if _, e := legacyModelOperation(tx, sessionID, request.OperationId); !errors.Is(e, ErrNotFound) {
				return ErrTurnOperationConflict
			}
			if recovery {
				return ErrNotFound
			}
		}
		record, err = s.store.loadRecord(tx, sessionID)
		if err != nil {
			return err
		}
		// Market/expert and draft execution remain fenced until their Native
		// versioned migration path is implemented; history remains readable.
		if record.Purpose != purposeOrdinary {
			return ErrModelUnavailable
		}
		if !s.runtime.(modelRuntime).ModelAvailable(string(request.ProfileId)) || !s.chatModelsV2Ready() {
			return ErrModelUnavailable
		}
		if recovery {
			return s.validateModelRecovery(tx, op, record)
		}
		if record.ModelRevision != request.ExpectedRevision || request.ExpectedRevision >= 9007199254740991 {
			return ErrModelRevision
		}
		if record.ModelSelectionState != "" && record.ModelSelectionState != "ready" {
			return ErrModelUnknown
		}
		if err = s.requireModelIdle(tx, record); err != nil {
			return err
		}
		if err = noOtherModelOperation(tx, sessionID, ""); err != nil {
			return err
		}
		if wire.Validate("ProfileId", recordProfile(record)) != nil {
			return ErrModelUnknown
		}
		if b := tx.Bucket(expertOperationsBucket); b != nil && b.Get([]byte(request.OperationId)) != nil {
			return ErrTurnOperationConflict
		}
		op = modelOperationV2{Version: 2, SessionID: sessionID, ThreadID: record.CodexThreadID, PreviousProfile: recordProfile(record), Request: request, Stage: "prepared"}
		record.ModelSelectionState, record.ModelSelectionOperation = "switching", request.OperationId
		if err = s.store.saveRecord(tx, record); err != nil {
			return err
		}
		return putModelOperationV2(tx, op)
	})
	if err != nil {
		return wire.Selection{}, err
	}
	if op.Stage == "committed" {
		return *op.Result, nil
	}
	// Expert resource checks are required on each explicit attempt, including
	// recovery; a previous attempt never grants a new configuration capability.
	ctx, err = s.contextForStoredExpert(ctx, sessionID)
	if err != nil {
		return wire.Selection{}, err
	}
	ctx = codex.WithModelProfile(ctx, string(request.ProfileId))
	err = s.store.db.Update(func(tx *bolt.Tx) error {
		r, e := s.store.loadRecord(tx, sessionID)
		if e != nil {
			return e
		}
		if e = s.validateModelRecovery(tx, op, r); e != nil {
			return e
		}
		op.Stage, op.Result, op.Receipt = "apply_dispatched", nil, nil
		return putModelOperationV2(tx, op)
	})
	if err != nil {
		return wire.Selection{}, err
	}
	thread, err := s.runtime.(modelRuntime).SwitchThread(ctx, op.ThreadID)
	profile, _ := codex.ModelProfile(string(request.ProfileId))
	if err != nil || thread.ID != op.ThreadID || thread.Model != profile.Model || thread.ModelProvider != profile.Provider || thread.ReasoningEffort != profile.Effort {
		_ = s.store.db.Update(func(tx *bolt.Tx) error {
			r, e := s.store.loadRecord(tx, sessionID)
			if e != nil {
				return e
			}
			if r.ModelSelectionOperation != request.OperationId || r.ModelRevision != request.ExpectedRevision {
				return ErrModelUnknown
			}
			r.ModelSelectionState = "unknown"
			if e = s.store.saveRecord(tx, r); e != nil {
				return e
			}
			op.Stage, op.Result, op.Receipt = "uncertain", nil, nil
			return putModelOperationV2(tx, op)
		})
		return wire.Selection{}, ErrModelUnknown
	}
	selected := wire.ProfileId(request.ProfileId)
	op.Stage = "receipt_validated"
	op.Receipt = &modelSwitchReceipt{ThreadID: thread.ID, Model: thread.Model, Provider: thread.ModelProvider, Effort: thread.ReasoningEffort}
	op.Result = &wire.Selection{SchemaVersion: 2, AgentSessionId: sessionID, State: wire.SelectionStateReady, Revision: request.ExpectedRevision + 1, ProfileId: &selected, OperationId: &request.OperationId, Availability: wire.SelectionAvailabilityReady}
	// Keep the validated receipt separate from the selection commit. A restart
	// in this window still requires explicit recovery, never thread/read inference.
	if err = s.store.db.Update(func(tx *bolt.Tx) error { return putModelOperationV2(tx, op) }); err != nil {
		return wire.Selection{}, ErrModelUnknown
	}
	err = s.store.db.Update(func(tx *bolt.Tx) error {
		r, e := s.store.loadRecord(tx, sessionID)
		if e != nil {
			return e
		}
		if e = s.validateModelRecovery(tx, op, r); e != nil {
			return e
		}
		r.ModelProfile, r.Model, r.ModelProvider = string(request.ProfileId), profile.Model, profile.Provider
		r.ModelRevision, r.ModelSelectionState = op.Result.Revision, "ready"
		if e = s.store.saveRecord(tx, r); e != nil {
			return e
		}
		op.Stage = "committed"
		return putModelOperationV2(tx, op)
	})
	if err != nil {
		return wire.Selection{}, ErrModelUnknown
	}
	return *op.Result, nil
}
