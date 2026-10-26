package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/36Dge/yijie-agent-host/internal/codex"
	"github.com/36Dge/yijie-agent-host/internal/security"
	"github.com/36Dge/yijie-agent-host/internal/session"
	"github.com/google/uuid"
)

// Real Host handlers/store and the pinned Runtime talk to the Rust Native test.
// The test bootstrap uses independent loopback ports, never the active App port.
func TestFEAT164NativeHostBoundary(t *testing.T) {
	binary, manifest, desktop := os.Getenv("YIJIE_CODEX_INTEGRATION_BINARY"), os.Getenv("YIJIE_CODEX_INTEGRATION_MANIFEST"), os.Getenv("YIJIE_FEAT164_DESKTOP_SOURCE")
	if binary == "" || manifest == "" || desktop == "" {
		t.Skip("explicit pinned artifacts and Native test source required")
	}
	parent := os.Getenv("YIJIE_FEAT164_S2_RETAIN_ROOT")
	if parent == "" || !filepath.IsAbs(parent) {
		t.Fatal("explicit absolute retained S2 output directory required")
	}
	info, err := os.Stat(parent)
	if err != nil || !info.IsDir() {
		t.Fatal("retained S2 output parent unavailable")
	}
	root, err := os.MkdirTemp(parent, "feat164-native-boundary-")
	if err != nil {
		t.Fatal(err)
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"host", "runtime", "workspace", "native"} {
		if err = os.Mkdir(filepath.Join(root, name), 0700); err != nil {
			t.Fatal(err)
		}
	}
	var requests, starts atomic.Int64
	providerListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	provider := &http.Server{ReadHeaderTimeout: 5 * time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if r.Method != "POST" || r.URL.Path != "/v1/responses" || json.NewDecoder(r.Body).Decode(&body) != nil {
			t.Error("unexpected loopback request")
			w.WriteHeader(400)
			return
		}
		if requests.Add(1) != 1 || body["model"] != "MiniMax-M3" {
			t.Error("model configuration or cancelled delivery sent a model request")
			w.WriteHeader(429)
			return
		}
		item := map[string]any{"id": "ordinary_message", "type": "message", "role": "assistant", "phase": "final_answer", "status": "completed", "content": []any{map[string]any{"type": "output_text", "text": "FEAT164 native boundary completed.", "annotations": []any{}}}}
		w.Header().Set("Content-Type", "text/event-stream")
		for _, event := range []any{map[string]any{"type": "response.created", "response": map[string]any{"id": "ordinary_response", "status": "in_progress", "output": []any{}}}, map[string]any{"type": "response.output_item.added", "output_index": 0, "item": item}, map[string]any{"type": "response.output_item.done", "output_index": 0, "item": item}, map[string]any{"type": "response.completed", "response": map[string]any{"id": "ordinary_response", "status": "completed", "output": []any{item}, "usage": map[string]any{"input_tokens": 1, "output_tokens": 1, "total_tokens": 2}}}} {
			raw, _ := json.Marshal(event)
			fmt.Fprintf(w, "data: %s\n\n", raw)
			w.(http.Flusher).Flush()
		}
	})}
	go func() { _ = provider.Serve(providerListener) }()
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if e := provider.Shutdown(ctx); e != nil {
			t.Error(e)
		}
	}()
	config := codex.DefaultConfig()
	config.BinaryPath = binary
	config.ManifestPath = manifest
	config.CodexHome = filepath.Join(root, "runtime")
	config.StartupTimeout = 30 * time.Second
	config.RequestTimeout = 30 * time.Second
	config.ShutdownTimeout = 20 * time.Second
	config.ChatModelsEnabled = true
	config.KimiK31Candidate = true
	config.RuntimePermissionsEnabled = true
	config.MiniMax = codex.MiniMaxConfig{Enabled: true, APIKey: "ordinary-local-minimax"}
	config.KimiAPIKey = "ordinary-local-kimi"
	config.PermissionVerificationBaseURL = "http://" + providerListener.Addr().String() + "/v1"
	var runtime *codex.Manager
	var store *session.Store
	var server *http.Server
	var address, nonce string
	stopped := true
	stop := func() bool {
		if stopped {
			return true
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if e := server.Shutdown(ctx); e != nil {
			t.Error("HTTP normal cleanup pending; retained", e)
			return false
		}
		if e := runtime.Shutdown(ctx); e != nil {
			t.Error("Runtime normal cleanup pending; retained", e)
			return false
		}
		if e := store.Close(); e != nil {
			t.Error(e)
			return false
		}
		stopped = true
		return true
	}
	// Retain the complete Runtime/Host/Native state on success and failure.
	// Only the normal owned shutdown path closes resources; never force cleanup.
	t.Logf("FEAT164_S2_RETAINED_ROOT=%s", root)
	defer stop()
	start := func() {
		runtime = codex.NewManager(config, nil)
		store, err = session.OpenStore(filepath.Join(root, "host"), session.WithChatModelV2Storage())
		if err != nil {
			t.Fatal(err)
		}
		service := session.NewService(runtime, store, session.NewEventHub(64, 32), nil, session.WithNativeReasoning())
		if err = runtime.SetNotificationHandler(service.HandleNotification); err != nil {
			t.Fatal(err)
		}
		token, e := security.LoadOrCreateAPIToken(filepath.Join(root, "host"))
		if e != nil {
			t.Fatal(e)
		}
		nonce = uuid.NewString()
		listener, e := net.Listen("tcp", "127.0.0.1:0")
		if e != nil {
			t.Fatal(e)
		}
		address = listener.Addr().String()
		handler := NewHandler(Config{Environment: "local", Runtime: config, HostHome: filepath.Join(root, "host"), NativeReasoningEnabled: true, MultimodalV2Enabled: true, InstanceNonce: nonce}, runtime, service, token)
		server = &http.Server{ReadHeaderTimeout: 5 * time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == "POST" && (strings.HasSuffix(r.URL.Path, "/turns") || strings.HasSuffix(r.URL.Path, "/permission-turns")) {
				starts.Add(1)
			}
			handler.ServeHTTP(w, r)
		})}
		stopped = false
		go func() { _ = server.Serve(listener) }()
		if e = runtime.Start(context.Background()); e != nil {
			t.Fatal(e)
		}
	}
	runNative := func(phase string) {
		setup := map[string]any{"root": root, "port": strings.Split(address, ":")[1], "nonce": nonce, "phase": phase, "owner": "00000000-0000-4000-8000-000000000164", "tenant": "00000000-0000-4000-8000-000000000165"}
		raw, _ := json.Marshal(setup)
		path := filepath.Join(root, "boundary.json")
		if err = os.WriteFile(path, raw, 0600); err != nil {
			t.Fatal(err)
		}
		cmd := exec.Command("cargo", "test", "--manifest-path", "src-tauri/Cargo.toml", "--features", "kimi-k31-pro-candidate", "--lib", "chat::feat164_boundary_tests::feat164_native_host_boundary", "--", "--exact", "--ignored", "--test-threads=1", "--nocapture")
		cmd.Dir = desktop
		for _, key := range []string{"PATH", "HOME", "TMPDIR", "CARGO_HOME", "RUSTUP_HOME", "SDKROOT", "DEVELOPER_DIR", "PKG_CONFIG_PATH"} {
			if value, ok := os.LookupEnv(key); ok {
				cmd.Env = append(cmd.Env, key+"="+value)
			}
		}
		cmd.Env = append(cmd.Env, "YIJIE_ENV=local", "YIJIE_LOCAL_PROFILE=demo_fast", "YIJIE_CHAT_MODELS_ENABLED=true", "YIJIE_KIMI_K31_PRO_CANDIDATE=true", "YIJIE_FEAT164_BOUNDARY_SETUP="+path)
		// No CommandContext/kill-on-timeout: the test owns a normal bounded protocol.
		out, e := cmd.CombinedOutput()
		t.Logf("Native phase %s:\n%s", phase, out)
		if e != nil || !strings.Contains(string(out), "1 passed; 0 failed") {
			t.Fatalf("Native phase did not execute successfully: %v", e)
		}
	}
	start()
	runNative("prepare")
	if !stop() {
		return
	}
	if requests.Load() != 1 || starts.Load() != 1 {
		t.Fatal("first phase call budget", requests.Load(), starts.Load())
	}
	start()
	runNative("reopen")
	if !stop() {
		return
	}
	if requests.Load() != 1 || starts.Load() != 1 {
		t.Fatal("reopen/cancel sent another turn", requests.Load(), starts.Load())
	}
	t.Log("FEAT164_NATIVE_BOUNDARY_PASS: Native SQL40 + Host Store11 + pinned Runtime; normal Host/Native reopen; original receipt/current selection; cancelled POST=0; loopback model requests=1; external model requests=0")
}
