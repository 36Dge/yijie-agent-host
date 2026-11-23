package codex

import (
	"context"
	"reflect"
	"time"

	broker "github.com/36Dge/yijie-agent-host/internal/contracts/marketbrokercontrol"
	wire "github.com/36Dge/yijie-agent-host/internal/contracts/markethost"
	"github.com/36Dge/yijie-agent-host/internal/marketbroker"
	"github.com/google/uuid"
)

// This private app-server callback freezes a call. It cannot authorize a send.
func (m *Manager) registerRuntimeMarketCall(ctx context.Context, thread, turn string, reg broker.NativeCallRegistration) (serverRequestResult, bool) {
	decline := func() (serverRequestResult, bool) { return marketNativeReply("decline", nil) }
	ctx, cancel := context.WithTimeout(ctx, time.Duration(broker.MaxUnboundWaitMs)*time.Millisecond)
	defer cancel()
	m.market.mu.Lock()
	state := m.market.turns[thread]
	if state == nil || state.closed || !state.prepared.NativeDispatch || state.prepared.Lease == nil {
		m.market.mu.Unlock()
		return decline()
	}
	ready := state.bindReady
	if state.turn == "" && ready != nil {
		m.market.mu.Unlock()
		select {
		case <-ready:
		case <-ctx.Done():
			return decline()
		}
		m.market.mu.Lock()
	}
	if state != m.market.turns[thread] || state.closed || state.turn != turn {
		m.market.mu.Unlock()
		return decline()
	}
	owner, binding := m.market.owner, state.prepared.Lease.Binding
	m.market.mu.Unlock()
	if owner == nil {
		return decline()
	}
	call, err := owner.RegisterNativeCall(ctx, broker.RegisterNativeCallRequest{SchemaVersion: 1, RequestId: broker.CanonicalId(uuid.NewString()), Method: "register_native_call",
		Payload: broker.RegisterNativeCallPayload{Binding: binding, NativeThreadId: broker.NativeId(thread), NativeTurnId: broker.NativeId(turn), Registration: reg}})
	if err != nil || call.Data.State != broker.CallStatePending || call.Data.RemainingTtlMs <= 0 {
		return decline()
	}
	view := wire.MarketApproval{Identity: call.Data.Identity, Review: call.Data.Review}
	if view.Identity.ServiceId == "kimi-web" && !state.prepared.WebAllowed {
		return decline()
	}
	if m.config.BuiltinMediaEnabled {
		if _, err := authorizeImageMethod(view, state.prepared.ImageRoute); err != nil {
			return decline()
		}
	}
	key, _ := nativeMarketScope(view, state.prepared.approvalAccounts[view.Identity.Reference])
	if key == nil {
		return decline()
	}
	return marketNativeReply("accept", map[string]any{"callRef": view.Identity.CallRef, "scope": key, "tool": nativeMarketToolName(view)})
}

func terminalDispatchPhase(phase string) bool {
	switch broker.Decision(phase) {
	case broker.DecisionNativeRuntime, broker.DecisionNativeRuntimeReject, broker.DecisionNativeFull, broker.DecisionNativeAuto, broker.DecisionNativeAutoReject, broker.DecisionNativeAlways, broker.DecisionNativeSession:
		return true
	}
	return false
}

// A manual callback can have a decision receipt before the Runtime has finished
// its own policy path. Seal that same receipt without changing the user's choice.
func (m *Manager) finishAnsweredMarketCall(ctx context.Context, owner *marketbroker.Owner, state *marketTurnState, call broker.PendingCall, envelope broker.NativeApprovalEnvelope) (serverRequestResult, bool) {
	decline := func() (serverRequestResult, bool) { return marketNativeReply("decline", nil) }
	if call.DecisionId == nil || call.Decision == nil || envelope.Persist != nil {
		return decline()
	}
	view := wire.MarketApproval{AgentSessionId: state.prepared.Lease.Binding.Context.AgentSessionId, Kind: "mcp_market", Identity: call.Identity, Review: call.Review, Revision: call.Revision, State: wire.ApprovalStateApproved}
	key, _ := nativeMarketScope(view, state.prepared.approvalAccounts[call.Identity.Reference])
	if key == nil || !sameMarketScopeKey(envelope.YijieScope, key) {
		return decline()
	}
	m.permissionCallbacks.mu.Lock()
	for _, entry := range m.permissionCallbacks.entries {
		if entry.market != nil && reflect.DeepEqual(entry.market.view.Identity, call.Identity) && entry.market.view.DecisionId != nil && *entry.market.view.DecisionId == *call.DecisionId {
			view = entry.market.view
			break
		}
	}
	m.permissionCallbacks.mu.Unlock()
	if call.State == broker.CallStateRejected || call.State == broker.CallStateCancelled {
		if envelope.DispatchRef == nil && (envelope.YijieNativePhase == "native_runtime_reject" || envelope.YijieNativePhase == "native_auto_reject") {
			return deniedMarketReply(call, view.ApprovalId)
		}
		return decline()
	}
	if call.State != broker.CallStateApproved || envelope.DispatchRef == nil || envelope.YijieNativePhase != "native_runtime" {
		return decline()
	}
	m.market.mu.Lock()
	valid := m.market.turns[state.thread] == state && !state.closed && state.turn == string(call.Identity.NativeTurnId)
	authorize := m.market.authorizeNative
	m.market.mu.Unlock()
	if !valid || authorize == nil || view.ApprovalId == "" || !authorize(ctx, view) {
		return decline()
	}
	out, err := owner.DecideCall(ctx, broker.DecideCallRequest{SchemaVersion: 1, RequestId: broker.CanonicalId(uuid.NewString()), Method: "decide_call", Payload: broker.DecideCallPayload{
		OperationId: *envelope.DispatchRef, NativeDispatchRef: envelope.DispatchRef, Identity: call.Identity, ExpectedRevision: call.Revision, ApprovalRef: view.ApprovalId, DecisionId: *call.DecisionId, Decision: *call.Decision}})
	if err != nil || out.Data.State != broker.CallStateApproved {
		return decline()
	}
	return marketNativeReply("accept", nil)
}

// The private Broker acknowledged the terminal state. Native refusals never
// send tools/call, so carry its existing identity and decision to the failed Item.
func deniedMarketReply(call broker.PendingCall, approval broker.CanonicalId) (serverRequestResult, bool) {
	if call.Decision == nil || call.DecisionId == nil || (call.State != broker.CallStateRejected && call.State != broker.CallStateCancelled) {
		return marketNativeReply("decline", nil)
	}
	i := call.Identity
	receipt := broker.GatewayCallReceipt{SchemaVersion: 1, CallRef: i.CallRef, ServiceId: i.ServiceId, Reference: i.Reference, NativeThreadId: i.NativeThreadId, NativeTurnId: i.NativeTurnId, TurnOperationId: i.Binding.TurnOperationId, SelectionDigest: broker.SelectionDigest(i.Binding.SelectionDigest), ToolName: i.ToolName, ArgsDigest: i.ArgsDigest, ArgsEncoding: i.ArgsEncoding,
		DecisionReceipt: &broker.GatewayDecisionReceipt{ApprovalId: approval, DecisionId: *call.DecisionId, Decision: *call.Decision}}
	if receipt.Validate() != nil {
		return marketNativeReply("decline", nil)
	}
	return marketNativeReply("accept", map[string]any{broker.GatewayResultKey: receipt})
}
