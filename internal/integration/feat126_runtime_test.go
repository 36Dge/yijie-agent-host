package integration

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/36Dge/yijie-agent-host/internal/codex"
	"github.com/36Dge/yijie-agent-host/internal/fakeresponses"
)

func TestPinnedRuntimeFEAT126FakeResponses(t *testing.T) {
	runPinnedRuntimeFEAT126FakeResponses(t, fakeresponses.ModeComplete, "completed", false)
}

func TestPinnedRuntimeFEAT126Disconnect(t *testing.T) {
	runPinnedRuntimeFEAT126FakeResponses(t, fakeresponses.ModeDisconnect, "failed", true)
}

func runPinnedRuntimeFEAT126FakeResponses(
	t *testing.T,
	mode fakeresponses.Mode,
	wantStatus string,
	wantNonRetryableError bool,
) {
	t.Helper()
	if os.Getenv("YIJIE_RUN_FEAT126_FAKE_INTEGRATION") != "1" {
		t.Skip("set YIJIE_RUN_FEAT126_FAKE_INTEGRATION=1")
	}
	binaryPath := os.Getenv("YIJIE_CODEX_INTEGRATION_BINARY")
	manifestPath := os.Getenv("YIJIE_CODEX_INTEGRATION_MANIFEST")
	if binaryPath == "" || manifestPath == "" {
		t.Skip("set pinned Runtime artifact paths")
	}
	const runID = "019fbd88-cbc3-7bf1-934d-7b05cd693f80"
	fake, err := fakeresponses.New(fakeresponses.Config{
		RunID: runID, FixtureID: codex.FEAT126FakeFixtureID, Mode: mode, MaxCalls: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp4", "127.0.0.1:18082")
	if err != nil {
		t.Fatalf("bind fixed FEAT-126 fake Responses endpoint: %v", err)
	}
	httpServer := &http.Server{
		Handler: fake.Handler(), ReadHeaderTimeout: time.Second, ReadTimeout: 5 * time.Second,
		WriteTimeout: 5 * time.Second, IdleTimeout: 5 * time.Second,
	}
	serveDone := make(chan error, 1)
	go func() { serveDone <- httpServer.Serve(listener) }()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = httpServer.Shutdown(ctx)
		select {
		case <-serveDone:
		case <-time.After(3 * time.Second):
			t.Error("fake Responses server did not stop")
		}
	})

	config := codex.DefaultConfig()
	config.BinaryPath = binaryPath
	config.ManifestPath = manifestPath
	config.CodexHome = t.TempDir()
	config.StartupTimeout = 30 * time.Second
	config.RequestTimeout = 30 * time.Second
	config.ShutdownTimeout = 10 * time.Second
	config.FakeResponses = codex.FakeResponsesConfig{
		Enabled: true, BaseURL: codex.FEAT126FakeBaseURL,
		RunID: runID, FixtureID: codex.FEAT126FakeFixtureID,
	}
	notifications := newNotificationRecorder()
	manager := codex.NewManager(config, nil)
	if err := manager.SetNotificationHandler(notifications.handle); err != nil {
		t.Fatal(err)
	}
	if err := manager.Start(context.Background()); err != nil {
		t.Fatalf("start pinned Runtime with FEAT-126 fake provider: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), config.ShutdownTimeout)
		defer cancel()
		if err := manager.Shutdown(ctx); err != nil {
			t.Errorf("shutdown pinned Runtime: %v", err)
		}
	})
	thread, err := manager.StartThread(context.Background(), t.TempDir())
	if err != nil {
		t.Fatalf("thread/start: %v", err)
	}
	if thread.Model != codex.MiniMaxModel || thread.ModelProvider != codex.MiniMaxProviderID {
		t.Fatalf("fake transport changed on-wire identity: %#v", thread)
	}
	turn, err := manager.StartTurn(context.Background(), thread.ID, "synthetic S10P1 input", "high")
	if err != nil {
		t.Fatalf("turn/start: %v", err)
	}
	status, methods, nonRetryableError, err := notifications.waitForTurn(turn.ID, 30*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if status != wantStatus {
		t.Fatalf("turn completed with status %q", status)
	}
	methodsRequired := []string{"turn/completed"}
	if mode == fakeresponses.ModeComplete {
		methodsRequired = append(methodsRequired,
			"item/reasoning/textDelta", "item/agentMessage/delta", "item/completed")
	} else {
		methodsRequired = append(methodsRequired, "error")
	}
	for _, method := range methodsRequired {
		if !contains(methods, method) {
			t.Fatalf("turn omitted %q; methods=%v", method, methods)
		}
	}
	if mode == fakeresponses.ModeDisconnect &&
		indexOf(methods, "error") >= indexOf(methods, "turn/completed") {
		t.Fatalf("disconnect terminal ordering is invalid; methods=%v", methods)
	}
	if nonRetryableError != wantNonRetryableError {
		t.Fatalf("non-retryable error observation=%t, want %t", nonRetryableError, wantNonRetryableError)
	}
	if err := manager.DeleteThread(context.Background(), thread.ID); err != nil {
		t.Fatalf("thread/delete: %v", err)
	}
	snapshot := fake.Snapshot()
	if snapshot.AcceptedCalls != 1 || snapshot.RejectedCalls != 0 {
		t.Fatalf("unexpected content-free fake counters: %#v", snapshot)
	}
}

type notificationRecorder struct {
	mu                sync.Mutex
	methods           map[string][]string
	completed         map[string]string
	nonRetryableError map[string]bool
	wake              chan struct{}
}

func newNotificationRecorder() *notificationRecorder {
	return &notificationRecorder{
		methods: make(map[string][]string), completed: make(map[string]string),
		nonRetryableError: make(map[string]bool), wake: make(chan struct{}, 1),
	}
}

func (recorder *notificationRecorder) handle(method string, params json.RawMessage) {
	var notification struct {
		TurnID string `json:"turnId"`
		Turn   struct {
			ID     string `json:"id"`
			Status string `json:"status"`
		} `json:"turn"`
		WillRetry *bool `json:"willRetry"`
	}
	_ = json.Unmarshal(params, &notification)
	turnID := notification.TurnID
	if turnID == "" {
		turnID = notification.Turn.ID
	}
	recorder.mu.Lock()
	if turnID != "" {
		recorder.methods[turnID] = append(recorder.methods[turnID], method)
		if method == "turn/completed" {
			recorder.completed[turnID] = notification.Turn.Status
		}
		if method == "error" && notification.WillRetry != nil && !*notification.WillRetry {
			recorder.nonRetryableError[turnID] = true
		}
	}
	recorder.mu.Unlock()
	select {
	case recorder.wake <- struct{}{}:
	default:
	}
}

func (recorder *notificationRecorder) waitForTurn(
	turnID string,
	timeout time.Duration,
) (string, []string, bool, error) {
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	for {
		recorder.mu.Lock()
		status, done := recorder.completed[turnID]
		methods := append([]string(nil), recorder.methods[turnID]...)
		nonRetryableError := recorder.nonRetryableError[turnID]
		recorder.mu.Unlock()
		if done {
			return status, methods, nonRetryableError, nil
		}
		select {
		case <-recorder.wake:
		case <-deadline.C:
			return "", methods, nonRetryableError, context.DeadlineExceeded
		}
	}
}

func contains(values []string, expected string) bool {
	return indexOf(values, expected) >= 0
}

func indexOf(values []string, expected string) int {
	for index, value := range values {
		if value == expected {
			return index
		}
	}
	return -1
}
