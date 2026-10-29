package codex

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	broker "github.com/36Dge/yijie-agent-host/internal/contracts/marketbrokercontrol"
	host "github.com/36Dge/yijie-agent-host/internal/contracts/markethost"
	"github.com/36Dge/yijie-agent-host/internal/marketbroker"
	"github.com/google/uuid"
)

const marketServerName = "yijie_market"

type marketPreparedKey struct{}
type MarketPrepared struct {
	Lease    *broker.Lease
	Config   map[string]any
	Scope    broker.ScopeBinding
	Snapshot broker.SelectionSnapshot
	Process  broker.ProcessBinding
}
type marketTurnState struct {
	prepared      *MarketPrepared
	thread, turn  string
	bindOperation broker.CanonicalId
	terminal      map[string]bool
	closed        bool
}

func WithMarketPrepared(ctx context.Context, p *MarketPrepared) context.Context {
	return context.WithValue(ctx, marketPreparedKey{}, p)
}
func marketPreparedFrom(ctx context.Context) *MarketPrepared {
	p, _ := ctx.Value(marketPreparedKey{}).(*MarketPrepared)
	return p
}

func (m *Manager) MarketProcess() (broker.ProcessBinding, error) {
	if !m.MarketEnabled() || !m.Snapshot().Ready {
		return broker.ProcessBinding{}, errors.New("market Runtime unavailable")
	}
	m.market.mu.Lock()
	defer m.market.mu.Unlock()
	if m.market.owner == nil || m.market.gatewayURL == "" {
		return broker.ProcessBinding{}, marketbroker.ErrNotStarted
	}
	return m.market.process, nil
}

func (m *Manager) PrepareMarket(ctx context.Context, scope broker.ScopeBinding, submission host.Submission, sessionID string, threadID *string) (*MarketPrepared, error) {
	// Submit owns the total deadline. Admission is local configuration plus
	// one worker prepare, never a fresh provider operation or network probe.
	budget := m.config.RequestTimeout
	if budget > 10*time.Second {
		budget = 10 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, budget)
	defer cancel()
	process, err := m.MarketProcess()
	if err != nil {
		return nil, err
	}
	if submission.Validate() != nil || scope.Validate() != nil {
		return nil, errors.New("invalid market submission")
	}
	if len(submission.Snapshot.Selection) > 0 && submission.PermissionMode != "ask" {
		return nil, errors.New("market permission mode unavailable")
	}
	owner, err := m.MarketOwner()
	if err != nil {
		return nil, err
	}
	// Submission.Validate already verifies the frozen service/reference shape.
	// The worker resolves each exact ref through its same-owner live registry:
	// the Native Enable path owns qualification and its frozen credential
	// binding. OAuth success or historical SQL observations cannot do so here.
	servers, err := m.marketDisabledServers(ctx, submission.Cwd)
	if err != nil {
		return nil, err
	}
	p := &MarketPrepared{Config: map[string]any{"mcp_servers": servers}, Scope: scope, Snapshot: submission.Snapshot, Process: process}
	if len(submission.Snapshot.Selection) == 0 {
		return p, nil
	}
	var nativeThread *broker.NativeId
	if threadID != nil {
		id := broker.NativeId(*threadID)
		nativeThread = &id
	}
	webVersion, mcpTimeout := broker.WebRetrievalVersion, int64(broker.WebMcpToolTimeoutMs)
	// This owner prepare must complete before Submit dispatches turn/start.
	// Worker time at prepare is a conservative lower bound for the MCP timer.
	lease, e := owner.Prepare(ctx, broker.PrepareRequest{SchemaVersion: 1, RequestId: broker.CanonicalId(uuid.NewString()), Method: "prepare", Payload: broker.PreparePayload{WebRetrievalVersion: &webVersion, McpToolTimeoutMs: &mcpTimeout, OperationId: broker.CanonicalId(uuid.NewString()), Context: broker.ContextBinding{Process: process, Scope: scope, AgentSessionId: broker.CanonicalId(sessionID), NativeThreadId: nativeThread}, Snapshot: submission.Snapshot, TtlMs: broker.RequestedTtlMs(broker.MaxLeaseTtlMs)}})
	if e != nil {
		return nil, e
	}
	p.Lease = &lease.Data
	m.market.mu.Lock()
	gateway := m.market.gatewayURL
	m.market.mu.Unlock()
	servers[marketServerName] = map[string]any{"url": gateway + "/" + string(lease.Data.Binding.CapabilityRef), "bearer_token_env_var": marketbroker.RuntimeCapabilityEnvironment, "enabled": true, "default_tools_approval_mode": "auto", "startup_timeout_sec": 30, "tool_timeout_sec": broker.WebMcpToolTimeoutMs / 1000, "supports_parallel_tool_calls": false}
	return p, nil
}

func (m *Manager) BeginMarketTurn(thread string, p *MarketPrepared) error {
	if p == nil || thread == "" {
		return errors.New("market turn binding unavailable")
	}
	m.market.mu.Lock()
	defer m.market.mu.Unlock()
	if m.market.turns == nil {
		m.market.turns = make(map[string]*marketTurnState)
	}
	if old := m.market.turns[thread]; old != nil && !old.closed {
		return errors.New("market previous turn is still live")
	}
	if len(m.market.turns) >= 512 && m.market.turns[thread] == nil {
		return errors.New("market thread capacity unavailable")
	}
	m.market.turns[thread] = &marketTurnState{prepared: p, thread: thread, bindOperation: broker.CanonicalId(uuid.NewString()), terminal: make(map[string]bool)}
	return nil
}

func (m *Manager) BindMarketTurn(ctx context.Context, thread, turn string) error {
	m.market.mu.Lock()
	defer m.market.mu.Unlock()
	state := m.market.turns[thread]
	if state == nil {
		return nil
	}
	if state.turn != "" {
		if state.turn == turn {
			return nil
		}
		return errors.New("market actual turn mismatch")
	}
	if state.closed {
		return errors.New("market turn is revoked")
	}
	state.turn = turn
	if state.prepared.Lease != nil {
		if state.terminal[turn] {
			return m.revokeMarketStateLocked(ctx, state, broker.RevokeReasonTurnTerminal)
		}
		lease := state.prepared.Lease
		reply, err := m.market.owner.BindTurn(ctx, broker.BindTurnRequest{SchemaVersion: 1, RequestId: broker.CanonicalId(uuid.NewString()), Method: "bind_turn", Payload: broker.BindTurnPayload{OperationId: state.bindOperation, Binding: lease.Binding, ExpectedRevision: lease.Revision, NativeThreadId: broker.NativeId(thread), NativeTurnId: broker.NativeId(turn)}})
		if err != nil {
			state.closed = true
			return err
		}
		state.prepared.Lease = &reply.Data
	}
	if state.terminal[turn] {
		state.closed = true
	}
	return nil
}

func (m *Manager) revokeMarketStateLocked(ctx context.Context, state *marketTurnState, reason broker.RevokeReason) error {
	state.closed = true
	if state.prepared.Lease == nil {
		return nil
	}
	lease := state.prepared.Lease
	status, err := m.market.owner.Status(ctx, broker.StatusRequest{SchemaVersion: 1, RequestId: broker.CanonicalId(uuid.NewString()), Method: "status", Payload: broker.StatusPayload{Binding: lease.Binding}})
	if err != nil {
		return err
	}
	if status.Data.State == broker.LeaseStateRevoked || status.Data.State == broker.LeaseStateExpired {
		state.prepared.Lease = &status.Data
		return nil
	}
	revoked, err := m.market.owner.Revoke(ctx, broker.RevokeRequest{SchemaVersion: 1, RequestId: broker.CanonicalId(uuid.NewString()), Method: "revoke", Payload: broker.RevokePayload{OperationId: broker.CanonicalId(uuid.NewString()), Binding: lease.Binding, ExpectedRevision: status.Data.Revision, Reason: reason}})
	if err != nil {
		return err
	}
	state.prepared.Lease = &revoked.Data
	return nil
}

func (m *Manager) ClearMarketThread(ctx context.Context, thread string) error {
	m.market.mu.Lock()
	defer m.market.mu.Unlock()
	if state := m.market.turns[thread]; state != nil {
		return m.revokeMarketStateLocked(ctx, state, broker.RevokeReasonSelectionChanged)
	}
	return nil
}

func (m *Manager) RevokeMarketAuthority(ctx context.Context, scope broker.ScopeBinding, installation *broker.CanonicalId) error {
	m.market.mu.Lock()
	defer m.market.mu.Unlock()
	for _, state := range m.market.turns {
		old := state.prepared.Scope
		if old.OwnerUserId != scope.OwnerUserId || old.TenantId != scope.TenantId || old.NativeProcessEpoch != scope.NativeProcessEpoch {
			continue
		}
		match := installation == nil
		for _, ref := range state.prepared.Snapshot.Selection {
			if installation != nil && ref.InstallationId == *installation {
				match = true
			}
		}
		if match {
			if err := m.revokeMarketStateLocked(ctx, state, broker.RevokeReasonAuthorizationChanged); err != nil {
				return err
			}
		}
	}
	return nil
}

func (m *Manager) observeMarketNotification(method string, raw json.RawMessage) {
	if !m.MarketEnabled() || (method != "turn/started" && method != "turn/completed") {
		return
	}
	var event struct {
		ThreadID string `json:"threadId"`
		Turn     struct {
			ID string `json:"id"`
		} `json:"turn"`
	}
	if json.Unmarshal(raw, &event) != nil || event.ThreadID == "" || event.Turn.ID == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), m.config.RequestTimeout)
	defer cancel()
	if method == "turn/started" {
		if err := m.BindMarketTurn(ctx, event.ThreadID, event.Turn.ID); err != nil {
			m.logger.Warn("market turn binding unavailable", "failure_code", "market_binding_failed")
		}
		return
	}
	m.market.mu.Lock()
	defer m.market.mu.Unlock()
	state := m.market.turns[event.ThreadID]
	if state == nil {
		return
	}
	if len(state.terminal) < 16 {
		state.terminal[event.Turn.ID] = true
	}
	if state.turn == event.Turn.ID {
		if err := m.revokeMarketStateLocked(ctx, state, broker.RevokeReasonTurnTerminal); err != nil {
			m.logger.Warn("market revocation not confirmed", "failure_code", "market_cleanup_pending")
		}
	}
}

// Explicitly disabling every inherited MCP server is part of each managed
// thread start/resume, including empty selection and model-only changes.
func (m *Manager) marketDisabledServers(ctx context.Context, cwd string) (map[string]any, error) {
	var effective struct {
		Config map[string]json.RawMessage `json:"config"`
	}
	if err := m.request(ctx, "config/read", map[string]any{"includeLayers": false, "cwd": cwd}, &effective); err != nil {
		return nil, err
	}
	if err := validateMarketEffectiveIsolation(effective.Config); err != nil {
		return nil, err
	}
	var inherited map[string]json.RawMessage
	if raw := effective.Config["mcp_servers"]; len(raw) > 0 && string(raw) != "null" {
		if json.Unmarshal(raw, &inherited) != nil || len(inherited) > 128 {
			return nil, errors.New("market MCP inventory unavailable")
		}
	}
	m.market.mu.Lock()
	gateway := m.market.gatewayURL
	m.market.mu.Unlock()
	return disabledMarketServers(inherited, gateway)
}

func disabledMarketServers(inherited map[string]json.RawMessage, gateway string) (map[string]any, error) {
	if gateway == "" {
		return nil, errors.New("market gateway configuration unavailable")
	}
	servers := make(map[string]any, len(inherited)+1)
	for name, raw := range inherited {
		var server map[string]any
		if json.Unmarshal(raw, &server) != nil || server == nil {
			return nil, errors.New("market MCP inventory unavailable")
		}
		// Native configuration validates transport before considering enabled.
		// Keep each inherited transport intact while disabling its execution.
		server["enabled"] = false
		servers[name] = server
	}
	if _, exists := servers[marketServerName]; !exists {
		// A fresh thread has no prior managed transport to merge into. Use the
		// current owned loopback gateway, with no capability path or credential,
		// solely to form a valid disabled native MCP entry. Disabled entries are
		// not initialized and cannot issue a gateway or provider request.
		servers[marketServerName] = map[string]any{"url": gateway, "enabled": false}
	}
	return servers, nil
}

func (m *Manager) ClearMarketConfiguration(ctx context.Context, thread, cwd string) (*MarketPrepared, error) {
	if err := m.ClearMarketThread(ctx, thread); err != nil {
		return nil, err
	}
	servers, err := m.marketDisabledServers(ctx, cwd)
	if err != nil {
		return nil, err
	}
	return &MarketPrepared{Config: map[string]any{"mcp_servers": servers}}, nil
}
func (m *Manager) DiscardMarketPrepared(ctx context.Context, prepared *MarketPrepared) error {
	if prepared == nil || prepared.Lease == nil {
		return nil
	}
	m.market.mu.Lock()
	defer m.market.mu.Unlock()
	return m.revokeMarketStateLocked(ctx, &marketTurnState{prepared: prepared}, broker.RevokeReasonSelectionChanged)
}
