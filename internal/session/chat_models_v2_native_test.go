package session

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/36Dge/yijie-agent-host/internal/codex"
	wire "github.com/36Dge/yijie-agent-host/internal/contracts/chatmodelsv2"
)

// Real pinned Runtime and Host service/store, ordinary loopback response only.
// The switch/recovery steps must generate no model HTTP at all.
func TestFEAT164V2NativeOperationLifecycle(t *testing.T) {
	binary, manifest := os.Getenv("YIJIE_CODEX_INTEGRATION_BINARY"), os.Getenv("YIJIE_CODEX_INTEGRATION_MANIFEST")
	if binary == "" || manifest == "" {
		t.Skip("set pinned integration artifact paths")
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var requests atomic.Int64
	server := &http.Server{ReadHeaderTimeout: 5 * time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if r.Method != "POST" || r.URL.Path != "/v1/responses" || json.NewDecoder(r.Body).Decode(&body) != nil {
			t.Error("unexpected loopback request")
			w.WriteHeader(400)
			return
		}
		if requests.Add(1) != 1 || body["model"] != "MiniMax-M3" {
			t.Error("configuration recovery sent model HTTP")
			w.WriteHeader(429)
			return
		}
		item := map[string]any{"id": "ordinary_message", "type": "message", "role": "assistant", "phase": "final_answer", "status": "completed", "content": []any{map[string]any{"type": "output_text", "text": "Ordinary local fixture.", "annotations": []any{}}}}
		w.Header().Set("Content-Type", "text/event-stream")
		for _, event := range []any{map[string]any{"type": "response.created", "response": map[string]any{"id": "ordinary_response", "status": "in_progress", "output": []any{}}}, map[string]any{"type": "response.output_item.added", "output_index": 0, "item": item}, map[string]any{"type": "response.output_item.done", "output_index": 0, "item": item}, map[string]any{"type": "response.completed", "response": map[string]any{"id": "ordinary_response", "status": "completed", "output": []any{item}, "usage": map[string]any{"input_tokens": 1, "output_tokens": 1, "total_tokens": 2}}}} {
			raw, _ := json.Marshal(event)
			fmt.Fprintf(w, "data: %s\n\n", raw)
			w.(http.Flusher).Flush()
		}
	})}
	go func() { _ = server.Serve(listener) }()
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(ctx); err != nil {
			t.Error(err)
		}
	}()
	root, err := os.MkdirTemp("", "feat164-s2-native-")
	if err != nil {
		t.Fatal(err)
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"runtime", "host", "workspace"} {
		if err = os.Mkdir(filepath.Join(root, name), 0700); err != nil {
			t.Fatal(err)
		}
	}
	config := codex.DefaultConfig()
	config.BinaryPath = binary
	config.ManifestPath = manifest
	config.CodexHome = filepath.Join(root, "runtime")
	config.StartupTimeout = 30 * time.Second
	config.RequestTimeout = 30 * time.Second
	config.ShutdownTimeout = 20 * time.Second
	config.ChatModelsEnabled, config.KimiK31Candidate, config.RuntimePermissionsEnabled = true, true, true
	config.MiniMax = codex.MiniMaxConfig{Enabled: true, APIKey: "ordinary-local-minimax"}
	config.KimiAPIKey = "ordinary-local-kimi"
	config.PermissionVerificationBaseURL = "http://" + listener.Addr().String() + "/v1"
	runtime := codex.NewManager(config, nil)
	store, err := OpenStore(filepath.Join(root, "host"), WithChatModelV2Storage())
	if err != nil {
		t.Fatal(err)
	}
	s := NewService(runtime, store, NewEventHub(32, 16), nil, WithNativeReasoning())
	if err = runtime.SetNotificationHandler(s.HandleNotification); err != nil {
		t.Fatal(err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
		defer cancel()
		if err := runtime.Shutdown(ctx); err != nil {
			t.Error("normal cleanup pending; files retained", err)
			_ = s.store.Close()
			return
		}
		if err := s.store.Close(); err != nil {
			t.Error(err)
			return
		}
		_ = os.RemoveAll(root)
	}()
	if err = runtime.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	ctx := codex.WithPermissionMode(codex.WithModelProfile(context.Background(), "minimax-m3-high-v1"), codex.PermissionAsk)
	record, err := s.StartSession(ctx, StartSessionInput{TaskID: testTaskID, Cwd: filepath.Join(root, "workspace")})
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.StartTurnV2(ctx, StartTurnV2Input{AgentSessionID: record.AgentSessionID, OperationID: testTurnOperationID, ContentBlocks: []TurnContentBlock{{Type: ContentBlockText, Text: "Return a short local fixture; do not call tools."}}})
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(30 * time.Second)
	for {
		record, err = s.store.Get(record.AgentSessionID)
		if err != nil {
			t.Fatal(err)
		}
		if record.State == StateIdle && record.ActiveTurnID == "" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("ordinary turn did not settle")
		}
		time.Sleep(20 * time.Millisecond)
	}
	request := wire.SelectRequest{SchemaVersion: 2, OperationId: "00000000-0000-4000-8000-000000000168", ExpectedRevision: record.ModelRevision, ProfileId: wire.ActiveProfileIdKimiK31ProMaxV1}
	selected, err := s.SelectChatModelV2(ctx, record.AgentSessionID, request)
	if err != nil || selected.State != wire.SelectionStateReady {
		t.Fatal(selected, err)
	}
	// Normal Runtime stop plus private Store reopen. Replaying the committed
	// operation returns its original result without applying Runtime configuration.
	stopCtx, stopCancel := context.WithTimeout(context.Background(), 25*time.Second)
	err = runtime.Shutdown(stopCtx)
	stopCancel()
	if err != nil {
		t.Fatal("normal Runtime stop failed", err)
	}
	if err = s.store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = OpenStore(filepath.Join(root, "host"), WithChatModelV2Storage())
	if err != nil {
		t.Fatal(err)
	}
	runtime = codex.NewManager(config, nil)
	s = NewService(runtime, store, NewEventHub(32, 16), nil, WithNativeReasoning())
	if err = runtime.SetNotificationHandler(s.HandleNotification); err != nil {
		t.Fatal(err)
	}
	if err = runtime.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	replayed, err := s.RecoverChatModelV2(ctx, record.AgentSessionID, modelV2Recover(request))
	if err != nil || replayed.Result == nil || replayed.Result.Revision != selected.Revision {
		t.Fatal(replayed, err)
	}
	// A new explicit operation switches back, retaining the same native thread.
	request.OperationId = "00000000-0000-4000-8000-000000000169"
	request.ExpectedRevision = selected.Revision
	request.ProfileId = wire.ActiveProfileIdMinimaxM3HighV1
	if _, err = s.SelectChatModelV2(ctx, record.AgentSessionID, request); err != nil {
		t.Fatal(err)
	}
	current, err := s.store.Get(record.AgentSessionID)
	if err != nil || current.CodexThreadID != record.CodexThreadID || current.ModelRevision != record.ModelRevision+2 || requests.Load() != 1 {
		t.Fatal("identity or request count changed", err, requests.Load())
	}
	t.Log("actual Runtime + Store11: MiniMax local turn, Kimi selection, terminal replay after Store reopen, MiniMax selection; 1 ordinary loopback HTTP, 0 external")
}
