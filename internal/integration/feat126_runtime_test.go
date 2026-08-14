package integration

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/36Dge/yijie-agent-host/internal/codex"
	"github.com/36Dge/yijie-agent-host/internal/fakeresponses"
	"github.com/36Dge/yijie-agent-host/internal/session"
)

func TestPinnedRuntimeFEAT126FakeResponses(t *testing.T) {
	runPinnedRuntimeFEAT126FakeResponses(t, fakeresponses.ModeComplete, "completed", false)
}

func TestPinnedRuntimeFEAT126Disconnect(t *testing.T) {
	runPinnedRuntimeFEAT126FakeResponses(t, fakeresponses.ModeDisconnect, "failed", true)
}

func TestPinnedRuntimeFEAT126RestartThenDisconnect(t *testing.T) {
	if os.Getenv("YIJIE_RUN_FEAT126_FAKE_INTEGRATION") != "1" {
		t.Skip("set YIJIE_RUN_FEAT126_FAKE_INTEGRATION=1")
	}
	binaryPath := os.Getenv("YIJIE_CODEX_INTEGRATION_BINARY")
	manifestPath := os.Getenv("YIJIE_CODEX_INTEGRATION_MANIFEST")
	if binaryPath == "" || manifestPath == "" {
		t.Skip("set pinned Runtime artifact paths")
	}
	const runID = "019fbd88-cbc3-7bf1-934d-7b05cd693f80"
	firstDisconnect, err := fakeresponses.New(fakeresponses.Config{
		RunID: runID, FixtureID: codex.FEAT126FakeFixtureID,
		Mode: fakeresponses.ModeDisconnect, MaxCalls: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	disconnect, err := fakeresponses.New(fakeresponses.Config{
		RunID: runID, FixtureID: codex.FEAT126FakeFixtureID,
		Mode: fakeresponses.ModeDisconnect, MaxCalls: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	switcher := &switchableHandler{handler: firstDisconnect.Handler()}
	listener, err := net.Listen("tcp4", "127.0.0.1:18082")
	if err != nil {
		t.Fatalf("bind fixed FEAT-126 fake Responses endpoint: %v", err)
	}
	httpServer := &http.Server{
		Handler: switcher, ReadHeaderTimeout: time.Second, ReadTimeout: 5 * time.Second,
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

	codexHome := t.TempDir()
	workspace := t.TempDir()
	newConfig := func() codex.Config {
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
		return config
	}

	firstConfig := newConfig()
	firstNotifications := newNotificationRecorder()
	first := codex.NewManager(firstConfig, nil)
	if err := first.SetNotificationHandler(firstNotifications.handle); err != nil {
		t.Fatal(err)
	}
	if err := first.Start(context.Background()); err != nil {
		t.Fatalf("start first pinned Runtime: %v", err)
	}
	thread, err := first.StartThread(context.Background(), workspace)
	if err != nil {
		t.Fatalf("first thread/start: %v", err)
	}
	turn, err := first.StartTurn(context.Background(), thread.ID, "synthetic first lifecycle", "high")
	if err != nil {
		t.Fatalf("first turn/start: %v", err)
	}
	status, _, _, err := firstNotifications.waitForTurn(turn.ID, 30*time.Second)
	if err != nil || status != "failed" {
		t.Fatalf("first lifecycle terminal status=%q err=%v", status, err)
	}
	shutdownManager(t, first, firstConfig.ShutdownTimeout)

	switcher.set(disconnect.Handler())
	secondConfig := newConfig()
	secondNotifications := newNotificationRecorder()
	second := codex.NewManager(secondConfig, nil)
	if err := second.SetNotificationHandler(secondNotifications.handle); err != nil {
		t.Fatal(err)
	}
	if err := second.Start(context.Background()); err != nil {
		t.Fatalf("start second pinned Runtime: %v", err)
	}
	defer shutdownManager(t, second, secondConfig.ShutdownTimeout)
	resumed, err := second.ResumeThread(context.Background(), thread.ID)
	if err != nil {
		t.Fatalf("thread/resume after Runtime restart: %v", err)
	}
	disconnectTurn, err := second.StartTurn(
		context.Background(), resumed.ID, "synthetic second lifecycle", "high",
	)
	if err != nil {
		t.Fatalf("disconnect turn/start after Runtime restart: %v", err)
	}
	status, methods, nonRetryable, err := secondNotifications.waitForTurn(
		disconnectTurn.ID, 30*time.Second,
	)
	if err != nil {
		t.Fatal(err)
	}
	if status != "failed" || !nonRetryable ||
		indexOf(methods, "error") >= indexOf(methods, "turn/completed") {
		t.Fatalf("restart disconnect terminal invalid: status=%q non_retryable=%t methods=%v", status, nonRetryable, methods)
	}
	if firstDisconnect.Snapshot().AcceptedCalls != 1 || disconnect.Snapshot().AcceptedCalls != 1 {
		t.Fatal("restart lifecycle did not preserve exact fake call counts")
	}
}

func TestPinnedRuntimeFEAT126ServiceRestartThenDisconnect(t *testing.T) {
	if os.Getenv("YIJIE_RUN_FEAT126_FAKE_INTEGRATION") != "1" {
		t.Skip("set YIJIE_RUN_FEAT126_FAKE_INTEGRATION=1")
	}
	binaryPath := os.Getenv("YIJIE_CODEX_INTEGRATION_BINARY")
	manifestPath := os.Getenv("YIJIE_CODEX_INTEGRATION_MANIFEST")
	if binaryPath == "" || manifestPath == "" {
		t.Skip("set pinned Runtime artifact paths")
	}
	const runID = "019fbd88-cbc3-7bf1-934d-7b05cd693f80"
	firstDisconnect, err := fakeresponses.New(fakeresponses.Config{
		RunID: runID, FixtureID: codex.FEAT126FakeFixtureID,
		Mode: fakeresponses.ModeDisconnect, MaxCalls: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	secondDisconnect, err := fakeresponses.New(fakeresponses.Config{
		RunID: runID, FixtureID: codex.FEAT126FakeFixtureID,
		Mode: fakeresponses.ModeDisconnect, MaxCalls: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	switcher := &switchableHandler{handler: firstDisconnect.Handler()}
	listener, err := net.Listen("tcp4", "127.0.0.1:18082")
	if err != nil {
		t.Fatalf("bind fixed FEAT-126 fake Responses endpoint: %v", err)
	}
	httpServer := &http.Server{Handler: switcher}
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

	codexHome := t.TempDir()
	hostHome := t.TempDir()
	workspace := t.TempDir()
	newConfig := func() codex.Config {
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
		return config
	}
	waitForStoreTerminal := func(store *session.Store, sessionID string) {
		t.Helper()
		deadline := time.Now().Add(30 * time.Second)
		for time.Now().Before(deadline) {
			record, getErr := store.Get(sessionID)
			if getErr != nil {
				t.Fatal(getErr)
			}
			if record.ActiveTurnID == "" && record.LastTurnStatus == "failed" {
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
		t.Fatal("Host store did not persist the failed terminal")
	}

	firstStore, err := session.OpenStore(hostHome)
	if err != nil {
		t.Fatal(err)
	}
	firstConfig := newConfig()
	first := codex.NewManager(firstConfig, nil)
	firstService := session.NewService(first, firstStore, session.NewEventHub(512, 64), nil)
	if err := first.SetNotificationHandler(firstService.HandleNotification); err != nil {
		t.Fatal(err)
	}
	if err := first.Start(context.Background()); err != nil {
		t.Fatalf("start first pinned Runtime: %v", err)
	}
	record, err := firstService.StartSession(context.Background(), session.StartSessionInput{
		TaskID: "019fbd88-cbc3-7bf1-934d-7b05cd693f81", Cwd: workspace,
	})
	if err != nil {
		t.Fatalf("start first Host session: %v", err)
	}
	if _, err := firstService.StartTurn(context.Background(), session.StartTurnInput{
		AgentSessionID: record.AgentSessionID, Input: "synthetic first lifecycle", ReasoningEffort: "high",
	}); err != nil {
		t.Fatalf("start first Host turn: %v", err)
	}
	waitForStoreTerminal(firstStore, record.AgentSessionID)
	shutdownManager(t, first, firstConfig.ShutdownTimeout)
	if err := firstStore.Close(); err != nil {
		t.Fatal(err)
	}

	switcher.set(secondDisconnect.Handler())
	secondStore, err := session.OpenStore(hostHome)
	if err != nil {
		t.Fatal(err)
	}
	defer secondStore.Close()
	secondConfig := newConfig()
	second := codex.NewManager(secondConfig, nil)
	secondService := session.NewService(second, secondStore, session.NewEventHub(512, 64), nil)
	if err := second.SetNotificationHandler(secondService.HandleNotification); err != nil {
		t.Fatal(err)
	}
	if err := second.Start(context.Background()); err != nil {
		t.Fatalf("start second pinned Runtime: %v", err)
	}
	defer shutdownManager(t, second, secondConfig.ShutdownTimeout)
	if _, err := secondService.ResumeSession(context.Background(), record.AgentSessionID, session.TraceContext{}); err != nil {
		t.Fatalf("resume Host session after Runtime restart: %v", err)
	}
	if _, err := secondService.StartTurn(context.Background(), session.StartTurnInput{
		AgentSessionID: record.AgentSessionID, Input: "synthetic second lifecycle", ReasoningEffort: "high",
	}); err != nil {
		t.Fatalf("start second Host turn: %v", err)
	}
	waitForStoreTerminal(secondStore, record.AgentSessionID)
	if firstDisconnect.Snapshot().AcceptedCalls != 1 || secondDisconnect.Snapshot().AcceptedCalls != 1 {
		t.Fatal("Host service restart did not preserve exact fake call counts")
	}
}

type switchableHandler struct {
	mu      sync.RWMutex
	handler http.Handler
}

func (handler *switchableHandler) ServeHTTP(response http.ResponseWriter, request *http.Request) {
	handler.mu.RLock()
	current := handler.handler
	handler.mu.RUnlock()
	current.ServeHTTP(response, request)
}

func (handler *switchableHandler) set(next http.Handler) {
	handler.mu.Lock()
	handler.handler = next
	handler.mu.Unlock()
}

func shutdownManager(t *testing.T, manager *codex.Manager, timeout time.Duration) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	if err := manager.Shutdown(ctx); err != nil {
		t.Fatalf("shutdown pinned Runtime: %v", err)
	}
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

	codexHome := t.TempDir()
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
	pluginClones, err := filepath.Glob(filepath.Join(codexHome, ".tmp", "plugins-clone-*"))
	if err != nil {
		t.Fatalf("inspect FEAT-126 plugin sync boundary: %v", err)
	}
	if len(pluginClones) != 0 {
		t.Fatalf("FEAT-126 fake Runtime started an out-of-scope plugin sync: count=%d", len(pluginClones))
	}
	if _, err := os.Lstat(filepath.Join(codexHome, ".tmp", "plugins.sync.lock")); !os.IsNotExist(err) {
		t.Fatalf("FEAT-126 fake Runtime created a plugin sync lock: %v", err)
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
