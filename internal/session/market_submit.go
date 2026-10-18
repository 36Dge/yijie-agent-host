package session

import (
	"context"
	"errors"
	"time"

	"github.com/36Dge/yijie-agent-host/internal/codex"
	broker "github.com/36Dge/yijie-agent-host/internal/contracts/marketbrokercontrol"
	wire "github.com/36Dge/yijie-agent-host/internal/contracts/markethost"
	"github.com/google/uuid"
)

type marketDeliveryRuntime interface {
	MarketProcess() (broker.ProcessBinding, error)
	PrepareMarket(context.Context, broker.ScopeBinding, wire.Submission, string, *string) (*codex.MarketPrepared, error)
	BeginMarketTurn(string, *codex.MarketPrepared) error
	DiscardMarketPrepared(context.Context, *codex.MarketPrepared) error
	ClearMarketThread(context.Context, string) error
	RevokeMarketAuthority(context.Context, broker.ScopeBinding, *broker.CanonicalId) error
	SwitchThread(context.Context, string) (codex.ThreadInfo, error)
}
type marketDigestKey struct{}

// Share 20 seconds across admission and native RPCs, plus the existing bounded
// five-second lease cleanup on failure. This leaves response time within the
// Native bridge's unchanged 30-second request budget. Individual RPC deadlines
// cannot extend the active submission lifetime.
const marketSubmissionBudget = 20 * time.Second

// Reuse the existing model operation mutex without letting its queue outlive
// this submission budget. Other model operations retain their existing lock.
func (s *Service) lockMarketModelOperation(ctx context.Context) error {
	wait := time.NewTicker(10 * time.Millisecond)
	defer wait.Stop()
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		if s.modelOperationMu.TryLock() {
			if err := ctx.Err(); err != nil {
				s.modelOperationMu.Unlock()
				return err
			}
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-wait.C:
		}
	}
}

// SubmitMarket accepts only an opaque reference established on the owned Native
// channel. The durable intent is checked before any admission, resume or dispatch.
func (s *Service) SubmitMarket(ctx context.Context, input wire.SubmitPayload) (wire.SubmissionReceipt, error) {
	return s.submitMarket(ctx, input, false)
}
func (s *Service) submitMarket(ctx context.Context, input wire.SubmitPayload, expertFamily bool) (wire.SubmissionReceipt, error) {
	if input.Validate() != nil {
		return wire.SubmissionReceipt{}, marketFail(wire.ErrorCodeInvalidRequest)
	}
	grant, err := s.marketGrant(string(input.GrantRef), string(input.TurnOperationId))
	if err != nil {
		return wire.SubmissionReceipt{}, err
	}
	if (grant.expertSnapshot != nil) != expertFamily {
		return wire.SubmissionReceipt{}, marketFail(wire.ErrorCodeAuthorityMismatch)
	}
	ctx, cancel := context.WithTimeout(ctx, marketSubmissionBudget)
	defer cancel()
	if err := s.lockMarketModelOperation(ctx); err != nil {
		return wire.SubmissionReceipt{}, marketFail(wire.ErrorCodeBusy)
	}
	defer s.modelOperationMu.Unlock()
	if prior, e := s.store.readMarketOperation(string(input.TurnOperationId)); e == nil {
		if prior.IntentDigest != grant.digest || prior.ScopeHash != grant.scopeHash {
			return wire.SubmissionReceipt{}, marketFail(wire.ErrorCodeRequestConflict)
		}
		return s.reconcileMarketReceipt(prior), nil
	} else if !errors.Is(e, ErrNotFound) {
		return wire.SubmissionReceipt{}, e
	}
	if ctx.Err() != nil {
		return wire.SubmissionReceipt{}, marketFail(wire.ErrorCodeBusy)
	}
	if grant.body == nil || grant.revoked || !time.Now().Before(grant.deadline) {
		return wire.SubmissionReceipt{}, marketFail(wire.ErrorCodeGrantExpired)
	}
	submission := *grant.body
	if submission.Validate() != nil {
		return wire.SubmissionReceipt{}, marketFail(wire.ErrorCodeInvalidRequest)
	}
	runtime, ok := s.runtime.(marketDeliveryRuntime)
	if !ok {
		return wire.SubmissionReceipt{}, marketFail(wire.ErrorCodeNotReady)
	}
	process, err := runtime.MarketProcess()
	if err != nil {
		return wire.SubmissionReceipt{}, marketFail(wire.ErrorCodeNotReady)
	}
	cwd, err := canonicalDirectory(submission.Cwd)
	if err != nil || cwd != submission.Cwd {
		return wire.SubmissionReceipt{}, marketFail(wire.ErrorCodeInvalidRequest)
	}
	blocks, err := MapTurnV2ContentBlocks(submission.ContentBlocks)
	if err != nil {
		return wire.SubmissionReceipt{}, marketFail(wire.ErrorCodeInvalidRequest)
	}
	if _, err = validateAndMapTurnV2Blocks(blocks); err != nil {
		return wire.SubmissionReceipt{}, marketFail(wire.ErrorCodeInvalidRequest)
	}
	ctx = codex.WithModelProfile(ctx, string(submission.Intent.ProfileId))
	ctx = codex.WithPermissionMode(ctx, codex.PermissionMode(submission.PermissionMode))
	var record Record
	var thread *string
	adopt := false
	if submission.AgentSessionId == nil {
		if err = s.checkModelRequest(ctx, Record{}); err != nil {
			return wire.SubmissionReceipt{}, marketFail(wire.ErrorCodeModelUnavailable)
		}
		record = Record{TaskID: string(submission.TaskId), AgentSessionID: uuid.NewString(), Cwd: cwd, Purpose: purposeMarket, MarketScopeHash: grant.scopeHash, MarketLocalSessionID: string(submission.LocalSessionId), ModelProfile: string(submission.Intent.ProfileId), ModelRevision: 1, ModelSelectionState: "ready"}
	} else {
		record, err = s.store.Get(string(*submission.AgentSessionId))
		if err != nil {
			return wire.SubmissionReceipt{}, marketFail(wire.ErrorCodeSessionNotFound)
		}
		if record.TaskID != string(submission.TaskId) || record.Cwd != cwd || record.CodexThreadID == "" || record.Purpose == purposeDraft {
			return wire.SubmissionReceipt{}, marketFail(wire.ErrorCodeAuthorityMismatch)
		}
		if record.Purpose == purposeMarket && (record.MarketScopeHash != grant.scopeHash || record.MarketLocalSessionID != string(submission.LocalSessionId)) {
			return wire.SubmissionReceipt{}, marketFail(wire.ErrorCodeAuthorityMismatch)
		}
		if record.ModelRevision != submission.Intent.ExpectedRevision || s.checkModelRequest(ctx, record) != nil {
			return wire.SubmissionReceipt{}, marketFail(wire.ErrorCodeRevisionConflict)
		}
		if record.CleanupOperationID != "" || s.requireNoRuntimeApprovalForThread(record.CodexThreadID) != nil {
			return wire.SubmissionReceipt{}, marketFail(wire.ErrorCodeBusy)
		}
		if err = s.store.marketNoPendingTurns(record.AgentSessionID); err != nil {
			return wire.SubmissionReceipt{}, marketFail(wire.ErrorCodeBusy)
		}
		if record.State == StateActive && record.ActiveTurnID != "" {
			if err = s.store.checkExpertSubmission(record, grant.expertSnapshot, false); err != nil {
				return wire.SubmissionReceipt{}, marketFail(wire.ErrorCodeRequestConflict)
			}
			record, err = s.reconcileMarketIdle(ctx, record)
			if err != nil {
				return wire.SubmissionReceipt{}, marketFail(wire.ErrorCodeBusy)
			}
		}
		if record.State != StateIdle || record.ActiveTurnID != "" {
			return wire.SubmissionReceipt{}, marketFail(wire.ErrorCodeBusy)
		}
		adopt = record.Purpose == purposeOrdinary
		record.Purpose, record.MarketScopeHash, record.MarketLocalSessionID = purposeMarket, grant.scopeHash, string(submission.LocalSessionId)
		thread = &record.CodexThreadID
	}
	if err = s.store.checkExpertSubmission(record, grant.expertSnapshot, thread == nil); err != nil {
		return wire.SubmissionReceipt{}, marketFail(wire.ErrorCodeRequestConflict)
	}
	if grant.expertSnapshot != nil {
		ctx, err = s.contextForExpert(ctx, grant.expertSnapshot.Expert)
		if err != nil {
			return wire.SubmissionReceipt{}, err
		}
	}
	prepared, err := runtime.PrepareMarket(ctx, grant.scope, submission, record.AgentSessionID, thread)
	if err != nil {
		return wire.SubmissionReceipt{}, marketFail(wire.ErrorCodeExecutionUnavailable)
	}
	accepted := false
	defer func() {
		if !accepted {
			cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = runtime.DiscardMarketPrepared(cleanup, prepared)
		}
	}()
	// Recheck trusted authority after worker admission, while no native mutation
	// exists yet. A known expired grant is rejected without an uncertain ledger.
	if err = s.consumeMarketGrant(string(input.GrantRef), grant); err != nil {
		return wire.SubmissionReceipt{}, err
	}
	defer s.finishMarketGrant(string(input.GrantRef))
	// Record the complete intent before the first native thread/turn mutation.
	// Worker admission cannot execute a tool; unknown native delivery is never retried.
	sessionID := wire.CanonicalId(record.AgentSessionID)
	generation := process.RuntimeGeneration
	receipt := wire.SubmissionReceipt{HostInstanceId: process.HostInstanceId, RuntimeGeneration: &generation, TaskId: submission.TaskId, LocalSessionId: submission.LocalSessionId, SubmissionOperationId: submission.SubmissionOperationId, TurnOperationId: submission.Snapshot.TurnOperationId, SelectionDigest: submission.Snapshot.SelectionDigest, State: wire.SubmissionStatePending, AgentSessionId: &sessionID, ProfileId: submission.Intent.ProfileId, ModelRevision: record.ModelRevision}
	operation := marketOperation{Version: 1, ScopeHash: grant.scopeHash, IntentDigest: grant.digest, Receipt: receipt, ExpertSnapshot: cloneExpertSnapshot(grant.expertSnapshot)}
	if grant.expertSnapshot != nil {
		operation.Version = 2
	}
	if err = s.store.beginMarketOperation(record, operation, adopt, submission.Intent.ExpectedRevision); err != nil {
		return wire.SubmissionReceipt{}, marketFail(wire.ErrorCodeRequestConflict)
	}
	uncertain := func() (wire.SubmissionReceipt, error) {
		operation.Receipt.State = wire.SubmissionStateUncertain
		_ = s.store.finishMarketOperation(operation)
		return operation.Receipt, nil
	}
	ctx = codex.WithMarketPrepared(ctx, prepared)
	var actual codex.ThreadInfo
	if thread == nil {
		actual, err = s.runtime.StartThread(ctx, cwd)
	} else {
		if err = runtime.ClearMarketThread(ctx, *thread); err == nil {
			actual, err = runtime.SwitchThread(ctx, *thread)
		}
	}
	profile, _ := codex.ModelProfile(string(submission.Intent.ProfileId))
	if err != nil || !isCanonicalNonZeroUUID(actual.ID) || actual.Model != profile.Model || actual.ModelProvider != profile.Provider || actual.ReasoningEffort != profile.Effort || (thread != nil && actual.ID != *thread) {
		return uncertain()
	}
	s.notificationMu.Lock()
	record, err = s.store.BindThread(record.AgentSessionID, actual.ID, actual.RuntimeSession, actual.Model, actual.ModelProvider)
	if err == nil {
		s.flushPending(actual.ID)
	}
	s.notificationMu.Unlock()
	if err != nil {
		return uncertain()
	}
	nativeThread := wire.NativeId(actual.ID)
	operation.Receipt.NativeThreadId = &nativeThread
	if err = runtime.BeginMarketTurn(actual.ID, prepared); err != nil {
		return uncertain()
	}
	ctx = context.WithValue(withMarketSubmission(ctx), marketDigestKey{}, grant.digest)
	turn, err := s.startTurnV2Locked(ctx, StartTurnV2Input{AgentSessionID: record.AgentSessionID, OperationID: string(input.TurnOperationId), ContentBlocks: blocks, PermissionMode: codex.PermissionMode(submission.PermissionMode)}, false)
	if err != nil {
		return uncertain()
	}
	nativeTurn := wire.NativeId(turn.ID)
	operation.Receipt.NativeTurnId = &nativeTurn
	operation.Receipt.State = wire.SubmissionStateAccepted
	// The existing accepted turn ledger remains the fallback if this final
	// projection write fails after Runtime has acknowledged the real turn.
	if err = s.store.finishMarketOperation(operation); err != nil {
		return uncertain()
	}
	accepted = true
	return operation.Receipt, nil
}

func (s *Service) reconcileMarketReceipt(entry marketOperation) wire.SubmissionReceipt {
	if entry.Receipt.State != wire.SubmissionStateAccepted && entry.Receipt.AgentSessionId != nil {
		op, e := s.store.TurnOperation(string(*entry.Receipt.AgentSessionId), string(entry.Receipt.TurnOperationId))
		if e == nil && op.State == TurnOperationStateAccepted {
			record, e := s.store.Get(op.SessionID)
			if e == nil && record.Purpose == purposeMarket && record.MarketScopeHash == entry.ScopeHash && record.CodexThreadID != "" {
				turn, thread := wire.NativeId(op.TurnID), wire.NativeId(record.CodexThreadID)
				entry.Receipt.NativeTurnId = &turn
				entry.Receipt.NativeThreadId = &thread
				entry.Receipt.State = wire.SubmissionStateAccepted
				_ = s.store.finishMarketOperation(entry)
			}
		}
	}
	return entry.Receipt
}

func (s *Service) RevokeMarketAuthority(ctx context.Context, input wire.RevokeAuthorityPayload) (wire.RevokeAuthorityResult, error) {
	if input.Validate() != nil {
		return wire.RevokeAuthorityResult{}, marketFail(wire.ErrorCodeInvalidRequest)
	}
	s.modelOperationMu.Lock()
	defer s.modelOperationMu.Unlock()
	if err := s.ValidateMarketScope(input.HostInstanceId, input.Scope, false); err != nil {
		return wire.RevokeAuthorityResult{}, err
	}
	digest, e := s.marketHMAC("yijie.market-revoke/v1", input)
	if e != nil {
		return wire.RevokeAuthorityResult{}, e
	}
	a := s.market
	a.mu.Lock()
	if previous, ok := a.receipts[string(input.OperationId)]; ok {
		if previous.digest != digest || previous.revoke == nil {
			a.mu.Unlock()
			return wire.RevokeAuthorityResult{}, marketFail(wire.ErrorCodeRequestConflict)
		}
		result := *previous.revoke
		a.mu.Unlock()
		return result, nil
	}
	retire := len(a.receipts) >= wire.MaxControlReceipts
	if retire {
		a.closed = true
	} else {
		a.receipts[string(input.OperationId)] = marketControlReceipt{digest: digest}
	}
	for _, g := range a.grants {
		match := input.InstallationId == nil || retire
		for _, r := range g.identity.Snapshot.Selection {
			if input.InstallationId != nil && r.InstallationId == *input.InstallationId {
				match = true
			}
		}
		if match {
			g.revoked = true
			a.releaseBody(g)
		}
	}
	if int64(input.Scope.AuthorizationRevision) > a.revision {
		a.revision = int64(input.Scope.AuthorizationRevision)
	}
	a.mu.Unlock()
	result := wire.RevokeAuthorityResult{Outcome: "admission_revoked", ExternalOutcomes: "not_asserted"}
	if rt, ok := s.runtime.(marketDeliveryRuntime); ok {
		if err := rt.RevokeMarketAuthority(ctx, input.Scope, input.InstallationId); err != nil {
			result.Outcome = "retirement_pending"
		}
	}
	if retire {
		if runtime, ok := s.runtime.(interface{ CloseMarketAdmission(context.Context) error }); ok {
			if e := runtime.CloseMarketAdmission(ctx); e != nil {
				result.Outcome = "retirement_pending"
			}
		}
	} else {
		a.mu.Lock()
		copy := result
		a.receipts[string(input.OperationId)] = marketControlReceipt{digest: digest, revoke: &copy}
		a.mu.Unlock()
	}
	return result, nil
}
