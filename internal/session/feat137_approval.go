package session

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/36Dge/yijie-agent-host/internal/codex"
)

const (
	approvalSchemaVersion      = 6
	approvalPendingRevision    = int64(1)
	approvalResolvedRevision   = int64(2)
	approvalTTL                = 120 * time.Second
	approvalTTLSeconds         = 120
	approvalAuditLimit         = 128
	approvalActionID           = "git_repository_check"
	approvalWorkspaceScope     = "current_workspace"
	approvalDecisionAcceptOnce = "accept_once"
	approvalDecisionCancelTurn = "cancel_current_turn"
	approvalOutcomeAccepted    = "accepted_once"
	approvalOutcomeCancelled   = "cancelled_current_turn"
	approvalOutcomeExpired     = "expired"
	approvalOutcomeElsewhere   = "resolved_elsewhere"
	approvalRuntimeCommand     = "git rev-parse --is-inside-work-tree"
	approvalRuntimeEnvironment = "local"
	approvalPhasePending       = "pending"
	approvalPhaseCommitted     = "response_committed_wait_ack"
	approvalPhaseTTLCommitted  = "ttl_cancel_committed_wait_ack"
)

const (
	ApprovalErrorInvalidRequest   = "invalid_approval_request"
	ApprovalErrorVersionMismatch  = "approval_version_mismatch"
	ApprovalErrorNotFound         = "approval_not_found"
	ApprovalErrorStale            = "approval_stale"
	ApprovalErrorExpired          = "approval_expired"
	ApprovalErrorAlreadyResolved  = "approval_already_resolved"
	ApprovalErrorDecisionConflict = "approval_decision_conflict"
	ApprovalErrorUnavailable      = "approval_unavailable"
	ApprovalErrorInternal         = "internal_error"
)

type ApprovalError struct {
	Code string
}

func (e *ApprovalError) Error() string { return e.Code }

type PendingApproval struct {
	ApprovalRequestID string
	Revision          int64
	TaskID            string
	AgentSessionID    string
	CodexThreadID     string
	TurnID            string
	ItemID            string
	ActionID          string
	WorkspaceScope    string
	RequestedAt       time.Time
	ExpiresAt         time.Time
	TTLSeconds        int
}

type PendingApprovalSnapshot struct {
	SchemaVersion int
	StreamID      string
	SnapshotAt    time.Time
	Pending       []PendingApproval
}

type ApprovalDecisionInput struct {
	SchemaVersion    int
	DecisionID       string
	ExpectedStreamID string
	ExpectedRevision int64
	Decision         string
}

type ApprovalDecisionResult struct {
	SchemaVersion     int
	ApprovalRequestID string
	DecisionID        string
	StreamID          string
	Revision          int64
	Decision          string
	Outcome           string
	ResolvedAt        time.Time
}

type approvalAuthority struct {
	mu                     sync.Mutex
	pendingBySession       map[string]*approvalPending
	pendingByRuntime       map[string]*approvalPending
	resolvedRuntime        map[string]string
	resolvedRuntimeDigests map[string][]string
	closedRuntime          map[string]struct{}
	auditBySession         map[string][]approvalAuditRecord
	acknowledgementTimeout time.Duration
	// admissionBarrier is nil in production and supplies deterministic package
	// test synchronization between the two closed-generation admission checks.
	admissionBarrier func()
}

type approvalPending struct {
	record                        Record
	runtimeGeneration             string
	runtimeKey                    string
	requestFingerprint            string
	approvalID                    string
	itemID                        string
	streamID                      string
	requestedAt                   time.Time
	expiresAt                     time.Time
	monotonicDeadline             time.Time
	phase                         string
	decisionID                    string
	decision                      string
	decisionFingerprint           string
	runtimeResponseWritten        bool
	runtimeResolutionAcknowledged bool
	handlerOutcome                chan codex.CommandApprovalResult
	decisionWaiters               []chan approvalDecisionCompletion
}

type approvalDecisionCompletion struct {
	result ApprovalDecisionResult
	err    error
}

type approvalAuditRecord struct {
	approvalID          string
	outcome             string
	decisionID          string
	decisionFingerprint string
	requestedAt         time.Time
	resolvedAt          time.Time
	response            *ApprovalDecisionResult
}

func newApprovalAuthority(acknowledgementTimeout time.Duration) *approvalAuthority {
	if acknowledgementTimeout <= 0 {
		acknowledgementTimeout = 30 * time.Second
	}
	return &approvalAuthority{
		pendingBySession:       make(map[string]*approvalPending),
		pendingByRuntime:       make(map[string]*approvalPending),
		resolvedRuntime:        make(map[string]string),
		resolvedRuntimeDigests: make(map[string][]string),
		closedRuntime:          make(map[string]struct{}),
		auditBySession:         make(map[string][]approvalAuditRecord),
		acknowledgementTimeout: acknowledgementTimeout,
	}
}

// rememberResolvedRuntimeLocked retains only a digest of the Runtime replay
// identity for the current process generation. The frozen v6 contract does not
// authorize a per-generation cardinality cutoff: every new request must receive
// one response while a resolved typed key must never reopen. Generation close
// is the bounded lifecycle boundary and clears every retained digest.
func (authority *approvalAuthority) rememberResolvedRuntimeLocked(digest, generation string) bool {
	if digest == "" || generation == "" {
		return false
	}
	if _, closed := authority.closedRuntime[generation]; closed {
		return false
	}
	if authority.suppressResolvedRuntimeLocked(digest, generation) {
		return false
	}
	digests := authority.resolvedRuntimeDigests[generation]
	authority.resolvedRuntime[digest] = generation
	authority.resolvedRuntimeDigests[generation] = append(digests, digest)
	return true
}

func (authority *approvalAuthority) suppressResolvedRuntimeLocked(digest, generation string) bool {
	if _, closed := authority.closedRuntime[generation]; closed {
		return true
	}
	if _, resolved := authority.resolvedRuntime[digest]; resolved {
		return true
	}
	return false
}

func (authority *approvalAuthority) clearResolvedRuntimeGenerationLocked(generation string) {
	if generation == "" {
		return
	}
	for _, digest := range authority.resolvedRuntimeDigests[generation] {
		delete(authority.resolvedRuntime, digest)
	}
	delete(authority.resolvedRuntimeDigests, generation)
}

func (s *Service) HandleCommandApproval(
	ctx context.Context,
	request codex.CommandApprovalRequest,
) codex.CommandApprovalResult {
	cancel := codex.CommandApprovalResult{Respond: true, Decision: "cancel"}
	suppress := codex.CommandApprovalResult{}
	if s.approvals == nil || s.eventsV6 == nil || request.RuntimeGeneration == "" {
		return cancel
	}
	authority := s.approvals
	authority.mu.Lock()
	_, generationClosed := authority.closedRuntime[request.RuntimeGeneration]
	authority.mu.Unlock()
	if generationClosed {
		return suppress
	}
	if !validApprovalRuntimeRequestKey(request.RequestIDKey) {
		return cancel
	}
	runtimeKey := request.RuntimeGeneration + "\x00" + request.RequestIDKey
	runtimeDigest := approvalRuntimeKeyDigest(runtimeKey)
	authority.mu.Lock()
	if authority.suppressResolvedRuntimeLocked(runtimeDigest, request.RuntimeGeneration) {
		authority.mu.Unlock()
		return suppress
	}
	admissionBarrier := authority.admissionBarrier
	authority.mu.Unlock()
	if admissionBarrier != nil {
		admissionBarrier()
	}

	record, fingerprint, eligible := s.validateCommandApprovalRequest(request)

	authority.mu.Lock()
	if authority.suppressResolvedRuntimeLocked(runtimeDigest, request.RuntimeGeneration) {
		authority.mu.Unlock()
		return suppress
	}
	existing := authority.pendingByRuntime[runtimeKey]
	if existing != nil && existing.phase != approvalPhasePending {
		authority.mu.Unlock()
		return suppress
	}
	if !eligible {
		if existing != nil {
			s.resolveApprovalLocked(existing, approvalOutcomeElsewhere, "", "", cancel, nil)
			authority.mu.Unlock()
			return suppress
		}
		respond := authority.rememberResolvedRuntimeLocked(runtimeDigest, request.RuntimeGeneration)
		authority.mu.Unlock()
		if !respond {
			return suppress
		}
		return cancel
	}
	if existing != nil {
		if existing.requestFingerprint == fingerprint {
			authority.mu.Unlock()
			return suppress
		}
		s.resolveApprovalLocked(existing, approvalOutcomeElsewhere, "", "", cancel, nil)
		authority.mu.Unlock()
		return suppress
	}
	if authority.pendingBySession[record.AgentSessionID] != nil {
		respond := authority.rememberResolvedRuntimeLocked(runtimeDigest, request.RuntimeGeneration)
		authority.mu.Unlock()
		if !respond {
			return suppress
		}
		return cancel
	}
	approvalID, err := newUUID()
	if err != nil {
		respond := authority.rememberResolvedRuntimeLocked(runtimeDigest, request.RuntimeGeneration)
		authority.mu.Unlock()
		if !respond {
			return suppress
		}
		return cancel
	}
	receivedAt := time.Now()
	pending := &approvalPending{
		record: record, runtimeGeneration: request.RuntimeGeneration, runtimeKey: runtimeKey,
		requestFingerprint: fingerprint, approvalID: approvalID,
		itemID:      request.ItemID,
		requestedAt: receivedAt.UTC(), expiresAt: receivedAt.Add(approvalTTL).UTC(),
		monotonicDeadline: receivedAt.Add(approvalTTL), phase: approvalPhasePending,
		handlerOutcome: make(chan codex.CommandApprovalResult, 1),
	}
	published, err := s.publishApprovalRequested(record, pending)
	if err != nil {
		respond := authority.rememberResolvedRuntimeLocked(runtimeDigest, request.RuntimeGeneration)
		authority.mu.Unlock()
		if !respond {
			return suppress
		}
		return cancel
	}
	pending.streamID = published.StreamID
	authority.pendingByRuntime[runtimeKey] = pending
	authority.pendingBySession[record.AgentSessionID] = pending
	authority.mu.Unlock()

	timer := time.NewTimer(time.Until(pending.monotonicDeadline))
	defer timer.Stop()
	for {
		select {
		case outcome := <-pending.handlerOutcome:
			return outcome
		case <-timer.C:
			s.expireApproval(pending)
		case <-ctx.Done():
			s.resolveApprovalForRuntimeKey(request.RuntimeGeneration, runtimeKey)
			return codex.CommandApprovalResult{}
		}
	}
}

func (s *Service) validateCommandApprovalRequest(request codex.CommandApprovalRequest) (Record, string, bool) {
	record, err := s.store.GetByThread(request.ThreadID)
	if err != nil || record.ActiveTurnID == "" || record.ActiveTurnID != request.TurnID ||
		!validUUID(request.TurnID) || request.ItemID == "" ||
		!s.expectedLiveCommandItem(record, request.TurnID, request.ItemID) {
		return Record{}, "", false
	}
	canonicalCwd, err := canonicalDirectory(request.Cwd)
	if err != nil || canonicalCwd != record.Cwd || filepath.Clean(canonicalCwd) != canonicalCwd ||
		request.Command != approvalRuntimeCommand || len(request.CommandActions) != 1 ||
		request.CommandActions[0].Type != "unknown" || request.CommandActions[0].Command != approvalRuntimeCommand ||
		(request.EnvironmentID != nil && *request.EnvironmentID != approvalRuntimeEnvironment) {
		return Record{}, "", false
	}
	return record, approvalRequestFingerprint(request, canonicalCwd), true
}

func (s *Service) HandleCommandApprovalResponseWritten(result codex.CommandApprovalResponseWriteResult) {
	if s.approvals == nil || result.RuntimeGeneration == "" || result.RequestIDKey == "" || result.ThreadID == "" {
		return
	}
	runtimeKey := result.RuntimeGeneration + "\x00" + result.RequestIDKey
	authority := s.approvals
	authority.mu.Lock()
	pending := authority.pendingByRuntime[runtimeKey]
	if pending == nil || pending.runtimeGeneration != result.RuntimeGeneration ||
		pending.record.CodexThreadID != result.ThreadID {
		authority.mu.Unlock()
		return
	}
	if !result.Succeeded {
		if pending.phase == approvalPhaseTTLCommitted {
			s.discardApprovalLocked(pending)
		} else if pending.phase == approvalPhaseCommitted {
			s.resolveApprovalLocked(pending, approvalOutcomeElsewhere, "", "", codex.CommandApprovalResult{},
				&ApprovalError{Code: ApprovalErrorUnavailable})
		}
		authority.mu.Unlock()
		return
	}
	if pending.phase != approvalPhaseTTLCommitted {
		authority.mu.Unlock()
		return
	}
	pending.runtimeResponseWritten = true
	if pending.runtimeResolutionAcknowledged {
		s.resolveApprovalLocked(pending, approvalOutcomeExpired, "", "", codex.CommandApprovalResult{}, nil)
	}
	authority.mu.Unlock()
}

func (s *Service) HandleCommandApprovalResolved(resolved codex.CommandApprovalResolved) {
	if s.approvals == nil || resolved.RuntimeGeneration == "" || resolved.RequestIDKey == "" || resolved.ThreadID == "" {
		return
	}
	runtimeKey := resolved.RuntimeGeneration + "\x00" + resolved.RequestIDKey
	authority := s.approvals
	authority.mu.Lock()
	pending := authority.pendingByRuntime[runtimeKey]
	if pending == nil || pending.runtimeGeneration != resolved.RuntimeGeneration ||
		pending.record.CodexThreadID != resolved.ThreadID {
		authority.mu.Unlock()
		return
	}
	if pending.phase == approvalPhasePending {
		s.resolveApprovalLocked(pending, approvalOutcomeElsewhere, "", "", codex.CommandApprovalResult{}, nil)
		authority.mu.Unlock()
		return
	}
	if pending.phase == approvalPhaseTTLCommitted {
		pending.runtimeResolutionAcknowledged = true
		if pending.runtimeResponseWritten {
			s.resolveApprovalLocked(pending, approvalOutcomeExpired, "", "", codex.CommandApprovalResult{}, nil)
		}
		authority.mu.Unlock()
		return
	}
	if pending.phase != approvalPhaseCommitted {
		authority.mu.Unlock()
		return
	}
	outcome := approvalOutcomeAccepted
	if pending.decision == approvalDecisionCancelTurn {
		outcome = approvalOutcomeCancelled
	}
	resolvedAt := time.Now().UTC()
	result := ApprovalDecisionResult{
		SchemaVersion: approvalSchemaVersion, ApprovalRequestID: pending.approvalID,
		DecisionID: pending.decisionID, StreamID: pending.streamID,
		Revision: approvalResolvedRevision, Decision: pending.decision, Outcome: outcome,
		ResolvedAt: resolvedAt,
	}
	if err := s.publishApprovalResolved(pending.record, pending, outcome, pending.decisionID, pending.decision, resolvedAt); err != nil {
		s.finishApprovalLocked(pending, approvalAuditRecord{
			approvalID: pending.approvalID, outcome: outcome,
			requestedAt: pending.requestedAt, resolvedAt: resolvedAt,
		})
		s.notifyDecisionWaitersLocked(pending, approvalDecisionCompletion{err: &ApprovalError{Code: ApprovalErrorInternal}})
		authority.mu.Unlock()
		return
	}
	copyResult := result
	audit := approvalAuditRecord{
		approvalID: pending.approvalID, outcome: outcome, decisionID: pending.decisionID,
		decisionFingerprint: pending.decisionFingerprint,
		requestedAt:         pending.requestedAt, resolvedAt: resolvedAt, response: &copyResult,
	}
	s.finishApprovalLocked(pending, audit)
	s.notifyDecisionWaitersLocked(pending, approvalDecisionCompletion{result: result})
	authority.mu.Unlock()
}

func (s *Service) HandleCommandApprovalGenerationClosed(generation string) {
	if s.approvals == nil || generation == "" {
		return
	}
	authority := s.approvals
	authority.mu.Lock()
	var pending []*approvalPending
	for _, candidate := range authority.pendingBySession {
		if candidate.runtimeGeneration == generation {
			pending = append(pending, candidate)
		}
	}
	for _, candidate := range pending {
		if candidate.phase == approvalPhaseTTLCommitted {
			s.discardApprovalLocked(candidate)
		} else {
			s.resolveApprovalLocked(candidate, approvalOutcomeElsewhere, "", "", codex.CommandApprovalResult{},
				&ApprovalError{Code: ApprovalErrorUnavailable})
		}
	}
	authority.clearResolvedRuntimeGenerationLocked(generation)
	authority.closedRuntime[generation] = struct{}{}
	authority.mu.Unlock()
}

func (s *Service) PendingApprovals(sessionID string) (PendingApprovalSnapshot, error) {
	if s.approvals == nil || s.eventsV6 == nil {
		return PendingApprovalSnapshot{}, &ApprovalError{Code: ApprovalErrorUnavailable}
	}
	if !validApprovalUUID(sessionID) {
		return PendingApprovalSnapshot{}, &ApprovalError{Code: ApprovalErrorInvalidRequest}
	}
	if _, err := s.store.Get(sessionID); err != nil {
		return PendingApprovalSnapshot{}, err
	}
	streamID, err := s.eventsV6.CurrentStreamID(sessionID)
	if err != nil {
		return PendingApprovalSnapshot{}, &ApprovalError{Code: ApprovalErrorInternal}
	}
	authority := s.approvals
	authority.mu.Lock()
	now := time.Now()
	pending := authority.pendingBySession[sessionID]
	if pending != nil && pending.phase == approvalPhasePending && !now.Before(pending.monotonicDeadline) {
		s.commitApprovalExpiryLocked(pending)
		pending = nil
	}
	snapshot := PendingApprovalSnapshot{
		SchemaVersion: approvalSchemaVersion, StreamID: streamID, SnapshotAt: now.UTC(),
		Pending: make([]PendingApproval, 0, 1),
	}
	if pending != nil && pending.phase == approvalPhasePending {
		snapshot.Pending = append(snapshot.Pending, pending.public())
	}
	authority.mu.Unlock()
	return snapshot, nil
}

func (s *Service) DecideApproval(
	ctx context.Context,
	sessionID, approvalID string,
	input ApprovalDecisionInput,
) (ApprovalDecisionResult, error) {
	if s.approvals == nil || s.eventsV6 == nil {
		return ApprovalDecisionResult{}, &ApprovalError{Code: ApprovalErrorUnavailable}
	}
	if input.SchemaVersion != approvalSchemaVersion {
		return ApprovalDecisionResult{}, &ApprovalError{Code: ApprovalErrorVersionMismatch}
	}
	if !validApprovalUUID(sessionID) || !validApprovalUUID(approvalID) ||
		!validApprovalUUID(input.DecisionID) || !validApprovalUUID(input.ExpectedStreamID) ||
		input.ExpectedRevision != approvalPendingRevision ||
		(input.Decision != approvalDecisionAcceptOnce && input.Decision != approvalDecisionCancelTurn) {
		return ApprovalDecisionResult{}, &ApprovalError{Code: ApprovalErrorInvalidRequest}
	}
	if _, err := s.store.Get(sessionID); err != nil {
		return ApprovalDecisionResult{}, err
	}
	fingerprint := approvalDecisionFingerprint(approvalID, input)
	waiter := make(chan approvalDecisionCompletion, 1)
	authority := s.approvals
	authority.mu.Lock()
	for _, audit := range authority.auditBySession[sessionID] {
		if audit.decisionID == input.DecisionID && audit.decisionID != "" {
			if audit.decisionFingerprint == fingerprint && audit.response != nil {
				result := *audit.response
				authority.mu.Unlock()
				return result, nil
			}
			authority.mu.Unlock()
			return ApprovalDecisionResult{}, &ApprovalError{Code: ApprovalErrorDecisionConflict}
		}
	}
	for _, audit := range authority.auditBySession[sessionID] {
		if audit.approvalID == approvalID {
			authority.mu.Unlock()
			if audit.outcome == approvalOutcomeExpired {
				return ApprovalDecisionResult{}, &ApprovalError{Code: ApprovalErrorExpired}
			}
			return ApprovalDecisionResult{}, &ApprovalError{Code: ApprovalErrorAlreadyResolved}
		}
	}
	pending := authority.pendingBySession[sessionID]
	if pending == nil || pending.approvalID != approvalID {
		authority.mu.Unlock()
		return ApprovalDecisionResult{}, &ApprovalError{Code: ApprovalErrorNotFound}
	}
	if pending.phase == approvalPhaseTTLCommitted {
		authority.mu.Unlock()
		return ApprovalDecisionResult{}, &ApprovalError{Code: ApprovalErrorExpired}
	}
	if pending.phase == approvalPhaseCommitted {
		if pending.decisionID == input.DecisionID {
			if pending.decisionFingerprint != fingerprint {
				authority.mu.Unlock()
				return ApprovalDecisionResult{}, &ApprovalError{Code: ApprovalErrorDecisionConflict}
			}
			pending.decisionWaiters = append(pending.decisionWaiters, waiter)
			authority.mu.Unlock()
			return s.waitForApprovalDecision(ctx, waiter)
		}
		authority.mu.Unlock()
		return ApprovalDecisionResult{}, &ApprovalError{Code: ApprovalErrorAlreadyResolved}
	}
	if !time.Now().Before(pending.monotonicDeadline) {
		s.commitApprovalExpiryLocked(pending)
		authority.mu.Unlock()
		return ApprovalDecisionResult{}, &ApprovalError{Code: ApprovalErrorExpired}
	}
	if pending.streamID != input.ExpectedStreamID || input.ExpectedRevision != approvalPendingRevision {
		authority.mu.Unlock()
		return ApprovalDecisionResult{}, &ApprovalError{Code: ApprovalErrorStale}
	}
	pending.phase = approvalPhaseCommitted
	pending.decisionID = input.DecisionID
	pending.decision = input.Decision
	pending.decisionFingerprint = fingerprint
	pending.decisionWaiters = append(pending.decisionWaiters, waiter)
	runtimeDecision := "accept"
	if input.Decision == approvalDecisionCancelTurn {
		runtimeDecision = "cancel"
	}
	pending.handlerOutcome <- codex.CommandApprovalResult{Respond: true, Decision: runtimeDecision}
	authority.mu.Unlock()
	return s.waitForApprovalDecision(ctx, waiter)
}

func (s *Service) waitForApprovalDecision(
	ctx context.Context,
	waiter <-chan approvalDecisionCompletion,
) (ApprovalDecisionResult, error) {
	timer := time.NewTimer(s.approvals.acknowledgementTimeout)
	defer timer.Stop()
	select {
	case completed := <-waiter:
		return completed.result, completed.err
	case <-ctx.Done():
		return ApprovalDecisionResult{}, &ApprovalError{Code: ApprovalErrorUnavailable}
	case <-timer.C:
		return ApprovalDecisionResult{}, &ApprovalError{Code: ApprovalErrorUnavailable}
	}
}

func (s *Service) expireApproval(pending *approvalPending) {
	if s.approvals == nil || pending == nil {
		return
	}
	authority := s.approvals
	authority.mu.Lock()
	if authority.pendingBySession[pending.record.AgentSessionID] == pending && pending.phase == approvalPhasePending {
		s.commitApprovalExpiryLocked(pending)
	}
	authority.mu.Unlock()
}

// commitApprovalExpiryLocked claims the first-writer-wins TTL transition and
// releases the waiting Runtime reverse request with exactly one Cancel. The
// authority remains non-actionable until Runtime confirms that response with
// the matching serverRequest/resolved notification. Only that acknowledgement
// may publish and finish the expired terminal.
func (s *Service) commitApprovalExpiryLocked(pending *approvalPending) bool {
	if pending == nil || s.approvals.pendingBySession[pending.record.AgentSessionID] != pending ||
		pending.phase != approvalPhasePending {
		return false
	}
	pending.phase = approvalPhaseTTLCommitted
	pending.handlerOutcome <- codex.CommandApprovalResult{Respond: true, Decision: "cancel"}
	return true
}

func (s *Service) resolveApprovalForRuntimeKey(generation, runtimeKey string) {
	if s.approvals == nil {
		return
	}
	authority := s.approvals
	authority.mu.Lock()
	pending := authority.pendingByRuntime[runtimeKey]
	if pending != nil && pending.runtimeGeneration == generation {
		if pending.phase == approvalPhaseTTLCommitted {
			s.discardApprovalLocked(pending)
		} else {
			s.resolveApprovalLocked(pending, approvalOutcomeElsewhere, "", "", codex.CommandApprovalResult{},
				&ApprovalError{Code: ApprovalErrorUnavailable})
		}
	}
	authority.mu.Unlock()
}

func (s *Service) resolveApprovalForItem(record Record, turnID, itemID string) {
	s.resolveApprovalForAuthority(record.AgentSessionID, turnID, itemID, false)
}

func (s *Service) resolveApprovalForTurn(record Record, turnID string) {
	s.resolveApprovalForAuthority(record.AgentSessionID, turnID, "", true)
}

func (s *Service) resolveApprovalForAuthority(sessionID, turnID, itemID string, wholeTurn bool) {
	if s.approvals == nil {
		return
	}
	authority := s.approvals
	authority.mu.Lock()
	pending := authority.pendingBySession[sessionID]
	if pending != nil && pending.record.ActiveTurnID == turnID && (wholeTurn || pending.itemID == itemID) {
		s.resolveApprovalLocked(pending, approvalOutcomeElsewhere, "", "", codex.CommandApprovalResult{},
			&ApprovalError{Code: ApprovalErrorUnavailable})
	}
	authority.mu.Unlock()
}

func (s *Service) clearApprovalSession(sessionID string) {
	if s.approvals == nil {
		return
	}
	authority := s.approvals
	authority.mu.Lock()
	if pending := authority.pendingBySession[sessionID]; pending != nil {
		if pending.phase == approvalPhaseTTLCommitted {
			s.discardApprovalLocked(pending)
		} else {
			s.resolveApprovalLocked(pending, approvalOutcomeElsewhere, "", "", codex.CommandApprovalResult{},
				&ApprovalError{Code: ApprovalErrorUnavailable})
		}
	}
	delete(authority.auditBySession, sessionID)
	authority.mu.Unlock()
}

func (s *Service) resolveApprovalLocked(
	pending *approvalPending,
	outcome, decisionID, decision string,
	handlerOutcome codex.CommandApprovalResult,
	waiterErr error,
) {
	if pending == nil || s.approvals.pendingBySession[pending.record.AgentSessionID] != pending {
		return
	}
	if pending.phase == approvalPhaseTTLCommitted {
		// TTL already won and its Cancel was committed. Only the matching Runtime
		// acknowledgement may publish its expired terminal. Later item/turn
		// cleanup cannot rewrite the winner or race ahead of the response ack.
		if outcome != approvalOutcomeExpired {
			return
		}
		outcome, decisionID, decision = approvalOutcomeExpired, "", ""
		handlerOutcome = codex.CommandApprovalResult{}
	}
	resolvedAt := time.Now().UTC()
	_ = s.publishApprovalResolved(pending.record, pending, outcome, decisionID, decision, resolvedAt)
	audit := approvalAuditRecord{
		approvalID: pending.approvalID, outcome: outcome,
		requestedAt: pending.requestedAt, resolvedAt: resolvedAt,
	}
	s.finishApprovalLocked(pending, audit)
	if handlerOutcome.Respond || handlerOutcome.Decision != "" {
		pending.handlerOutcome <- handlerOutcome
	} else if pending.phase == approvalPhasePending {
		pending.handlerOutcome <- codex.CommandApprovalResult{}
	}
	if waiterErr != nil {
		s.notifyDecisionWaitersLocked(pending, approvalDecisionCompletion{err: waiterErr})
	}
}

// discardApprovalLocked retires authority only when its Runtime generation,
// request context, or owning session is gone and a matching acknowledgement
// can no longer arrive. It deliberately emits neither a false expired terminal
// nor a second Runtime response.
func (s *Service) discardApprovalLocked(pending *approvalPending) {
	if pending == nil || s.approvals.pendingBySession[pending.record.AgentSessionID] != pending {
		return
	}
	delete(s.approvals.pendingBySession, pending.record.AgentSessionID)
	delete(s.approvals.pendingByRuntime, pending.runtimeKey)
	s.approvals.rememberResolvedRuntimeLocked(
		approvalRuntimeKeyDigest(pending.runtimeKey), pending.runtimeGeneration,
	)
}

func (s *Service) finishApprovalLocked(pending *approvalPending, audit approvalAuditRecord) {
	delete(s.approvals.pendingBySession, pending.record.AgentSessionID)
	delete(s.approvals.pendingByRuntime, pending.runtimeKey)
	s.approvals.rememberResolvedRuntimeLocked(
		approvalRuntimeKeyDigest(pending.runtimeKey), pending.runtimeGeneration,
	)
	records := append(s.approvals.auditBySession[pending.record.AgentSessionID], audit)
	if len(records) > approvalAuditLimit {
		records = append([]approvalAuditRecord(nil), records[len(records)-approvalAuditLimit:]...)
	}
	s.approvals.auditBySession[pending.record.AgentSessionID] = records
}

func (s *Service) notifyDecisionWaitersLocked(pending *approvalPending, completed approvalDecisionCompletion) {
	for _, waiter := range pending.decisionWaiters {
		select {
		case waiter <- completed:
		default:
		}
	}
	pending.decisionWaiters = nil
}

func (s *Service) expectedLiveCommandItem(record Record, turnID, itemID string) bool {
	key := v5ItemKey{sessionID: record.AgentSessionID, turnID: turnID, itemID: itemID}
	s.v5ItemsMu.Lock()
	state := s.v5Items[key]
	valid := state != nil && state.kind == "commandExecution" && !state.sealed
	s.v5ItemsMu.Unlock()
	return valid
}

func (pending *approvalPending) public() PendingApproval {
	return PendingApproval{
		ApprovalRequestID: pending.approvalID, Revision: approvalPendingRevision,
		TaskID: pending.record.TaskID, AgentSessionID: pending.record.AgentSessionID,
		CodexThreadID: pending.record.CodexThreadID, TurnID: pending.record.ActiveTurnID,
		ItemID: pending.itemID, ActionID: approvalActionID, WorkspaceScope: approvalWorkspaceScope,
		RequestedAt: pending.requestedAt, ExpiresAt: pending.expiresAt, TTLSeconds: approvalTTLSeconds,
	}
}

func approvalRequestFingerprint(request codex.CommandApprovalRequest, canonicalCwd string) string {
	environment := approvalRuntimeEnvironment
	canonical := struct {
		ItemID, ThreadID, TurnID, Command, ActionType, ActionCommand, Cwd, Environment string
		StartedAtMS                                                                    int64
	}{
		ItemID: request.ItemID, ThreadID: request.ThreadID, TurnID: request.TurnID,
		Command: request.Command, ActionType: request.CommandActions[0].Type,
		ActionCommand: request.CommandActions[0].Command, Cwd: canonicalCwd,
		Environment: environment, StartedAtMS: request.StartedAtMS,
	}
	encoded, _ := json.Marshal(canonical)
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:])
}

func validApprovalRuntimeRequestKey(value string) bool {
	if strings.HasPrefix(value, "s:") {
		return true
	}
	if !strings.HasPrefix(value, "n:") {
		return false
	}
	numeric := strings.TrimPrefix(value, "n:")
	parsed, err := strconv.ParseInt(numeric, 10, 64)
	return err == nil && numeric == strconv.FormatInt(parsed, 10)
}

func approvalRuntimeKeyDigest(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func validApprovalUUID(value string) bool {
	return validUUID(value) && value == strings.ToLower(value) &&
		strings.ReplaceAll(value, "-", "") != strings.Repeat("0", 32)
}

func approvalDecisionFingerprint(approvalID string, input ApprovalDecisionInput) string {
	canonical := struct {
		ApprovalID, StreamID, Decision string
		SchemaVersion                  int
		Revision                       int64
	}{
		ApprovalID: approvalID, StreamID: input.ExpectedStreamID,
		Decision: input.Decision, SchemaVersion: input.SchemaVersion, Revision: input.ExpectedRevision,
	}
	encoded, _ := json.Marshal(canonical)
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:])
}

func (s *Service) publishApprovalRequested(record Record, pending *approvalPending) (Event, error) {
	revision := approvalPendingRevision
	ttlSeconds := approvalTTLSeconds
	requestedAt, expiresAt := pending.requestedAt, pending.expiresAt
	event := Event{
		TurnID: record.ActiveTurnID, ItemID: pending.itemID, EventType: EventApprovalRequested,
		Payload: EventPayload{
			ItemType: "commandExecution", ApprovalRequestID: pending.approvalID, Revision: &revision,
			ActionID: approvalActionID, WorkspaceScope: approvalWorkspaceScope,
			Decisions:   &ApprovalDecisions{Primary: approvalDecisionAcceptOnce, Secondary: approvalDecisionCancelTurn},
			RequestedAt: &requestedAt, ExpiresAt: &expiresAt, TTLSeconds: &ttlSeconds,
		},
	}
	decorateEvent(record, &event)
	event.RequestID = ""
	return s.eventsV6.Publish(event)
}

func (s *Service) publishApprovalResolved(
	record Record,
	pending *approvalPending,
	outcome, decisionID, decision string,
	resolvedAt time.Time,
) error {
	revision := approvalResolvedRevision
	requestedAt, expiresAt := pending.requestedAt, pending.expiresAt
	event := Event{
		TurnID: record.ActiveTurnID, ItemID: pending.itemID, EventType: EventApprovalResolved,
		Payload: EventPayload{
			ItemType: "commandExecution", ApprovalRequestID: pending.approvalID, Revision: &revision,
			ActionID: approvalActionID, WorkspaceScope: approvalWorkspaceScope, Outcome: outcome,
			DecisionID: decisionID, Decision: decision, RequestedAt: &requestedAt,
			ExpiresAt: &expiresAt, ResolvedAt: &resolvedAt,
		},
	}
	decorateEvent(record, &event)
	event.RequestID = ""
	_, err := s.eventsV6.Publish(event)
	return err
}

func validateV6RetainedEvent(event Event) error {
	if event.SchemaVersion != EventSchemaVersionV6 || event.Sequence == 0 || event.OccurredAt.IsZero() ||
		!validUUID(event.EventID) || !validUUID(event.StreamID) || !validUUID(event.TaskID) ||
		!validUUID(event.AgentSessionID) || !validUUID(event.CodexThreadID) {
		return ErrEventLimitExceeded
	}
	for _, value := range []string{event.TraceID, event.RequestID, event.TenantID, event.UserID} {
		if value != "" && (utf8.RuneCountInString(value) > 256 || len(value) > 1024) {
			return ErrEventLimitExceeded
		}
	}
	if event.EventType != EventApprovalRequested && event.EventType != EventApprovalResolved {
		inherited := event
		inherited.SchemaVersion = EventSchemaVersionV5
		return validateV5Event(inherited)
	}
	if event.RequestID != "" || event.Terminal || !validUUID(event.TurnID) || event.ItemID == "" ||
		utf8.RuneCountInString(event.ItemID) > 256 || len(event.ItemID) > 1024 ||
		event.Payload.ItemType != "commandExecution" || !validUUID(event.Payload.ApprovalRequestID) ||
		event.Payload.Revision == nil || event.Payload.ActionID != approvalActionID ||
		event.Payload.WorkspaceScope != approvalWorkspaceScope || event.Payload.RequestedAt == nil ||
		event.Payload.ExpiresAt == nil || event.Payload.ExpiresAt.Sub(*event.Payload.RequestedAt) != approvalTTL {
		return ErrEventLimitExceeded
	}
	if event.EventType == EventApprovalRequested {
		if *event.Payload.Revision != approvalPendingRevision || event.Payload.Decisions == nil ||
			event.Payload.Decisions.Primary != approvalDecisionAcceptOnce ||
			event.Payload.Decisions.Secondary != approvalDecisionCancelTurn ||
			event.Payload.TTLSeconds == nil || *event.Payload.TTLSeconds != approvalTTLSeconds ||
			event.Payload.Outcome != "" || event.Payload.DecisionID != "" || event.Payload.Decision != "" ||
			event.Payload.ResolvedAt != nil ||
			!payloadShape(event.Payload,
				[]string{"item_type", "approval_request_id", "revision", "action_id", "workspace_scope", "decisions", "requested_at", "expires_at", "ttl_seconds"},
				"item_type", "approval_request_id", "revision", "action_id", "workspace_scope", "decisions", "requested_at", "expires_at", "ttl_seconds") {
			return ErrEventLimitExceeded
		}
		return nil
	}
	if *event.Payload.Revision != approvalResolvedRevision || event.Payload.ResolvedAt == nil ||
		event.Payload.Decisions != nil || event.Payload.TTLSeconds != nil {
		return ErrEventLimitExceeded
	}
	allowed := []string{"item_type", "approval_request_id", "revision", "action_id", "workspace_scope", "outcome", "requested_at", "expires_at", "resolved_at"}
	switch event.Payload.Outcome {
	case approvalOutcomeAccepted:
		if !validUUID(event.Payload.DecisionID) || event.Payload.Decision != approvalDecisionAcceptOnce {
			return ErrEventLimitExceeded
		}
		allowed = append(allowed, "decision_id", "decision")
	case approvalOutcomeCancelled:
		if !validUUID(event.Payload.DecisionID) || event.Payload.Decision != approvalDecisionCancelTurn {
			return ErrEventLimitExceeded
		}
		allowed = append(allowed, "decision_id", "decision")
	case approvalOutcomeExpired:
		if event.Payload.DecisionID != "" || event.Payload.Decision != "" || event.Payload.ResolvedAt.Before(*event.Payload.ExpiresAt) {
			return ErrEventLimitExceeded
		}
	case approvalOutcomeElsewhere:
		if event.Payload.DecisionID != "" || event.Payload.Decision != "" {
			return ErrEventLimitExceeded
		}
	default:
		return ErrEventLimitExceeded
	}
	if !payloadShape(event.Payload, allowed, allowed...) {
		return ErrEventLimitExceeded
	}
	return nil
}

var _ codex.CommandApprovalHandler = (*Service)(nil)
