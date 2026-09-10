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

	"github.com/google/uuid"
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

const (
	feat137D4DeterministicProducerHostEnv   = "YIJIE_FEAT137_D4_DETERMINISTIC_PRODUCER_ENABLED"
	feat137DeterministicApprovalProducerEnv = "YIJIE_FEAT137_DETERMINISTIC_APPROVAL_PRODUCER"
)

type Config struct {
	BinaryPath                    string
	ManifestPath                  string
	CodexHome                     string
	StartupTimeout                time.Duration
	RequestTimeout                time.Duration
	ShutdownTimeout               time.Duration
	MaxMessageBytes               int
	WriteQueueDepth               int
	StderrTailBytes               int
	MiniMax                       MiniMaxConfig
	Sorftime                      SorftimeConfig
	FakeResponses                 FakeResponsesConfig
	ManagedReasoningProfile       ManagedReasoningProfile
	DynamicToolsEnabled           bool
	CommandApprovalEnabled        bool
	RuntimePermissionsEnabled     bool
	PermissionVerificationBaseURL string
	PermissionVerificationPolicy  string
	// DeterministicApprovalProducerEnabled is a D4-only, Host-validated
	// producer gate. It never changes the command approval authority or
	// execution permissions and cannot be enabled without that authority.
	DeterministicApprovalProducerEnabled bool

	// testArtifactPolicy is intentionally package-private. Production always
	// uses the exact Runtime Baseline 0 artifact policy.
	testArtifactPolicy *artifactPolicy
	// testBeforeArtifactVerification exposes a deterministic cancellation
	// boundary without starting a process. Production never sets it.
	testBeforeArtifactVerification func(context.Context) error
	// These two additional safe test boundaries prove Shutdown linearization
	// around CODEX_HOME mutation and process start. Production never sets them.
	testAfterCodexHomeAuthorityAcquired func(context.Context) error
	testBeforeProcessStart              func(context.Context) error
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
	if c.FakeResponses.Enabled {
		if err := validateFEAT126CodexHome(c.CodexHome); err != nil {
			return err
		}
	} else {
		if err := validateManagedCodexHome(c.CodexHome); err != nil {
			return err
		}
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
	if err := c.Sorftime.validate(c.RuntimePermissionsEnabled, c.MiniMax.Enabled); err != nil {
		return err
	}
	if c.Sorftime.Enabled && c.CommandApprovalEnabled {
		return errors.New("Sorftime cannot use retired approvals")
	}
	if err := c.FakeResponses.validate(); err != nil {
		return err
	}
	if c.MiniMax.Enabled && c.FakeResponses.Enabled {
		return errors.New("MiniMax and fake Responses providers are mutually exclusive")
	}
	if err := c.ManagedReasoningProfile.validate(c.MiniMax.Enabled, c.DynamicToolsEnabled); err != nil {
		return err
	}
	if c.DynamicToolsEnabled && !c.MiniMax.Enabled {
		return errors.New("Runtime dynamic tools require the MiniMax provider")
	}
	if c.CommandApprovalEnabled && (!c.MiniMax.Enabled || c.FakeResponses.Enabled || c.DynamicToolsEnabled) {
		return errors.New("Runtime command approvals require the exact stable MiniMax profile without dynamic tools")
	}
	if c.DeterministicApprovalProducerEnabled && !c.CommandApprovalEnabled {
		return errors.New("Runtime deterministic approval producer requires FEAT-137 command approval authority")
	}
	return nil
}

func validateFEAT126CodexHome(path string) error {
	if err := validateManagedCodexHome(path); err != nil {
		return fmt.Errorf("FEAT-126 CODEX_HOME authority: %w", err)
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
	permissionCallbacks runtimeApprovalCallbacks
	sorftime            sorftimeRuntimeState
	config              Config
	logger              *slog.Logger

	mu       sync.Mutex
	status   Status
	artifact ArtifactInfo
	started  bool
	cmd      *exec.Cmd
	client   *Client
	exitDone chan struct{}
	stderr   *tailBuffer

	startDone             chan struct{}
	startupCancel         context.CancelFunc
	shutdownRequested     bool
	codexHomeAuthority    *managedCodexHomeAuthority
	authorityCleanupError error

	notificationHandler    NotificationHandler
	dynamicToolHandler     DynamicToolHandler
	commandApprovalHandler CommandApprovalHandler
	runtimeGeneration      string
	deleteWaiters          map[string]chan struct{}
	titleCollectors        map[string]*titleCollector
	pendingTitleStarts     int
}

func NewManager(config Config, logger *slog.Logger) *Manager {
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	return &Manager{
		config:   config,
		sorftime: sorftimeRuntimeState{configured: config.Sorftime.Enabled, enabled: config.Sorftime.Enabled, threads: make(map[string]string), verified: make(map[string]bool), startup: make(map[string]string), catalogAttempted: make(map[string]bool), catalogVerified: make(map[string]bool), changed: make(chan struct{})},
		logger:   logger,
		status: Status{
			State:           StateNotConfigured,
			Transport:       ExpectedTransport,
			ExperimentalAPI: config.DynamicToolsEnabled,
		},
		runtimeGeneration: uuid.NewString(),
		deleteWaiters:     make(map[string]chan struct{}),
		titleCollectors:   make(map[string]*titleCollector),
	}
}

func (m *Manager) Snapshot() Status {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.status
}

func (m *Manager) Start(ctx context.Context) (returnErr error) {
	lifecycleCtx, lifecycleCancel := context.WithCancel(ctx)
	m.mu.Lock()
	if m.started {
		m.mu.Unlock()
		lifecycleCancel()
		return errors.New("runtime manager may only be started once")
	}
	if m.shutdownRequested {
		m.mu.Unlock()
		lifecycleCancel()
		return errors.New("runtime shutdown was requested before startup")
	}
	m.started = true
	m.startDone = make(chan struct{})
	m.startupCancel = lifecycleCancel
	startDone := m.startDone
	m.mu.Unlock()
	defer func() {
		m.mu.Lock()
		m.startupCancel = nil
		close(startDone)
		m.mu.Unlock()
		lifecycleCancel()
	}()

	if !m.config.Configured() {
		m.fail("runtime_not_configured")
		return errors.New("runtime is not configured")
	}
	if err := m.config.validate(); err != nil {
		m.fail("runtime_config_invalid")
		return err
	}
	m.mu.Lock()
	dynamicToolHandler := m.dynamicToolHandler
	commandApprovalHandler := m.commandApprovalHandler
	m.mu.Unlock()
	if m.config.DynamicToolsEnabled && dynamicToolHandler == nil {
		m.fail("runtime_config_invalid")
		return errors.New("Runtime dynamic tool handler is not configured")
	}
	if m.config.CommandApprovalEnabled && commandApprovalHandler == nil {
		m.fail("runtime_config_invalid")
		return errors.New("Runtime command approval handler is not configured")
	}
	if err := m.startupBoundaryError(lifecycleCtx); err != nil {
		m.fail("startup_cancelled")
		return err
	}
	startupCtx, cancel := context.WithTimeout(lifecycleCtx, m.config.StartupTimeout)
	defer cancel()
	m.setState(StateVerifying)
	if hook := m.config.testBeforeArtifactVerification; hook != nil {
		if err := hook(startupCtx); err != nil {
			m.fail("startup_cancelled")
			return err
		}
	}
	if err := m.startupBoundaryError(startupCtx); err != nil {
		m.fail("startup_cancelled")
		return err
	}
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
	if err := m.startupBoundaryError(startupCtx); err != nil {
		m.fail("startup_cancelled")
		return err
	}

	authority, err := acquireManagedCodexHomeAuthority(m.config.CodexHome)
	if err != nil {
		m.fail("provider_config_failed")
		return err
	}
	m.mu.Lock()
	if m.shutdownRequested || startupCtx.Err() != nil {
		m.mu.Unlock()
		releaseErr := authority.releaseWithoutCleanup()
		m.fail("startup_cancelled")
		return errors.Join(m.startupBoundaryError(startupCtx), releaseErr)
	}
	m.codexHomeAuthority = authority
	m.authorityCleanupError = nil
	m.mu.Unlock()
	processOwnsAuthority := false
	cleanupAuthority := false
	defer func() {
		if processOwnsAuthority {
			return
		}
		if cleanupErr := m.releaseManagedCodexHomeAuthority(cleanupAuthority); cleanupErr != nil {
			m.fail("provider_config_failed")
			returnErr = errors.Join(returnErr, fmt.Errorf("cleanup managed CODEX_HOME: %w", cleanupErr))
		}
	}()

	if hook := m.config.testAfterCodexHomeAuthorityAcquired; hook != nil {
		if err := hook(startupCtx); err != nil {
			m.fail("startup_cancelled")
			return err
		}
	}
	if err := m.startupBoundaryError(startupCtx); err != nil {
		m.fail("startup_cancelled")
		return err
	}

	var providerErr error
	m.mu.Lock()
	if m.shutdownRequested || startupCtx.Err() != nil {
		m.mu.Unlock()
		m.fail("startup_cancelled")
		return m.startupBoundaryError(startupCtx)
	}
	cleanupAuthority = true
	rulePlan, providerErr := authority.preflightFEAT137ExecPolicy()
	if providerErr == nil && m.config.MiniMax.Enabled {
		providerErr = prepareMiniMaxCodexHomeForAuthority(
			authority,
			m.config.ManagedReasoningProfile,
			m.config.CommandApprovalEnabled,
			m.config.Sorftime.Enabled,
		)
		if providerErr == nil {
			m.status.ModelProvider = MiniMaxProviderID
			m.status.Model = MiniMaxModel
		}
	}
	if providerErr == nil && m.config.FakeResponses.Enabled {
		providerErr = prepareFakeResponsesCodexHome(authority, m.config.FakeResponses)
		if providerErr == nil {
			m.status.ModelProvider = MiniMaxProviderID
			m.status.Model = MiniMaxModel
		}
	}
	if providerErr == nil {
		providerErr = authority.applyFEAT137ExecPolicy(m.config.CommandApprovalEnabled, rulePlan)
	}
	m.mu.Unlock()
	if providerErr != nil {
		m.fail("provider_config_failed")
		return providerErr
	}

	if hook := m.config.testBeforeProcessStart; hook != nil {
		if err := hook(startupCtx); err != nil {
			m.fail("startup_cancelled")
			return err
		}
	}
	if err := m.startupBoundaryError(startupCtx); err != nil {
		m.fail("startup_cancelled")
		return err
	}
	if err := m.validateManagedProviderAuthority(authority); err != nil {
		m.fail("provider_config_failed")
		return err
	}

	m.mu.Lock()
	if m.shutdownRequested || startupCtx.Err() != nil {
		m.mu.Unlock()
		m.fail("startup_cancelled")
		return m.startupBoundaryError(startupCtx)
	}
	m.status.State = StateStarting
	m.status.Ready = false
	stdin, stdout, cmd, stderr, err := m.startProcess()
	if err != nil {
		m.status.State = StateFailed
		m.status.Ready = false
		m.status.FailureCode = "runtime_start_failed"
		m.mu.Unlock()
		return err
	}
	m.cmd = cmd
	m.stderr = stderr
	m.exitDone = make(chan struct{})
	m.mu.Unlock()
	processOwnsAuthority = true

	client := NewClient(
		stdin,
		stdout,
		m.config.MaxMessageBytes,
		m.config.WriteQueueDepth,
		m.handleNotification,
		m.handleClientFailure,
	)
	client.setServerRequestHandler(m.handleServerRequest)
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
		Capabilities: initializeCapabilities{ExperimentalAPI: m.config.DynamicToolsEnabled},
	}, &initialized); err != nil {
		abortErr := m.abortStartup("initialize_failed")
		return errors.Join(fmt.Errorf("initialize app-server: %w", err), abortErr)
	}
	if err := m.validateInitializeResponse(initialized); err != nil {
		abortErr := m.abortStartup("initialize_response_invalid")
		return errors.Join(err, abortErr)
	}
	if err := client.Notify(requestCtx, "initialized", struct{}{}); err != nil {
		abortErr := m.abortStartup("initialized_notification_failed")
		return errors.Join(fmt.Errorf("notify app-server initialized: %w", err), abortErr)
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

func (m *Manager) validateManagedProviderAuthority(authority *managedCodexHomeAuthority) error {
	if !m.config.MiniMax.Enabled {
		return nil
	}
	catalog, err := miniMaxModelCatalog()
	if err != nil {
		return err
	}
	if err := authority.validateManagedFileExact(managedModelCatalogName, catalog); err != nil {
		return err
	}
	expected, err := miniMaxManagedConfigForRuntime(
		m.config.CodexHome,
		m.config.ManagedReasoningProfile,
		m.config.CommandApprovalEnabled,
		m.config.Sorftime.Enabled,
	)
	if err != nil {
		return err
	}
	if err := authority.validateManagedFileExact("config.toml", expected); err != nil {
		return err
	}
	if m.config.CommandApprovalEnabled {
		if _, err := authority.preflightFEAT137ExecPolicy(); err != nil {
			return err
		}
	}
	return nil
}

// RuntimeEvidence is a content-free Host-owned projection for the S10BO1
// orchestrator. It never includes executable paths, argv, environment values,
// Runtime messages, or task content.
type RuntimeEvidence struct {
	SchemaVersion  int    `json:"schema_version"`
	RunID          string `json:"run_id"`
	Role           string `json:"role"`
	PID            int    `json:"pid"`
	PPID           int    `json:"ppid"`
	BinarySHA256   string `json:"binary_sha256"`
	ManifestSHA256 string `json:"manifest_sha256"`
	Nonce          string `json:"nonce"`
	Profile        string `json:"profile"`
	State          string `json:"state"`
	Ready          bool   `json:"ready"`
}

func (m *Manager) RuntimeEvidence(runID, nonce, profile string) (RuntimeEvidence, error) {
	if !isCanonicalRFC4122UUIDv4(runID) || !isCanonicalRFC4122UUIDv4(nonce) ||
		profile != "feat-126-s10-local-lab" {
		return RuntimeEvidence{}, errors.New("runtime evidence authority is invalid")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.cmd == nil || m.cmd.Process == nil || m.artifact.BinarySHA256 == "" || m.artifact.ManifestSHA256 == "" {
		return RuntimeEvidence{}, errors.New("runtime process evidence is unavailable")
	}
	return RuntimeEvidence{
		SchemaVersion: 1, RunID: runID, Role: "runtime",
		PID: m.cmd.Process.Pid, PPID: os.Getpid(),
		BinarySHA256: m.artifact.BinarySHA256, ManifestSHA256: m.artifact.ManifestSHA256,
		Nonce: nonce, Profile: profile, State: m.status.State, Ready: m.status.Ready,
	}, nil
}

func (m *Manager) Shutdown(ctx context.Context) error {
	m.mu.Lock()
	m.shutdownRequested = true
	startDone := m.startDone
	startupCancel := m.startupCancel
	started := m.started
	if !started {
		m.status.State = StateStopped
		m.status.Ready = false
		m.status.FailureCode = ""
	}
	m.mu.Unlock()
	if startupCancel != nil {
		startupCancel()
	}
	if started && startDone != nil {
		select {
		case <-startDone:
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	m.mu.Lock()
	state := m.status.State
	if state == StateNotConfigured {
		m.mu.Unlock()
		return nil
	}
	if state == StateStopped {
		cleanupErr := m.authorityCleanupError
		m.mu.Unlock()
		return cleanupErr
	}
	client := m.client
	process := m.cmd
	exitDone := m.exitDone
	alreadyExited := false
	if exitDone != nil {
		select {
		case <-exitDone:
			alreadyExited = true
		default:
		}
	}
	if alreadyExited {
		cleanupErr := m.authorityCleanupError
		if cleanupErr == nil {
			m.status.State = StateStopped
			m.status.Ready = false
			m.status.FailureCode = ""
		}
		m.mu.Unlock()
		return cleanupErr
	}
	m.status.State = StateStopping
	m.status.Ready = false
	m.mu.Unlock()

	if client != nil {
		_ = client.CloseInput()
	}
	if process == nil || exitDone == nil {
		cleanupErr := m.releaseManagedCodexHomeAuthority(true)
		if cleanupErr != nil {
			m.fail("provider_config_failed")
			return cleanupErr
		}
		m.mu.Lock()
		m.status.State = StateStopped
		m.status.Ready = false
		m.status.FailureCode = ""
		m.mu.Unlock()
		return m.managedCodexHomeCleanupError()
	}

	select {
	case <-exitDone:
		return m.managedCodexHomeCleanupError()
	case <-ctx.Done():
		// Keep the lease and exact Runtime rule until waitForExit observes a
		// real process exit. A timeout must not strong-kill Runtime or expose a
		// gate-off Host to a still-running session reloader.
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
	cmd.Env = runtimeEnvironment(
		os.Environ(),
		m.config.CodexHome,
		miniMaxKey,
		m.config.DeterministicApprovalProducerEnabled,
	)
	if m.config.Sorftime.Enabled {
		// Use the existing native HTTP proxy support for the current system
		// HTTPS route. Keep the normal provider and loopback routes direct.
		clean := cmd.Env[:0]
		for _, entry := range cmd.Env {
			name, _, _ := strings.Cut(entry, "=")
			switch strings.ToUpper(name) {
			case "HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "NO_PROXY":
				continue
			}
			clean = append(clean, entry)
		}
		cmd.Env = clean
		if m.config.Sorftime.HTTPSProxy != "" {
			cmd.Env = append(cmd.Env, "HTTPS_PROXY="+m.config.Sorftime.HTTPSProxy, "NO_PROXY=api.minimaxi.com,127.0.0.1,localhost")
		}
		cmd.Env = append(cmd.Env, SorftimeSecretEnv+"="+m.config.Sorftime.Token)
	}
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
	cleanupErr := m.releaseManagedCodexHomeAuthority(true)
	m.mu.Lock()
	state := m.status.State
	if cleanupErr != nil {
		m.status.State = StateFailed
		m.status.FailureCode = "provider_config_failed"
	} else if state == StateStopping {
		m.status.State = StateStopped
		m.status.FailureCode = ""
	} else if state != StateFailed {
		m.status.State = StateFailed
		m.status.FailureCode = "runtime_exited"
	}
	m.status.Ready = false
	exitDone := m.exitDone
	approvalHandler := m.commandApprovalHandler
	generation := m.runtimeGeneration
	m.mu.Unlock()
	if cleanupErr != nil {
		m.logger.Warn("managed CODEX_HOME cleanup failed", "failure_code", "provider_config_failed", "detail", "authority_cleanup")
	}
	if approvalHandler != nil && generation != "" {
		approvalHandler.HandleCommandApprovalGenerationClosed(generation)
	}
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
	client := m.client
	if state != StateStopping && state != StateStopped && state != StateFailed {
		m.status.State = StateFailed
		m.status.Ready = false
		if errors.Is(err, io.EOF) {
			m.status.FailureCode = "runtime_disconnected"
		} else {
			m.status.FailureCode = "protocol_failure"
		}
	}
	approvalHandler := m.commandApprovalHandler
	generation := m.runtimeGeneration
	m.mu.Unlock()
	if approvalHandler != nil && generation != "" {
		approvalHandler.HandleCommandApprovalGenerationClosed(generation)
	}
	if state != StateStopping && state != StateStopped && client != nil {
		_ = client.CloseInput()
	}
}

func (m *Manager) handleNotification(method string, params json.RawMessage) {
	m.observeSorftimeStartup(method, params)
	m.handlePermissionNotification(method, params)
	m.logger.Debug("Codex Runtime notification", "method", method)
	if method == RuntimeNotificationServerRequestResolved {
		fields, err := decodeUniqueJSONObject(params, map[string]struct{}{"requestId": {}, "threadId": {}})
		if err != nil || len(fields) != 2 {
			m.logger.Warn("discarding malformed Runtime request resolution", "failure_code", "approval_resolution_invalid")
			return
		}
		requestIDKey, err := requestIDKey(fields["requestId"])
		var threadID string
		if err != nil || json.Unmarshal(fields["threadId"], &threadID) != nil || threadID == "" {
			m.logger.Warn("discarding malformed Runtime request resolution", "failure_code", "approval_resolution_invalid")
			return
		}
		m.mu.Lock()
		handler := m.commandApprovalHandler
		generation := m.runtimeGeneration
		enabled := m.config.CommandApprovalEnabled
		m.mu.Unlock()
		if enabled && handler != nil && generation != "" {
			handler.HandleCommandApprovalResolved(CommandApprovalResolved{
				RuntimeGeneration: generation,
				RequestIDKey:      requestIDKey,
				ThreadID:          threadID,
			})
		}
		return
	}
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

func (m *Manager) abortStartup(code string) error {
	m.mu.Lock()
	if m.status.State != StateFailed {
		m.status.State = StateFailed
		m.status.Ready = false
		m.status.FailureCode = code
	}
	client := m.client
	exitDone := m.exitDone
	m.mu.Unlock()
	if client != nil {
		_ = client.CloseInput()
	}
	if exitDone == nil {
		return nil
	}
	timer := time.NewTimer(m.config.ShutdownTimeout)
	defer timer.Stop()
	select {
	case <-exitDone:
		return m.managedCodexHomeCleanupError()
	case <-timer.C:
		return errors.New("Runtime did not exit normally after startup failure; managed CODEX_HOME lease retained")
	}
}

func (m *Manager) releaseManagedCodexHomeAuthority(cleanup bool) error {
	m.mu.Lock()
	authority := m.codexHomeAuthority
	m.codexHomeAuthority = nil
	m.mu.Unlock()
	if authority == nil {
		return m.managedCodexHomeCleanupError()
	}
	var err error
	if cleanup {
		err = authority.Close()
	} else {
		err = authority.releaseWithoutCleanup()
	}
	m.mu.Lock()
	if err != nil && m.authorityCleanupError == nil {
		m.authorityCleanupError = err
	}
	stored := m.authorityCleanupError
	m.mu.Unlock()
	return stored
}

func (m *Manager) startupBoundaryError(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	m.mu.Lock()
	requested := m.shutdownRequested
	m.mu.Unlock()
	if requested {
		return errors.New("runtime shutdown was requested during startup")
	}
	return nil
}

func (m *Manager) managedCodexHomeCleanupError() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.authorityCleanupError
}

func (m *Manager) setArtifact(artifact ArtifactInfo) {
	m.mu.Lock()
	m.artifact = artifact
	m.status.RuntimeVersion = artifact.RuntimeVersion
	m.status.UpstreamTag = artifact.UpstreamTag
	m.status.UpstreamCommit = artifact.UpstreamCommit
	m.status.Transport = artifact.Transport
	m.status.ExperimentalAPI = m.config.DynamicToolsEnabled
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

func runtimeEnvironment(
	current []string,
	codexHome string,
	miniMaxAPIKey string,
	deterministicApprovalProducer bool,
) []string {
	blocked := map[string]struct{}{
		"OPENAI_API_KEY":                        {},
		"CODEX_API_KEY":                         {},
		"CODEX_ACCESS_TOKEN":                    {},
		"CHATGPT_ACCESS_TOKEN":                  {},
		"MINIMAX_API_KEY":                       {},
		"MINIMAX_API_KEY_FILE":                  {},
		"YIJIE_MINIMAX_API_KEY":                 {},
		"YIJIE_MINIMAX_API_KEY_FILE":            {},
		"CODEX_HOME":                            {},
		SorftimeSecretEnv:                       {},
		SorftimeEnabledEnv:                      {},
		SorftimeProxyEnv:                        {},
		feat137D4DeterministicProducerHostEnv:   {},
		feat137DeterministicApprovalProducerEnv: {},
	}
	clean := make([]string, 0, len(current)+2)
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
	if deterministicApprovalProducer {
		clean = append(clean, feat137DeterministicApprovalProducerEnv+"=1")
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
