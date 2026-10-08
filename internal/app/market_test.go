package app

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/36Dge/yijie-agent-host/internal/codex"
	wire "github.com/36Dge/yijie-agent-host/internal/contracts/markethost"
	provider "github.com/36Dge/yijie-agent-host/internal/contracts/marketprovider"
	"github.com/36Dge/yijie-agent-host/internal/session"
)

var _ marketHTTPService = (*session.Service)(nil)
var _ MarketControlService = (*session.Service)(nil)

func marketID(tail int) wire.CanonicalId {
	return wire.CanonicalId(fmt.Sprintf("15700000-0000-4000-8000-%012d", tail))
}

type marketAdapterFixture struct {
	enabled                      bool
	hello, close, submits, reads int
}

func (f *marketAdapterFixture) MarketEnabled() bool { return f.enabled }
func (f *marketAdapterFixture) MarketHello(v wire.HelloPayload) (wire.HelloResult, error) {
	f.hello++
	return wire.HelloResult{HostInstanceId: v.HostInstanceId, NativeProcessEpoch: v.NativeProcessEpoch, OwnerUserId: v.OwnerUserId, TenantId: v.TenantId, Ready: true}, nil
}
func (f *marketAdapterFixture) RegisterMarketGrant(wire.GrantRegisterPayload) (wire.GrantRegistration, error) {
	return wire.GrantRegistration{}, nil
}
func (f *marketAdapterFixture) RevokeMarketAuthority(context.Context, wire.RevokeAuthorityPayload) (wire.RevokeAuthorityResult, error) {
	return wire.RevokeAuthorityResult{}, nil
}
func (f *marketAdapterFixture) DecideMarketApproval(context.Context, wire.ApprovalDecidePayload) (wire.MarketApproval, error) {
	return wire.MarketApproval{}, nil
}
func (f *marketAdapterFixture) MarketProviderControl(context.Context, string, provider.CanonicalId, provider.ProviderPayload) (provider.ProviderStatus, error) {
	return provider.ProviderStatus{}, nil
}
func (f *marketAdapterFixture) CloseMarketAuthority(context.Context) { f.close++ }
func marketReceipt() wire.SubmissionReceipt {
	return wire.SubmissionReceipt{HostInstanceId: marketID(1), TaskId: marketID(2), LocalSessionId: marketID(3), SubmissionOperationId: marketID(4), TurnOperationId: marketID(5), SelectionDigest: wire.SelectionDigest(strings.Repeat("a", 64)), State: "pending", ProfileId: "kimi-k3-max-v1", ModelRevision: 1}
}
func (f *marketAdapterFixture) SubmitMarket(context.Context, wire.SubmitPayload) (wire.SubmissionReceipt, error) {
	f.submits++
	return marketReceipt(), nil
}
func (f *marketAdapterFixture) ReadMarketOperation(string, string) (wire.SubmissionReceipt, error) {
	f.reads++
	return marketReceipt(), nil
}
func (f *marketAdapterFixture) ReadMarketApprovals(_ context.Context, id string) (wire.ApprovalSnapshot, error) {
	f.reads++
	return wire.ApprovalSnapshot{AgentSessionId: wire.CanonicalId(id), Requests: []wire.MarketApproval{}}, nil
}
func (f *marketAdapterFixture) ReadMarketTools(_ context.Context, id, turn string) (wire.ToolSnapshot, error) {
	f.reads++
	return wire.ToolSnapshot{AgentSessionId: wire.CanonicalId(id), NativeTurnId: wire.NativeId(turn), Items: []wire.ToolObservation{}, Truncated: false}, nil
}

func TestMarketControlHelloNormalEOFClosesAuthority(t *testing.T) {
	service := &marketAdapterFixture{enabled: true}
	request := wire.HelloRequest{SchemaVersion: 1, RequestId: marketID(10), Method: "hello", Payload: wire.HelloPayload{HostInstanceId: marketID(1), NativeProcessEpoch: marketID(6), OwnerUserId: marketID(7), TenantId: marketID(8)}}
	input, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	inputReader, inputWriter := io.Pipe()
	outputReader, outputWriter := io.Pipe()
	finished := make(chan error, 1)
	go func() { finished <- ServeMarketControl(context.Background(), inputReader, outputWriter, service) }()
	if _, err := inputWriter.Write(append(input, '\n')); err != nil {
		t.Fatal(err)
	}
	output, err := bufio.NewReader(outputReader).ReadBytes('\n')
	if err != nil {
		t.Fatal(err)
	}
	var response wire.HelloResponse
	if err := json.Unmarshal(output, &response); err != nil {
		t.Fatal(err)
	}
	_ = inputWriter.Close()
	if err := <-finished; err != nil {
		t.Fatal(err)
	}
	_ = outputReader.Close()
	_ = outputWriter.Close()
	if response.RequestId != request.RequestId || !response.Data.Ready || service.hello != 1 || service.close != 1 || service.submits != 0 {
		t.Fatal("hello/normal EOF changed execution semantics")
	}
}

func TestMarketHTTPReadOnlyViewsAndOpaqueTrigger(t *testing.T) {
	service := &marketAdapterFixture{enabled: true}
	mux := http.NewServeMux()
	const token = "declared-local-http-fixture"
	registerMarketRoutes(mux, service, token, string(marketID(1)))
	paths := []string{
		"/v1/market-chat/tasks/" + string(marketID(2)) + "/operations/" + string(marketID(5)),
		"/v1/market-chat/agent-sessions/" + string(marketID(11)) + "/approvals",
		"/v1/market-chat/agent-sessions/" + string(marketID(11)) + "/turns/" + string(marketID(12)) + "/tools",
	}
	for _, path := range paths {
		request := httptest.NewRequest(http.MethodGet, path, nil)
		request.Header.Set("Authorization", "Bearer "+token)
		request.Header.Set("X-Yijie-Market-Request-Id", string(marketID(10)))
		response := httptest.NewRecorder()
		mux.ServeHTTP(response, request)
		if response.Code != http.StatusOK || response.Header().Get("Cache-Control") != "no-store" || response.Header().Get("Content-Type") != "application/json" {
			t.Fatalf("read failed: %d", response.Code)
		}
		var envelope map[string]json.RawMessage
		if json.Unmarshal(response.Body.Bytes(), &envelope) != nil || string(envelope["requestId"]) != `"`+string(marketID(10))+`"` {
			t.Fatal("correlation not echoed")
		}
	}
	if service.reads != 3 || service.submits != 0 || service.hello != 0 {
		t.Fatal("read acquired write authority")
	}
	payload := wire.SubmitRequest{SchemaVersion: 1, RequestId: marketID(10), Payload: wire.SubmitPayload{GrantRef: marketID(13), TurnOperationId: marketID(5)}}
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/v1/market-chat/submissions", bytes.NewReader(body))
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, request)
	var result wire.SubmitResponse
	if response.Code != http.StatusAccepted || json.Unmarshal(response.Body.Bytes(), &result) != nil || result.Data.State != "pending" || service.submits != 1 {
		t.Fatal("opaque trigger did not preserve receipt")
	}
}
func TestMarketProviderOnlyConfigurationAndReadiness(t *testing.T) {
	home := t.TempDir()
	manifest := filepath.Join(home, "worker-manifest.json")
	if err := os.WriteFile(manifest, []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("YIJIE_ENV", "local")
	t.Setenv("YIJIE_LOCAL_PROFILE", "demo_fast")
	t.Setenv("YIJIE_MARKET_CONNECTORS_ENABLED", "true")
	t.Setenv("YIJIE_MARKET_PROVIDER_ONLY", "true")
	t.Setenv("YIJIE_MARKET_WORKER_MANIFEST", manifest)
	t.Setenv("YIJIE_AGENT_HOST_INSTANCE_NONCE", string(marketID(1)))
	config := Config{Environment: "local", HostHome: home, ParentPID: os.Getppid(), InstanceNonce: string(marketID(1)), Runtime: codex.DefaultConfig()}
	if err := loadMarketProfile(&config); err != nil {
		t.Fatal(err)
	}
	if !config.MarketProviderOnly || config.Runtime.Configured() || config.Runtime.Market.LibraryHome != filepath.Join(home, "market-worker", "library-home") {
		t.Fatal("provider-only imported Runtime or non-isolated storage")
	}
	handler := NewHandler(config, staticRuntimeStatus{status: codex.Status{State: codex.StateNotConfigured}}, nil, "")
	ready := httptest.NewRecorder()
	handler.ServeHTTP(ready, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	live := httptest.NewRecorder()
	handler.ServeHTTP(live, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if ready.Code != http.StatusServiceUnavailable || live.Code != http.StatusOK {
		t.Fatal("provider-only fabricated Runtime readiness")
	}
}
