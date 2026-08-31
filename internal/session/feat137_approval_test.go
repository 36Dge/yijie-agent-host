package session

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/santhosh-tekuri/jsonschema/v6"

	"github.com/36Dge/yijie-agent-host/internal/codex"
)

const (
	feat137ItemID            = "feat137-command-item"
	feat137GenerationCanary  = "FEAT137_RUNTIME_GENERATION_CANARY"
	feat137NextGeneration    = "FEAT137_NEXT_RUNTIME_GENERATION"
	feat137RequestIDCanary   = "s:FEAT137_RUNTIME_REQUEST_ID_CANARY"
	feat137WorkspaceCanary   = "FEAT137_PRIVATE_WORKSPACE_CANARY"
	feat137DecisionID        = "019c0123-4567-7abc-8123-456789abcdf0"
	feat137SecondDecisionID  = "019c0123-4567-7abc-8123-456789abcdf1"
	feat137WrongThreadID     = "019c0123-4567-7abc-8123-456789abcdf2"
	feat137ZeroUUID          = "00000000-0000-0000-0000-000000000000"
	feat137AckTestRequestKey = "n:7"
)

type feat137ApprovalHarness struct {
	service *Service
	store   *Store
	v6      *EventHub
	record  Record
	request codex.CommandApprovalRequest
}

type feat137DecisionCompletion struct {
	result ApprovalDecisionResult
	err    error
}

func TestFEAT137DecisionWaitsForMatchingTypedAckReplayAndIdempotency(t *testing.T) {
	harness := newFEAT137ApprovalHarness(t)
	request := harness.request
	request.RequestIDKey = feat137AckTestRequestKey
	handlerResult, cancelHandler := startFEAT137ApprovalHandler(harness.service, request)
	defer cancelHandler()

	snapshot := awaitFEAT137Pending(t, harness.service)
	pending := snapshot.Pending[0]
	if pending.Revision != approvalPendingRevision || pending.TTLSeconds != approvalTTLSeconds ||
		pending.ExpiresAt.Sub(pending.RequestedAt) != approvalTTL ||
		snapshot.SnapshotAt.Before(pending.RequestedAt) || !snapshot.SnapshotAt.Before(pending.ExpiresAt) {
		t.Fatalf("pending authority drifted: %+v", pending)
	}

	if replay := harness.service.HandleCommandApproval(context.Background(), request); replay.Respond {
		t.Fatalf("equivalent pending replay produced a second response: %+v", replay)
	}
	typedDistinct := request
	typedDistinct.RequestIDKey = "s:7"
	if result := harness.service.HandleCommandApproval(context.Background(), typedDistinct); !result.Respond || result.Decision != "cancel" {
		t.Fatalf("typed-distinct request did not fail closed once: %+v", result)
	}

	input := ApprovalDecisionInput{
		SchemaVersion: approvalSchemaVersion, DecisionID: feat137DecisionID,
		ExpectedStreamID: snapshot.StreamID, ExpectedRevision: approvalPendingRevision,
		Decision: approvalDecisionAcceptOnce,
	}
	decisionDone := make(chan feat137DecisionCompletion, 1)
	go func() {
		result, err := harness.service.DecideApproval(context.Background(), testSessionID, pending.ApprovalRequestID, input)
		decisionDone <- feat137DecisionCompletion{result: result, err: err}
	}()

	handler := awaitFEAT137HandlerResult(t, handlerResult)
	if !handler.Respond || handler.Decision != "accept" {
		t.Fatalf("decision did not map to one Runtime accept: %+v", handler)
	}
	assertFEAT137DecisionStillWaiting(t, decisionDone)

	for _, wrong := range []codex.CommandApprovalResolved{
		{RuntimeGeneration: "wrong-generation", RequestIDKey: request.RequestIDKey, ThreadID: testThreadID},
		{RuntimeGeneration: request.RuntimeGeneration, RequestIDKey: "s:7", ThreadID: testThreadID},
		{RuntimeGeneration: request.RuntimeGeneration, RequestIDKey: request.RequestIDKey, ThreadID: feat137WrongThreadID},
	} {
		harness.service.HandleCommandApprovalResolved(wrong)
		assertFEAT137DecisionStillWaiting(t, decisionDone)
	}
	harness.service.HandleCommandApprovalResolved(codex.CommandApprovalResolved{
		RuntimeGeneration: request.RuntimeGeneration,
		RequestIDKey:      request.RequestIDKey,
		ThreadID:          testThreadID,
	})
	completion := awaitFEAT137Decision(t, decisionDone)
	if completion.err != nil || completion.result.Outcome != approvalOutcomeAccepted ||
		completion.result.Decision != approvalDecisionAcceptOnce || completion.result.Revision != approvalResolvedRevision {
		t.Fatalf("matching acknowledgement did not complete decision: result=%+v err=%v", completion.result, completion.err)
	}

	replayed, err := harness.service.DecideApproval(context.Background(), testSessionID, pending.ApprovalRequestID, input)
	if err != nil || replayed != completion.result {
		t.Fatalf("identical decision retry was not idempotent: result=%+v err=%v", replayed, err)
	}
	conflict := input
	conflict.Decision = approvalDecisionCancelTurn
	assertFEAT137ApprovalError(t,
		func() error {
			_, conflictErr := harness.service.DecideApproval(context.Background(), testSessionID, pending.ApprovalRequestID, conflict)
			return conflictErr
		}(),
		ApprovalErrorDecisionConflict,
	)
	newDecision := input
	newDecision.DecisionID = feat137SecondDecisionID
	if approvalDecisionFingerprint(pending.ApprovalRequestID, input) !=
		approvalDecisionFingerprint(pending.ApprovalRequestID, newDecision) {
		t.Fatal("decision_id incorrectly entered the canonical decision-content fingerprint")
	}
	assertFEAT137ApprovalError(t,
		func() error {
			_, resolvedErr := harness.service.DecideApproval(context.Background(), testSessionID, pending.ApprovalRequestID, newDecision)
			return resolvedErr
		}(),
		ApprovalErrorAlreadyResolved,
	)

	approvalEventsBefore := feat137ApprovalEvents(t, harness.service)
	if len(approvalEventsBefore) != 2 {
		t.Fatalf("expected one requested and one resolved event, got %+v", approvalEventsBefore)
	}
	if replay := harness.service.HandleCommandApproval(context.Background(), request); replay.Respond {
		t.Fatalf("resolved replay produced a second Runtime response: %+v", replay)
	}
	drift := request
	drift.StartedAtMS++
	if replay := harness.service.HandleCommandApproval(context.Background(), drift); replay.Respond {
		t.Fatalf("resolved replay drift produced a second Runtime response: %+v", replay)
	}
	if after := feat137ApprovalEvents(t, harness.service); len(after) != len(approvalEventsBefore) {
		t.Fatalf("resolved replay reopened approval projection: before=%d after=%d", len(approvalEventsBefore), len(after))
	}

	audit := latestFEAT137Audit(t, harness.service)
	if audit.outcome != approvalOutcomeAccepted || audit.requestedAt.IsZero() || audit.resolvedAt.IsZero() ||
		!audit.requestedAt.Equal(pending.RequestedAt) || !audit.resolvedAt.Equal(completion.result.ResolvedAt) ||
		audit.resolvedAt.Before(audit.requestedAt) {
		t.Fatalf("resolved audit timestamps/outcome drifted: %+v", audit)
	}
	validateFEAT137V6Events(t, feat137V6Replay(t, harness.service))
}

func TestFEAT137CancelDecisionCommittedWritersShareOneAckAndRuntimeResponse(t *testing.T) {
	harness := newFEAT137ApprovalHarness(t)
	request := harness.request
	request.RequestIDKey = "n:8"
	handlerResult, cancelHandler := startFEAT137ApprovalHandler(harness.service, request)
	defer cancelHandler()
	snapshot := awaitFEAT137Pending(t, harness.service)
	pending := snapshot.Pending[0]
	input := ApprovalDecisionInput{
		SchemaVersion: approvalSchemaVersion, DecisionID: feat137DecisionID,
		ExpectedStreamID: snapshot.StreamID, ExpectedRevision: approvalPendingRevision,
		Decision: approvalDecisionCancelTurn,
	}

	first := make(chan feat137DecisionCompletion, 1)
	go func() {
		result, err := harness.service.DecideApproval(context.Background(), testSessionID, pending.ApprovalRequestID, input)
		first <- feat137DecisionCompletion{result: result, err: err}
	}()
	handler := awaitFEAT137HandlerResult(t, handlerResult)
	if !handler.Respond || handler.Decision != "cancel" {
		t.Fatalf("cancel_current_turn did not map to one Runtime cancel: %+v", handler)
	}

	second := make(chan feat137DecisionCompletion, 1)
	go func() {
		result, err := harness.service.DecideApproval(context.Background(), testSessionID, pending.ApprovalRequestID, input)
		second <- feat137DecisionCompletion{result: result, err: err}
	}()
	awaitFEAT137DecisionWaiters(t, harness.service, 2)
	assertFEAT137DecisionStillWaiting(t, first)
	assertFEAT137DecisionStillWaiting(t, second)

	differentFingerprint := input
	differentFingerprint.Decision = approvalDecisionAcceptOnce
	_, err := harness.service.DecideApproval(
		context.Background(), testSessionID, pending.ApprovalRequestID, differentFingerprint,
	)
	assertFEAT137ApprovalError(t, err, ApprovalErrorDecisionConflict)
	differentWriter := input
	differentWriter.DecisionID = feat137SecondDecisionID
	_, err = harness.service.DecideApproval(
		context.Background(), testSessionID, pending.ApprovalRequestID, differentWriter,
	)
	assertFEAT137ApprovalError(t, err, ApprovalErrorAlreadyResolved)
	select {
	case duplicate := <-handlerResult:
		t.Fatalf("committed retry produced a second Runtime result: %+v", duplicate)
	default:
	}

	harness.service.HandleCommandApprovalResolved(codex.CommandApprovalResolved{
		RuntimeGeneration: request.RuntimeGeneration,
		RequestIDKey:      request.RequestIDKey,
		ThreadID:          testThreadID,
	})
	firstResult := awaitFEAT137Decision(t, first)
	secondResult := awaitFEAT137Decision(t, second)
	if firstResult.err != nil || secondResult.err != nil || firstResult.result != secondResult.result {
		t.Fatalf("same committed writer did not share one acknowledged result: first=%+v second=%+v",
			firstResult, secondResult)
	}
	result := firstResult.result
	if result.SchemaVersion != approvalSchemaVersion || result.ApprovalRequestID != pending.ApprovalRequestID ||
		result.DecisionID != input.DecisionID || result.StreamID != snapshot.StreamID ||
		result.Revision != approvalResolvedRevision || result.Decision != approvalDecisionCancelTurn ||
		result.Outcome != approvalOutcomeCancelled || result.ResolvedAt.IsZero() {
		t.Fatalf("cancel decision response correlation drifted: %+v", result)
	}
	events := feat137ApprovalEvents(t, harness.service)
	if len(events) != 2 || events[1].Payload.Outcome != approvalOutcomeCancelled ||
		events[1].Payload.DecisionID != input.DecisionID ||
		events[1].Payload.Decision != approvalDecisionCancelTurn ||
		events[1].Payload.ResolvedAt == nil || !events[1].Payload.ResolvedAt.Equal(result.ResolvedAt) {
		t.Fatalf("cancelled_current_turn event correlation drifted: %+v", events)
	}
	validateFEAT137V6Events(t, feat137V6Replay(t, harness.service))
}

func TestFEAT137TTLWaitsForMatchingRuntimeAckBeforeExpiredProjection(t *testing.T) {
	harness := newFEAT137ApprovalHarness(t)
	request := harness.request
	request.RequestIDKey = feat137RequestIDCanary
	pending := installExpiredFEAT137Pending(t, harness, request)

	snapshot, err := harness.service.PendingApprovals(testSessionID)
	if err != nil || len(snapshot.Pending) != 0 {
		t.Fatalf("overdue pending snapshot remained actionable: snapshot=%+v err=%v", snapshot, err)
	}
	result := awaitFEAT137HandlerResult(t, pending.handlerOutcome)
	if !result.Respond || result.Decision != "cancel" {
		t.Fatalf("TTL did not send exactly one Runtime cancel: %+v", result)
	}
	if events := feat137ApprovalEvents(t, harness.service); len(events) != 1 ||
		events[0].EventType != EventApprovalRequested {
		t.Fatalf("TTL published expired before Runtime acknowledged Cancel: %+v", events)
	}
	harness.service.approvals.mu.Lock()
	phase := pending.phase
	auditCount := len(harness.service.approvals.auditBySession[testSessionID])
	harness.service.approvals.mu.Unlock()
	if phase != approvalPhaseTTLCommitted || auditCount != 0 {
		t.Fatalf("TTL authority was not committed and non-terminal before Runtime ack: phase=%q audit=%d", phase, auditCount)
	}

	input := ApprovalDecisionInput{
		SchemaVersion: approvalSchemaVersion, DecisionID: feat137DecisionID,
		ExpectedStreamID: snapshot.StreamID, ExpectedRevision: approvalPendingRevision,
		Decision: approvalDecisionAcceptOnce,
	}
	_, decisionErr := harness.service.DecideApproval(
		context.Background(), testSessionID, pending.approvalID, input,
	)
	assertFEAT137ApprovalError(t, decisionErr, ApprovalErrorExpired)
	harness.service.HandleCommandApprovalResolved(codex.CommandApprovalResolved{
		RuntimeGeneration: request.RuntimeGeneration,
		RequestIDKey:      request.RequestIDKey,
		ThreadID:          feat137WrongThreadID,
	})
	if events := feat137ApprovalEvents(t, harness.service); len(events) != 1 {
		t.Fatalf("wrong Runtime acknowledgement resolved TTL authority: %+v", events)
	}

	harness.service.HandleCommandApprovalResolved(codex.CommandApprovalResolved{
		RuntimeGeneration: request.RuntimeGeneration,
		RequestIDKey:      request.RequestIDKey,
		ThreadID:          testThreadID,
	})
	if events := feat137ApprovalEvents(t, harness.service); len(events) != 1 {
		t.Fatalf("Runtime ack raced ahead of response-write commit: %+v", events)
	}
	harness.service.HandleCommandApprovalResponseWritten(codex.CommandApprovalResponseWriteResult{
		RuntimeGeneration: request.RuntimeGeneration,
		RequestIDKey:      request.RequestIDKey,
		ThreadID:          testThreadID,
		Succeeded:         true,
	})
	select {
	case duplicate := <-pending.handlerOutcome:
		t.Fatalf("TTL emitted a duplicate Runtime response: %+v", duplicate)
	default:
	}

	audit := latestFEAT137Audit(t, harness.service)
	if audit.outcome != approvalOutcomeExpired || !audit.requestedAt.Equal(pending.requestedAt) ||
		audit.resolvedAt.Before(pending.expiresAt) {
		t.Fatalf("expired audit is incomplete: %+v", audit)
	}
	events := feat137V6Replay(t, harness.service)
	validateFEAT137V6Events(t, events)
	approvalEvents := feat137ApprovalEvents(t, harness.service)
	if len(approvalEvents) != 2 || approvalEvents[1].Payload.Outcome != approvalOutcomeExpired {
		t.Fatalf("matching Runtime acknowledgement did not publish one expired terminal: %+v", approvalEvents)
	}
	harness.service.HandleCommandApprovalResolved(codex.CommandApprovalResolved{
		RuntimeGeneration: request.RuntimeGeneration,
		RequestIDKey:      request.RequestIDKey,
		ThreadID:          testThreadID,
	})
	harness.service.resolveApprovalForItem(harness.record, testTurnID, request.ItemID)
	if after := feat137ApprovalEvents(t, harness.service); len(after) != len(approvalEvents) {
		t.Fatalf("late acknowledgement or cleanup duplicated expired terminal: before=%d after=%+v", len(approvalEvents), after)
	}
	encoded, err := json.Marshal(struct {
		Events []Event
		Audit  approvalAuditRecord
	}{Events: events, Audit: audit})
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{
		feat137GenerationCanary,
		strings.TrimPrefix(feat137RequestIDCanary, "s:"),
		approvalRuntimeCommand,
		harness.record.Cwd,
		feat137WorkspaceCanary,
	} {
		if bytes.Contains(encoded, []byte(forbidden)) {
			t.Fatalf("v6 approval projection leaked raw authority %q: %s", forbidden, encoded)
		}
	}
}

func TestFEAT137TTLWinnerSurvivesCleanupBeforeRuntimeAck(t *testing.T) {
	harness := newFEAT137ApprovalHarness(t)
	request := harness.request
	request.RequestIDKey = feat137RequestIDCanary
	pending := installExpiredFEAT137Pending(t, harness, request)

	harness.service.expireApproval(pending)
	if queued := len(pending.handlerOutcome); queued != 1 {
		t.Fatalf("TTL Cancel was not retained in the buffered responder channel: queued=%d", queued)
	}
	if events := feat137ApprovalEvents(t, harness.service); len(events) != 1 {
		t.Fatalf("TTL published a terminal before response consumption: %+v", events)
	}

	harness.service.resolveApprovalForItem(harness.record, testTurnID, request.ItemID)
	events := feat137ApprovalEvents(t, harness.service)
	if len(events) != 1 || events[0].EventType != EventApprovalRequested {
		t.Fatalf("cleanup raced ahead of Runtime ack or rewrote the TTL winner: %+v", events)
	}
	harness.service.approvals.mu.Lock()
	phase := pending.phase
	auditCount := len(harness.service.approvals.auditBySession[testSessionID])
	harness.service.approvals.mu.Unlock()
	if phase != approvalPhaseTTLCommitted || auditCount != 0 {
		t.Fatalf("cleanup changed committed TTL authority before Runtime ack: phase=%q audit=%d", phase, auditCount)
	}
	result := awaitFEAT137HandlerResult(t, pending.handlerOutcome)
	if !result.Respond || result.Decision != "cancel" {
		t.Fatalf("TTL winner did not commit one Runtime Cancel: %+v", result)
	}
	harness.service.HandleCommandApprovalResponseWritten(codex.CommandApprovalResponseWriteResult{
		RuntimeGeneration: request.RuntimeGeneration,
		RequestIDKey:      request.RequestIDKey,
		ThreadID:          testThreadID,
		Succeeded:         true,
	})
	if afterWrite := feat137ApprovalEvents(t, harness.service); len(afterWrite) != 1 {
		t.Fatalf("response-write commit resolved TTL authority before Runtime ack: %+v", afterWrite)
	}
	harness.service.HandleCommandApprovalResolved(codex.CommandApprovalResolved{
		RuntimeGeneration: request.RuntimeGeneration,
		RequestIDKey:      request.RequestIDKey,
		ThreadID:          testThreadID,
	})
	afterAck := feat137ApprovalEvents(t, harness.service)
	if len(afterAck) != 2 || afterAck[1].Payload.Outcome != approvalOutcomeExpired {
		t.Fatalf("matching Runtime ack did not finish the preserved TTL winner: %+v", afterAck)
	}
	if audit := latestFEAT137Audit(t, harness.service); audit.outcome != approvalOutcomeExpired {
		t.Fatalf("matching Runtime ack did not preserve the TTL winner in audit: %+v", audit)
	}
	harness.service.resolveApprovalForTurn(harness.record, testTurnID)
	if afterCleanup := feat137ApprovalEvents(t, harness.service); len(afterCleanup) != len(afterAck) {
		t.Fatalf("late cleanup duplicated acknowledged expiry: before=%d after=%+v", len(afterAck), afterCleanup)
	}
	select {
	case duplicate := <-pending.handlerOutcome:
		t.Fatalf("cleanup sent a second Runtime response after TTL won: %+v", duplicate)
	default:
	}
	validateFEAT137V6Events(t, feat137V6Replay(t, harness.service))
}

func TestFEAT137TTLResponseWriteFailureDiscardsWithoutForgedTerminal(t *testing.T) {
	harness := newFEAT137ApprovalHarness(t)
	request := harness.request
	request.RequestIDKey = feat137RequestIDCanary
	pending := installExpiredFEAT137Pending(t, harness, request)

	harness.service.expireApproval(pending)
	result := awaitFEAT137HandlerResult(t, pending.handlerOutcome)
	if !result.Respond || result.Decision != "cancel" {
		t.Fatalf("TTL did not hand one Cancel to the responder: %+v", result)
	}
	harness.service.HandleCommandApprovalResponseWritten(codex.CommandApprovalResponseWriteResult{
		RuntimeGeneration: request.RuntimeGeneration,
		RequestIDKey:      request.RequestIDKey,
		ThreadID:          testThreadID,
		Succeeded:         false,
	})
	if events := feat137ApprovalEvents(t, harness.service); len(events) != 1 ||
		events[0].EventType != EventApprovalRequested {
		t.Fatalf("failed response write forged an expired terminal: %+v", events)
	}
	harness.service.approvals.mu.Lock()
	pendingCount := len(harness.service.approvals.pendingBySession)
	auditCount := len(harness.service.approvals.auditBySession[testSessionID])
	harness.service.approvals.mu.Unlock()
	if pendingCount != 0 || auditCount != 0 {
		t.Fatalf("failed response write retained actionable or forged audit state: pending=%d audit=%d", pendingCount, auditCount)
	}
	harness.service.HandleCommandApprovalResolved(codex.CommandApprovalResolved{
		RuntimeGeneration: request.RuntimeGeneration,
		RequestIDKey:      request.RequestIDKey,
		ThreadID:          testThreadID,
	})
	if events := feat137ApprovalEvents(t, harness.service); len(events) != 1 {
		t.Fatalf("late Runtime ack reopened failed-write authority: %+v", events)
	}
}

func TestFEAT137V6ApprovalProjectionRejectsOversizedOptionalIdentity(t *testing.T) {
	harness := newFEAT137ApprovalHarness(t)
	pending := &approvalPending{
		record: harness.record, approvalID: feat137DecisionID, itemID: harness.request.ItemID,
		requestedAt: time.Now().UTC(), phase: approvalPhasePending,
	}
	pending.expiresAt = pending.requestedAt.Add(approvalTTL)

	for _, mutate := range []func(*Record){
		func(record *Record) { record.Trace.TraceID = strings.Repeat("t", 257) },
		func(record *Record) { record.Trace.TenantID = strings.Repeat("租", 257) },
		func(record *Record) { record.Trace.UserID = strings.Repeat("u", 1025) },
	} {
		record := harness.record
		mutate(&record)
		if _, err := harness.service.publishApprovalRequested(record, pending); !errors.Is(err, ErrEventLimitExceeded) {
			t.Fatalf("oversized optional identity was not rejected: %v", err)
		}
	}
}

func TestFEAT137GenerationCleanupResolvesElsewhereWithoutResponse(t *testing.T) {
	harness := newFEAT137ApprovalHarness(t)
	handlerResult, cancelHandler := startFEAT137ApprovalHandler(harness.service, harness.request)
	defer cancelHandler()
	pending := awaitFEAT137Pending(t, harness.service).Pending[0]

	harness.service.HandleCommandApprovalGenerationClosed(harness.request.RuntimeGeneration)
	if result := awaitFEAT137HandlerResult(t, handlerResult); result.Respond || result.Decision != "" {
		t.Fatalf("generation cleanup responded after authority resolved elsewhere: %+v", result)
	}
	snapshot, err := harness.service.PendingApprovals(testSessionID)
	if err != nil || len(snapshot.Pending) != 0 {
		t.Fatalf("generation cleanup left an actionable pending: snapshot=%+v err=%v", snapshot, err)
	}
	audit := latestFEAT137Audit(t, harness.service)
	if audit.outcome != approvalOutcomeElsewhere || !audit.requestedAt.Equal(pending.RequestedAt) ||
		audit.resolvedAt.IsZero() || audit.resolvedAt.Before(audit.requestedAt) {
		t.Fatalf("resolved_elsewhere audit is incomplete: %+v", audit)
	}
	events := feat137ApprovalEvents(t, harness.service)
	if len(events) != 2 || events[1].Payload.Outcome != approvalOutcomeElsewhere ||
		events[1].Payload.DecisionID != "" || events[1].Payload.Decision != "" {
		t.Fatalf("cleanup projection drifted: %+v", events)
	}
	validateFEAT137V6Events(t, feat137V6Replay(t, harness.service))
}

func TestFEAT137ClosedGenerationSuppressesLateRequestAndKeepsNewGenerationIndependent(t *testing.T) {
	harness := newFEAT137ApprovalHarness(t)
	harness.service.HandleCommandApprovalGenerationClosed(feat137GenerationCanary)
	if result := harness.service.HandleCommandApproval(context.Background(), harness.request); result.Respond {
		t.Fatalf("closed generation accepted a late request: %+v", result)
	}
	if events := feat137ApprovalEvents(t, harness.service); len(events) != 0 {
		t.Fatalf("closed generation projected a late request: %+v", events)
	}

	harness.service.approvals.mu.Lock()
	_, oldClosed := harness.service.approvals.closedRuntime[feat137GenerationCanary]
	harness.service.approvals.mu.Unlock()
	if !oldClosed {
		t.Fatal("generation close did not retain its permanent closed marker")
	}

	newRequest := harness.request
	newRequest.RuntimeGeneration = feat137NextGeneration
	handlerResult, cancelHandler := startFEAT137ApprovalHandler(harness.service, newRequest)
	defer cancelHandler()
	pending := awaitFEAT137Pending(t, harness.service)
	if len(pending.Pending) != 1 {
		t.Fatalf("new generation did not receive independent authority: %+v", pending)
	}
	harness.service.HandleCommandApprovalGenerationClosed(feat137NextGeneration)
	if result := awaitFEAT137HandlerResult(t, handlerResult); result.Respond {
		t.Fatalf("new generation cleanup emitted a Runtime response: %+v", result)
	}
	harness.service.approvals.mu.Lock()
	_, oldClosed = harness.service.approvals.closedRuntime[feat137GenerationCanary]
	_, newClosed := harness.service.approvals.closedRuntime[feat137NextGeneration]
	closedCount := len(harness.service.approvals.closedRuntime)
	harness.service.approvals.mu.Unlock()
	if !oldClosed || !newClosed || closedCount != 2 {
		t.Fatalf("closed generation markers drifted: old=%v new=%v count=%d", oldClosed, newClosed, closedCount)
	}
	validateFEAT137V6Events(t, feat137V6Replay(t, harness.service))
}

func TestFEAT137GenerationCloseWinsAdmissionValidationRace(t *testing.T) {
	harness := newFEAT137ApprovalHarness(t)
	entered := make(chan struct{})
	release := make(chan struct{})
	defer func() {
		select {
		case <-release:
		default:
			close(release)
		}
	}()
	harness.service.approvals.mu.Lock()
	harness.service.approvals.admissionBarrier = func() {
		close(entered)
		<-release
	}
	harness.service.approvals.mu.Unlock()

	handlerResult, cancelHandler := startFEAT137ApprovalHandler(harness.service, harness.request)
	defer cancelHandler()
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("approval handler did not reach the controlled admission barrier")
	}
	harness.service.HandleCommandApprovalGenerationClosed(feat137GenerationCanary)
	close(release)
	if result := awaitFEAT137HandlerResult(t, handlerResult); result.Respond {
		t.Fatalf("request validated across generation close produced a response: %+v", result)
	}

	harness.service.approvals.mu.Lock()
	harness.service.approvals.admissionBarrier = nil
	_, closed := harness.service.approvals.closedRuntime[feat137GenerationCanary]
	pendingCount := len(harness.service.approvals.pendingBySession)
	harness.service.approvals.mu.Unlock()
	if !closed || pendingCount != 0 {
		t.Fatalf("close/validation race reopened authority: closed=%v pending=%d", closed, pendingCount)
	}
	if events := feat137ApprovalEvents(t, harness.service); len(events) != 0 {
		t.Fatalf("close/validation race emitted an approval projection: %+v", events)
	}
}

func TestFEAT137InvalidRequestsAndCanonicalUUIDsFailClosed(t *testing.T) {
	remote := "remote"
	for name, mutate := range map[string]func(*codex.CommandApprovalRequest){
		"malformed typed request key": func(request *codex.CommandApprovalRequest) { request.RequestIDKey = "n:01" },
		"wrong command":               func(request *codex.CommandApprovalRequest) { request.Command = "git status" },
		"wrong action":                func(request *codex.CommandApprovalRequest) { request.CommandActions[0].Type = "read" },
		"wrong cwd":                   func(request *codex.CommandApprovalRequest) { request.Cwd = t.TempDir() },
		"foreign environment":         func(request *codex.CommandApprovalRequest) { request.EnvironmentID = &remote },
	} {
		t.Run(name, func(t *testing.T) {
			harness := newFEAT137ApprovalHarness(t)
			request := harness.request
			request.CommandActions = append([]codex.CommandApprovalAction(nil), request.CommandActions...)
			mutate(&request)
			result := harness.service.HandleCommandApproval(context.Background(), request)
			if !result.Respond || result.Decision != "cancel" {
				t.Fatalf("invalid request did not fail closed: %+v", result)
			}
			if len(feat137ApprovalEvents(t, harness.service)) != 0 {
				t.Fatal("invalid request created an approval projection")
			}
			if request.RequestIDKey == harness.request.RequestIDKey {
				if duplicate := harness.service.HandleCommandApproval(context.Background(), request); duplicate.Respond {
					t.Fatalf("invalid request replay produced a second response: %+v", duplicate)
				}
			}
		})
	}

	harness := newFEAT137ApprovalHarness(t)
	valid := ApprovalDecisionInput{
		SchemaVersion: approvalSchemaVersion, DecisionID: feat137DecisionID,
		ExpectedStreamID: feat137WrongThreadID, ExpectedRevision: approvalPendingRevision,
		Decision: approvalDecisionAcceptOnce,
	}
	for _, test := range []struct {
		name, sessionID, approvalID string
		mutate                      func(*ApprovalDecisionInput)
	}{
		{name: "missing decision", sessionID: testSessionID, approvalID: feat137WrongThreadID, mutate: func(input *ApprovalDecisionInput) { input.DecisionID = "" }},
		{name: "zero session", sessionID: feat137ZeroUUID, approvalID: feat137WrongThreadID},
		{name: "zero approval", sessionID: testSessionID, approvalID: feat137ZeroUUID},
		{name: "zero stream", sessionID: testSessionID, approvalID: feat137WrongThreadID, mutate: func(input *ApprovalDecisionInput) { input.ExpectedStreamID = feat137ZeroUUID }},
		{name: "noncanonical decision", sessionID: testSessionID, approvalID: feat137WrongThreadID, mutate: func(input *ApprovalDecisionInput) { input.DecisionID = strings.ToUpper(feat137DecisionID) }},
	} {
		t.Run(test.name, func(t *testing.T) {
			input := valid
			if test.mutate != nil {
				test.mutate(&input)
			}
			_, err := harness.service.DecideApproval(
				context.Background(), test.sessionID, test.approvalID, input,
			)
			assertFEAT137ApprovalError(t, err, ApprovalErrorInvalidRequest)
		})
	}
	if _, err := harness.service.PendingApprovals(feat137ZeroUUID); err == nil {
		t.Fatal("zero session UUID was accepted by pending snapshot")
	} else {
		assertFEAT137ApprovalError(t, err, ApprovalErrorInvalidRequest)
	}
}

func TestFEAT137RuntimeReplayRetentionHasNoUncontractedCardinalityCutoff(t *testing.T) {
	harness := newFEAT137ApprovalHarness(t)
	authority := harness.service.approvals
	const requestCount = 130
	requestKeys := make([]string, 0, requestCount)
	for index := 0; index < requestCount; index++ {
		requestKey := "n:" + strconv.Itoa(index)
		if index%2 == 1 {
			requestKey = "s:alternate-" + strconv.Itoa(index)
		}
		requestKeys = append(requestKeys, requestKey)
		request := harness.request
		request.RequestIDKey = requestKey
		request.Command = "git status --short"
		result := harness.service.HandleCommandApproval(context.Background(), request)
		if !result.Respond || result.Decision != "cancel" {
			t.Fatalf("new key %d did not receive its one fail-closed response: %+v", index+1, result)
		}
	}
	if events := feat137ApprovalEvents(t, harness.service); len(events) != 0 {
		t.Fatalf("ineligible replay-retention requests created an approval projection: %+v", events)
	}

	authority.mu.Lock()
	retainedMap := len(authority.resolvedRuntime)
	retainedDigests := len(authority.resolvedRuntimeDigests[feat137GenerationCanary])
	authority.mu.Unlock()
	if retainedMap != requestCount || retainedDigests != requestCount {
		t.Fatalf("Runtime replay retention lost a key: map=%d digests=%d want=%d",
			retainedMap, retainedDigests, requestCount)
	}

	for _, requestKey := range []string{requestKeys[0], requestKeys[1], requestKeys[128], requestKeys[129]} {
		request := harness.request
		request.RequestIDKey = requestKey
		request.Command = "git status --short"
		if replay := harness.service.HandleCommandApproval(context.Background(), request); replay.Respond {
			t.Fatalf("resolved typed key %q reopened after more than 128 requests: %+v", requestKey, replay)
		}
	}

	harness.service.HandleCommandApprovalGenerationClosed(feat137GenerationCanary)
	authority.mu.Lock()
	_, closed := authority.closedRuntime[feat137GenerationCanary]
	remainingMap := len(authority.resolvedRuntime)
	remainingDigests := len(authority.resolvedRuntimeDigests[feat137GenerationCanary])
	authority.mu.Unlock()
	if !closed || remainingMap != 0 || remainingDigests != 0 {
		t.Fatalf("generation close did not clear replay retention: closed=%v map=%d digests=%d",
			closed, remainingMap, remainingDigests)
	}
}

func newFEAT137ApprovalHarness(t *testing.T) *feat137ApprovalHarness {
	t.Helper()
	root := t.TempDir()
	workspace := filepath.Join(root, feat137WorkspaceCanary)
	if err := os.Mkdir(workspace, 0o700); err != nil {
		t.Fatal(err)
	}
	workspace, err := canonicalDirectory(workspace)
	if err != nil {
		t.Fatal(err)
	}
	store, err := OpenStore(filepath.Join(root, "host-home"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.Reserve(Record{TaskID: testTaskID, AgentSessionID: testSessionID, Cwd: workspace}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.BindThread(
		testSessionID, testThreadID, "runtime-session", codex.MiniMaxModel, codex.MiniMaxProviderID,
	); err != nil {
		t.Fatal(err)
	}
	record, err := store.BindTurn(testSessionID, testTurnID)
	if err != nil {
		t.Fatal(err)
	}
	v5 := NewEventHubVersion(EventSchemaVersionV5, 64, 8)
	v6 := NewEventHubVersion(EventSchemaVersionV6, 64, 8)
	service := NewService(
		&fakeRuntime{}, store, NewEventHub(64, 8), nil,
		WithV5Events(v5), WithV6Approvals(v6, 2*time.Second),
	)
	service.HandleNotification(RuntimeNotificationItemStarted, rawJSON(t, map[string]any{
		"threadId": testThreadID,
		"turnId":   testTurnID,
		"item": map[string]any{
			"id": feat137ItemID, "type": "commandExecution", "status": "inProgress", "cwd": record.Cwd,
		},
	}))
	if !service.expectedLiveCommandItem(record, testTurnID, feat137ItemID) {
		_, v5Replay, _, cancelV5, v5Err := v5.Subscribe(testSessionID, "", 0)
		if cancelV5 != nil {
			cancelV5()
		}
		_, v6Replay, _, cancelV6, v6Err := v6.Subscribe(testSessionID, "", 0)
		if cancelV6 != nil {
			cancelV6()
		}
		t.Fatalf("failed to establish live command authority: v5=%+v v5err=%v v6=%+v v6err=%v",
			v5Replay, v5Err, v6Replay, v6Err)
	}
	request := codex.CommandApprovalRequest{
		RuntimeGeneration: feat137GenerationCanary,
		RequestIDKey:      feat137RequestIDCanary,
		ThreadID:          testThreadID,
		TurnID:            testTurnID,
		ItemID:            feat137ItemID,
		StartedAtMS:       1723456789000,
		Command:           approvalRuntimeCommand,
		CommandActions: []codex.CommandApprovalAction{{
			Type: "unknown", Command: approvalRuntimeCommand,
		}},
		Cwd: record.Cwd,
	}
	if _, _, eligible := service.validateCommandApprovalRequest(request); !eligible {
		t.Fatal("canonical FEAT-137 test request is not eligible")
	}
	return &feat137ApprovalHarness{service: service, store: store, v6: v6, record: record, request: request}
}

func startFEAT137ApprovalHandler(
	service *Service,
	request codex.CommandApprovalRequest,
) (<-chan codex.CommandApprovalResult, context.CancelFunc) {
	ctx, cancel := context.WithCancel(context.Background())
	completed := make(chan codex.CommandApprovalResult, 1)
	go func() { completed <- service.HandleCommandApproval(ctx, request) }()
	return completed, cancel
}

func installExpiredFEAT137Pending(
	t *testing.T,
	harness *feat137ApprovalHarness,
	request codex.CommandApprovalRequest,
) *approvalPending {
	t.Helper()
	requestedAt := time.Now().UTC().Add(-approvalTTL - time.Second)
	expiresAt := requestedAt.Add(approvalTTL)
	approvalID, err := newUUID()
	if err != nil {
		t.Fatal(err)
	}
	runtimeKey := request.RuntimeGeneration + "\x00" + request.RequestIDKey
	pending := &approvalPending{
		record: harness.record, runtimeGeneration: request.RuntimeGeneration, runtimeKey: runtimeKey,
		requestFingerprint: approvalRequestFingerprint(request, harness.record.Cwd),
		approvalID:         approvalID, itemID: request.ItemID,
		requestedAt: requestedAt, expiresAt: expiresAt, monotonicDeadline: expiresAt,
		phase: approvalPhasePending, handlerOutcome: make(chan codex.CommandApprovalResult, 1),
	}
	published, err := harness.service.publishApprovalRequested(harness.record, pending)
	if err != nil {
		t.Fatal(err)
	}
	pending.streamID = published.StreamID
	harness.service.approvals.mu.Lock()
	harness.service.approvals.pendingByRuntime[runtimeKey] = pending
	harness.service.approvals.pendingBySession[testSessionID] = pending
	harness.service.approvals.mu.Unlock()
	return pending
}

func awaitFEAT137Pending(t *testing.T, service *Service) PendingApprovalSnapshot {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		snapshot, err := service.PendingApprovals(testSessionID)
		if err != nil {
			t.Fatal(err)
		}
		if len(snapshot.Pending) == 1 {
			return snapshot
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("timed out waiting for FEAT-137 pending approval")
	return PendingApprovalSnapshot{}
}

func awaitFEAT137HandlerResult(t *testing.T, completed <-chan codex.CommandApprovalResult) codex.CommandApprovalResult {
	t.Helper()
	select {
	case result := <-completed:
		return result
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for FEAT-137 Runtime response disposition")
		return codex.CommandApprovalResult{}
	}
}

func assertFEAT137DecisionStillWaiting(t *testing.T, completed <-chan feat137DecisionCompletion) {
	t.Helper()
	select {
	case result := <-completed:
		t.Fatalf("decision completed before matching acknowledgement: %+v", result)
	case <-time.After(15 * time.Millisecond):
	}
}

func awaitFEAT137DecisionWaiters(t *testing.T, service *Service, count int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		service.approvals.mu.Lock()
		pending := service.approvals.pendingBySession[testSessionID]
		got := 0
		if pending != nil {
			got = len(pending.decisionWaiters)
		}
		service.approvals.mu.Unlock()
		if got == count {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("timed out waiting for %d committed decision waiters", count)
}

func awaitFEAT137Decision(t *testing.T, completed <-chan feat137DecisionCompletion) feat137DecisionCompletion {
	t.Helper()
	select {
	case result := <-completed:
		return result
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for acknowledged FEAT-137 decision")
		return feat137DecisionCompletion{}
	}
}

func latestFEAT137Audit(t *testing.T, service *Service) approvalAuditRecord {
	t.Helper()
	service.approvals.mu.Lock()
	defer service.approvals.mu.Unlock()
	records := service.approvals.auditBySession[testSessionID]
	if len(records) == 0 {
		t.Fatal("FEAT-137 resolved audit is empty")
	}
	return records[len(records)-1]
}

func feat137V6Replay(t *testing.T, service *Service) []Event {
	t.Helper()
	_, replay, _, cancel, err := service.SubscribeEventsV6(testSessionID, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	return replay
}

func feat137ApprovalEvents(t *testing.T, service *Service) []Event {
	t.Helper()
	var filtered []Event
	for _, event := range feat137V6Replay(t, service) {
		if event.EventType == EventApprovalRequested || event.EventType == EventApprovalResolved {
			filtered = append(filtered, event)
		}
	}
	return filtered
}

func compileFEAT137V6Contract(t *testing.T) *jsonschema.Schema {
	t.Helper()
	_, sourceFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate FEAT-137 test source")
	}
	compiler := jsonschema.NewCompiler()
	compiler.UseRegexpEngine(compileFEAT136ECMAScriptRegexp)
	contract, err := compiler.Compile(filepath.Clean(filepath.Join(
		filepath.Dir(sourceFile), "..", "..", "api", "jsonschema", "agent-session-event-v6.schema.json",
	)))
	if err != nil {
		t.Fatalf("compile AgentSessionEventV6 JSON Schema: %v", err)
	}
	return contract
}

func validateFEAT137V6Events(t *testing.T, events []Event) {
	t.Helper()
	contract := compileFEAT137V6Contract(t)
	for _, event := range events {
		encoded, err := json.Marshal(event)
		if err != nil {
			t.Fatal(err)
		}
		instance, err := jsonschema.UnmarshalJSON(bytes.NewReader(encoded))
		if err != nil {
			t.Fatal(err)
		}
		if err := contract.Validate(instance); err != nil {
			t.Fatalf("v6 event violates frozen contract: %v\n%s", err, encoded)
		}
	}
}

func assertFEAT137ApprovalError(t *testing.T, err error, code string) {
	t.Helper()
	var approvalErr *ApprovalError
	if !errors.As(err, &approvalErr) || approvalErr.Code != code {
		t.Fatalf("approval error = %v, want %s", err, code)
	}
}
