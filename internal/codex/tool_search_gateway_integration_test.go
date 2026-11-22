package codex

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	broker "github.com/36Dge/yijie-agent-host/internal/contracts/marketbrokercontrol"
	host "github.com/36Dge/yijie-agent-host/internal/contracts/markethost"
	provider "github.com/36Dge/yijie-agent-host/internal/contracts/marketprovider"
	selection "github.com/36Dge/yijie-agent-host/internal/contracts/marketselection"
	"github.com/google/uuid"
)

// Real product worker/Gateway and native exec; the normal reject decision is
// delivered before execution admission. No supplier metadata or business call.
func TestToolSearchRuntimeGatewayApprovalReject(t *testing.T) {
	manifest := os.Getenv("YIJIE_MARKET_BROKER_TEST_MANIFEST")
	if manifest == "" {
		t.Skip("canonical product worker required")
	}
	h := newNativeToolsHarness(t, func(_ *nativeToolsHarness, body map[string]any, n int64) map[string]any {
		assertSearchDeclaration(t, body, true)
		switch n {
		case 1:
			return feat164AuxTool("", "tool_search", "gateway-search", map[string]any{"query": "kimi_web_search", "limit": 3})
		case 2:
			result := decodedSearchResult(t, body, "gateway-search")
			raw, _ := json.Marshal(result)
			if !strings.Contains(string(raw), "mcp__yijie_market__kimi_web_search") {
				t.Error("current gateway tool not discovered")
			}
			return feat164AuxTool("", "exec", "gateway-exec", map[string]any{"input": "text(await tools.mcp__yijie_market__kimi_web_search({query: 'ordinary public sample', limit: 1, include_content: false}));"})
		default:
			if !strings.Contains(nativeToolsOutput(body, "gateway-exec"), "isError") {
				t.Error("gateway decline did not return a tool result")
			}
			return feat164AuxText("已按原审批链拒绝，没有执行外部查询。")
		}
	}, toolSearchConfig, func(c *Config) {
		c.Market = MarketConfig{Manifest: manifest, LibraryHome: filepath.Join(c.CodexHome, "market-worker/library-home"), HostInstanceID: uuid.NewString()}
	})
	id := func() broker.CanonicalId { return broker.CanonicalId(uuid.NewString()) }
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	thread, turn, scope, session, prepared := startMarketGatewaySample(t, h, ctx, 3*time.Minute)
	var approval host.MarketApproval
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		rows := h.manager.ListMarketApprovals(thread.ID)
		if len(rows) > 0 {
			approval = rows[0]
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if approval.ApprovalId == "" {
		_ = h.manager.InterruptTurn(context.Background(), thread.ID, turn.ID)
		t.Fatal("bound gateway approval not observed")
	}
	if approval.Identity.NativeThreadId != broker.NativeId(thread.ID) || approval.Identity.NativeTurnId != broker.NativeId(turn.ID) {
		t.Fatal("approval lost original thread/turn")
	}
	receipt, err := h.manager.DecideMarketApproval(ctx, thread.ID, host.ApprovalDecidePayload{OperationId: id(), HostInstanceId: broker.CanonicalId(h.manager.config.Market.HostInstanceID), Scope: scope, AgentSessionId: session, ApprovalId: approval.ApprovalId, CallRef: approval.Identity.CallRef, ExpectedRevision: approval.Revision, DecisionId: id(), Decision: broker.DecisionReject})
	if err != nil || receipt.State != "rejected" {
		t.Fatal("normal rejection failed", err, receipt.State)
	}
	status, _, err := h.notifications.waitForTurn(turn.ID, 20*time.Second)
	if err != nil || status != "completed" {
		t.Fatal(status, err)
	}
	if h.calls.Load() != 3 {
		t.Fatal("unexpected retry after rejection", h.calls.Load())
	}
	// The real owned worker result must carry the same rejected decision through
	// the unchanged native MCP Item, without exposing an executable binding.
	history, err := h.manager.ReadThread(ctx, thread.ID)
	if err != nil {
		t.Fatal(err)
	}
	var native struct {
		Turns []struct {
			Items []struct {
				ID     string `json:"id"`
				Type   string `json:"type"`
				Result struct {
					Meta map[string]json.RawMessage `json:"_meta"`
				} `json:"result"`
			} `json:"items"`
		} `json:"turns"`
	}
	if json.Unmarshal(history, &native) != nil {
		t.Fatal("native history unavailable")
	}
	linked := 0
	for _, nativeTurn := range native.Turns {
		for _, item := range nativeTurn.Items {
			if item.Type != "mcpToolCall" {
				continue
			}
			var link broker.GatewayCallReceipt
			if json.Unmarshal(item.Result.Meta[broker.GatewayResultKey], &link) != nil || link.Validate() != nil {
				t.Fatal("real Gateway receipt missing")
			}
			if item.ID == "" || link.CallRef != approval.Identity.CallRef || link.NativeThreadId != broker.NativeId(thread.ID) || link.NativeTurnId != broker.NativeId(turn.ID) || link.DecisionReceipt == nil || link.DecisionReceipt.ApprovalId != approval.ApprovalId || receipt.DecisionId == nil || link.DecisionReceipt.DecisionId != *receipt.DecisionId || link.DecisionReceipt.Decision != broker.DecisionReject || link.AdmissionConsumed {
				t.Fatal("real approval and native result are not linked")
			}
			linked++
		}
	}
	if linked != 1 {
		t.Fatal("expected one actual linked MCP Item", linked)
	}
	if err = h.manager.DiscardMarketPrepared(ctx, prepared); err != nil {
		t.Fatal(err)
	}
	t.Log("native search -> exec -> product Gateway -> exact original approval -> reject; no approve/consume or supplier request")
}

func startMarketGatewaySample(t *testing.T, h *nativeToolsHarness, ctx context.Context, authorityTTL time.Duration) (ThreadInfo, TurnInfo, broker.ScopeBinding, broker.CanonicalId, *MarketPrepared) {
	t.Helper()
	id := func() broker.CanonicalId { return broker.CanonicalId(uuid.NewString()) }
	scope := broker.ScopeBinding{OwnerUserId: id(), TenantId: id(), NativeProcessEpoch: id(), AuthorizationRevision: 1, AuthorizationExpiresAtUnixMs: broker.UnixMilliseconds(time.Now().Add(authorityTTL).UnixMilli())}
	ref := broker.SelectionRef{InstallationId: id(), Revision: 1, Generation: 1}
	binding := provider.ProviderBinding{Reference: ref, ServiceId: "kimi-web", CredentialRef: id()}
	owner, err := h.manager.MarketOwner()
	if err != nil {
		t.Fatal(err)
	}
	payload := provider.ProviderPayload{OperationId: id(), HostInstanceId: broker.CanonicalId(h.manager.config.Market.HostInstanceID), Scope: scope, Binding: binding}
	for {
		probe, err := owner.Probe(ctx, provider.ProbeRequest{SchemaVersion: 1, RequestId: id(), Method: "provider_probe", Payload: payload})
		if err != nil {
			t.Fatal(err)
		}
		if probe.Data.OperationState != "starting" {
			if !probe.Data.ExecutionAvailable {
				t.Fatal("local built-in metadata unavailable")
			}
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-time.After(10 * time.Millisecond):
		}
	}
	op := id()
	refs := selection.Selection{ref}
	digest, err := selection.SelectionDigest(string(op), refs)
	if err != nil {
		t.Fatal(err)
	}
	var block host.ContentBlock
	if err = json.Unmarshal([]byte(`{"type":"text","text":"ordinary approval verification"}`), &block); err != nil {
		t.Fatal(err)
	}
	session := id()
	submitted := host.Submission{TaskId: id(), LocalSessionId: id(), SubmissionOperationId: id(), Cwd: h.cwd, ContentBlocks: []host.ContentBlock{block}, Intent: host.ModelIntent{ProfileId: host.ProfileId(KimiK31ProfileID)}, PermissionMode: host.PermissionModeAsk, Snapshot: selection.SelectionSnapshot{SchemaVersion: 1, TurnOperationId: op, Selection: refs, SelectionDigest: selection.SelectionDigestValue(digest)}, Services: []host.ServiceBinding{binding}}
	prepared, err := h.manager.PrepareMarket(ctx, scope, submitted, string(session), nil)
	if err != nil {
		t.Fatal(err)
	}
	selected := WithMarketPrepared(feat164AuxCtx(), prepared)
	thread, err := h.manager.StartThread(selected, h.cwd)
	if err != nil {
		t.Fatal(err)
	}
	if err = h.manager.BeginMarketTurn(thread.ID, prepared); err != nil {
		t.Fatal(err)
	}
	input, _ := TextUserInput("Use the selected market connector for the ordinary public sample.")
	turn, err := h.manager.StartTurnV2(selected, thread.ID, []UserInput{input}, "max")
	if err != nil {
		t.Fatal(err)
	}
	return thread, turn, scope, session, prepared
}

// Real Runtime, real Gateway and normal private controls. Use a short original
// scope so this also exercises the wall-clock boundary without a long test.
func TestMarketRenewalAndTerminalStopAcrossToolsInCodeMode(t *testing.T) {
	manifest := os.Getenv("YIJIE_MARKET_BROKER_TEST_MANIFEST")
	if manifest == "" {
		t.Skip("canonical product worker required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 70*time.Second)
	defer cancel()
	originalEnded, renewalEnded := make(chan struct{}), make(chan struct{})
	h := newNativeToolsHarness(t, func(_ *nativeToolsHarness, body map[string]any, n int64) map[string]any {
		switch n {
		case 1:
			select {
			case <-originalEnded:
			case <-ctx.Done():
				return feat164AuxText("test ended")
			}
			return feat164AuxTool("", "tool_search", "live-search", map[string]any{"query": "kimi_web_search", "limit": 3})
		case 2:
			return feat164AuxTool("", "exec", "approved-window", map[string]any{"input": "text(await tools.mcp__yijie_market__kimi_web_search({query:'public sample',limit:1}));"})
		case 3:
			if !strings.Contains(nativeToolsOutput(body, "approved-window"), "isError") {
				t.Error("missing individual rejection")
			}
			select {
			case <-renewalEnded:
			case <-ctx.Done():
				return feat164AuxText("test ended")
			}
			// The first result is intentionally not printed. A second cached tool
			// in the same native Code Mode cell must stop locally before RPC.
			return feat164AuxTool("", "exec", "ended-market", map[string]any{"input": "await tools.mcp__yijie_market__kimi_web_search({query:'another public sample',limit:1}); const stopped=await tools.mcp__yijie_market__kimi_web_fetch({url:'https://example.com'}); text(stopped);"})
		case 4:
			if !strings.Contains(nativeToolsOutput(body, "ended-market"), "请停止本轮所有市场工具调用") {
				t.Error("cached sibling did not receive the Runtime stop")
			}
			instructions, _ := body["instructions"].(string)
			if !strings.Contains(instructions, "本轮市场连接器授权已到期") {
				t.Error("model did not receive confirmed scope expiry")
			}
			// This fixture has no other deferred server. Its discovery tool
			// disappears when the last market tools leave the model catalogue.
			assertSearchDeclaration(t, body, false)
			return feat164AuxTool("", "exec", "remaining-tools", map[string]any{"input": "text(typeof tools.mcp__yijie_market__kimi_web_search); text(6 * 7);"})
		case 5:
			output := nativeToolsOutput(body, "remaining-tools")
			if !strings.Contains(output, "undefined") || !strings.Contains(output, "42") {
				t.Errorf("unexpected remaining Code Mode tools: %s", output)
			}
		default:
			t.Errorf("unexpected model continuation %d", n)
		}
		return feat164AuxText("市场授权已到期，已停止本轮市场查询。")
	}, toolSearchConfig, dailySkillMarkets(t), func(c *Config) {
		c.Market = MarketConfig{Manifest: manifest, LibraryHome: filepath.Join(c.CodexHome, "market-worker/library-home"), HostInstanceID: uuid.NewString()}
	})
	for _, root := range h.manager.config.SkillMarketplaceRoots {
		if err := h.manager.RegisterSkillMarketplace(ctx, root); err != nil {
			t.Fatal(err)
		}
	}
	h.manager.SetSkillSubmissionReady(true)
	thread, turn, scope, session, prepared := startMarketGatewaySample(t, h, ctx, 10*time.Second)
	owner, err := h.manager.MarketOwner()
	if err != nil {
		t.Fatal(err)
	}
	id := func() broker.CanonicalId { return broker.CanonicalId(uuid.NewString()) }
	renew := func(sequence broker.Revision, until time.Time) broker.ScopeBinding {
		fresh := scope
		fresh.AuthorizationExpiresAtUnixMs = broker.UnixMilliseconds(until.UnixMilli())
		request := broker.RenewScopeRequest{SchemaVersion: 1, RequestId: id(), Method: "renew_market_scope", Payload: broker.RenewScopePayload{HostInstanceId: prepared.Process.HostInstanceId, Scope: fresh, Sequence: sequence}}
		if _, err := owner.RenewScope(ctx, prepared.Process, request); err != nil {
			t.Fatal(err)
		}
		return fresh
	}
	current := renew(1, time.Now().Add(time.Minute))
	time.Sleep(time.Until(time.UnixMilli(int64(scope.AuthorizationExpiresAtUnixMs)).Add(100 * time.Millisecond)))
	close(originalEnded)
	var approval host.MarketApproval
	for deadline := time.Now().Add(15 * time.Second); time.Now().Before(deadline); {
		entries := h.manager.ListMarketApprovals(thread.ID)
		if len(entries) > 0 {
			approval = entries[0]
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if approval.State != host.ApprovalStatePending {
		t.Fatal("no fresh approval beyond the original deadline")
	}
	_, err = h.manager.DecideMarketApproval(ctx, thread.ID, host.ApprovalDecidePayload{OperationId: id(), HostInstanceId: prepared.Process.HostInstanceId, Scope: current, AgentSessionId: session, ApprovalId: approval.ApprovalId, CallRef: approval.Identity.CallRef, ExpectedRevision: approval.Revision, DecisionId: id(), Decision: broker.DecisionReject})
	if err != nil {
		t.Fatal(err)
	}
	end := time.Now().Add(time.Second)
	renew(2, end)
	time.Sleep(time.Until(end.Add(50 * time.Millisecond)))
	close(renewalEnded)
	if status, _, err := h.notifications.waitForTurn(turn.ID, 20*time.Second); err != nil || status != "completed" {
		t.Fatal(status, err)
	}
	if len(h.manager.ListMarketApprovals(thread.ID)) != 1 || h.calls.Load() != 5 {
		t.Fatal("extra market approval or model step")
	}
}
