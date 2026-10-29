package codex

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"time"

	broker "github.com/36Dge/yijie-agent-host/internal/contracts/marketbrokercontrol"
	wire "github.com/36Dge/yijie-agent-host/internal/contracts/markethost"
	"github.com/google/uuid"
)

type marketApprovalCallback struct {
	view      wire.MarketApproval
	scope     broker.ScopeBinding
	operation string
	deciding  bool
}

func marketDecisionExpiry(view wire.MarketApproval) int64 {
	if view.DecisionExpiresAtUnixMs != nil {
		return *view.DecisionExpiresAtUnixMs
	}
	return view.ExpiresAtUnixMs
}

func marketApprovalState(status string) wire.ApprovalState {
	value := wire.ApprovalState(status)
	if value.Validate() != nil {
		return wire.ApprovalStateUnavailable
	}
	return value
}

func (m *Manager) handleMarketElicitation(ctx context.Context, request serverRequest) (serverRequestResult, bool) {
	var p struct {
		ThreadID string          `json:"threadId"`
		TurnID   string          `json:"turnId"`
		Server   string          `json:"serverName"`
		Mode     string          `json:"mode"`
		Meta     json.RawMessage `json:"_meta"`
		Schema   struct {
			Type       string                     `json:"type"`
			Properties map[string]json.RawMessage `json:"properties"`
			Required   []string                   `json:"required"`
		} `json:"requestedSchema"`
	}
	if json.Unmarshal(request.params, &p) != nil || p.Server != marketServerName {
		return serverRequestResult{}, false
	}
	decline := func() (serverRequestResult, bool) {
		return serverRequestResult{respond: true, value: map[string]any{"action": "decline", "content": nil, "_meta": nil}}, true
	}
	var meta broker.ElicitationMetadata
	if !m.MarketEnabled() || !request.exactEnvelope || len(request.params) > broker.MaxFrameBytes || p.Mode != "form" || p.Schema.Type != "object" || p.Schema.Properties == nil || len(p.Schema.Properties) != 0 || len(p.Schema.Required) != 0 || json.Unmarshal(p.Meta, &meta) != nil {
		return decline()
	}
	m.market.mu.Lock()
	state := m.market.turns[p.ThreadID]
	if state == nil || state.closed || state.turn != p.TurnID || state.prepared.Lease == nil {
		m.market.mu.Unlock()
		return decline()
	}
	lease := *state.prepared.Lease
	scope := state.prepared.Scope
	owner := m.market.owner
	m.market.mu.Unlock()
	bounded, cancel := context.WithTimeout(ctx, 5*time.Second)
	pending, err := owner.PendingCall(bounded, broker.PendingCallRequest{SchemaVersion: 1, RequestId: broker.CanonicalId(uuid.NewString()), Method: "pending_call", Payload: broker.PendingCallPayload{Binding: lease.Binding, NativeThreadId: broker.NativeId(p.ThreadID), NativeTurnId: broker.NativeId(p.TurnID), CallRef: meta.YijieMarketCallRef}})
	cancel()
	if err != nil || pending.Data.State != broker.CallStatePending || pending.Data.RemainingTtlMs <= 0 || !reflect.DeepEqual(pending.Data.Identity.Binding, lease.Binding) {
		return decline()
	}
	entry := &runtimeApprovalCallback{view: RuntimeApproval{ID: uuid.NewString(), Kind: "mcp_market", Summary: pending.Data.Review.Title, Reason: pending.Data.Review.Summary, Status: "pending"}, threadID: p.ThreadID, turnID: p.TurnID, requestKey: request.idKey, resolve: make(chan string, 1), written: make(chan struct{})}
	view := wire.MarketApproval{ApprovalId: wire.CanonicalId(entry.view.ID), AgentSessionId: lease.Binding.Context.AgentSessionId, Kind: "mcp_market", Revision: pending.Data.Revision, State: wire.ApprovalStatePending, Identity: pending.Data.Identity, Review: pending.Data.Review, ExpiresAtUnixMs: time.Now().UnixMilli() + int64(pending.Data.RemainingTtlMs)}
	if pending.Data.DecisionExpiresAtUnixMs != nil && pending.Data.ExecutionExpiresAtUnixMs != nil {
		a, d := int64(*pending.Data.DecisionExpiresAtUnixMs), int64(*pending.Data.ExecutionExpiresAtUnixMs)
		view.DecisionExpiresAtUnixMs, view.ExecutionExpiresAtUnixMs = &a, &d
		view.ExpiresAtUnixMs = d
	}
	if view.Validate() != nil || marketDecisionExpiry(view) <= time.Now().UnixMilli() {
		return decline()
	}
	entry.market = &marketApprovalCallback{view: view, scope: scope}
	if err = m.addRuntimeApproval(entry); err != nil {
		return decline()
	}
	timer := time.NewTimer(time.Until(time.UnixMilli(marketDecisionExpiry(view))))
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			m.finishRuntimeApproval(entry, ctx.Err())
			return serverRequestResult{}, true
		case <-timer.C:
			m.permissionCallbacks.mu.Lock()
			waiting := entry.market.view.ExecutionExpiresAtUnixMs != nil && (entry.market.deciding || entry.decision != "")
			executionEnd := entry.market.view.ExpiresAtUnixMs
			m.permissionCallbacks.mu.Unlock()
			if waiting && time.Now().UnixMilli() < executionEnd {
				timer.Reset(time.Until(time.UnixMilli(executionEnd)))
				continue
			}
			m.finishRuntimeApproval(entry, errors.New("market approval expired"))
			return decline()
		case decision := <-entry.resolve:
			action := "decline"
			var content any
			if decision == "approve_once" {
				action = "accept"
				content = map[string]any{}
			} else if decision == "cancel" {
				action = "cancel"
			}
			return serverRequestResult{respond: true, value: map[string]any{"action": action, "content": content, "_meta": nil}, onResponseWritten: func(e error) { m.finishRuntimeApproval(entry, e) }}, true
		}
	}
}

func (m *Manager) ListMarketApprovals(thread string) []wire.MarketApproval {
	c := &m.permissionCallbacks
	c.mu.Lock()
	defer c.mu.Unlock()
	views := make([]wire.MarketApproval, 0)
	for _, entry := range c.entries {
		if entry.threadID == thread && entry.market != nil {
			view := entry.market.view
			if view.State == wire.ApprovalStatePending && !entry.market.deciding && entry.decision == "" && time.Now().UnixMilli() >= marketDecisionExpiry(view) {
				view.State = wire.ApprovalStateExpired
			}
			views = append(views, view)
		}
	}
	return views
}

func (m *Manager) DecideMarketApproval(ctx context.Context, thread string, input wire.ApprovalDecidePayload) (wire.MarketApproval, error) {
	c := &m.permissionCallbacks
	c.mu.Lock()
	var entry *runtimeApprovalCallback
	for _, candidate := range c.entries {
		if candidate.threadID == thread && candidate.view.ID == string(input.ApprovalId) && candidate.market != nil {
			entry = candidate
			break
		}
	}
	if entry == nil {
		c.mu.Unlock()
		return wire.MarketApproval{}, errors.New("market approval not found")
	}
	value := entry.market
	if value.view.DecisionId != nil {
		if *value.view.DecisionId != input.DecisionId || value.view.Decision == nil || *value.view.Decision != input.Decision || value.operation != string(input.OperationId) {
			c.mu.Unlock()
			return wire.MarketApproval{}, errors.New("market approval decision conflict")
		}
		c.mu.Unlock()
		select {
		case <-entry.written:
			c.mu.Lock()
			defer c.mu.Unlock()
			return value.view, entry.writtenErr
		case <-ctx.Done():
			return wire.MarketApproval{}, ctx.Err()
		}
	}
	if value.deciding || value.view.State != wire.ApprovalStatePending || value.view.Revision != input.ExpectedRevision || value.view.Identity.CallRef != input.CallRef || value.view.AgentSessionId != input.AgentSessionId || value.scope.OwnerUserId != input.Scope.OwnerUserId || value.scope.TenantId != input.Scope.TenantId || value.scope.NativeProcessEpoch != input.Scope.NativeProcessEpoch || time.Now().UnixMilli() >= marketDecisionExpiry(value.view) || c.activeTurns[thread] != entry.turnID {
		c.mu.Unlock()
		return wire.MarketApproval{}, errors.New("market approval unavailable")
	}
	value.deciding = true
	identity := value.view.Identity
	c.mu.Unlock()
	owner, err := m.MarketOwner()
	if err == nil {
		var response broker.DecideCallResponse
		response, err = owner.DecideCall(ctx, broker.DecideCallRequest{SchemaVersion: 1, RequestId: broker.CanonicalId(uuid.NewString()), Method: "decide_call", Payload: broker.DecideCallPayload{OperationId: input.OperationId, Identity: identity, ExpectedRevision: input.ExpectedRevision, ApprovalRef: input.ApprovalId, DecisionId: input.DecisionId, Decision: input.Decision}})
		if err == nil && (!reflect.DeepEqual(response.Data.Identity, identity) || response.Data.DecisionId == nil || *response.Data.DecisionId != input.DecisionId || response.Data.Decision == nil || *response.Data.Decision != input.Decision) {
			err = errors.New("market decision correlation unavailable")
		}
		if err == nil {
			c.mu.Lock()
			value.view.Revision = response.Data.Revision
			c.mu.Unlock()
		}
	}
	c.mu.Lock()
	value.deciding = false
	if err != nil {
		c.mu.Unlock()
		return wire.MarketApproval{}, err
	}
	value.operation = string(input.OperationId)
	decisionID, decision := input.DecisionId, input.Decision
	value.view.DecisionId = &decisionID
	value.view.Decision = &decision
	if entry.view.Status != "pending" {
		c.mu.Unlock()
		return wire.MarketApproval{}, errors.New("market callback already ended")
	}
	native := "reject"
	if input.Decision == broker.DecisionApproveOnce {
		native = "approve_once"
	} else if input.Decision == broker.DecisionCancel {
		native = "cancel"
	}
	entry.decision = native
	entry.resolve <- native
	c.mu.Unlock()
	select {
	case <-entry.written:
		c.mu.Lock()
		defer c.mu.Unlock()
		return value.view, entry.writtenErr
	case <-ctx.Done():
		return wire.MarketApproval{}, ctx.Err()
	}
}
