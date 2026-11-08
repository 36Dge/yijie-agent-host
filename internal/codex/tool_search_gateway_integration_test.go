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
	scope := broker.ScopeBinding{OwnerUserId: id(), TenantId: id(), NativeProcessEpoch: id(), AuthorizationRevision: 1, AuthorizationExpiresAtUnixMs: broker.UnixMilliseconds(time.Now().Add(3 * time.Minute).UnixMilli())}
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
	input, _ := TextUserInput("Discover the selected web connector tool; its proposed query will be declined.")
	turn, err := h.manager.StartTurnV2(selected, thread.ID, []UserInput{input}, "max")
	if err != nil {
		t.Fatal(err)
	}
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
	if err = h.manager.DiscardMarketPrepared(ctx, prepared); err != nil {
		t.Fatal(err)
	}
	t.Log("native search -> exec -> product Gateway -> exact original approval -> reject; no approve/consume or supplier request")
}
