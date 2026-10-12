package codex

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"sync/atomic"
	"testing"
	"time"
)

// The actual fixed Runtime parses the same disabled configuration used for a
// fresh expert thread. No turn, MCP call, Provider call or credential is used.
func TestFEAT158PinnedRuntimeStartsExpertWithDisabledMarket(t *testing.T) {
	binary, manifest := integrationArtifactPaths(t)
	listener, err := net.Listen("tcp", "127.0.0.1:18083")
	if err != nil {
		t.Fatal("local qualification port occupied; no process stopped")
	}
	var requests atomic.Int64
	server := &http.Server{ReadHeaderTimeout: 5 * time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusNoContent)
	})}
	go func() { _ = server.Serve(listener) }()
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(ctx); err != nil {
			t.Error("normal local observer shutdown pending")
		}
	}()
	config := integrationConfig(binary, manifest, integrationPrivateTempDir(t), "local-no-turn-minimax")
	config.RequestTimeout = 20 * time.Second
	config.ChatModelsEnabled = true
	config.KimiAPIKey = "local-no-turn-kimi"
	config.RuntimePermissionsEnabled = true
	config.PermissionVerificationBaseURL = "http://127.0.0.1:18083/v1"
	m := NewManager(config, nil)
	if err := m.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer shutdownIntegrationManager(t, m, config.ShutdownTimeout)
	for _, profile := range []string{"kimi-k3-max-v1", "minimax-m3-high-v1"} {
		for _, inherited := range []map[string]json.RawMessage{
			nil,
			{"ordinary_local": json.RawMessage(`{"url":"http://127.0.0.1:18083/mcp","enabled":true}`)},
		} {
			servers, err := disabledMarketServers(inherited, "http://127.0.0.1:18083")
			if err != nil {
				t.Fatal(err)
			}
			ctx, err := WithExpertInstructions(WithModelProfile(context.Background(), profile), "Organize these benign local notes.", "Reference index: ordinary notes.")
			if err != nil {
				t.Fatal(err)
			}
			ctx = WithMarketPrepared(ctx, &MarketPrepared{Config: map[string]any{"mcp_servers": servers}})
			thread, err := m.StartThread(ctx, t.TempDir())
			selected, _ := ModelProfile(profile)
			if err != nil || thread.ID == "" || thread.Model != selected.Model || thread.ModelProvider != selected.Provider || thread.ReasoningEffort != selected.Effort {
				t.Fatal("fixed Runtime rejected normal expert/disabled-MCP configuration", thread, err)
			}
		}
	}
	if requests.Load() != 0 {
		t.Fatal("zero-turn configuration qualification issued an HTTP request")
	}
	t.Log("4 thread/start acknowledged; 0 turns; 0 Provider/MCP HTTP requests")
}
