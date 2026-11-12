package app

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/36Dge/yijie-agent-host/internal/artifact"
	"github.com/36Dge/yijie-agent-host/internal/codex"
	video "github.com/36Dge/yijie-agent-host/internal/contracts/videogeneration"
	"github.com/36Dge/yijie-agent-host/internal/session"
	"github.com/36Dge/yijie-agent-host/internal/videogen"
	"github.com/google/uuid"
)

type normalVideoProducer struct {
	creates, gets, downloads atomic.Int64
	clip                     []byte
}

func (p *normalVideoProducer) Create(_ context.Context, r videogen.Request) (videogen.Task, error) {
	p.creates.Add(1)
	return videogen.Task{ID: "cgt-normal-integration"}, nil
}
func (p *normalVideoProducer) Get(_ context.Context, id string) (videogen.Task, error) {
	p.gets.Add(1)
	return videogen.Task{ID: id, Status: video.ProviderStatusSucceeded, VideoURL: "https://unused.example/clip.mp4"}, nil
}
func (p *normalVideoProducer) Download(context.Context, videogen.Task) (videogen.Result, error) {
	p.downloads.Add(1)
	return videogen.Result{Bytes: p.clip}, nil
}

// Real Runtime -> shared Rust MCP -> authenticated Host -> session admission and
// durable operation -> original media events/spool/ACK. Only model/provider
// content is an ordinary local fixture; this does not certify real Seedance.
func TestVideoGenerationCanonicalRuntimeHostAndArtifactHandoff(t *testing.T) {
	binary, manifest, adapterBinary := os.Getenv("YIJIE_CODEX_INTEGRATION_BINARY"), os.Getenv("YIJIE_CODEX_INTEGRATION_MANIFEST"), os.Getenv("YIJIE_MEDIA_TEST_ADAPTER")
	if binary == "" || manifest == "" || adapterBinary == "" {
		t.Skip("explicit canonical Runtime and adapter required")
	}
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Mkdir(filepath.Join(root, "runtime"), 0700); err != nil {
		t.Fatal(err)
	}
	cwd := filepath.Join(root, "workspace")
	if err = os.Mkdir(cwd, 0700); err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int64
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if r.URL.Path != "/v1/responses" || json.NewDecoder(r.Body).Decode(&body) != nil {
			t.Error("unexpected loopback model request")
			w.WriteHeader(400)
			return
		}
		n := calls.Add(1)
		input, _ := json.Marshal(body["input"])
		var item map[string]any
		call := func(id, source string) map[string]any {
			args, _ := json.Marshal(map[string]any{"input": source})
			return map[string]any{"id": "item-" + id, "type": "function_call", "call_id": id, "name": "exec", "arguments": string(args)}
		}
		switch n {
		case 1:
			item = call("create-video", `text(await tools.mcp__yijie_media__generate_video({prompt:"A blue cup",image_mode:"none",image_indices:[]}));`)
		case 2:
			if !strings.Contains(string(input), "provider_pending") {
				t.Error("real create receipt missing")
			}
			item = call("get-video", `text(await tools.mcp__yijie_media__get_video_result({}));`)
		case 3:
			if !strings.Contains(string(input), "staged") {
				t.Error("real handoff receipt missing", string(input))
			}
			item = map[string]any{"id": "message", "type": "message", "role": "assistant", "phase": "final_answer", "status": "completed", "content": []any{map[string]any{"type": "output_text", "text": "本机视频已交接，请在对话流查看。", "annotations": []any{}}}}
		default:
			t.Error("unexpected model continuation")
			w.WriteHeader(429)
			return
		}
		id := fmt.Sprintf("response-%d", n)
		w.Header().Set("Content-Type", "text/event-stream")
		for _, event := range []any{
			map[string]any{"type": "response.created", "response": map[string]any{"id": id, "status": "in_progress", "output": []any{}}},
			map[string]any{"type": "response.output_item.added", "output_index": 0, "item": item},
			map[string]any{"type": "response.output_item.done", "output_index": 0, "item": item},
			map[string]any{"type": "response.completed", "response": map[string]any{"id": id, "status": "completed", "output": []any{item}, "usage": map[string]any{"input_tokens": 1, "output_tokens": 1, "total_tokens": 2}}},
		} {
			encoded, _ := json.Marshal(event)
			fmt.Fprintf(w, "data: %s\n\n", encoded)
			w.(http.Flusher).Flush()
		}
	}))
	defer model.Close()
	cfg := codex.DefaultConfig()
	cfg.BinaryPath = binary
	cfg.ManifestPath = manifest
	cfg.CodexHome = filepath.Join(root, "runtime")
	cfg.ChatModelsEnabled = true
	cfg.KimiK31Candidate = true
	cfg.RuntimePermissionsEnabled = true
	cfg.NativeToolsEnabled = true
	cfg.InteractionToolsEnabled = true
	cfg.ToolSearchEnabled = true
	cfg.BuiltinMediaEnabled = true
	cfg.BuiltinVideoEnabled = true
	cfg.MiniMax = codex.MiniMaxConfig{Enabled: true, APIKey: "ordinary-loopback-minimax"}
	cfg.KimiAPIKey = "ordinary-loopback-kimi"
	cfg.PermissionVerificationBaseURL = model.URL + "/v1"
	runtime := codex.NewManager(cfg, nil)
	store, err := session.OpenStore(filepath.Join(root, "host"), session.WithChatModelStorage(), session.WithChatModelV2Storage(), session.WithExpertStorageReader())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	spool, err := artifact.OpenStore(filepath.Join(root, "host", "artifact-spool"), artifact.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer spool.Close()
	raw, err := os.ReadFile("../session/fixtures/synthetic-video-16x16.mp4.base64")
	if err != nil {
		t.Fatal(err)
	}
	clip, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(raw)))
	if err != nil {
		t.Fatal(err)
	}
	producer := &normalVideoProducer{clip: clip}
	service := session.NewService(runtime, store, session.NewEventHub(128, 64), nil, session.WithV3Artifacts(session.NewEventHubVersion(3, 128, 64), spool, false), session.WithArtifactMedia(), session.WithVideoGenerator(producer))
	done := make(chan string, 1)
	if err = runtime.SetNotificationHandler(func(method string, raw json.RawMessage) {
		service.HandleNotification(method, raw)
		if method == "turn/completed" {
			var event struct {
				Turn struct {
					Status string `json:"status"`
				} `json:"turn"`
			}
			if json.Unmarshal(raw, &event) == nil {
				done <- event.Turn.Status
			}
		}
	}); err != nil {
		t.Fatal(err)
	}
	host := httptest.NewUnstartedServer(nil)
	token := strings.Repeat("h", 64)
	adapter, err := artifact.StartAdapter(adapterBinary, "http://"+host.Listener.Addr().String(), token, false, true)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := runtime.Shutdown(ctx); err != nil {
			t.Error("normal Runtime exit", err)
		}
		if err := adapter.Stop(ctx); err != nil {
			t.Error("normal adapter exit", err)
		}
	}()
	host.Config.Handler = NewHandler(Config{Environment: "local", Runtime: cfg, HostHome: filepath.Join(root, "host"), InstanceNonce: uuid.NewString(), ArtifactV3Enabled: true, ArtifactMediaEnabled: true, MultimodalV2Enabled: true, ImageGenerationEnabled: true, MediaBridgeToken: adapter.MediaHostToken}, runtime, service, token)
	host.Start()
	defer host.Close()
	if err = runtime.SetArtifactMCP(adapter.URL, adapter.Token); err != nil {
		t.Fatal(err)
	}
	if err = runtime.SetMediaMCP(adapter.MediaURL, adapter.MediaToken); err != nil {
		t.Fatal(err)
	}
	if err = runtime.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	ctx := codex.WithPermissionMode(codex.WithModelProfile(context.Background(), "minimax-m3-high-v1"), codex.PermissionAsk)
	record, err := service.StartSession(ctx, session.StartSessionInput{TaskID: uuid.NewString(), Cwd: cwd})
	if err != nil {
		t.Fatal(err)
	}
	turn, err := service.StartTurnV2(ctx, session.StartTurnV2Input{AgentSessionID: record.AgentSessionID, OperationID: uuid.NewString(), PermissionMode: codex.PermissionAsk, ReasoningEffort: "high", ContentBlocks: []session.TurnContentBlock{{Type: "text", Text: "Execute the approved ordinary local video check."}}})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case status := <-done:
		if status != "completed" {
			t.Fatal("turn", status)
		}
	case <-time.After(30 * time.Second):
		_ = service.InterruptTurn(ctx, record.AgentSessionID, turn.ID, session.TraceContext{})
		t.Fatal("normal video check timed out")
	}
	_, events, _, cancel, err := service.SubscribeMediaEvents(record.AgentSessionID, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	started, completed, terminal := -1, -1, -1
	for i, e := range events {
		switch e.EventType {
		case session.EventItemArtifactStarted:
			started = i
		case session.EventItemArtifactCompleted:
			completed = i
		case session.EventTurnCompleted:
			terminal = i
		}
	}
	if started < 0 || completed <= started || terminal <= completed {
		t.Fatalf("video/event terminal order: start=%d complete=%d terminal=%d", started, completed, terminal)
	}
	event := events[completed]
	if event.TurnID != turn.ID || event.SchemaVersion != 9 || event.Payload.Provenance != "provider" || event.Payload.Kind != "video" || event.Payload.MediaType != "video/mp4" {
		t.Fatal("wrong native video projection")
	}
	request, _ := http.NewRequest("GET", host.URL+event.Payload.ContentHref, nil)
	request.Header.Set("Authorization", "Bearer "+token)
	response, err := host.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	bytes, err := io.ReadAll(response.Body)
	response.Body.Close()
	if err != nil || response.StatusCode != 200 || string(bytes) != string(clip) {
		t.Fatal("original Artifact resource failed", err)
	}
	data, err := service.ReadArtifact(record.AgentSessionID, event.Payload.ArtifactID, session.ArtifactContent)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = service.AcknowledgeArtifact(record.AgentSessionID, event.Payload.ArtifactID, session.ArtifactAcknowledgement{AckID: uuid.NewString(), SHA256: data.SHA256, SizeBytes: int64(len(data.Bytes)), LocalCommittedAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	if spool.Status(record.AgentSessionID, event.Payload.ArtifactID) != "committed" || producer.creates.Load() != 1 || producer.gets.Load() != 1 || producer.downloads.Load() != 1 || calls.Load() != 3 {
		t.Fatal("duplicate creation/download or missing commit")
	}
}
