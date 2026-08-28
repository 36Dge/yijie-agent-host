package integration

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/36Dge/yijie-agent-host/internal/codex"
	"github.com/36Dge/yijie-agent-host/internal/fakeresponses"
	"github.com/36Dge/yijie-agent-host/internal/session"
)

func TestPinnedRuntimeFEAT127MultimodalFakeResponses(t *testing.T) {
	if os.Getenv("YIJIE_RUN_FEAT126_FAKE_INTEGRATION") != "1" {
		t.Skip("set YIJIE_RUN_FEAT126_FAKE_INTEGRATION=1")
	}
	binaryPath := os.Getenv("YIJIE_CODEX_INTEGRATION_BINARY")
	manifestPath := os.Getenv("YIJIE_CODEX_INTEGRATION_MANIFEST")
	if binaryPath == "" || manifestPath == "" {
		t.Skip("set pinned Runtime artifact paths")
	}

	const runID = "123e4567-e89b-42d3-a456-426614174000"
	fake, err := fakeresponses.New(fakeresponses.Config{
		RunID: runID, FixtureID: codex.FEAT126FakeFixtureID,
		Mode: fakeresponses.ModeFEAT127Context, MaxCalls: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp4", "127.0.0.1:18082")
	if err != nil {
		t.Fatalf("bind fixed fake Responses endpoint: %v", err)
	}
	server := &http.Server{
		Handler: fake.Handler(), ReadHeaderTimeout: time.Second, ReadTimeout: 5 * time.Second,
		WriteTimeout: 5 * time.Second, IdleTimeout: 5 * time.Second,
	}
	serveDone := make(chan error, 1)
	go func() { serveDone <- server.Serve(listener) }()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = server.Shutdown(ctx)
		select {
		case <-serveDone:
		case <-time.After(3 * time.Second):
			t.Error("fake Responses server did not stop")
		}
	})

	root := exactPrivateTempDir(t)
	runRoot := filepath.Join(root, runID)
	hostHome := filepath.Join(runRoot, "host-home")
	codexHome := filepath.Join(runRoot, "codex-home")
	workspace := filepath.Join(runRoot, "project")
	for _, directory := range []string{runRoot, hostHome, codexHome, workspace} {
		if err := os.Mkdir(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}

	store, err := session.OpenStore(hostHome, session.WithFEAT126Authority(workspace, runID))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	config := codex.DefaultConfig()
	config.BinaryPath = binaryPath
	config.ManifestPath = manifestPath
	config.CodexHome = codexHome
	config.StartupTimeout = 30 * time.Second
	config.RequestTimeout = 30 * time.Second
	config.ShutdownTimeout = 10 * time.Second
	config.FakeResponses = codex.FakeResponsesConfig{
		Enabled: true, BaseURL: codex.FEAT126FakeBaseURL,
		RunID: runID, FixtureID: codex.FEAT126FakeFixtureID,
	}
	manager := codex.NewManager(config, nil)
	service := session.NewService(manager, store, session.NewEventHub(512, 64), nil)
	if err := manager.SetNotificationHandler(service.HandleNotification); err != nil {
		t.Fatal(err)
	}
	if err := manager.Start(context.Background()); err != nil {
		t.Fatalf("start pinned Runtime with local fake provider: %v", err)
	}
	defer shutdownManager(t, manager, config.ShutdownTimeout)

	record, err := service.StartSession(context.Background(), session.StartSessionInput{
		TaskID: "019fbd88-cbc3-7bf1-934d-7b05cd693f81", Cwd: workspace,
	})
	if err != nil {
		t.Fatalf("start Host session: %v", err)
	}
	pngBytes := feat127AcceptancePNG(t)
	pngDigest := sha256.Sum256(pngBytes)
	fileContext := "Context verification code: ALPHA-7319"
	fileDigest := sha256.Sum256([]byte(fileContext))
	turn, err := service.StartTurnV2(context.Background(), session.StartTurnV2Input{
		AgentSessionID:  record.AgentSessionID,
		OperationID:     "019fbd88-cbc3-7bf1-934d-7b05cd693f82",
		ReasoningEffort: "high",
		ContentBlocks: []session.TurnContentBlock{
			{Type: session.ContentBlockText, Text: "Compare the image with the attached note."},
			{Type: session.ContentBlockImage, Image: &session.TurnImageBlock{
				AttachmentID: "019fbd88-cbc3-7bf1-934d-7b05cd693f83",
				MediaType:    "image/png", SizeBytes: int64(len(pngBytes)),
				SHA256:  hex.EncodeToString(pngDigest[:]),
				DataURL: "data:image/png;base64," + base64.StdEncoding.EncodeToString(pngBytes),
			}},
			{Type: session.ContentBlockFile, File: &session.TurnFileBlock{
				AttachmentID: "019fbd88-cbc3-7bf1-934d-7b05cd693f84",
				Name:         "context-marker.txt", MediaType: "text/plain", SizeBytes: int64(len(fileContext)),
				SHA256:        hex.EncodeToString(fileDigest[:]),
				ContextChunks: []string{fileContext},
			}},
		},
	})
	if err != nil {
		t.Fatalf("start ordered multimodal turn: %v", err)
	}
	if turn.ID == "" {
		t.Fatal("Runtime returned an empty multimodal turn id")
	}
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		restored, getErr := store.Get(record.AgentSessionID)
		if getErr != nil {
			t.Fatal(getErr)
		}
		if restored.ActiveTurnID == "" && restored.LastTurnStatus == "completed" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	restored, err := store.Get(record.AgentSessionID)
	if err != nil {
		t.Fatal(err)
	}
	if restored.ActiveTurnID != "" || restored.LastTurnStatus != "completed" {
		t.Fatalf("multimodal Runtime turn did not complete: %+v", restored)
	}
	_, replay, _, cancel, err := service.SubscribeEvents(record.AgentSessionID, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	var observedAnswer strings.Builder
	for _, event := range replay {
		if event.EventType == session.EventItemAgentMessageDelta && event.Payload.Delta != nil {
			observedAnswer.WriteString(*event.Payload.Delta)
		}
		if event.EventType == session.EventItemCompleted && event.Payload.ItemType == "agentMessage" {
			if event.Payload.Text != nil {
				observedAnswer.WriteString(*event.Payload.Text)
			}
		}
	}
	if answer := observedAnswer.String(); !strings.Contains(answer, "ALPHA-7319") || !strings.Contains(answer, "\u52a0\u53f7") {
		t.Fatalf("content-aware fake did not verify both transported blocks: %q", answer)
	}
	snapshot := fake.Snapshot()
	if snapshot.Mode != fakeresponses.ModeFEAT127Context || snapshot.AcceptedCalls != 1 || snapshot.RejectedCalls != 0 {
		t.Fatalf("unexpected local fake call counters: %#v", snapshot)
	}
}

func feat127AcceptancePNG(t *testing.T) []byte {
	t.Helper()
	path := os.Getenv("YIJIE_FEAT127_TEST_IMAGE")
	if path == "" {
		workingDirectory, err := os.Getwd()
		if err != nil {
			t.Fatal(err)
		}
		for current, depth := workingDirectory, 0; depth < 6; depth++ {
			candidate := filepath.Join(current, "FEAT-127-manual-acceptance", "valid-image.png")
			if _, err := os.Stat(candidate); err == nil {
				path = candidate
				break
			}
			parent := filepath.Dir(current)
			if parent == current {
				break
			}
			current = parent
		}
	}
	if path == "" {
		t.Fatal("set YIJIE_FEAT127_TEST_IMAGE to the FEAT-127 valid-image.png fixture")
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read FEAT-127 acceptance image: %v", err)
	}
	digest := sha256.Sum256(content)
	if got := hex.EncodeToString(digest[:]); got != "ea933b4091578cb14de9e3d7659aba8d9d1bd1aa5453b60327bffb459e5d383c" {
		t.Fatalf("FEAT-127 acceptance image digest changed: %s", got)
	}
	return content
}
