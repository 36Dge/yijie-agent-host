package codex

import (
	"context"
	"testing"

	wire "github.com/36Dge/yijie-agent-host/internal/contracts/markethost"
	"github.com/google/uuid"
)

func TestFEAT157MarketApprovalNeverUsesLegacyDecisionAuthority(t *testing.T) {
	m := &Manager{}
	id, thread, turn := uuid.NewString(), uuid.NewString(), uuid.NewString()
	entry := &runtimeApprovalCallback{view: RuntimeApproval{ID: id, Kind: "mcp_market", Status: "pending"}, threadID: thread, turnID: turn, resolve: make(chan string, 1), written: make(chan struct{}), market: &marketApprovalCallback{view: wire.MarketApproval{State: wire.ApprovalStatePending}}}
	if e := m.addRuntimeApproval(entry); e != nil {
		t.Fatal(e)
	}
	m.permissionCallbacks.activeTurns = map[string]string{thread: turn}
	if _, e := m.DecideRuntimeApproval(context.Background(), thread, id, "approve_once"); e == nil {
		t.Fatal("legacy HTTP authority decided market request")
	}
	if entry.decision != "" || len(entry.resolve) != 0 {
		t.Fatal("rejected legacy decision changed the callback")
	}
	// Normal native completion closes the pending callback through the existing
	// permission lifecycle, without a synthetic gateway approval or provider call.
	m.finishRuntimeApproval(entry, context.Canceled)
	if entry.market.view.State != wire.ApprovalStateUnavailable {
		t.Fatal("ended native callback remained actionable")
	}
}
