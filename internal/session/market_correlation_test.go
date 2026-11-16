package session

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	broker "github.com/36Dge/yijie-agent-host/internal/contracts/marketbrokercontrol"
	wire "github.com/36Dge/yijie-agent-host/internal/contracts/markethost"
	"github.com/google/uuid"
)

func TestMarketResultCorrelationUsesNativeItemAndOriginalReceiptAfterNormalReopen(t *testing.T) {
	s, runtime, scope, home := marketService(t)
	v := marketSubmission(t, home)
	accepted, err := s.SubmitMarket(context.Background(), registerMarket(t, s, scope, v))
	if err != nil {
		t.Fatal(err)
	}
	runtime.toolTurnID = string(*accepted.NativeTurnId)
	correlation := broker.GatewayCallReceipt{
		SchemaVersion: 1, CallRef: broker.CanonicalId(uuid.NewString()), ServiceId: "tushareMcp",
		Reference:      broker.SelectionRef{InstallationId: wire.CanonicalId(uuid.NewString()), Revision: 1, Generation: 1},
		NativeThreadId: *accepted.NativeThreadId, NativeTurnId: *accepted.NativeTurnId,
		TurnOperationId: accepted.TurnOperationId, SelectionDigest: accepted.SelectionDigest,
		ToolName: "public_lookup", ArgsDigest: broker.ArgsDigest(strings.Repeat("a", 64)), ArgsEncoding: "worker-json-v1",
		DecisionReceipt: &broker.GatewayDecisionReceipt{ApprovalId: wire.CanonicalId(uuid.NewString()), DecisionId: wire.CanonicalId(uuid.NewString()), Decision: broker.DecisionApproveOnce}, AdmissionConsumed: true,
	}
	if err = correlation.Validate(); err != nil {
		t.Fatal(err)
	}
	runtime.toolResult = map[string]any{"content": []any{map[string]string{"type": "text", "text": "ordinary public result"}}, "_meta": map[string]any{broker.GatewayResultKey: correlation}}
	check := func(service *Service) {
		t.Helper()
		result, err := service.ReadMarketTools(context.Background(), string(*accepted.AgentSessionId), runtime.toolTurnID)
		if err != nil || len(result.Items) != 1 {
			t.Fatal(result, err)
		}
		item := result.Items[0]
		if item.NativeItemId != "native-tool-item-1" || item.Correlation == nil || item.CallRef == nil || *item.CallRef != correlation.CallRef || item.Service != nil || item.State != "completed" {
			t.Fatal(item)
		}
		if item.Correlation.DecisionReceipt.DecisionId != correlation.DecisionReceipt.DecisionId {
			t.Fatal("decision identity changed")
		}
	}
	check(s)
	if err = s.store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err := OpenStore(home, WithMarketStorage())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	instance := wire.CanonicalId(uuid.NewString())
	reopened := NewService(runtime, store, NewEventHub(32, 16), nil, WithMarketAuthority(string(instance)))
	if _, err = reopened.MarketHello(wire.HelloPayload{HostInstanceId: instance, OwnerUserId: scope.OwnerUserId, TenantId: scope.TenantId, NativeProcessEpoch: wire.CanonicalId(uuid.NewString())}); err != nil {
		t.Fatal(err)
	}
	check(reopened) // no live worker, approval cache, or current Host nonce is needed
	for _, meta := range []any{nil, map[string]any{broker.GatewayResultKey: map[string]any{"schemaVersion": 2}}, map[string]any{broker.GatewayResultKey: map[string]any{"schemaVersion": 1}}} {
		runtime.toolResult = map[string]any{"content": []any{map[string]string{"type": "text", "text": "original result remains readable"}}, "_meta": meta}
		result, err := reopened.ReadMarketTools(context.Background(), string(*accepted.AgentSessionId), runtime.toolTurnID)
		if err != nil || len(result.Items) != 1 || result.Items[0].Correlation != nil || result.Items[0].CallRef != nil || result.Items[0].ResultText == nil {
			t.Fatal(result, err)
		}
	}
}

func TestMarketResultConflictingLinksDoNotLoseNativeResults(t *testing.T) {
	ref := wire.CanonicalId(uuid.NewString())
	items := []wire.ToolObservation{
		{NativeItemId: "first", CallRef: &ref, Correlation: &wire.GatewayCallReceipt{CallRef: ref}, State: "completed"},
		{NativeItemId: "second", CallRef: &ref, Correlation: &wire.GatewayCallReceipt{CallRef: ref}, State: "failed"},
	}
	removeConflictingMarketLinks(items)
	for _, item := range items {
		if item.Correlation != nil || item.CallRef != nil {
			t.Fatal("ambiguous link retained")
		}
	}
	if items[0].State != "completed" || items[1].State != "failed" {
		t.Fatal("native facts changed")
	}
	if _, err := json.Marshal(items); err != nil {
		t.Fatal(err)
	}
}
