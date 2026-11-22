package codex

import (
	"context"
	"slices"
	"testing"

	broker "github.com/36Dge/yijie-agent-host/internal/contracts/marketbrokercontrol"
	wire "github.com/36Dge/yijie-agent-host/internal/contracts/markethost"
)

func TestMarketNativeGrantSurvivesRestartWithinItsInstallation(t *testing.T) {
	args := `{"amz_site":"US","search_name":"outdoor"}`
	schema := "frozen-schema"
	view := wire.MarketApproval{Review: broker.ReviewProjection{Risk: "read", Title: "product_search", ArgumentsJson: &args, SchemaDigest: &schema},
		Identity: broker.CallIdentity{ServiceId: "sorftime", ToolName: "m_public_lookup", Reference: broker.SelectionRef{InstallationId: "installation", Revision: 2, Generation: 3},
			Binding: broker.CapabilityBinding{Context: broker.ContextBinding{Scope: broker.ScopeBinding{OwnerUserId: "owner", TenantId: "tenant", NativeProcessEpoch: "old", AuthorizationRevision: 1}}}}}
	key, _ := nativeMarketScope(view, "checked-account")
	if key == nil {
		t.Fatal("reviewed query should have a native scope")
	}
	renewed := view
	renewed.Identity.Reference.Revision++ // normal startup provider probe
	renewed.Identity.Binding.Context.Scope.NativeProcessEpoch = "fresh"
	renewed.Identity.Binding.Context.Scope.AuthorizationRevision++
	renewed.Identity.Binding.Context.Scope.AuthorizationExpiresAtUnixMs += 300000
	renewed.Identity.NativeTurnId = "another-turn"
	renewed.Identity.CallRef = "different-call"
	same, _ := nativeMarketScope(renewed, "checked-account")
	if same == nil || *same != *key {
		t.Fatal("transient authority leaked into the native memory key")
	}
	for _, change := range []func(*wire.MarketApproval){
		func(v *wire.MarketApproval) { v.Identity.Binding.Context.Scope.OwnerUserId = "another-user" },
		func(v *wire.MarketApproval) { v.Identity.ServiceId = "another-service" },
		func(v *wire.MarketApproval) { v.Identity.Reference.InstallationId = "another-installation" },
		func(v *wire.MarketApproval) { v.Identity.Binding.Context.Scope.TenantId = "other" },
		func(v *wire.MarketApproval) { v.Identity.Reference.Generation++ },
		func(v *wire.MarketApproval) { v.Identity.ToolName = "different" },
		func(v *wire.MarketApproval) { changed := "changed-schema"; v.Review.SchemaDigest = &changed },
		func(v *wire.MarketApproval) { other := `{"amz_site":"GB"}`; v.Review.ArgumentsJson = &other },
	} {
		changed := view
		change(&changed)
		other, _ := nativeMarketScope(changed, "checked-account")
		if other == nil || *other == *key {
			t.Fatal("different installation, scope or tool shared consent")
		}
	}
	if changed, _ := nativeMarketScope(renewed, "changed-account"); changed == nil || *changed == *key {
		t.Fatal("account reauthorization or explicit disable must invalidate the previous authority")
	}
	if nativeMarketToolName(view) != "product_search · US" {
		t.Fatal("native rule does not identify the actual tool/site")
	}
	config := map[string]any{"yijie-market-" + *key: map[string]any{"tools": map[string]any{nativeMarketToolName(view): map[string]any{"approval_mode": "approve"}}}}
	if !validNativeMarketConfig(config) {
		t.Fatal("native persistent tool config rejected")
	}
	view.Review.Risk = "write"
	if key, _ := nativeMarketScope(view, "checked-account"); key != nil {
		t.Fatal("unqualified write offered reusable consent")
	}
}

func TestNativeMarketFunctionNamespaceComposesWithOtherOwnedServers(t *testing.T) {
	m := NewManager(Config{BuiltinMediaEnabled: true, BuiltinWebEnabled: true}, nil)
	config := map[string]any{}
	if err := addNativeFunctionNamespaces(config, "mcp__"+marketServerName); err != nil {
		t.Fatal(err)
	}
	config, err := m.mediaThreadConfig(context.Background(), config, MiniMaxProviderID)
	if err != nil {
		t.Fatal(err)
	}
	config, err = m.builtinWebThreadConfig(context.Background(), config, MiniMaxProviderID)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := config["features.code_mode.function_namespaces"].([]any)
	want := []any{"collaboration", "memories", "mcp__yijie_market", "mcp__yijie_media", "mcp__yijie_artifacts", "mcp__yijie_web"}
	if !ok || !slices.Equal(got, want) {
		t.Fatalf("native function namespaces: got %v, want %v", got, want)
	}
}
