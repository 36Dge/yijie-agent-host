package codex

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestPinnedRuntimeIntegration(t *testing.T) {
	binaryPath := os.Getenv("YIJIE_CODEX_INTEGRATION_BINARY")
	manifestPath := os.Getenv("YIJIE_CODEX_INTEGRATION_MANIFEST")
	if binaryPath == "" || manifestPath == "" {
		t.Skip("set YIJIE_CODEX_INTEGRATION_BINARY and YIJIE_CODEX_INTEGRATION_MANIFEST")
	}

	config := DefaultConfig()
	config.BinaryPath = binaryPath
	config.ManifestPath = manifestPath
	config.CodexHome = integrationPrivateTempDir(t)
	config.StartupTimeout = 20 * time.Second
	config.RequestTimeout = 10 * time.Second
	config.ShutdownTimeout = 10 * time.Second
	manager := NewManager(config, nil)

	if err := manager.Start(context.Background()); err != nil {
		t.Fatalf("start pinned Runtime: %v", err)
	}
	status := manager.Snapshot()
	if !status.Ready || status.State != StateReady {
		t.Fatalf("expected pinned Runtime to be ready, got %+v", status)
	}
	if status.RuntimeVersion != ExpectedRuntimeVersion ||
		status.UpstreamTag != ExpectedUpstreamTag ||
		status.UpstreamCommit != ExpectedUpstreamCommit ||
		status.Transport != ExpectedTransport ||
		status.ExperimentalAPI {
		t.Fatalf("pinned Runtime identity does not match Baseline 1: %+v", status)
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), config.ShutdownTimeout)
	defer cancel()
	if err := manager.Shutdown(shutdownCtx); err != nil {
		t.Fatalf("shutdown pinned Runtime: %v", err)
	}
	if status := manager.Snapshot(); status.State != StateStopped || status.Ready {
		t.Fatalf("expected pinned Runtime to stop cleanly, got %+v", status)
	}
}

func TestPinnedRuntimeMiniMaxConfigurationIntegration(t *testing.T) {
	binaryPath, manifestPath := integrationArtifactPaths(t)
	config := integrationConfig(binaryPath, manifestPath, integrationPrivateTempDir(t), "baseline2-placeholder-key")
	manager := NewManager(config, nil)
	notifications := newIntegrationNotifications()
	if err := manager.SetNotificationHandler(notifications.handle); err != nil {
		t.Fatal(err)
	}
	if err := manager.Start(context.Background()); err != nil {
		t.Fatalf("start pinned Runtime with managed MiniMax configuration: %v", err)
	}
	defer shutdownIntegrationManager(t, manager, config.ShutdownTimeout)

	thread, err := manager.StartThread(context.Background(), t.TempDir())
	if err != nil {
		t.Fatalf("thread/start with managed MiniMax configuration: %v", err)
	}
	if thread.Model != MiniMaxModel || thread.ModelProvider != MiniMaxProviderID {
		t.Fatalf("unexpected provider identity: model=%q provider=%q", thread.Model, thread.ModelProvider)
	}
	if err := notifications.waitForMethod("thread/started", 5*time.Second); err != nil {
		t.Fatal(err)
	}
}

func TestPinnedRuntimeMiniMaxTurnIntegration(t *testing.T) {
	if os.Getenv("YIJIE_RUN_MINIMAX_INTEGRATION") != "1" {
		t.Skip("set YIJIE_RUN_MINIMAX_INTEGRATION=1 for the opt-in provider test")
	}
	key, err := integrationMiniMaxKey()
	if err != nil {
		t.Fatal(err)
	}
	binaryPath, manifestPath := integrationArtifactPaths(t)
	codexHome := integrationPrivateTempDir(t)
	workspace := t.TempDir()

	firstNotifications := newIntegrationNotifications()
	firstConfig := integrationConfig(binaryPath, manifestPath, codexHome, key)
	first := NewManager(firstConfig, nil)
	if err := first.SetNotificationHandler(firstNotifications.handle); err != nil {
		t.Fatal(err)
	}
	if err := first.Start(context.Background()); err != nil {
		t.Fatalf("start first pinned Runtime: %v", err)
	}
	thread, err := first.StartThread(context.Background(), workspace)
	if err != nil {
		shutdownIntegrationManager(t, first, firstConfig.ShutdownTimeout)
		t.Fatalf("thread/start: %v", err)
	}
	turn, err := first.StartTurn(
		context.Background(),
		thread.ID,
		"Reply with exactly BASELINE2_OK and nothing else. Do not use tools.",
		"none",
	)
	if err != nil {
		shutdownIntegrationManager(t, first, firstConfig.ShutdownTimeout)
		t.Fatalf("first turn/start: %v", err)
	}
	status, methods, err := firstNotifications.waitForTurn(turn.ID, 3*time.Minute)
	if err != nil {
		shutdownIntegrationManager(t, first, firstConfig.ShutdownTimeout)
		t.Fatal(err)
	}
	if status != "completed" {
		shutdownIntegrationManager(t, first, firstConfig.ShutdownTimeout)
		t.Fatalf("first turn completed with status %q", status)
	}
	for _, method := range []string{
		"turn/started", "item/started", "item/agentMessage/delta", "item/completed", "turn/completed",
	} {
		if !containsString(methods, method) {
			shutdownIntegrationManager(t, first, firstConfig.ShutdownTimeout)
			t.Fatalf("first turn omitted required notification %q; received %v", method, methods)
		}
	}
	shutdownIntegrationManager(t, first, firstConfig.ShutdownTimeout)

	secondNotifications := newIntegrationNotifications()
	secondConfig := integrationConfig(binaryPath, manifestPath, codexHome, key)
	second := NewManager(secondConfig, nil)
	if err := second.SetNotificationHandler(secondNotifications.handle); err != nil {
		t.Fatal(err)
	}
	if err := second.Start(context.Background()); err != nil {
		t.Fatalf("start second pinned Runtime: %v", err)
	}
	defer shutdownIntegrationManager(t, second, secondConfig.ShutdownTimeout)
	resumed, err := second.ResumeThread(context.Background(), thread.ID)
	if err != nil {
		t.Fatalf("thread/resume after Runtime restart: %v", err)
	}
	interruptTurn, err := second.StartTurn(
		context.Background(),
		resumed.ID,
		"Read-only cancellation test. Slowly enumerate the integers from 1 to 500, one per line.",
		"high",
	)
	if err != nil {
		t.Fatalf("interrupt test turn/start: %v", err)
	}
	if err := secondNotifications.waitForTurnMethod(
		interruptTurn.ID,
		"turn/started",
		30*time.Second,
	); err != nil {
		t.Fatal(err)
	}
	if err := second.InterruptTurn(context.Background(), resumed.ID, interruptTurn.ID); err != nil {
		t.Fatalf("turn/interrupt: %v", err)
	}
	status, methods, err = secondNotifications.waitForTurn(interruptTurn.ID, 3*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if status != "interrupted" {
		t.Fatalf("interrupted turn completed with status %q", status)
	}
	if !containsString(methods, "turn/completed") {
		t.Fatalf("interrupted turn omitted turn/completed; received %v", methods)
	}
}

func integrationArtifactPaths(t *testing.T) (string, string) {
	t.Helper()
	binaryPath := os.Getenv("YIJIE_CODEX_INTEGRATION_BINARY")
	manifestPath := os.Getenv("YIJIE_CODEX_INTEGRATION_MANIFEST")
	if binaryPath == "" || manifestPath == "" {
		t.Skip("set YIJIE_CODEX_INTEGRATION_BINARY and YIJIE_CODEX_INTEGRATION_MANIFEST")
	}
	return binaryPath, manifestPath
}

func integrationConfig(binaryPath, manifestPath, codexHome, key string) Config {
	config := DefaultConfig()
	config.BinaryPath = binaryPath
	config.ManifestPath = manifestPath
	config.CodexHome = codexHome
	config.StartupTimeout = 30 * time.Second
	config.RequestTimeout = 3 * time.Minute
	config.ShutdownTimeout = 10 * time.Second
	config.MiniMax = MiniMaxConfig{Enabled: true, APIKey: key}
	return config
}

func integrationPrivateTempDir(t *testing.T) string {
	t.Helper()
	directory := t.TempDir()
	if err := os.Chmod(directory, 0o700); err != nil {
		t.Fatalf("secure integration CODEX_HOME: %v", err)
	}
	canonical, err := filepath.EvalSymlinks(directory)
	if err != nil {
		t.Fatalf("resolve integration CODEX_HOME: %v", err)
	}
	return canonical
}

func shutdownIntegrationManager(t *testing.T, manager *Manager, timeout time.Duration) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	if err := manager.Shutdown(ctx); err != nil {
		t.Errorf("shutdown pinned Runtime: %v", err)
	}
}

func integrationMiniMaxKey() (string, error) {
	direct := os.Getenv("YIJIE_MINIMAX_API_KEY")
	path := os.Getenv("YIJIE_MINIMAX_API_KEY_FILE")
	if direct != "" && path != "" {
		return "", errors.New("configure only one MiniMax integration key source")
	}
	if direct != "" {
		if strings.TrimSpace(direct) != direct || strings.ContainsAny(direct, "\x00\r\n") {
			return "", errors.New("MiniMax integration key is invalid")
		}
		return direct, nil
	}
	if path == "" {
		return "", errors.New("set YIJIE_MINIMAX_API_KEY or YIJIE_MINIMAX_API_KEY_FILE")
	}
	if !filepath.IsAbs(path) {
		return "", errors.New("MiniMax integration key file path must be absolute")
	}
	info, err := os.Lstat(path)
	if err != nil {
		return "", errors.New("MiniMax integration key file is unavailable")
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0 {
		return "", errors.New("MiniMax integration key file must be owner-only")
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return "", errors.New("MiniMax integration key file cannot be read")
	}
	key := strings.TrimSpace(string(content))
	if key == "" || len(key) > 16<<10 || strings.ContainsAny(key, "\x00\r\n") {
		return "", errors.New("MiniMax integration key file is invalid")
	}
	return key, nil
}

type integrationNotifications struct {
	mu        sync.Mutex
	all       []string
	methods   map[string][]string
	completed map[string]string
	wake      chan struct{}
}

func newIntegrationNotifications() *integrationNotifications {
	return &integrationNotifications{
		methods:   make(map[string][]string),
		completed: make(map[string]string),
		wake:      make(chan struct{}, 1),
	}
}

func (n *integrationNotifications) handle(method string, params json.RawMessage) {
	var notification struct {
		TurnID string `json:"turnId"`
		Turn   struct {
			ID     string `json:"id"`
			Status string `json:"status"`
		} `json:"turn"`
	}
	n.mu.Lock()
	n.all = append(n.all, method)
	n.mu.Unlock()
	select {
	case n.wake <- struct{}{}:
	default:
	}
	if json.Unmarshal(params, &notification) != nil {
		return
	}
	turnID := notification.TurnID
	if turnID == "" {
		turnID = notification.Turn.ID
	}
	if turnID == "" {
		return
	}
	n.mu.Lock()
	n.methods[turnID] = append(n.methods[turnID], method)
	if method == "turn/completed" {
		n.completed[turnID] = notification.Turn.Status
	}
	n.mu.Unlock()
	select {
	case n.wake <- struct{}{}:
	default:
	}
}

func (n *integrationNotifications) waitForMethod(method string, timeout time.Duration) error {
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	for {
		n.mu.Lock()
		found := containsString(n.all, method)
		n.mu.Unlock()
		if found {
			return nil
		}
		select {
		case <-n.wake:
		case <-timer.C:
			return errors.New("timed out waiting for Runtime notification")
		}
	}
}

func (n *integrationNotifications) waitForTurnMethod(
	turnID, method string,
	timeout time.Duration,
) error {
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	for {
		n.mu.Lock()
		found := containsString(n.methods[turnID], method)
		n.mu.Unlock()
		if found {
			return nil
		}
		select {
		case <-n.wake:
		case <-timer.C:
			return errors.New("timed out waiting for turn notification")
		}
	}
}

func (n *integrationNotifications) waitForTurn(turnID string, timeout time.Duration) (string, []string, error) {
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	for {
		n.mu.Lock()
		status, complete := n.completed[turnID]
		methods := append([]string(nil), n.methods[turnID]...)
		n.mu.Unlock()
		if complete {
			return status, methods, nil
		}
		select {
		case <-n.wake:
		case <-timer.C:
			return "", methods, errors.New("timed out waiting for turn/completed")
		}
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
