package session

import (
	"context"
	"encoding/json"

	"github.com/36Dge/yijie-agent-host/internal/codex"
	wire "github.com/36Dge/yijie-agent-host/internal/contracts/markethost"
	"github.com/36Dge/yijie-agent-host/internal/marketbroker"
)

func marketRuntimeReady(runtime Runtime) bool {
	status, available := runtime.(interface{ Snapshot() codex.Status })
	return !available || status.Snapshot().Ready
}

func (s *Service) marketRecord(id string) (Record, error) {
	if wire.CanonicalId(id).Validate() != nil {
		return Record{}, marketFail(wire.ErrorCodeInvalidRequest)
	}
	owner, e := s.marketReadableOwner()
	if e != nil {
		return Record{}, e
	}
	r, e := s.store.Get(id)
	if e != nil {
		return Record{}, marketFail(wire.ErrorCodeSessionNotFound)
	}
	if r.Purpose != purposeMarket || r.MarketScopeHash != owner || r.CodexThreadID == "" {
		return Record{}, marketFail(wire.ErrorCodeAuthorityMismatch)
	}
	return r, nil
}
func (s *Service) ReadMarketApprovals(_ context.Context, id string) (wire.ApprovalSnapshot, error) {
	r, e := s.marketRecord(id)
	if e != nil {
		return wire.ApprovalSnapshot{}, e
	}
	if !marketRuntimeReady(s.runtime) {
		return wire.ApprovalSnapshot{}, marketFail(wire.ErrorCodeNotReady)
	}
	rt, ok := s.runtime.(interface {
		ListMarketApprovals(string) []wire.MarketApproval
	})
	if !ok {
		return wire.ApprovalSnapshot{}, marketFail(wire.ErrorCodeNotReady)
	}
	out := wire.ApprovalSnapshot{AgentSessionId: wire.CanonicalId(id), Requests: rt.ListMarketApprovals(r.CodexThreadID)}
	if out.Validate() != nil {
		return wire.ApprovalSnapshot{}, marketFail(wire.ErrorCodeTemporarilyUnavailable)
	}
	return out, nil
}
func (s *Service) DecideMarketApproval(ctx context.Context, input wire.ApprovalDecidePayload) (wire.MarketApproval, error) {
	if input.Validate() != nil {
		return wire.MarketApproval{}, marketFail(wire.ErrorCodeInvalidRequest)
	}
	s.modelOperationMu.Lock()
	defer s.modelOperationMu.Unlock()
	if e := s.ValidateMarketScope(input.HostInstanceId, input.Scope, true); e != nil {
		return wire.MarketApproval{}, e
	}
	r, e := s.marketRecord(string(input.AgentSessionId))
	if e != nil {
		return wire.MarketApproval{}, e
	}
	rt, ok := s.runtime.(interface {
		DecideMarketApproval(context.Context, string, wire.ApprovalDecidePayload) (wire.MarketApproval, error)
	})
	if !ok {
		return wire.MarketApproval{}, marketFail(wire.ErrorCodeNotReady)
	}
	// The retired connector may still be present in a frozen selection. Its
	// pending web call must honor the same verified offline expert policy.
	// Completed historical decision receipts keep their original replay path.
	if input.Decision == "approve_once" || input.Decision == "approve_session" || input.Decision == "approve_always" {
		if lister, available := s.runtime.(interface {
			ListMarketApprovals(string) []wire.MarketApproval
		}); available {
			for _, view := range lister.ListMarketApprovals(r.CodexThreadID) {
				if view.State == wire.ApprovalStatePending && view.ApprovalId == input.ApprovalId && view.Identity.CallRef == input.CallRef && string(view.Identity.ServiceId) == "kimi-web" {
					resolved, err := s.contextForStoredExpert(ctx, r.AgentSessionID)
					if err != nil || !codex.ExpertWebAllowed(resolved) {
						return wire.MarketApproval{}, marketFail(wire.ErrorCodePermissionDenied)
					}
				}
			}
		}
	}
	result, e := rt.DecideMarketApproval(ctx, r.CodexThreadID, input)
	if e != nil {
		return wire.MarketApproval{}, marketFail(wire.ErrorCodeApprovalStale)
	}
	return result, nil
}

// Bounded projection of Runtime-owned MCP Items. A Gateway-owned result receipt
// can link a completed native Item; missing metadata never invites a guess.
func (s *Service) ReadMarketTools(ctx context.Context, id, turnID string) (wire.ToolSnapshot, error) {
	if wire.NativeId(turnID).Validate() != nil {
		return wire.ToolSnapshot{}, marketFail(wire.ErrorCodeInvalidRequest)
	}
	r, e := s.marketRecord(id)
	if e != nil {
		return wire.ToolSnapshot{}, e
	}
	rt, ok := s.runtime.(interface {
		ReadThread(context.Context, string) (json.RawMessage, error)
	})
	if !ok {
		return wire.ToolSnapshot{}, marketFail(wire.ErrorCodeNotReady)
	}
	key := marketSnapshotIdentity(r, turnID)
	revision := s.marketTools.version(key)
	raw, e := rt.ReadThread(ctx, r.CodexThreadID)
	marketbroker.RecordLocalAcceptanceRead("thread_read")
	if e != nil {
		return wire.ToolSnapshot{}, marketFail(wire.ErrorCodeTemporarilyUnavailable)
	}
	var native struct {
		ID    string           `json:"id"`
		Turns []nativeWireTurn `json:"turns"`
	}
	if len(raw) > 32<<20 || json.Unmarshal(raw, &native) != nil || native.ID != r.CodexThreadID {
		return wire.ToolSnapshot{}, marketFail(wire.ErrorCodeTemporarilyUnavailable)
	}
	out := wire.ToolSnapshot{AgentSessionId: wire.CanonicalId(id), NativeTurnId: wire.NativeId(turnID), Items: []wire.ToolObservation{}}
	for _, turn := range native.Turns {
		if turn.ID != turnID {
			continue
		}
		for _, rawItem := range turn.Items {
			var item nativeWireItem
			if json.Unmarshal(rawItem, &item) != nil || item.Type != "mcpToolCall" || item.Server != "yijie_market" {
				continue
			}
			if len(out.Items) >= wire.MaxToolItems {
				out.Truncated = true
				break
			}
			if view := s.projectMarketTool(r, turnID, item); view != nil {
				out.Items = append(out.Items, *view)
			} else {
				out.Truncated = true
			}
		}
		s.marketTools.seed(key, out, revision)
		removeConflictingMarketLinks(out.Items)
		return out, nil
	}
	return wire.ToolSnapshot{}, marketFail(wire.ErrorCodeSessionNotFound)
}

// The Worker checks the live (possibly renewed) deadline. Recheck the current
// Native owner/revision and original session here without extending a lease.
func (s *Service) nativeMarketCallAllowed(ctx context.Context, v wire.MarketApproval) bool {
	if !s.MarketEnabled() {
		return false
	}
	b := v.Identity.Binding
	scope := b.Context.Scope
	s.market.mu.Lock()
	valid := s.market.checkScope(b.Context.Process.HostInstanceId, scope, false) == nil && s.market.revision == int64(scope.AuthorizationRevision)
	s.market.mu.Unlock()
	if !valid {
		return false
	}
	record, err := s.marketRecord(string(v.AgentSessionId))
	if err != nil || record.CodexThreadID != string(v.Identity.NativeThreadId) {
		return false
	}
	// The intent is recorded before turn/start. Its native IDs may still be
	// arriving; the manager has already matched the active Runtime turn itself.
	entry, err := s.store.readMarketOperation(string(b.TurnOperationId))
	if err != nil || entry.ScopeHash != record.MarketScopeHash {
		return false
	}
	r := entry.Receipt
	if r.TaskId != wire.CanonicalId(record.TaskID) || r.AgentSessionId == nil || *r.AgentSessionId != v.AgentSessionId ||
		r.TurnOperationId != b.TurnOperationId || r.SelectionDigest != b.SelectionDigest ||
		r.HostInstanceId != b.Context.Process.HostInstanceId || r.RuntimeGeneration == nil || *r.RuntimeGeneration != b.Context.Process.RuntimeGeneration ||
		(r.NativeThreadId != nil && *r.NativeThreadId != v.Identity.NativeThreadId) || (r.NativeTurnId != nil && *r.NativeTurnId != v.Identity.NativeTurnId) {
		return false
	}
	if string(v.Identity.ServiceId) == "kimi-web" {
		resolved, err := s.contextForStoredExpert(ctx, record.AgentSessionID)
		return err == nil && codex.ExpertWebAllowed(resolved)
	}
	return true
}

func (s *Service) bindNativeMarketAuthorization() {
	if rt, ok := s.runtime.(interface {
		SetNativeMarketAuthorization(func(context.Context, wire.MarketApproval) bool)
	}); ok {
		rt.SetNativeMarketAuthorization(s.nativeMarketCallAllowed)
	}
}

func (s *Service) RememberedMarketRules(ctx context.Context, p wire.RememberedRulesPayload) (wire.RememberedToolRules, error) {
	var out wire.RememberedToolRules
	if err := p.Validate(); err != nil {
		return out, marketFail(wire.ErrorCodeInvalidRequest)
	}
	s.modelOperationMu.Lock()
	defer s.modelOperationMu.Unlock()
	if err := s.ValidateMarketScope(p.HostInstanceId, p.Scope, true); err != nil {
		return out, err
	}
	rt, ok := s.runtime.(interface {
		RememberedMarketRules(context.Context, wire.RememberedRulesPayload) (wire.RememberedToolRules, error)
	})
	if !ok {
		return out, marketFail(wire.ErrorCodeNotReady)
	}
	out, err := rt.RememberedMarketRules(ctx, p)
	if err != nil {
		return out, marketFail(wire.ErrorCodeTemporarilyUnavailable)
	}
	return out, nil
}
