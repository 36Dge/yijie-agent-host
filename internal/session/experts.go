package session

import (
	"context"
	"errors"

	"github.com/36Dge/yijie-agent-host/internal/codex"
	expert "github.com/36Dge/yijie-agent-host/internal/contracts/experts"
	market "github.com/36Dge/yijie-agent-host/internal/contracts/markethost"
	"github.com/36Dge/yijie-agent-host/internal/skills"
)

// Resources supplies only verified managed content; Native sends references.
type ExpertResources interface {
	ResolveExpert(expert.ExpertRef) (skills.ResolvedExpert, error)
}

func WithExpertResources(resources ExpertResources) ServiceOption {
	return func(s *Service) {
		if s.store.expertWriter {
			s.expertResources = resources
		}
	}
}
func (s *Service) ExpertsEnabled() bool {
	runtime, ok := s.runtime.(interface{ ExpertInstructionsAvailable() bool })
	return ok && runtime.ExpertInstructionsAvailable() && s.store.expertWriter && s.expertResources != nil && s.MarketEnabled() && s.ChatModelsEnabled()
}

type expertError struct{ code expert.ErrorCode }

func (e *expertError) Error() string         { return "expert adapter: " + string(e.code) }
func expertFail(code expert.ErrorCode) error { return &expertError{code} }
func ExpertFailureCode(err error) expert.ErrorCode {
	var e *expertError
	if errors.As(err, &e) {
		return e.code
	}
	code := expert.ErrorCode(MarketFailureCode(err))
	if code.Validate() == nil {
		return code
	}
	return expert.ErrorCodeContextInvalid
}
func cloneExpertSnapshot(s *expert.TurnExpertSnapshot) *expert.TurnExpertSnapshot {
	if s == nil {
		return nil
	}
	c := *s
	if s.Expert != nil {
		r := *s.Expert
		c.Expert = &r
	}
	return &c
}
func validMarketOperation(op marketOperation) bool {
	if op.Receipt.Validate() != nil {
		return false
	}
	if op.Version == 1 {
		return op.ExpertSnapshot == nil
	}
	return op.Version == 2 && op.ExpertSnapshot != nil && op.ExpertSnapshot.Validate() == nil && op.ExpertSnapshot.TurnOperationId == op.Receipt.TurnOperationId
}
func (s *Service) contextForExpert(ctx context.Context, ref *expert.ExpertRef) (context.Context, error) {
	if !s.ExpertsEnabled() {
		return nil, expertFail(expert.ErrorCodeQualificationRequired)
	}
	if ref == nil {
		return codex.WithExpertInstructions(ctx, "", "")
	}
	if s.store.noPendingExpertResource(string(ref.ExpertId)) != nil {
		return nil, expertFail(expert.ErrorCodeOperationPending)
	}
	resolved, e := s.expertResources.ResolveExpert(*ref)
	if e != nil {
		return nil, expertFail(expert.ErrorCodeResourceUnavailable)
	}
	return codex.WithExpertInstructions(ctx, resolved.Role, resolved.Index)
}
func (s *Service) contextForStoredExpert(ctx context.Context, sessionID string) (context.Context, error) {
	b, e := s.store.expertBinding(sessionID)
	if errors.Is(e, ErrNotFound) {
		return ctx, nil
	}
	if e != nil {
		return nil, e
	}
	if b.PendingOperation != "" {
		return nil, expertFail(expert.ErrorCodeOperationPending)
	}
	return s.contextForExpert(ctx, b.Expert)
}
func (s *Service) RegisterExpertGrant(input expert.PrivateGrantPayload) (expert.PrivateGrant, error) {
	if input.Validate() != nil {
		return expert.PrivateGrant{}, expertFail(expert.ErrorCodeInvalidRequest)
	}
	if !s.ExpertsEnabled() {
		return expert.PrivateGrant{}, expertFail(expert.ErrorCodeQualificationRequired)
	}
	// Resource removal and new frozen expert grants share the same admission
	// mutex. Registration is bounded and performs no Runtime request.
	if !s.modelOperationMu.TryLock() {
		return expert.PrivateGrant{}, expertFail(expert.ErrorCodeBusy)
	}
	defer s.modelOperationMu.Unlock()
	if input.Submission.ExpertSnapshot.Expert != nil && s.store.noPendingExpertResource(string(input.Submission.ExpertSnapshot.Expert.ExpertId)) != nil {
		return expert.PrivateGrant{}, expertFail(expert.ErrorCodeOperationPending)
	}
	grant, e := s.registerMarketGrant(market.GrantRegisterPayload{OperationId: input.OperationId, HostInstanceId: input.HostInstanceId, Scope: input.Scope, Submission: input.Submission.Submission}, &input.Submission.ExpertSnapshot)
	if e != nil {
		return expert.PrivateGrant{}, e
	}
	return expert.PrivateGrant{Grant: grant, ExpertSnapshotDigest: input.Submission.ExpertSnapshot.SnapshotDigest}, nil
}
func (s *Service) SubmitExpert(ctx context.Context, input market.SubmitPayload) (expert.PrivateReceipt, error) {
	if !s.ExpertsEnabled() {
		return expert.PrivateReceipt{}, expertFail(expert.ErrorCodeQualificationRequired)
	}
	receipt, e := s.submitMarket(ctx, input, true)
	if e != nil {
		return expert.PrivateReceipt{}, e
	}
	op, e := s.store.readMarketOperation(string(input.TurnOperationId))
	if e != nil || op.ExpertSnapshot == nil {
		return expert.PrivateReceipt{}, expertFail(expert.ErrorCodeOutcomeUnknown)
	}
	return expert.PrivateReceipt{Receipt: receipt, ExpertSnapshot: *cloneExpertSnapshot(op.ExpertSnapshot)}, nil
}
func (s *Service) ReadExpertSubmission(input expert.SubmissionReadPayload) (expert.PrivateReceipt, error) {
	if input.Validate() != nil {
		return expert.PrivateReceipt{}, expertFail(expert.ErrorCodeInvalidRequest)
	}
	if e := s.ValidateMarketScope(input.HostInstanceId, input.Scope, false); e != nil {
		return expert.PrivateReceipt{}, e
	}
	if !s.modelOperationMu.TryLock() {
		return expert.PrivateReceipt{}, expertFail(expert.ErrorCodeBusy)
	}
	defer s.modelOperationMu.Unlock()
	owner := s.marketScopeHash(input.Scope)
	op, e := s.store.readMarketOperation(string(input.TurnOperationId))
	if e != nil {
		// A missing receipt is distinct from an unreadable store. Retain the
		// original task/session/scope binding before reporting actual absence.
		if errors.Is(e, ErrNotFound) {
			record, readErr := s.store.GetByTask(string(input.TaskId))
			if readErr == nil && record.Purpose == purposeMarket && record.MarketScopeHash == owner && record.MarketLocalSessionID == string(input.LocalSessionId) && record.CodexThreadID != "" && record.CleanupOperationID == "" && record.State != StateCleaning {
				if _, turnErr := s.store.TurnOperation(record.AgentSessionID, string(input.TurnOperationId)); errors.Is(turnErr, ErrNotFound) {
					return expert.PrivateReceipt{}, expertFail(expert.ErrorCodeNotFound)
				}
			}
		}
		return expert.PrivateReceipt{}, expertFail(expert.ErrorCodeOutcomeUnknown)
	}
	if op.ExpertSnapshot == nil || op.ScopeHash != owner || op.Receipt.TaskId != input.TaskId || op.Receipt.LocalSessionId != input.LocalSessionId || op.Receipt.TurnOperationId != input.TurnOperationId {
		return expert.PrivateReceipt{}, expertFail(expert.ErrorCodeContextInvalid)
	}
	return expert.PrivateReceipt{Receipt: s.reconcileMarketReceipt(op), ExpertSnapshot: *cloneExpertSnapshot(op.ExpertSnapshot)}, nil
}
func (s *Service) ReadExpertOperation(input expert.PrivateOperationReadPayload) (expert.ApplyReceipt, error) {
	if input.Validate() != nil {
		return expert.ApplyReceipt{}, expertFail(expert.ErrorCodeInvalidRequest)
	}
	if e := s.ValidateMarketScope(input.HostInstanceId, input.Scope, false); e != nil {
		return expert.ApplyReceipt{}, e
	}
	op, e := s.store.readExpertOperation(string(input.OperationId))
	if e != nil {
		return expert.ApplyReceipt{}, expertFail(expert.ErrorCodeNotFound)
	}
	if op.ScopeHash != s.marketScopeHash(input.Scope) || op.Receipt.LocalSessionId != input.LocalSessionId || op.Receipt.AgentSessionId != input.AgentSessionId {
		return expert.ApplyReceipt{}, expertFail(expert.ErrorCodeContextInvalid)
	}
	return op.Receipt, nil
}

func (s *Service) ApplyExpertBinding(ctx context.Context, input expert.ApplyPayload) (expert.ApplyReceipt, error) {
	if input.Validate() != nil || input.BindingRevision != input.ExpectedBindingRevision+1 {
		return expert.ApplyReceipt{}, expertFail(expert.ErrorCodeInvalidRequest)
	}
	if !s.ExpertsEnabled() {
		return expert.ApplyReceipt{}, expertFail(expert.ErrorCodeQualificationRequired)
	}
	if e := s.lockMarketModelOperation(ctx); e != nil {
		return expert.ApplyReceipt{}, expertFail(expert.ErrorCodeBusy)
	}
	defer s.modelOperationMu.Unlock()
	if e := s.ValidateMarketScope(input.HostInstanceId, input.Scope, true); e != nil {
		return expert.ApplyReceipt{}, e
	}
	owner := s.marketScopeHash(input.Scope)
	digest, e := s.marketHMAC("yijie.expert-apply/v1", input)
	if e != nil {
		return expert.ApplyReceipt{}, e
	}
	if prior, err := s.store.readExpertOperation(string(input.OperationId)); err == nil {
		if prior.ScopeHash != owner || prior.IntentDigest != digest {
			return expert.ApplyReceipt{}, expertFail(expert.ErrorCodeRequestConflict)
		}
		return prior.Receipt, nil
	} else if !errors.Is(err, ErrNotFound) {
		return expert.ApplyReceipt{}, err
	}
	record, e := s.store.Get(string(input.AgentSessionId))
	if e != nil {
		return expert.ApplyReceipt{}, expertFail(expert.ErrorCodeNotFound)
	}
	if record.CodexThreadID != string(input.NativeThreadId) || record.Purpose == purposeDraft || (record.Purpose == purposeMarket && (record.MarketScopeHash != owner || record.MarketLocalSessionID != string(input.LocalSessionId))) {
		return expert.ApplyReceipt{}, expertFail(expert.ErrorCodeContextInvalid)
	}
	ctx = codex.WithModelProfile(ctx, string(input.Intent.ProfileId))
	ctx = codex.WithPermissionMode(ctx, codex.PermissionMode(input.PermissionMode))
	if record.ModelRevision != input.Intent.ExpectedRevision || s.checkModelRequest(ctx, record) != nil {
		return expert.ApplyReceipt{}, expertFail(expert.ErrorCodeModelUnavailable)
	}
	if record.State != StateIdle || record.ActiveTurnID != "" || record.CleanupOperationID != "" {
		return expert.ApplyReceipt{}, expertFail(expert.ErrorCodeBusy)
	}
	if s.requireNoRuntimeApprovalForThread(record.CodexThreadID) != nil {
		return expert.ApplyReceipt{}, expertFail(expert.ErrorCodeApprovalPending)
	}
	if s.store.marketNoPendingTurns(record.AgentSessionID) != nil {
		return expert.ApplyReceipt{}, expertFail(expert.ErrorCodeOperationPending)
	}
	ctx, e = s.contextForExpert(ctx, input.Expert)
	if e != nil {
		return expert.ApplyReceipt{}, e
	}
	if _, ok := s.runtime.(expertBindingRuntime); !ok {
		return expert.ApplyReceipt{}, expertFail(expert.ErrorCodeUnsupportedCapability)
	}
	receipt := expert.ApplyReceipt{OperationId: input.OperationId, HostInstanceId: input.HostInstanceId, LocalSessionId: input.LocalSessionId, AgentSessionId: input.AgentSessionId, NativeThreadId: input.NativeThreadId, ExpectedBindingRevision: input.ExpectedBindingRevision, BindingRevision: input.ExpectedBindingRevision, Expert: input.Expert, State: expert.OperationStatePending}
	operation := expertOperation{Version: 1, ScopeHash: owner, IntentDigest: digest, Receipt: receipt}
	if e = s.store.beginExpertOperation(record, operation); e != nil {
		return expert.ApplyReceipt{}, expertFail(expert.ErrorCodeRevisionConflict)
	}
	return s.performExpertBinding(ctx, record, input, operation)
}

type expertBindingRuntime interface {
	ClearMarketConfiguration(context.Context, string, string) (*codex.MarketPrepared, error)
	SwitchThread(context.Context, string) (codex.ThreadInfo, error)
}

// RecoverExpertBinding is an explicit, freshly authorized mutation. Reads and
// repeated apply requests never call it. Reapplying the complete original base
// on the original idle thread provides new evidence without sending a turn.
func (s *Service) RecoverExpertBinding(ctx context.Context, recovery expert.PrivateBindingRecoverPayload) (expert.ApplyReceipt, error) {
	input := recovery.Original
	if recovery.Validate() != nil || input.BindingRevision != input.ExpectedBindingRevision+1 {
		return expert.ApplyReceipt{}, expertFail(expert.ErrorCodeInvalidRequest)
	}
	if !s.ExpertsEnabled() {
		return expert.ApplyReceipt{}, expertFail(expert.ErrorCodeQualificationRequired)
	}
	if e := s.lockMarketModelOperation(ctx); e != nil {
		return expert.ApplyReceipt{}, expertFail(expert.ErrorCodeBusy)
	}
	defer s.modelOperationMu.Unlock()
	if e := s.ValidateMarketScope(recovery.HostInstanceId, recovery.Scope, true); e != nil {
		return expert.ApplyReceipt{}, e
	}
	owner := s.marketScopeHash(recovery.Scope)
	if owner != s.marketScopeHash(input.Scope) {
		return expert.ApplyReceipt{}, expertFail(expert.ErrorCodeContextInvalid)
	}
	digest, e := s.marketHMAC("yijie.expert-apply/v1", input)
	if e != nil {
		return expert.ApplyReceipt{}, e
	}
	operation, e := s.store.readExpertOperation(string(input.OperationId))
	absent := errors.Is(e, ErrNotFound)
	if e != nil && !absent {
		return expert.ApplyReceipt{}, e
	}
	if !absent {
		if operation.ScopeHash != owner || operation.IntentDigest != digest {
			return expert.ApplyReceipt{}, expertFail(expert.ErrorCodeRequestConflict)
		}
		if operation.Receipt.State == expert.OperationStateSucceeded || operation.Receipt.State == expert.OperationStateRejected {
			return operation.Receipt, nil
		}
	}
	record, e := s.store.Get(string(input.AgentSessionId))
	if e != nil {
		return expert.ApplyReceipt{}, expertFail(expert.ErrorCodeNotFound)
	}
	if record.CodexThreadID != string(input.NativeThreadId) || record.Purpose == purposeDraft || (record.Purpose == purposeMarket && (record.MarketScopeHash != owner || record.MarketLocalSessionID != string(input.LocalSessionId))) {
		return expert.ApplyReceipt{}, expertFail(expert.ErrorCodeContextInvalid)
	}
	ctx = codex.WithModelProfile(ctx, string(input.Intent.ProfileId))
	ctx = codex.WithPermissionMode(ctx, codex.PermissionMode(input.PermissionMode))
	if record.ModelRevision != input.Intent.ExpectedRevision || s.checkModelRequest(ctx, record) != nil {
		return expert.ApplyReceipt{}, expertFail(expert.ErrorCodeModelUnavailable)
	}
	if record.State != StateIdle || record.ActiveTurnID != "" || record.CleanupOperationID != "" {
		return expert.ApplyReceipt{}, expertFail(expert.ErrorCodeBusy)
	}
	if s.requireNoRuntimeApprovalForThread(record.CodexThreadID) != nil {
		return expert.ApplyReceipt{}, expertFail(expert.ErrorCodeApprovalPending)
	}
	if s.store.marketNoPendingTurnsExcept(record.AgentSessionID, string(input.OperationId)) != nil {
		return expert.ApplyReceipt{}, expertFail(expert.ErrorCodeOperationPending)
	}
	if !absent && s.store.validateExpertRecovery(record, operation) != nil {
		return expert.ApplyReceipt{}, expertFail(expert.ErrorCodeRevisionConflict)
	}
	ctx, e = s.contextForExpert(ctx, input.Expert)
	if e != nil {
		return expert.ApplyReceipt{}, e
	}
	if _, ok := s.runtime.(expertBindingRuntime); !ok {
		return expert.ApplyReceipt{}, expertFail(expert.ErrorCodeUnsupportedCapability)
	}
	if absent {
		operation = expertOperation{Version: 1, ScopeHash: owner, IntentDigest: digest, Receipt: expert.ApplyReceipt{OperationId: input.OperationId, HostInstanceId: input.HostInstanceId, LocalSessionId: input.LocalSessionId, AgentSessionId: input.AgentSessionId, NativeThreadId: input.NativeThreadId, ExpectedBindingRevision: input.ExpectedBindingRevision, BindingRevision: input.ExpectedBindingRevision, Expert: input.Expert, State: expert.OperationStatePending}}
		if e = s.store.beginExpertOperation(record, operation); e != nil {
			return expert.ApplyReceipt{}, expertFail(expert.ErrorCodeRevisionConflict)
		}
	}
	// The original scope/nonce remain part of the immutable receipt. Only the
	// outer, current authority permits this recovery request to change Runtime.
	return s.performExpertBinding(ctx, record, input, operation)
}

func (s *Service) performExpertBinding(ctx context.Context, record Record, input expert.ApplyPayload, operation expertOperation) (expert.ApplyReceipt, error) {
	runtime := s.runtime.(expertBindingRuntime)
	unknown := func() (expert.ApplyReceipt, error) {
		operation.Receipt.State = expert.OperationStateUnknown
		operation.Receipt.BindingRevision = input.ExpectedBindingRevision
		code := expert.ErrorCodeOutcomeUnknown
		operation.Receipt.ErrorCode = &code
		_ = s.store.finishExpertOperation(operation)
		return operation.Receipt, nil
	}
	// No old selection or lease is reused as new authority. The next original
	// market submission performs normal admission with its frozen snapshot.
	clear, e := runtime.ClearMarketConfiguration(ctx, record.CodexThreadID, record.Cwd)
	if e != nil {
		return unknown()
	}
	ctx = codex.WithMarketPrepared(ctx, clear)
	actual, e := runtime.SwitchThread(ctx, record.CodexThreadID)
	profile, _ := codex.ModelProfile(string(input.Intent.ProfileId))
	if e != nil || actual.ID != record.CodexThreadID || actual.Model != profile.Model || actual.ModelProvider != profile.Provider || actual.ReasoningEffort != profile.Effort {
		return unknown()
	}
	operation.Receipt.State = expert.OperationStateSucceeded
	operation.Receipt.BindingRevision = input.BindingRevision
	operation.Receipt.ErrorCode = nil
	if e = s.store.finishExpertOperation(operation); e != nil {
		return unknown()
	}
	return operation.Receipt, nil
}
