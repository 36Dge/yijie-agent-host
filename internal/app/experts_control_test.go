package app

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	expert "github.com/36Dge/yijie-agent-host/internal/contracts/experts"
	broker "github.com/36Dge/yijie-agent-host/internal/contracts/marketbrokercontrol"
	market "github.com/36Dge/yijie-agent-host/internal/contracts/markethost"
	selection "github.com/36Dge/yijie-agent-host/internal/contracts/marketselection"
	"github.com/google/uuid"
)

type feat158Control struct {
	MarketControlService
	receipt  expert.ApplyReceipt
	reads    int
	recovers int
}

func (s *feat158Control) ApplyExpertBinding(context.Context, expert.ApplyPayload) (expert.ApplyReceipt, error) {
	return s.receipt, nil
}
func (s *feat158Control) RecoverExpertBinding(context.Context, expert.PrivateBindingRecoverPayload) (expert.ApplyReceipt, error) {
	s.recovers++
	return s.receipt, nil
}
func (s *feat158Control) ExpertStatus(context.Context, expert.PrivateStatusPayload) (expert.PrivateStatus, error) {
	return expert.PrivateStatus{}, nil
}
func (s *feat158Control) PrepareExpertResources(context.Context, expert.PrivateResourcePayload) (expert.ResourceOperation, error) {
	return expert.ResourceOperation{}, nil
}
func (s *feat158Control) RemoveExpertResources(context.Context, expert.PrivateResourcePayload) (expert.ResourceOperation, error) {
	return expert.ResourceOperation{}, nil
}
func (s *feat158Control) ReadExpertResourceOperation(expert.PrivateResourceReadPayload) (expert.ResourceOperation, error) {
	s.reads++
	return expert.ResourceOperation{}, nil
}
func (s *feat158Control) RecoverExpertResources(_ context.Context, payload expert.PrivateResourceRecoverPayload) (expert.ResourceOperation, error) {
	s.recovers++
	return expert.ResourceOperation{OperationId: payload.Original.OperationId, Expert: payload.Original.Expert, Action: payload.Action, State: expert.OperationStateRejected}, nil
}
func (s *feat158Control) ReadExpertOperation(expert.PrivateOperationReadPayload) (expert.ApplyReceipt, error) {
	s.reads++
	return s.receipt, nil
}
func (s *feat158Control) RegisterExpertGrant(expert.PrivateGrantPayload) (expert.PrivateGrant, error) {
	return expert.PrivateGrant{}, nil
}
func (s *feat158Control) SubmitExpert(context.Context, market.SubmitPayload) (expert.PrivateReceipt, error) {
	return expert.PrivateReceipt{}, nil
}
func (s *feat158Control) ReadExpertSubmission(expert.SubmissionReadPayload) (expert.PrivateReceipt, error) {
	return expert.PrivateReceipt{}, nil
}
func TestFEAT158ExpertControlUsesGeneratedPrivateFamily(t *testing.T) {
	id := func() market.CanonicalId { return market.CanonicalId(uuid.NewString()) }
	host, local, session, op, requestID := id(), id(), id(), id(), id()
	scope := market.ScopeBinding{OwnerUserId: id(), TenantId: id(), NativeProcessEpoch: id(), AuthorizationRevision: 1, AuthorizationExpiresAtUnixMs: broker.UnixMilliseconds(time.Now().Add(time.Minute).UnixMilli())}
	request := expert.PrivateOperationReadRequest{SchemaVersion: 1, RequestId: requestID, Method: "expert_operation_read", Payload: expert.PrivateOperationReadPayload{HostInstanceId: host, Scope: scope, LocalSessionId: local, AgentSessionId: session, OperationId: op}}
	service := &feat158Control{receipt: expert.ApplyReceipt{OperationId: op, HostInstanceId: host, LocalSessionId: local, AgentSessionId: session, NativeThreadId: expert.NativeId(uuid.NewString()), ExpectedBindingRevision: 0, BindingRevision: 1, State: expert.OperationStateSucceeded}}
	frame, e := json.Marshal(request)
	if e != nil {
		t.Fatal(e)
	}
	result, rid, e := routeMarketControl(context.Background(), service, frame)
	if e != nil || result.Validate() != nil || rid == nil || *rid != requestID || service.reads != 1 {
		t.Fatal(result, rid, e)
	}
	response, ok := result.(expert.PrivateOperationReadResponse)
	if !ok || response.Data.OperationId != op {
		t.Fatal("expert family was projected to an old receipt")
	}
}
func TestFEAT158PrivateMethodBudgetsPreserveExistingFiveSeconds(t *testing.T) {
	for method, want := range map[string]time.Duration{"hello": 5 * time.Second, "grant_register": 5 * time.Second, "expert_apply_binding": 20 * time.Second, "expert_binding_recover": 20 * time.Second, "expert_resources_recover": 25 * time.Second, "expert_submit": 25 * time.Second, "expert_submission_read": 5 * time.Second} {
		frame, _ := json.Marshal(map[string]string{"method": method})
		if got := expertControlTimeout(frame); got != want {
			t.Fatalf("%s: %s", method, got)
		}
	}
}

func TestFEAT158ExplicitRecoveryRoutesKeepSourcePayloadAndOriginalIdentity(t *testing.T) {
	id := func() market.CanonicalId { return market.CanonicalId(uuid.NewString()) }
	host, originalHost, local, session, op, turn := id(), id(), id(), id(), id(), id()
	scope := market.ScopeBinding{OwnerUserId: id(), TenantId: id(), NativeProcessEpoch: id(), AuthorizationRevision: 1, AuthorizationExpiresAtUnixMs: broker.UnixMilliseconds(time.Now().Add(time.Minute).UnixMilli())}
	digest, e := selection.SelectionDigest(string(turn), nil)
	if e != nil {
		t.Fatal(e)
	}
	original := expert.ApplyPayload{OperationId: op, HostInstanceId: originalHost, Scope: scope, LocalSessionId: local, AgentSessionId: session, NativeThreadId: expert.NativeId(uuid.NewString()), BindingRevision: 1, Intent: market.ModelIntent{ProfileId: "kimi-k3-max-v1", ExpectedRevision: 1}, PermissionMode: market.PermissionModeAsk, MarketSnapshot: selection.SelectionSnapshot{SchemaVersion: 1, TurnOperationId: turn, Selection: selection.Selection{}, SelectionDigest: selection.SelectionDigestValue(digest)}}
	svc := &feat158Control{receipt: expert.ApplyReceipt{OperationId: op, HostInstanceId: originalHost, LocalSessionId: local, AgentSessionId: session, NativeThreadId: original.NativeThreadId, BindingRevision: 1, State: expert.OperationStateSucceeded}}
	requests := []marketValidated{
		expert.PrivateBindingRecoverRequest{SchemaVersion: 1, RequestId: id(), Method: "expert_binding_recover", Payload: expert.PrivateBindingRecoverPayload{HostInstanceId: host, Scope: scope, Original: original}},
		expert.PrivateResourceRecoverRequest{SchemaVersion: 1, RequestId: id(), Method: "expert_resources_recover", Payload: expert.PrivateResourceRecoverPayload{HostInstanceId: host, Scope: scope, Original: expert.PrivateResourcePayload{HostInstanceId: originalHost, Scope: scope, OperationId: op, Expert: expert.ExpertSelection{ExpertId: "jd-ops-expert", Version: "0.1.0"}}, Action: "prepare"}},
	}
	for _, request := range requests {
		if e := request.Validate(); e != nil {
			t.Fatal(e)
		}
		frame, _ := json.Marshal(request)
		response, _, e := routeMarketControl(context.Background(), svc, frame)
		if e != nil || response.Validate() != nil {
			t.Fatal(response, e)
		}
	}
	if svc.recovers != 2 || svc.reads != 0 {
		t.Fatal("explicit recovery routed through ordinary read", svc.recovers, svc.reads)
	}
}
