package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/36Dge/yijie-agent-host/internal/codex"
	agenthostcontract "github.com/36Dge/yijie-agent-host/internal/contracts"
	"github.com/36Dge/yijie-agent-host/internal/security"
	"github.com/36Dge/yijie-agent-host/internal/session"
	"github.com/36Dge/yijie-agent-host/internal/skills"
	"github.com/google/uuid"
)

// Real Host handlers/store and the pinned Runtime talk to the Rust Native test.
// The test bootstrap uses independent loopback ports, never the active App port.
func TestFEAT164NativeHostBoundary(t *testing.T) {
	runNativeHostBoundary(t, false)
}

func TestPromptSelectedSkillBoundary(t *testing.T) {
	runNativeHostBoundary(t, true)
}

func runNativeHostBoundary(t *testing.T, selectedSkill bool) {
	t.Helper()
	binary, manifest, desktop := os.Getenv("YIJIE_CODEX_INTEGRATION_BINARY"), os.Getenv("YIJIE_CODEX_INTEGRATION_MANIFEST"), os.Getenv("YIJIE_FEAT164_DESKTOP_SOURCE")
	if binary == "" || manifest == "" || desktop == "" {
		t.Skip("explicit pinned artifacts and Native test source required")
	}
	root, err := os.MkdirTemp("", "feat164-native-boundary-")
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
	var replayMu sync.Mutex
	var replayBody []byte
	var replayPath string
	var replayHeaders http.Header
	var bundle string
	var skillManifest skills.Manifest
	if selectedSkill {
		bundle = filepath.Join(root, "bundle")
		if err = os.MkdirAll(filepath.Join(bundle, "packages"), 0700); err != nil {
			t.Fatal(err)
		}
		// Copy only the ordinary pinned model-only bundle. No attack archives.
		for source, target := range map[string]string{
			"manifest-valid.json":                   "bundle-manifest.json",
			"packages/fixture-model-only-0.1.0.zip": "packages/fixture-model-only-0.1.0.zip",
		} {
			raw, e := os.ReadFile(filepath.Join("../../api/fixtures/skills/bundle-v1", source))
			if e != nil {
				t.Fatal(e)
			}
			if e = os.WriteFile(filepath.Join(bundle, target), raw, 0600); e != nil {
				t.Fatal(e)
			}
			if source == "manifest-valid.json" {
				if e = json.Unmarshal(raw, &skillManifest); e != nil {
					t.Fatal(e)
				}
			}
		}
	}
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
		model := "MiniMax-M3"
		if selectedSkill {
			model = "kimi-k3.1-pro"
		}
		if requests.Add(1) != 1 || body["model"] != model {
			t.Error("model configuration or cancelled delivery sent a model request")
			w.WriteHeader(429)
			return
		}
		if selectedSkill {
			input, _ := json.Marshal(body["input"])
			if strings.Count(string(input), "Return the supplied text unchanged.") != 1 ||
				!strings.Contains(string(input), "## Available skills") {
				t.Error("native Skill body must be loaded once with the native metadata catalogue")
			}
			if !strings.Contains(string(input), "请简短说明六乘七。") {
				t.Error("renderer text did not reach Runtime")
			}
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
	var resources *skills.Service
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
		if resources != nil {
			if e := resources.Close(); e != nil {
				t.Error(e)
				return false
			}
			resources = nil
		}
		if e := store.Close(); e != nil {
			t.Error(e)
			return false
		}
		stopped = true
		return true
	}
	defer func() {
		if stop() {
			_ = os.RemoveAll(root)
		}
	}()
	start := func() {
		runtime = codex.NewManager(config, nil)
		options := []session.ServiceOption{session.WithNativeReasoning()}
		if selectedSkill {
			resources, err = skills.NewService(skills.Config{BundleRoot: bundle, ManagedRoot: filepath.Join(root, "installed"), Runtime: runtime})
			if err != nil {
				t.Fatal(err)
			}
			options = append(options, session.WithSelectedSkillResources(resources))
		}
		store, err = session.OpenStore(filepath.Join(root, "host"), session.WithChatModelV2Storage())
		if err != nil {
			t.Fatal(err)
		}
		service := session.NewService(runtime, store, session.NewEventHub(64, 32), nil, options...)
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
				if selectedSkill {
					raw, e := io.ReadAll(r.Body)
					if e != nil {
						t.Error(e)
						w.WriteHeader(400)
						return
					}
					r.Body = io.NopCloser(bytes.NewReader(raw))
					replayMu.Lock()
					replayBody, replayPath, replayHeaders = raw, r.URL.Path, r.Header.Clone()
					replayMu.Unlock()
				}
			}
			handler.ServeHTTP(w, r)
		})}
		stopped = false
		go func() { _ = server.Serve(listener) }()
		if e = runtime.Start(context.Background()); e != nil {
			t.Fatal(e)
		}
		if selectedSkill {
			snapshot, e := resources.List(context.Background())
			if e != nil {
				t.Fatal(e)
			}
			skill := skillManifest.Skills[0]
			if snapshot.Skills[0].InstallationStatus != "installed" {
				_, e = resources.Install(context.Background(), skills.InstallInput{OperationID: uuid.NewString(), SkillID: skill.ID, ExpectedVersion: skill.Version, ExpectedArchiveSHA256: skill.Archive.SHA256, CatalogRevision: snapshot.CatalogRevision})
				if e != nil {
					t.Fatal(e)
				}
			}
			_, e = resources.ResolveSelectedSkills(context.Background(), []agenthostcontract.SelectedSkillRef{{Id: skill.ID, Version: "9.0.0"}})
			if skills.ErrorCodeOf(e) != skills.CodeCapabilityUnavailable {
				t.Fatal("ordinary stale version was not rejected", e)
			}
		}
	}
	runNative := func(phase string) {
		setup := map[string]any{"root": root, "port": strings.Split(address, ":")[1], "nonce": nonce, "phase": phase, "owner": "00000000-0000-4000-8000-000000000164", "tenant": "00000000-0000-4000-8000-000000000165", "prompt_skill": selectedSkill}
		raw, _ := json.Marshal(setup)
		path := filepath.Join(root, "boundary.json")
		if err = os.WriteFile(path, raw, 0600); err != nil {
			t.Fatal(err)
		}
		cmd := exec.Command("cargo", "test", "--manifest-path", "src-tauri/Cargo.toml", "--features", "kimi-k31-pro-candidate", "--lib", "chat::feat164_boundary_tests::feat164_native_host_boundary", "--", "--exact", "--ignored", "--test-threads=1", "--nocapture")
		if selectedSkill && phase == "prepare" {
			cmd = exec.Command("pnpm", "exec", "vitest", "run", "src/pages/chat/ChatPage.test.ts", "-t", "selected skill continuous Native Host Runtime boundary")
		}
		cmd.Dir = desktop
		for _, key := range []string{"PATH", "HOME", "TMPDIR", "CARGO_HOME", "RUSTUP_HOME", "SDKROOT", "DEVELOPER_DIR", "PKG_CONFIG_PATH"} {
			if value, ok := os.LookupEnv(key); ok {
				cmd.Env = append(cmd.Env, key+"="+value)
			}
		}
		cmd.Env = append(cmd.Env, "YIJIE_ENV=local", "YIJIE_LOCAL_PROFILE=demo_fast", "YIJIE_CHAT_MODELS_ENABLED=true", "YIJIE_KIMI_K31_PRO_CANDIDATE=true", "YIJIE_FEAT164_BOUNDARY_SETUP="+path)
		if selectedSkill {
			cmd.Env = append(cmd.Env, "YIJIE_PROMPT_BOUNDARY_SETUP="+path, "VITE_YIJIE_CHAT_MODELS_ENABLED=true", "VITE_YIJIE_KIMI_K31_PRO_CANDIDATE=true", "VITE_YIJIE_NATIVE_SKILL_INPUT_ENABLED=true")
		}
		// No CommandContext/kill-on-timeout: the test owns a normal bounded protocol.
		out, e := cmd.CombinedOutput()
		t.Logf("Native phase %s:\n%s", phase, out)
		marker := "1 passed; 0 failed"
		if selectedSkill && phase == "prepare" {
			marker = "PROMPT_SKILL_RENDERER_PASS"
		}
		if e != nil || !strings.Contains(string(out), marker) {
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
	if selectedSkill {
		// Accepted operation replay goes through the real HTTP/session service;
		// removing a selection is not required to manufacture a failure.
		replayMu.Lock()
		body, path, headers := append([]byte(nil), replayBody...), replayPath, replayHeaders.Clone()
		replayMu.Unlock()
		req, e := http.NewRequest(http.MethodPost, "http://"+address+path, bytes.NewReader(body))
		if e != nil {
			t.Fatal(e)
		}
		req.Header = headers
		response, e := (&http.Client{Timeout: 15 * time.Second}).Do(req)
		if e != nil {
			t.Fatal(e)
		}
		_ = response.Body.Close()
		if response.StatusCode != http.StatusAccepted {
			t.Fatal("accepted Skill replay rejected", response.StatusCode)
		}
	}
	if !stop() {
		return
	}
	expectedStarts := int64(1)
	if selectedSkill {
		expectedStarts = 2
	}
	if requests.Load() != 1 || starts.Load() != expectedStarts {
		t.Fatal("reopen/cancel sent another turn", requests.Load(), starts.Load())
	}
	if selectedSkill {
		t.Log("PROMPT_SKILL_BOUNDARY_PASS: actual Vue picker/page/store/client + Native decoder/SQL reopen + verified Host installation/version + unchanged Runtime Skill loader; body copies=1; accepted replay model requests=0; total loopback requests=1; supplier requests=0; macOS IPC/Native authorization facade/coordinator scheduling/visual/real-model semantics NOT RUN")
	} else {
		t.Log("FEAT164_NATIVE_BOUNDARY_PASS: Native/Host current readers + pinned Runtime; normal Host/Native reopen; original receipt/current selection; cancelled POST=0; loopback model requests=1; external model requests=0")
	}
}
