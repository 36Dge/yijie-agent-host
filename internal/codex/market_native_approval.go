package codex

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/url"
	"reflect"
	"strings"

	broker "github.com/36Dge/yijie-agent-host/internal/contracts/marketbrokercontrol"
	wire "github.com/36Dge/yijie-agent-host/internal/contracts/markethost"
	"github.com/36Dge/yijie-agent-host/internal/marketbroker"
)

func marketNativeReply(action string, content any) (serverRequestResult, bool) {
	return serverRequestResult{respond: true, value: map[string]any{"action": action, "content": content, "_meta": nil}}, true
}

// Only the worker can qualify a real installed tool as read-only. Runtime
// config stores the resulting opaque principal/installation-specific app key.
func nativeMarketScope(view wire.MarketApproval, account string) (*string, map[string]any) {
	args := map[string]any{}
	if view.Review.ArgumentsJson != nil {
		decoder := json.NewDecoder(strings.NewReader(*view.Review.ArgumentsJson))
		decoder.UseNumber()
		if decoder.Decode(&args) != nil || args == nil {
			return nil, map[string]any{}
		}
	}
	if view.Review.Risk != "read" || account == "" {
		return nil, args
	}
	id := view.Identity
	target := ""
	if site, ok := args["amz_site"].(string); ok {
		target = site
	}
	if raw, ok := args["url"].(string); ok {
		u, err := url.ParseRequestURI(raw)
		if err != nil || u.Host == "" || u.User != nil {
			return nil, args
		}
		target = strings.ToLower(u.Scheme + "://" + u.Host)
	}
	// Tool-specific version for the fixed product tools lacking an upstream digest.
	schema := "native-market-read-v1"
	if view.Review.SchemaDigest != nil {
		schema = *view.Review.SchemaDigest
	}
	binding := id.Binding.Context.Scope
	// The prefix groups the current installation generation and account. The
	// suffix isolates the schema, upstream action and reviewed target. Each live
	// call still rechecks its current authority revision and deadline.
	prefix := marketInstallPrefix(binding, id.ServiceId, id.Reference, account)
	suffix := marketScopeDigest([]any{"yijie.market.tool/v1", id.ToolName, schema, target})
	key := prefix + suffix
	return &key, args
}

func marketScopeDigest(fields []any) string {
	// All callers provide a closed tuple of strings and integer revisions.
	bytes, err := json.Marshal(fields)
	if err != nil {
		panic("invalid market scope tuple")
	}
	sum := sha256.Sum256(bytes)
	return hex.EncodeToString(sum[:16])
}

func nativeMarketToolName(v wire.MarketApproval) string {
	if v.Identity.ServiceId == "sorftime" && v.Review.Risk == "read" && v.Review.ArgumentsJson != nil {
		var args struct {
			Site string `json:"amz_site"`
		}
		if json.Unmarshal([]byte(*v.Review.ArgumentsJson), &args) == nil && args.Site != "" {
			return v.Review.Title + " · " + args.Site
		}
	}
	if v.Identity.ServiceId == "market-local-verification" {
		return v.Review.Title
	}
	return string(v.Identity.ToolName)
}

func sameMarketScopeKey(supplied *broker.ArgsDigest, expected *string) bool {
	if supplied == nil || expected == nil {
		return supplied == nil && expected == nil
	}
	return string(*supplied) == *expected
}

func (m *Manager) applyNativeMarketDecision(ctx context.Context, owner *marketbroker.Owner, state *marketTurnState,
	envelope broker.NativeApprovalEnvelope, view wire.MarketApproval) (serverRequestResult, bool) {
	mode := state.prepared.NativeApprovalMode
	decision := broker.Decision(envelope.YijieNativePhase)
	if view.Review.Risk != "read" || envelope.YijieScope == nil || envelope.Persist != nil {
		return marketNativeReply("decline", nil)
	}
	var allowed bool
	switch decision {
	case broker.DecisionNativeAuto, broker.DecisionNativeAutoReject:
		allowed = mode == "auto"
	case broker.DecisionNativeFull:
		allowed = mode == "full"
	case broker.DecisionNativeSession, broker.DecisionNativeAlways:
		allowed = mode == "ask"
	}
	if !allowed {
		return marketNativeReply("decline", nil)
	}
	m.market.ruleMu.RLock()
	defer m.market.ruleMu.RUnlock()
	if decision == broker.DecisionNativeAlways {
		prefix := marketInstallPrefix(view.Identity.Binding.Context.Scope, view.Identity.ServiceId, view.Identity.Reference, state.prepared.approvalAccounts[view.Identity.Reference])
		saved, _, err := m.marketRuleSnapshot(ctx, prefix)
		if err != nil || saved[string(*envelope.YijieScope)] != nativeMarketToolName(view) {
			return marketNativeReply("decline", nil)
		}
	}
	m.market.mu.Lock()
	active := !state.closed && m.market.turns[state.thread] == state && state.turn == string(view.Identity.NativeTurnId)
	authorize := m.market.authorizeNative
	m.market.mu.Unlock()
	if !active || authorize == nil || !authorize(ctx, view) {
		return marketNativeReply("decline", nil)
	}
	// This is a policy receipt, never a manufactured click or reusable grant.
	ref := view.Identity.CallRef
	reply, err := owner.DecideCall(ctx, broker.DecideCallRequest{SchemaVersion: 1, RequestId: ref, Method: "decide_call", Payload: broker.DecideCallPayload{
		OperationId: ref, Identity: view.Identity, ExpectedRevision: view.Revision, ApprovalRef: ref, DecisionId: ref, Decision: decision}})
	if err != nil || !reflect.DeepEqual(reply.Data.Identity, view.Identity) || reply.Data.Decision == nil || *reply.Data.Decision != decision ||
		reply.Data.DecisionId == nil || *reply.Data.DecisionId != ref {
		return marketNativeReply("decline", nil)
	}
	return marketNativeReply("accept", map[string]any{})
}

func (m *Manager) recordMarketPersistence(envelope broker.NativeApprovalEnvelope, call broker.PendingCall, thread, account string) (serverRequestResult, bool) {
	if call.DecisionId == nil || call.Decision == nil || call.State != broker.CallStateApproved {
		return marketNativeReply("decline", nil)
	}
	c := &m.permissionCallbacks
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, entry := range c.entries {
		if entry.market == nil || entry.threadID != thread {
			continue
		}
		v := &entry.market.view
		if v.Identity.CallRef != call.Identity.CallRef || v.DecisionId == nil || *v.DecisionId != *call.DecisionId {
			continue
		}
		scope, _ := nativeMarketScope(*v, account)
		if scope == nil || !sameMarketScopeKey(envelope.YijieScope, scope) {
			break
		}
		var applied string
		switch envelope.YijieNativePhase {
		case "stored_always":
			if *call.Decision != broker.DecisionApproveAlways {
				return marketNativeReply("decline", nil)
			}
			applied = "always"
		case "stored_session":
			if *call.Decision != broker.DecisionApproveAlways && *call.Decision != broker.DecisionApproveSession {
				return marketNativeReply("decline", nil)
			}
			applied = "session"
		default:
			applied = "unavailable"
		}
		v.AppliedPersist = &applied
		return marketNativeReply("accept", map[string]any{})
	}
	return marketNativeReply("decline", nil)
}

// Installed by the product session service, the owner of session/account facts.
func (m *Manager) SetNativeMarketAuthorization(check func(context.Context, wire.MarketApproval) bool) {
	m.market.mu.Lock()
	defer m.market.mu.Unlock()
	m.market.authorizeNative = check
}

// The only new writable native config leaves are per-tool approval modes. In
// particular the per-turn Gateway URL and process capability never go to disk.
func validNativeMarketConfig(value any) bool {
	apps, ok := value.(map[string]any)
	if !ok {
		return false
	}
	for app, raw := range apps {
		digest, ok := strings.CutPrefix(app, "yijie-market-")
		if !ok || len(digest) != 64 || strings.ToLower(digest) != digest {
			return false
		}
		if _, err := hex.DecodeString(digest); err != nil {
			return false
		}
		entry, ok := raw.(map[string]any)
		if !ok || len(entry) != 1 {
			return false
		}
		tools, ok := entry["tools"].(map[string]any)
		if !ok {
			return false
		}
		for tool, v := range tools {
			if broker.ToolName(tool).Validate() != nil {
				return false
			}
			setting, ok := v.(map[string]any)
			if !ok || len(setting) != 1 || setting["approval_mode"] != "approve" {
				return false
			}
		}
	}
	return true
}
