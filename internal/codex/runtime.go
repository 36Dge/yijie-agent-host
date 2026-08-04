package codex

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	StateNotConfigured = "not_configured"
	StateVerifying     = "verifying"
	StateStarting      = "starting"
	StateInitializing  = "initializing"
	StateReady         = "ready"
	StateStopping      = "stopping"
	StateStopped       = "stopped"
	StateFailed        = "failed"
)

const (
	defaultStartupTimeout  = 10 * time.Second
	defaultRequestTimeout  = 30 * time.Second
	defaultShutdownTimeout = 10 * time.Second
	defaultMaxMessageBytes = 16 << 20
	defaultWriteQueueDepth = 64
	defaultStderrTailBytes = 64 << 10
)

type Config struct {
	BinaryPath      string
	ManifestPath    string
	CodexHome       string
	StartupTimeout  time.Duration
	RequestTimeout  time.Duration
	ShutdownTimeout time.Duration
	MaxMessageBytes int
	WriteQueueDepth int
	StderrTailBytes int
	MiniMax         MiniMaxConfig
	FakeResponses   FakeResponsesConfig

	// testArtifactPolicy is intentionally package-private. Production always
	// uses the exact Runtime Baseline 0 artifact policy.
	testArtifactPolicy *artifactPolicy
}

func DefaultConfig() Config {
	return Config{
		StartupTimeout:  defaultStartupTimeout,
		RequestTimeout:  defaultRequestTimeout,
		ShutdownTimeout: defaultShutdownTimeout,
		MaxMessageBytes: defaultMaxMessageBytes,
		WriteQueueDepth: defaultWriteQueueDepth,
		StderrTailBytes: defaultStderrTailBytes,
	}
}

func (c Config) Configured() bool {
	return c.BinaryPath != "" || c.ManifestPath != "" || c.CodexHome != ""
}

func (c Config) validate() error {
	if c.BinaryPath == "" || c.ManifestPath == "" || c.CodexHome == "" {
		return errors.New("runtime binary, manifest, and CODEX_HOME must all be configured")
	}
	if !filepath.IsAbs(c.CodexHome) {
		return errors.New("CODEX_HOME must be an absolute path")
	}
	home, err := os.Stat(c.CodexHome)
	if err != nil {
		return fmt.Errorf("stat CODEX_HOME: %w", err)
	}
	if !home.IsDir() {
		return errors.New("CODEX_HOME is not a directory")
	}
	if c.StartupTimeout <= 0 || c.RequestTimeout <= 0 || c.ShutdownTimeout <= 0 {
		return errors.New("runtime timeouts must be positive")
	}
	if c.MaxMessageBytes < 1024 {
		return errors.New("runtime maximum message size must be at least 1024 bytes")
	}
	if c.WriteQueueDepth < 1 {
		return errors.New("runtime write queue depth must be positive")
	}
	if c.StderrTailBytes < 1024 {
		return errors.New("runtime stderr tail size must be at least 1024 bytes")
	}
	if err := c.MiniMax.validate(); err != nil {
		return err
	}
	if err := c.FakeResponses.validate(); err != nil {
		return err
	}
	if c.MiniMax.Enabled && c.FakeResponses.Enabled {
		return errors.New("MiniMax and fake Responses providers are mutually exclusive")
	}
	return nil
}

type Status struct {
	State           string `json:"state"`
	Ready           bool   `json:"ready"`
	RuntimeVersion  string `json:"runtime_version,omitempty"`
	UpstreamTag     string `json:"upstream_tag,omitempty"`
	UpstreamCommit  string `json:"upstream_commit,omitempty"`
	Transport       string `json:"transport"`
	ExperimentalAPI bool   `json:"experimental_api"`
	FailureCode     string `json:"failure_code,omitempty"`
	ModelProvider   string `json:"model_provider,omitempty"`
	Model           string `json:"model,omitempty"`
}

type Manager struct {
	config Config
	logger *slog.Logger

	mu       sync.Mutex
	status   Status
	started  bool
	cmd      *exec.Cmd
	client   *Client
	exitDone chan struct{}
	stderr   *tailBuffer

	notificationHandler NotificationHandler
	deleteWaiters       map[string]chan struct{}
	titleCollectors     map[string]*titleCollector
	pendingTitleStarts  int
}

func NewManager(config Config, logger *slog.Logger) *Manager {
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	return &Manager{
		config: config,
		logger: logger,
		status: Status{
			State:           StateNotConfigured,
			Transport:       ExpectedTransport,
			ExperimentalAPI: false,
		},
		deleteWaiters:   make(map[string]chan struct{}),
		titleCollectors: make(map[string]*titleCollector),
	}
}

func (m *Manager) Snapshot() Status {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.status
}

func (m *Manager) Start(ctx context.Context) error {
	m.mu.Lock()
	if m.started {
		m.mu.Unlock()
		return errors.New("runtime manager may only be started once")
	}
	m.started = true
	m.mu.Unlock()

	if !m.config.Configured() {
		m.fail("runtime_not_configured")
		return errors.New("runtime is not configured")
	}
	if err := m.config.validate(); err != nil {
		m.fail("runtime_config_invalid")
		return err
	}
	if m.config.MiniMax.Enabled {
		if err := prepareMiniMaxCodexHome(m.config.CodexHome); err != nil {
			m.fail("provider_config_failed")
			return err
		}
		m.mu.Lock()
		m.status.ModelProvider = MiniMaxProviderID
		m.status.Model = MiniMaxModel
		m.mu.Unlock()
	}
	if m.config.FakeResponses.Enabled {
		if err := prepareFakeResponsesCodexHome(m.config.CodexHome, m.config.FakeResponses); err != nil {
			m.fail("provider_config_failed")
			return err
		}
		m.mu.Lock()
		m.status.ModelProvider = MiniMaxProviderID
		m.status.Model = MiniMaxModel
		m.mu.Unlock()
	}

	startupCtx, cancel := context.WithTimeout(ctx, m.config.StartupTimeout)
	defer cancel()
	m.setState(StateVerifying)
	var artifact ArtifactInfo
	var err error
	if m.config.testArtifactPolicy == nil {
		artifact, err = VerifyArtifact(
			startupCtx, m.config.BinaryPath, m.config.ManifestPath, m.config.StartupTimeout,
		)
	} else {
		artifact, err = verifyArtifactWithPolicy(
			startupCtx,
			m.config.BinaryPath,
			m.config.ManifestPath,
			m.config.StartupTimeout,
			*m.config.testArtifactPolicy,
		)
	}
	if err != nil {
		m.fail("artifact_verification_failed")
		return err
	}
	m.setArtifact(artifact)
	if err := startupCtx.Err(); err != nil {
		m.fail("startup_cancelled")
		return err
	}

	m.setState(StateStarting)
	stdin, stdout, cmd, stderr, err := m.startProcess()
	if err != nil {
		m.fail("runtime_start_failed")
		return err
	}
	m.mu.Lock()
	m.cmd = cmd
	m.stderr = stderr
	m.exitDone = make(chan struct{})
	m.mu.Unlock()

	client := NewClient(
		stdin,
		stdout,
		m.config.MaxMessageBytes,
		m.config.WriteQueueDepth,
		m.handleNotification,
		m.handleClientFailure,
	)
	m.mu.Lock()
	m.client = client
	m.mu.Unlock()
	client.Start()
	go m.waitForExit(cmd)

	m.setState(StateInitializing)
	requestCtx, requestCancel := context.WithTimeout(startupCtx, m.config.RequestTimeout)
	defer requestCancel()
	var initialized initializeResponse
	if err := client.Request(requestCtx, "initialize", initializeParams{
		ClientInfo: clientInfo{
			Name:    "yijie_agent_host",
			Title:   "Yijie Agent Host",
			Version: "0.1.0",
		},
		Capabilities: initializeCapabilities{ExperimentalAPI: false},
	}, &initialized); err != nil {
		m.abortStartup("initialize_failed")
		return fmt.Errorf("initialize app-server: %w", err)
	}
	if err := m.validateInitializeResponse(initialized); err != nil {
		m.abortStartup("initialize_response_invalid")
		return err
	}
	if err := client.Notify(requestCtx, "initialized", struct{}{}); err != nil {
		m.abortStartup("initialized_notification_failed")
		return fmt.Errorf("notify app-server initialized: %w", err)
	}

	m.mu.Lock()
	if m.status.State == StateFailed {
		m.mu.Unlock()
		return errors.New("runtime failed during initialization")
	}
	m.status.State = StateReady
	m.status.Ready = true
	m.status.FailureCode = ""
	m.mu.Unlock()
	return nil
}

func (m *Manager) Shutdown(ctx context.Context) error {
	m.mu.Lock()
	state := m.status.State
	if state == StateNotConfigured || state == StateStopped {
		m.mu.Unlock()
		return nil
	}
	m.status.State = StateStopping
	m.status.Ready = false
	client := m.client
	process := m.cmd
	exitDone := m.exitDone
	m.mu.Unlock()

	if client != nil {
		_ = client.CloseInput()
	}
	if process == nil || exitDone == nil {
		m.setState(StateStopped)
		return nil
	}

	select {
	case <-exitDone:
		return nil
	case <-ctx.Done():
		if process.Process != nil {
			_ = process.Process.Kill()
		}
		select {
		case <-exitDone:
		case <-time.After(time.Second):
		}
		return ctx.Err()
	}
}

func (m *Manager) startProcess() (io.WriteCloser, io.ReadCloser, *exec.Cmd, *tailBuffer, error) {
	cmd := exec.Command(
		m.config.BinaryPath,
		"app-server",
		"--listen", "stdio://",
		"--strict-config",
	)
	miniMaxKey := ""
	if m.config.MiniMax.Enabled {
		miniMaxKey = m.config.MiniMax.APIKey
	}
	cmd.Env = runtimeEnvironment(os.Environ(), m.config.CodexHome, miniMaxKey)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("open runtime stdin: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("open runtime stdout: %w", err)
	}
	stderr := newTailBuffer(m.config.StderrTailBytes)
	cmd.Stderr = stderr
	if err := cmd.Start(); err != nil {
		return nil, nil, nil, nil, fmt.Errorf("start runtime: %w", err)
	}
	return stdin, stdout, cmd, stderr, nil
}

func (m *Manager) waitForExit(cmd *exec.Cmd) {
	err := cmd.Wait()
	m.mu.Lock()
	state := m.status.State
	if state == StateStopping {
		m.status.State = StateStopped
		m.status.FailureCode = ""
	} else if state != StateFailed {
		m.status.State = StateFailed
		m.status.FailureCode = "runtime_exited"
	}
	m.status.Ready = false
	exitDone := m.exitDone
	m.mu.Unlock()
	if exitDone != nil {
		close(exitDone)
	}
	if err != nil && state != StateStopping {
		m.logger.Warn("Codex Runtime exited", "failure_code", "runtime_exited")
	}
}

func (m *Manager) handleClientFailure(err error) {
	m.mu.Lock()
	state := m.status.State
	process := m.cmd
	if state != StateStopping && state != StateStopped && state != StateFailed {
		m.status.State = StateFailed
		m.status.Ready = false
		if errors.Is(err, io.EOF) {
			m.status.FailureCode = "runtime_disconnected"
		} else {
			m.status.FailureCode = "protocol_failure"
		}
	}
	m.mu.Unlock()
	if state != StateStopping && state != StateStopped && process != nil && process.Process != nil {
		_ = process.Process.Kill()
	}
}

func (m *Manager) handleNotification(method string, params json.RawMessage) {
	m.logger.Debug("Codex Runtime notification", "method", method)
	m.mu.Lock()
	if method == "thread/started" {
		var notification struct {
			Thread struct {
				ID        string `json:"id"`
				Ephemeral bool   `json:"ephemeral"`
			} `json:"thread"`
		}
		if json.Unmarshal(params, &notification) == nil && notification.Thread.ID != "" {
			if _, private := m.titleCollectors[notification.Thread.ID]; private || (m.pendingTitleStarts > 0 && notification.Thread.Ephemeral) {
				m.mu.Unlock()
				return
			}
		}
	}
	if method == RuntimeNotificationThreadDeleted {
		var notification struct {
			ThreadID string `json:"threadId"`
		}
		if json.Unmarshal(params, &notification) == nil {
			if waiter := m.deleteWaiters[notification.ThreadID]; waiter != nil {
				close(waiter)
				delete(m.deleteWaiters, notification.ThreadID)
			}
		}
	}
	var correlated struct {
		ThreadID string `json:"threadId"`
	}
	if json.Unmarshal(params, &correlated) == nil && correlated.ThreadID != "" {
		if collector := m.titleCollectors[correlated.ThreadID]; collector != nil {
			m.mu.Unlock()
			collector.handle(method, params)
			return
		}
	}
	handler := m.notificationHandler
	m.mu.Unlock()
	if handler != nil {
		handler(method, append(json.RawMessage(nil), params...))
	}
}

func (m *Manager) validateInitializeResponse(response initializeResponse) error {
	if response.UserAgent == "" || response.PlatformFamily == "" || response.PlatformOS == "" {
		return errors.New("app-server initialize response is incomplete")
	}
	if response.PlatformFamily != "unix" || response.PlatformOS != "macos" {
		return errors.New("app-server platform does not match the pinned Runtime target")
	}
	if !filepath.IsAbs(response.CodexHome) {
		return errors.New("app-server returned a non-absolute CODEX_HOME")
	}
	expected, err := filepath.EvalSymlinks(m.config.CodexHome)
	if err != nil {
		return fmt.Errorf("resolve configured CODEX_HOME: %w", err)
	}
	actual, err := filepath.EvalSymlinks(response.CodexHome)
	if err != nil {
		return fmt.Errorf("resolve app-server CODEX_HOME: %w", err)
	}
	if filepath.Clean(expected) != filepath.Clean(actual) {
		return errors.New("app-server initialized with an unexpected CODEX_HOME")
	}
	return nil
}

func (m *Manager) abortStartup(code string) {
	m.mu.Lock()
	if m.status.State != StateFailed {
		m.status.State = StateFailed
		m.status.Ready = false
		m.status.FailureCode = code
	}
	process := m.cmd
	m.mu.Unlock()
	if process != nil && process.Process != nil {
		_ = process.Process.Kill()
	}
}

func (m *Manager) setArtifact(artifact ArtifactInfo) {
	m.mu.Lock()
	m.status.RuntimeVersion = artifact.RuntimeVersion
	m.status.UpstreamTag = artifact.UpstreamTag
	m.status.UpstreamCommit = artifact.UpstreamCommit
	m.status.Transport = artifact.Transport
	m.status.ExperimentalAPI = artifact.ExperimentalAPI
	m.mu.Unlock()
}

func (m *Manager) setState(state string) {
	m.mu.Lock()
	m.status.State = state
	m.status.Ready = state == StateReady
	m.mu.Unlock()
}

func (m *Manager) fail(code string) {
	m.mu.Lock()
	m.status.State = StateFailed
	m.status.Ready = false
	m.status.FailureCode = code
	m.mu.Unlock()
}

type clientInfo struct {
	Name    string `json:"name"`
	Title   string `json:"title"`
	Version string `json:"version"`
}

type initializeCapabilities struct {
	ExperimentalAPI bool `json:"experimentalApi"`
}

type initializeParams struct {
	ClientInfo   clientInfo             `json:"clientInfo"`
	Capabilities initializeCapabilities `json:"capabilities"`
}

type initializeResponse struct {
	UserAgent      string `json:"userAgent"`
	CodexHome      string `json:"codexHome"`
	PlatformFamily string `json:"platformFamily"`
	PlatformOS     string `json:"platformOs"`
}

func runtimeEnvironment(current []string, codexHome, miniMaxAPIKey string) []string {
	blocked := map[string]struct{}{
		"OPENAI_API_KEY":             {},
		"CODEX_API_KEY":              {},
		"CODEX_ACCESS_TOKEN":         {},
		"CHATGPT_ACCESS_TOKEN":       {},
		"MINIMAX_API_KEY":            {},
		"MINIMAX_API_KEY_FILE":       {},
		"YIJIE_MINIMAX_API_KEY":      {},
		"YIJIE_MINIMAX_API_KEY_FILE": {},
		"CODEX_HOME":                 {},
	}
	clean := make([]string, 0, len(current)+1)
	for _, entry := range current {
		name, _, found := strings.Cut(entry, "=")
		if !found {
			continue
		}
		if _, remove := blocked[name]; remove {
			continue
		}
		clean = append(clean, entry)
	}
	clean = append(clean, "CODEX_HOME="+codexHome)
	if miniMaxAPIKey != "" {
		clean = append(clean, MiniMaxRuntimeEnvKey+"="+miniMaxAPIKey)
	}
	return clean
}

type tailBuffer struct {
	mu    sync.Mutex
	limit int
	data  []byte
}

func newTailBuffer(limit int) *tailBuffer {
	return &tailBuffer{limit: limit}
}

func (b *tailBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.data = append(b.data, p...)
	if len(b.data) > b.limit {
		b.data = append([]byte(nil), b.data[len(b.data)-b.limit:]...)
	}
	return len(p), nil
}

func (b *tailBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return string(append([]byte(nil), b.data...))
}
