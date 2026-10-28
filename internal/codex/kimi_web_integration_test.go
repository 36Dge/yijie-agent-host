package codex

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	wire "github.com/36Dge/yijie-agent-host/internal/contracts/marketbrokercontrol"
	connectors "github.com/36Dge/yijie-agent-host/internal/contracts/marketconnectors"
	provider "github.com/36Dge/yijie-agent-host/internal/contracts/marketprovider"
	"github.com/google/uuid"
)

// This uses the canonical product worker, an ordinary in-memory key and local
// preparation only. It never starts Runtime, sends a model turn or calls a tool.
func TestFEAT162K31ManagerPreparesSharedCredentialWithoutInference(t *testing.T) {
	manifest := os.Getenv("YIJIE_MARKET_BROKER_TEST_MANIFEST")
	if manifest == "" {
		t.Skip("canonical product worker manifest required")
	}
	for _, available := range []bool{true, false} {
		name := "missing_source"
		if available {
			name = "existing_model_source"
		}
		t.Run(name, func(t *testing.T) {
			home, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			home = filepath.Join(home, "host-home")
			if err := os.Mkdir(home, 0700); err != nil {
				t.Fatal(err)
			}
			c := DefaultConfig()
			c.ChatModelsEnabled, c.KimiK31Candidate = true, true
			c.KimiAPIKey = ""
			if available {
				c.KimiAPIKey = "ordinary-local-model-source"
			}
			c.Market = MarketConfig{Manifest: manifest, LibraryHome: filepath.Join(home, "market-worker", "library-home"), HostInstanceID: uuid.NewString()}
			m := NewManager(c, nil)
			if m.ModelAvailable("kimi-k3-max-v1") || m.ModelAvailable(KimiK31ProfileID) != available {
				t.Fatal("active K3.1 / retired K3 model admission changed")
			}
			t.Cleanup(func() {
				ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
				defer cancel()
				if err := m.closeMarketBroker(ctx); err != nil {
					t.Errorf("normal worker EOF: %v; no signals sent", err)
				}
			})
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if err := m.StartMarketProvider(ctx); err != nil {
				t.Fatal(err)
			}
			owner, err := m.MarketOwner()
			if err != nil {
				t.Fatal(err)
			}
			payload := provider.ProviderPayload{
				OperationId: provider.CanonicalId(uuid.NewString()), HostInstanceId: provider.CanonicalId(c.Market.HostInstanceID),
				Scope:   wire.ScopeBinding{OwnerUserId: wire.CanonicalId(uuid.NewString()), TenantId: wire.CanonicalId(uuid.NewString()), NativeProcessEpoch: wire.CanonicalId(uuid.NewString()), AuthorizationRevision: 1, AuthorizationExpiresAtUnixMs: wire.UnixMilliseconds(time.Now().Add(time.Minute).UnixMilli())},
				Binding: provider.ProviderBinding{Reference: provider.SelectionRef{InstallationId: connectors.CanonicalId(uuid.NewString()), Revision: 1, Generation: 1}, ServiceId: "kimi-web", CredentialRef: provider.CanonicalId(uuid.NewString())},
			}
			for {
				response, err := owner.Probe(ctx, provider.ProbeRequest{SchemaVersion: 1, RequestId: provider.CanonicalId(uuid.NewString()), Method: "provider_probe", Payload: payload})
				if err != nil {
					t.Fatal(err)
				}
				s := response.Data
				if s.OperationState == "starting" {
					select {
					case <-ctx.Done():
						t.Fatal("local preparation did not settle")
					case <-time.After(10 * time.Millisecond):
					}
					continue
				}
				if s.AuthorizationUrl != nil || s.ExecutionAvailable != available {
					t.Fatal("preparation unexpectedly requires a credential page or has incorrect availability")
				}
				if available && (s.OperationState != "succeeded" || s.Qualification != "qualified") {
					t.Fatal("current K3.1 model source was not reused")
				}
				if !available && (s.OperationState != "failed" || s.ErrorCode == nil || *s.ErrorCode != "not_configured") {
					t.Fatal("missing source was not reported")
				}
				break
			}
		})
	}
}
