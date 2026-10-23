package app

import (
	"context"
	"encoding/json"
	wire "github.com/36Dge/yijie-agent-host/internal/contracts/markethost"
	provider "github.com/36Dge/yijie-agent-host/internal/contracts/marketprovider"
	"os"
	"testing"
)

type suspendRoutingFixture struct {
	marketAdapterFixture
	method  string
	payload provider.ProviderPayload
}

func (f *suspendRoutingFixture) MarketProviderControl(_ context.Context, method string, _ provider.CanonicalId, p provider.ProviderPayload) (provider.ProviderStatus, error) {
	f.method = method
	f.payload = p
	return provider.ProviderStatus{OperationId: p.OperationId, HostInstanceId: p.HostInstanceId, Scope: p.Scope, Binding: p.Binding, OperationState: "succeeded", AuthorizationStatus: "unknown", ConnectionStatus: "disconnected", Qualification: "not_qualified"}, nil
}
func TestMarketSuspendRoutesSeparatelyFromForget(t *testing.T) {
	data, err := os.ReadFile("../../../yijie-contracts/fixtures/market-host/normal-wire.json")
	if err != nil {
		t.Fatal(err)
	}
	var frames map[string]json.RawMessage
	if err := json.Unmarshal(data, &frames); err != nil {
		t.Fatal(err)
	}
	service := &suspendRoutingFixture{marketAdapterFixture: marketAdapterFixture{enabled: true}}
	response, _, err := routeMarketControl(context.Background(), service, frames["SuspendRequest"])
	if err != nil {
		t.Fatal(err)
	}
	typed, ok := response.(wire.SuspendResponse)
	if !ok || typed.Validate() != nil || service.method != "provider_suspend" || typed.Data.ExecutionAvailable || typed.Data.Binding != service.payload.Binding {
		t.Fatal("suspend must preserve the exact binding and never route to forget")
	}
}
