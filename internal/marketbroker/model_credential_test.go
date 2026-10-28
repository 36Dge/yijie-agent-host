package marketbroker

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	wire "github.com/36Dge/yijie-agent-host/internal/contracts/marketbrokercontrol"
	provider "github.com/36Dge/yijie-agent-host/internal/contracts/marketprovider"
)

func TestKimiModelCredentialPipeAndRedaction(t *testing.T) {
	key := strings.Repeat("a", wire.KimiModelCredentialMaxKeyBytes)
	reader, writer, err := prepareModelCredential(key)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	go writeModelCredential(writer, key)
	data, err := io.ReadAll(reader)
	if err != nil || string(data) != wire.KimiModelCredentialPreamble+key+wire.KimiModelCredentialTerminator {
		t.Fatal("model snapshot handoff differs")
	}
	if strings.Contains(modelCredentialHint(), key) {
		t.Fatal("secret in startup hint")
	}
	owner := NewOwner("ordinary-manifest")
	owner.kimiModelKey = "ordinary-private-model-value"
	if strings.Contains(fmt.Sprintf("%#v", owner), owner.kimiModelKey) {
		t.Fatal("secret in diagnostic")
	}
	if got := ScrubRuntimeCapability([]string{"PATH=/usr/bin", modelCredentialHint()}); len(got) != 1 {
		t.Fatal("private source hint inherited")
	}
}

// Only the canonical product worker and ordinary in-memory values are used.
// Neither legacy Configure nor direct preparation uses supplier HTTP.
// Cleanup uses normal EOF, never signals.
func TestKimiModelCredentialCanonicalLocalBinding(t *testing.T) {
	manifest := os.Getenv("YIJIE_MARKET_BROKER_TEST_MANIFEST")
	if manifest == "" {
		t.Skip("requires explicit canonical worker manifest")
	}
	for _, direct := range []bool{false, true} {
		for _, available := range []bool{true, false} {
			t.Run(fmt.Sprintf("direct=%t/source=%t", direct, available), func(t *testing.T) {
				parent, err := filepath.EvalSymlinks(t.TempDir())
				if err != nil {
					t.Fatal(err)
				}
				home := filepath.Join(parent, "host-home")
				if err := os.Mkdir(home, 0700); err != nil {
					t.Fatal(err)
				}
				key := ""
				if available {
					key = "ordinary-model-snapshot-value"
				}
				owner, err := NewOwnerWithModelCredential(manifest, filepath.Join(home, "market-worker", "library-home"), key)
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() {
					ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
					defer cancel()
					if err := owner.Close(ctx); err != nil {
						t.Errorf("normal EOF cleanup: %v; ownership retained, no signals", err)
					}
				})
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				if err := owner.StartControl(ctx); err != nil {
					t.Fatal(err)
				}
				payload := provider.ProviderPayload{OperationId: controlID(), HostInstanceId: controlID(), Scope: wire.ScopeBinding{OwnerUserId: controlID(), TenantId: controlID(), NativeProcessEpoch: controlID(), AuthorizationRevision: 1, AuthorizationExpiresAtUnixMs: wire.UnixMilliseconds(time.Now().Add(time.Minute).UnixMilli())}, Binding: provider.ProviderBinding{Reference: provider.SelectionRef{InstallationId: controlID(), Revision: 1, Generation: 1}, ServiceId: "kimi-web", CredentialRef: controlID()}}
				request := func(first bool) (provider.ProviderStatus, error) {
					if direct {
						response, err := owner.Probe(ctx, provider.ProbeRequest{SchemaVersion: 1, RequestId: controlID(), Method: "provider_probe", Payload: payload})
						return response.Data, err
					}
					if first {
						response, err := owner.AuthBegin(ctx, provider.AuthBeginRequest{SchemaVersion: 1, RequestId: controlID(), Method: "provider_auth_begin", Payload: payload})
						return response.Data, err
					}
					response, err := owner.AuthPoll(ctx, provider.AuthPollRequest{SchemaVersion: 1, RequestId: controlID(), Method: "provider_auth_poll", Payload: payload})
					return response.Data, err
				}
				status, err := request(true)
				if err != nil {
					t.Fatal(err)
				}
				for status.OperationState == "starting" {
					select {
					case <-ctx.Done():
						t.Fatal("local binding did not complete")
					case <-time.After(10 * time.Millisecond):
					}
					polled, err := request(false)
					if err != nil {
						t.Fatal(err)
					}
					status = polled
				}
				if available && (status.OperationState != "succeeded" || status.AuthorizationStatus != "authorized") {
					t.Fatal("model snapshot not bound")
				}
				if !available && (status.OperationState != "failed" || status.ErrorCode == nil || *status.ErrorCode != "not_configured") {
					t.Fatal("missing model source not reported")
				}
				if direct && available {
					if !status.ExecutionAvailable || status.Qualification != "qualified" || status.ConnectionStatus != "connected" {
						t.Fatal("direct local preparation did not register current invocation support")
					}
				} else if status.ExecutionAvailable || status.Qualification != "not_qualified" {
					t.Fatal("unprepared or missing model source granted invocation support")
				}
				if !direct && status.ConnectionStatus != "disconnected" {
					t.Fatal("legacy local binding implied a connection")
				}
				if status.AuthorizationUrl != nil {
					t.Fatal("model credential reuse requires no authorization page")
				}
				encoded, err := json.Marshal(status)
				if err != nil {
					t.Fatal(err)
				}
				if available && strings.Contains(string(encoded), key) {
					t.Fatal("model secret in control response")
				}
			})
		}
	}
}
