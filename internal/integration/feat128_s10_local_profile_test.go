package integration

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/36Dge/yijie-agent-host/internal/app"
	"github.com/36Dge/yijie-agent-host/internal/artifact"
	"github.com/36Dge/yijie-agent-host/internal/codex"
	"github.com/36Dge/yijie-agent-host/internal/fakeresponses"
	"github.com/36Dge/yijie-agent-host/internal/session"
	"github.com/google/uuid"
)

func TestFEAT128S10ExactLocalProfile(t *testing.T) {
	if os.Getenv("YIJIE_RUN_FEAT128_S10_PROFILE_INTEGRATION") != "1" {
		t.Skip("set YIJIE_RUN_FEAT128_S10_PROFILE_INTEGRATION=1")
	}
	const (
		runID         = "12800000-0000-4000-8000-000000000010"
		instanceNonce = "12800000-0000-4000-8000-000000000011"
		taskID        = "12800000-0000-4000-8000-000000000012"
		apiToken      = "feat128-s10-integration-token"
	)
	runAuthority := t.TempDir()
	if err := os.Mkdir(filepath.Join(runAuthority, runID), 0o700); err != nil {
		t.Fatal(err)
	}
	runRoot, err := filepath.EvalSymlinks(filepath.Join(runAuthority, runID))
	if err != nil {
		t.Fatal(err)
	}
	paths := prepareFEAT128S10Authority(t, runRoot, instanceNonce)
	runtimeBinary, runtimeManifest := pinnedRuntimeArtifacts(t)
	configureFEAT128S10Environment(t, paths, runtimeBinary, runtimeManifest, runID, instanceNonce)

	fake, err := fakeresponses.New(fakeresponses.Config{
		RunID: runID, FixtureID: codex.FEAT126FakeFixtureID,
		Mode: fakeresponses.ModeComplete, MaxCalls: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp4", "127.0.0.1:18082")
	if err != nil {
		t.Fatalf("fixed fake endpoint unavailable: %v", err)
	}
	fakeHTTP := &http.Server{
		Handler: fake.Handler(), ReadHeaderTimeout: time.Second, ReadTimeout: 5 * time.Second,
		WriteTimeout: 5 * time.Second, IdleTimeout: 5 * time.Second,
	}
	fakeDone := make(chan error, 1)
	go func() { fakeDone <- fakeHTTP.Serve(listener) }()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = fakeHTTP.Shutdown(ctx)
		select {
		case <-fakeDone:
		case <-ctx.Done():
			t.Error("fake Responses server did not stop")
		}
	})

	config, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("exact S10 profile rejected before integration: %v", err)
	}
	if !config.Runtime.FakeResponses.Enabled || config.Runtime.MiniMax.Enabled ||
		!config.ArtifactV3Enabled || !config.ArtifactSynthetic {
		t.Fatal("exact S10 profile did not stay keyless and synthetic")
	}

	manager := codex.NewManager(config.Runtime, nil)
	store, err := session.OpenStore(
		config.HostHome,
		session.WithFEAT126Authority(config.FEAT126ProjectDir, config.FEAT126TestRunID),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	spool, err := artifact.OpenStore(
		filepath.Join(config.HostHome, "artifact-spool"),
		artifact.Options{SessionLimit: artifact.DefaultSessionLimit, GlobalLimit: artifact.DefaultGlobalLimit, TTL: artifact.DefaultTTL},
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = spool.Close() })
	v3 := session.NewEventHubVersion(session.EventSchemaVersionV3, 512, 64)
	service := session.NewService(
		manager,
		store,
		session.NewEventHub(512, 64),
		nil,
		session.WithV2Events(session.NewEventHubVersion(session.EventSchemaVersionV2, 512, 64)),
		session.WithV3Artifacts(v3, spool, true),
	)
	if err := manager.SetNotificationHandler(service.HandleNotification); err != nil {
		t.Fatal(err)
	}
	runtimeContext, cancelRuntime := context.WithCancel(context.Background())
	if err := manager.Start(runtimeContext); err != nil {
		cancelRuntime()
		t.Fatalf("start pinned Runtime with exact fake profile: %v", err)
	}
	t.Cleanup(func() {
		cancelRuntime()
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := manager.Shutdown(ctx); err != nil {
			t.Errorf("shutdown pinned Runtime: %v", err)
		}
	})

	record, err := service.StartSession(context.Background(), session.StartSessionInput{
		TaskID: taskID, Cwd: paths["project"],
	})
	if err != nil {
		t.Fatalf("start exact local session: %v", err)
	}
	turn, err := service.StartTurn(context.Background(), session.StartTurnInput{
		AgentSessionID: record.AgentSessionID, Input: "emit strict-local structured artifacts", ReasoningEffort: "none",
	})
	if err != nil {
		t.Fatalf("start exact local turn: %v", err)
	}
	_, replay, updates, cancelSubscription, err := service.SubscribeEventsV3(record.AgentSessionID, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	defer cancelSubscription()
	turnComplete := false
	for _, event := range replay {
		if event.TurnID == turn.ID && event.EventType == session.EventTurnCompleted {
			turnComplete = true
		}
	}
	if !turnComplete {
		deadline := time.NewTimer(30 * time.Second)
		defer deadline.Stop()
		for !turnComplete {
			select {
			case event := <-updates:
				replay = append(replay, event)
				turnComplete = event.TurnID == turn.ID && event.EventType == session.EventTurnCompleted
			case <-deadline.C:
				t.Fatal("strict-local fake turn did not reach a terminal event")
			}
		}
	}
	counts := map[string]int{"started": 0, "progress": 0, "completed": 0}
	completed := make([]session.Event, 0, 4)
	var previous uint64
	for _, event := range replay {
		if event.Sequence <= previous || event.SchemaVersion != session.EventSchemaVersionV3 {
			t.Fatal("v3 replay was not strictly ordered")
		}
		previous = event.Sequence
		if event.TurnID != turn.ID {
			continue
		}
		switch event.EventType {
		case session.EventItemArtifactStarted:
			counts["started"]++
		case session.EventItemArtifactProgress:
			counts["progress"]++
		case session.EventItemArtifactCompleted:
			counts["completed"]++
			completed = append(completed, event)
		}
	}
	if counts["started"] != 4 || counts["progress"] != 4 || counts["completed"] != 4 {
		t.Fatalf("unexpected content-free Artifact lifecycle counts: %#v", counts)
	}

	handler := app.NewHandler(config, manager, service, apiToken)
	seenKinds := make(map[string]bool, 4)
	for _, event := range completed {
		payload := event.Payload
		if payload.SizeBytes == nil || payload.ContentHref == "" || payload.SHA256 == "" {
			t.Fatal("completed Artifact omitted its resource authority")
		}
		get := httptest.NewRequest(http.MethodGet, payload.ContentHref, nil)
		get.Header.Set("Authorization", "Bearer "+apiToken)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, get)
		if response.Code != http.StatusOK || int64(response.Body.Len()) != *payload.SizeBytes {
			t.Fatal("Artifact resource GET failed closed conformance")
		}
		digest := sha256.Sum256(response.Body.Bytes())
		if hex.EncodeToString(digest[:]) != payload.SHA256 {
			t.Fatal("Artifact resource digest did not match the completed manifest")
		}
		ackBody, err := json.Marshal(map[string]any{
			"ack_id": uuid.New().String(), "size_bytes": *payload.SizeBytes,
			"sha256": payload.SHA256, "local_committed_at": time.Now().UTC().Format(time.RFC3339Nano),
		})
		if err != nil {
			t.Fatal(err)
		}
		ack := httptest.NewRequest(
			http.MethodPost,
			fmt.Sprintf("/v3/agent-sessions/%s/artifacts/%s/ack", record.AgentSessionID, payload.ArtifactID),
			bytes.NewReader(ackBody),
		)
		ack.Header.Set("Authorization", "Bearer "+apiToken)
		ack.Header.Set("Content-Type", "application/json")
		ackResponse := httptest.NewRecorder()
		handler.ServeHTTP(ackResponse, ack)
		if ackResponse.Code != http.StatusOK {
			t.Fatal("Artifact acknowledgement failed")
		}
		seenKinds[payload.Kind] = true
	}
	for _, kind := range []string{"image", "video", "file", "report"} {
		if !seenKinds[kind] {
			t.Fatalf("missing synthetic kind %s", kind)
		}
	}
	spoolEntries, err := os.ReadDir(filepath.Join(config.HostHome, "artifact-spool"))
	if err != nil || len(spoolEntries) != 0 {
		t.Fatal("acknowledged Artifact spool retained resource residue")
	}
	snapshot := fake.Snapshot()
	if snapshot.AcceptedCalls != 1 || snapshot.RejectedCalls != 0 {
		t.Fatalf("unexpected content-free fake counters: accepted=%d rejected=%d", snapshot.AcceptedCalls, snapshot.RejectedCalls)
	}
	t.Logf("status=passed started=%d progress=%d completed=%d acked=%d zeroProvider=true zeroNonLoopback=true", counts["started"], counts["progress"], counts["completed"], len(completed))
}

func prepareFEAT128S10Authority(t *testing.T, runRoot, instanceNonce string) map[string]string {
	t.Helper()
	paths := map[string]string{
		"hostHome":  filepath.Join(runRoot, "host-home"),
		"codexHome": filepath.Join(runRoot, "codex-home"),
		"project":   filepath.Join(runRoot, "project"),
		"hostRoot":  filepath.Join(runRoot, "host"),
		"log":       filepath.Join(runRoot, "host", instanceNonce),
	}
	for _, path := range paths {
		if err := os.MkdirAll(path, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(path, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	return paths
}

func configureFEAT128S10Environment(
	t *testing.T,
	paths map[string]string,
	runtimeBinary, runtimeManifest, runID, instanceNonce string,
) {
	t.Helper()
	for _, key := range []string{
		"YIJIE_MODEL_PROVIDER", "YIJIE_MINIMAX_API_KEY", "YIJIE_MINIMAX_API_KEY_FILE",
	} {
		t.Setenv(key, "")
	}
	t.Setenv("YIJIE_ENV", "local")
	t.Setenv("YIJIE_AGENT_HOST_HOME", paths["hostHome"])
	t.Setenv("YIJIE_CODEX_HOME", paths["codexHome"])
	t.Setenv("YIJIE_CODEX_BINARY", runtimeBinary)
	t.Setenv("YIJIE_CODEX_MANIFEST", runtimeManifest)
	t.Setenv("YIJIE_CODEX_STARTUP_TIMEOUT", "30s")
	t.Setenv("YIJIE_CODEX_REQUEST_TIMEOUT", "30s")
	t.Setenv("YIJIE_CODEX_SHUTDOWN_TIMEOUT", "10s")
	t.Setenv("YIJIE_AGENT_HOST_INSTANCE_NONCE", instanceNonce)
	t.Setenv("YIJIE_AGENT_HOST_V2_RAW_REASONING_ENABLED", "true")
	t.Setenv("YIJIE_AGENT_HOST_V2_TITLE_ENABLED", "false")
	t.Setenv("YIJIE_AGENT_HOST_V2_CLEANUP_ENABLED", "true")
	t.Setenv("YIJIE_FEAT126_S10_TEST_PROFILE_ENABLED", "true")
	t.Setenv("YIJIE_FEAT126_S10_RUN_ID", runID)
	t.Setenv("YIJIE_FEAT126_FAKE_RESPONSES_BASE_URL", codex.FEAT126FakeBaseURL)
	t.Setenv("YIJIE_FEAT126_S10_PARENT_PID", fmt.Sprint(os.Getppid()))
	t.Setenv("YIJIE_FEAT126_S10_HOST_LOG_DIR", paths["log"])
	t.Setenv("YIJIE_FEAT126_S10_PROCESS_MANIFEST", filepath.Join(paths["log"], "process.json"))
	t.Setenv("YIJIE_AGENT_HOST_V3_ARTIFACTS_ENABLED", "true")
	t.Setenv("YIJIE_FEAT128_SYNTHETIC_ENABLED", "true")
	t.Setenv("YIJIE_FEAT128_SYNTHETIC_MANIFEST", session.SyntheticArtifactManifest)
	t.Setenv("YIJIE_FEAT128_S10_TEST_PROFILE_ENABLED", "true")
}

func pinnedRuntimeArtifacts(t *testing.T) (string, string) {
	t.Helper()
	binary := os.Getenv("YIJIE_CODEX_INTEGRATION_BINARY")
	manifest := os.Getenv("YIJIE_CODEX_INTEGRATION_MANIFEST")
	if binary == "" || manifest == "" {
		_, source, _, ok := runtime.Caller(0)
		if !ok {
			t.Fatal("locate integration source")
		}
		repository := filepath.Clean(filepath.Join(filepath.Dir(source), "..", ".."))
		runtimeRoot := filepath.Join(repository, "..", "yijie-codex", ".yijie", "build", "macos", "aarch64-apple-darwin")
		binary = filepath.Join(runtimeRoot, "codex")
		manifest = filepath.Join(runtimeRoot, "runtime-manifest.json")
	}
	for _, path := range []string{binary, manifest} {
		if !filepath.IsAbs(path) {
			t.Fatal("pinned Runtime artifact path is not absolute")
		}
		if _, err := os.Stat(path); err != nil {
			t.Fatal("pinned Runtime artifact is unavailable")
		}
	}
	return binary, manifest
}
