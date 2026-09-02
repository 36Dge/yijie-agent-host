package codex

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type feat137LifecycleApprovalHandler struct{}

func (feat137LifecycleApprovalHandler) HandleCommandApproval(
	context.Context,
	CommandApprovalRequest,
) CommandApprovalResult {
	return CommandApprovalResult{Respond: true, Decision: "cancel"}
}

func (feat137LifecycleApprovalHandler) HandleCommandApprovalResponseWritten(CommandApprovalResponseWriteResult) {
}

func (feat137LifecycleApprovalHandler) HandleCommandApprovalResolved(CommandApprovalResolved) {}

func (feat137LifecycleApprovalHandler) HandleCommandApprovalGenerationClosed(string) {}

func feat137AuthorityRuntimeFixture(t *testing.T, mode string) Config {
	t.Helper()
	t.Setenv("YIJIE_FAKE_MODE", mode)
	config := newRuntimeFixture(t)
	config.MiniMax = MiniMaxConfig{Enabled: true, APIKey: "test-minimax-key"}
	config.ManagedReasoningProfile = ManagedReasoningProfileHighRaw
	config.CommandApprovalEnabled = true
	return config
}

func newFEAT137AuthorityRuntimeManager(t *testing.T, mode string) (*Manager, Config) {
	t.Helper()
	config := feat137AuthorityRuntimeFixture(t, mode)
	manager := NewManager(config, nil)
	if err := manager.SetCommandApprovalHandler(feat137LifecycleApprovalHandler{}); err != nil {
		t.Fatal(err)
	}
	return manager, config
}

func TestFEAT137ManagerHoldsRuleAndLeaseAcrossRuntimeSessions(t *testing.T) {
	manager, config := newFEAT137AuthorityRuntimeManager(t, "feat137_authority")
	if err := manager.Start(context.Background()); err != nil {
		t.Fatalf("start FEAT-137 Runtime helper: %v", err)
	}
	configBytes, err := os.ReadFile(filepath.Join(config.CodexHome, "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if err := validateFEAT137ManagedConfig(configBytes, true); err != nil {
		t.Fatalf("ready FEAT-137 Runtime lacks the closed managed profile: %v", err)
	}
	rulePath := filepath.Join(config.CodexHome, managedRulesDirectory, managedFEAT137RulesFile)
	assertFEAT137ExactManagedRule(t, rulePath)
	if _, err := acquireManagedCodexHomeAuthority(config.CodexHome); err == nil ||
		!strings.Contains(err.Error(), "already leased by this Host process") {
		t.Fatalf("ready Runtime did not retain the CODEX_HOME lease: %v", err)
	}

	for session := 0; session < 2; session++ {
		if _, err := manager.StartThread(context.Background(), t.TempDir()); err != nil {
			t.Fatalf("start Runtime session %d: %v", session+1, err)
		}
		assertFEAT137ExactManagedRule(t, rulePath)
		if _, err := acquireManagedCodexHomeAuthority(config.CodexHome); err == nil {
			t.Fatalf("Runtime session %d released the lifecycle lease", session+1)
		}
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := manager.Shutdown(shutdownCtx); err != nil {
		t.Fatalf("normal FEAT-137 Runtime shutdown: %v", err)
	}
	if status := manager.Snapshot(); status.State != StateStopped || status.Ready || status.FailureCode != "" {
		t.Fatalf("normal shutdown did not converge to stopped: %+v", status)
	}
	if _, err := os.Lstat(rulePath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("normal shutdown retained FEAT-137 authority: %v", err)
	}
	authority, err := acquireManagedCodexHomeAuthority(config.CodexHome)
	if err != nil {
		t.Fatalf("normal shutdown did not release CODEX_HOME lease: %v", err)
	}
	if err := authority.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestFEAT137D4StartRejectsManagedProfileMissingClosedFeature(t *testing.T) {
	manager, config := newFEAT137AuthorityRuntimeManager(t, "feat137_authority")
	config.DeterministicApprovalProducerEnabled = true
	config.testBeforeProcessStart = func(context.Context) error {
		path := filepath.Join(config.CodexHome, "config.toml")
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		content = []byte(strings.Replace(string(content), "shell_snapshot = false\n", "", 1))
		return os.WriteFile(path, content, 0o600)
	}
	manager = NewManager(config, nil)
	if err := manager.SetCommandApprovalHandler(feat137LifecycleApprovalHandler{}); err != nil {
		t.Fatal(err)
	}
	if err := manager.Start(context.Background()); err == nil ||
		!strings.Contains(err.Error(), "failed exact authority validation") {
		t.Fatalf("D4 Runtime start accepted an incomplete closed managed profile: %v", err)
	}
	manager.mu.Lock()
	process := manager.cmd
	manager.mu.Unlock()
	if process != nil {
		t.Fatal("D4 Runtime process started without the five closed managed features")
	}
	if status := manager.Snapshot(); status.State != StateFailed || status.FailureCode != "provider_config_failed" {
		t.Fatalf("incomplete managed profile did not fail closed: %+v", status)
	}
	assertFEAT137RuleAbsentAndLeaseReleased(t, config.CodexHome)
}

func TestFEAT137D4StartRejectsManagedRetryPolicyDrift(t *testing.T) {
	for _, setting := range []string{
		"request_max_retries = 0\n",
		"stream_max_retries = 0\n",
	} {
		t.Run(strings.Fields(setting)[0], func(t *testing.T) {
			config := feat137AuthorityRuntimeFixture(t, "feat137_authority")
			config.DeterministicApprovalProducerEnabled = true
			config.testBeforeProcessStart = func(context.Context) error {
				path := filepath.Join(config.CodexHome, "config.toml")
				content, err := os.ReadFile(path)
				if err != nil {
					return err
				}
				content = []byte(strings.Replace(string(content), setting, "", 1))
				return os.WriteFile(path, content, 0o600)
			}
			manager := NewManager(config, nil)
			if err := manager.SetCommandApprovalHandler(feat137LifecycleApprovalHandler{}); err != nil {
				t.Fatal(err)
			}
			if err := manager.Start(context.Background()); err == nil ||
				!strings.Contains(err.Error(), "failed exact authority validation") {
				t.Fatalf("D4 Runtime start accepted managed retry-policy drift: %v", err)
			}
			manager.mu.Lock()
			process := manager.cmd
			manager.mu.Unlock()
			if process != nil {
				t.Fatal("D4 Runtime process started without the closed retry policy")
			}
			if status := manager.Snapshot(); status.State != StateFailed || status.FailureCode != "provider_config_failed" {
				t.Fatalf("managed retry-policy drift did not fail closed: %+v", status)
			}
			assertFEAT137RuleAbsentAndLeaseReleased(t, config.CodexHome)
		})
	}
}

func TestFEAT137StartRejectsPostPrepareManagedCatalogDrift(t *testing.T) {
	for _, commandApprovalEnabled := range []bool{false, true} {
		name := map[bool]string{false: "gate off", true: "approval D4"}[commandApprovalEnabled]
		t.Run(name, func(t *testing.T) {
			t.Setenv("YIJIE_FAKE_MODE", "feat137_authority")
			config := newRuntimeFixture(t)
			config.MiniMax = MiniMaxConfig{Enabled: true, APIKey: "test-minimax-key"}
			config.ManagedReasoningProfile = ManagedReasoningProfileHighRaw
			config.CommandApprovalEnabled = commandApprovalEnabled
			config.DeterministicApprovalProducerEnabled = commandApprovalEnabled
			config.testBeforeProcessStart = func(context.Context) error {
				path := filepath.Join(config.CodexHome, managedModelCatalogName)
				content, err := os.ReadFile(path)
				if err != nil {
					return err
				}
				return os.WriteFile(path, append(content, ' '), 0o600)
			}
			manager := NewManager(config, nil)
			if commandApprovalEnabled {
				if err := manager.SetCommandApprovalHandler(feat137LifecycleApprovalHandler{}); err != nil {
					t.Fatal(err)
				}
			}
			if err := manager.Start(context.Background()); err == nil ||
				!strings.Contains(err.Error(), managedModelCatalogName+" failed exact authority validation") {
				t.Fatalf("Runtime start accepted post-prepare managed catalog drift: %v", err)
			}
			manager.mu.Lock()
			process := manager.cmd
			manager.mu.Unlock()
			if process != nil {
				t.Fatal("Runtime process started after managed catalog authority drift")
			}
			if status := manager.Snapshot(); status.State != StateFailed || status.FailureCode != "provider_config_failed" {
				t.Fatalf("managed catalog drift did not fail closed: %+v", status)
			}
			assertFEAT137RuleAbsentAndLeaseReleased(t, config.CodexHome)
		})
	}
}

func TestFEAT137EveryRuntimeProfileUsesTheSameGateOffLease(t *testing.T) {
	tests := []struct {
		name      string
		mode      string
		configure func(*Config)
	}{
		{name: "providerless", mode: ""},
		{
			name: "MiniMax", mode: "baseline2",
			configure: func(config *Config) {
				config.MiniMax = MiniMaxConfig{Enabled: true, APIKey: "test-minimax-key"}
			},
		},
		{
			name: "Fake", mode: "feat126_fake",
			configure: func(config *Config) {
				config.FakeResponses = FakeResponsesConfig{
					Enabled: true, BaseURL: FEAT126FakeBaseURL,
					RunID: "123e4567-e89b-42d3-a456-426614174000", FixtureID: FEAT126FakeFixtureID,
				}
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("YIJIE_FAKE_MODE", test.mode)
			config := newRuntimeFixture(t)
			if test.configure != nil {
				test.configure(&config)
			}
			manager := NewManager(config, nil)
			if err := manager.Start(context.Background()); err != nil {
				t.Fatal(err)
			}
			if _, err := acquireManagedCodexHomeAuthority(config.CodexHome); err == nil {
				t.Fatal("gate-off Runtime profile did not hold the shared CODEX_HOME lease")
			}
			if _, err := os.Lstat(filepath.Join(config.CodexHome, managedRulesDirectory, managedFEAT137RulesFile)); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("gate-off Runtime profile installed FEAT-137 authority: %v", err)
			}
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := manager.Shutdown(shutdownCtx); err != nil {
				t.Fatal(err)
			}
			assertFEAT137RuleAbsentAndLeaseReleased(t, config.CodexHome)
		})
	}
}

func TestFEAT137ShutdownTimeoutLeavesLeaseWithExitWatcher(t *testing.T) {
	manager, config := newFEAT137AuthorityRuntimeManager(t, "feat137_slow_normal_exit")
	if err := manager.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := manager.Shutdown(shutdownCtx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("bounded Shutdown did not expose its caller timeout: %v", err)
	}
	if _, err := acquireManagedCodexHomeAuthority(config.CodexHome); err == nil {
		t.Fatal("Shutdown timeout released CODEX_HOME before normal process exit")
	}
	waitFor(t, 5*time.Second, func() bool {
		return manager.Snapshot().State == StateStopped
	})
	assertFEAT137RuleAbsentAndLeaseReleased(t, config.CodexHome)
}

func TestFEAT137InitializeFailureThenShutdownConvergesStopped(t *testing.T) {
	manager, config := newFEAT137AuthorityRuntimeManager(t, "feat137_bad_home")
	if err := manager.Start(context.Background()); err == nil {
		t.Fatal("expected the mismatched initialize response to fail")
	}
	if status := manager.Snapshot(); status.State != StateFailed ||
		status.FailureCode != "initialize_response_invalid" {
		t.Fatalf("unexpected initialize failure status: %+v", status)
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := manager.Shutdown(shutdownCtx); err != nil {
		t.Fatalf("shutdown after normal helper exit: %v", err)
	}
	if status := manager.Snapshot(); status.State != StateStopped || status.FailureCode != "" {
		t.Fatalf("shutdown overwrote the finalizer with a stale stopping state: %+v", status)
	}
	assertFEAT137RuleAbsentAndLeaseReleased(t, config.CodexHome)
}

func TestFEAT137ShutdownCancelsInitializeAndWaitsForNormalExit(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "initialize-received")
	t.Setenv("YIJIE_FAKE_RESULT_FILE", marker)
	manager, config := newFEAT137AuthorityRuntimeManager(t, "feat137_init_wait_for_close")
	startResult := make(chan error, 1)
	go func() {
		startResult <- manager.Start(context.Background())
	}()
	waitFor(t, 5*time.Second, func() bool {
		content, err := os.ReadFile(marker)
		return err == nil && string(content) == "initialize_received"
	})

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := manager.Shutdown(shutdownCtx); err != nil {
		t.Fatalf("normal shutdown during initialize: %v", err)
	}
	select {
	case err := <-startResult:
		if err == nil {
			t.Fatal("canceled initialize unexpectedly succeeded")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Start did not finish after normal initialize cancellation")
	}
	if status := manager.Snapshot(); status.State != StateStopped || status.Ready || status.FailureCode != "" {
		t.Fatalf("initialize cancellation did not converge to stopped: %+v", status)
	}
	assertFEAT137RuleAbsentAndLeaseReleased(t, config.CodexHome)
}

func TestFEAT137ShutdownCancelsVerificationBeforeCodexHomeMutation(t *testing.T) {
	manager, config := newFEAT137AuthorityRuntimeManager(t, "feat137_authority")
	entered := make(chan struct{})
	config.testBeforeArtifactVerification = func(ctx context.Context) error {
		close(entered)
		<-ctx.Done()
		return ctx.Err()
	}
	manager = NewManager(config, nil)
	if err := manager.SetCommandApprovalHandler(feat137LifecycleApprovalHandler{}); err != nil {
		t.Fatal(err)
	}
	startResult := make(chan error, 1)
	go func() {
		startResult <- manager.Start(context.Background())
	}()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("Start did not reach deterministic verification boundary")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := manager.Shutdown(shutdownCtx); err != nil {
		t.Fatalf("shutdown during verification: %v", err)
	}
	if err := <-startResult; !errors.Is(err, context.Canceled) {
		t.Fatalf("Start did not expose verification cancellation: %v", err)
	}
	if entries, err := os.ReadDir(config.CodexHome); err != nil || len(entries) != 0 {
		t.Fatalf("verification cancellation mutated CODEX_HOME: entries=%v err=%v", entries, err)
	}
	manager.mu.Lock()
	process := manager.cmd
	manager.mu.Unlock()
	if process != nil {
		t.Fatal("shutdown during verification launched a Runtime process")
	}
	if status := manager.Snapshot(); status.State != StateStopped || status.FailureCode != "" {
		t.Fatalf("verification cancellation did not converge to stopped: %+v", status)
	}
}

func TestFEAT137ShutdownBeforeStartPermanentlyRejectsStartup(t *testing.T) {
	manager, config := newFEAT137AuthorityRuntimeManager(t, "feat137_authority")
	verificationEntered := make(chan struct{}, 1)
	config.testBeforeArtifactVerification = func(context.Context) error {
		verificationEntered <- struct{}{}
		return nil
	}
	manager = NewManager(config, nil)
	if err := manager.SetCommandApprovalHandler(feat137LifecycleApprovalHandler{}); err != nil {
		t.Fatal(err)
	}
	if err := manager.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := manager.Shutdown(context.Background()); err != nil {
		t.Fatalf("repeated pre-start Shutdown was not idempotent: %v", err)
	}
	if err := manager.Start(context.Background()); err == nil || !strings.Contains(err.Error(), "shutdown was requested") {
		t.Fatalf("Start did not reject the pre-existing Shutdown latch: %v", err)
	}
	select {
	case <-verificationEntered:
		t.Fatal("Start reached artifact verification after pre-start Shutdown")
	default:
	}
	if entries, err := os.ReadDir(config.CodexHome); err != nil || len(entries) != 0 {
		t.Fatalf("rejected Start mutated CODEX_HOME: entries=%v err=%v", entries, err)
	}
	if status := manager.Snapshot(); status.State != StateStopped || status.FailureCode != "" {
		t.Fatalf("pre-start Shutdown did not remain authoritative: %+v", status)
	}
}

func TestFEAT137ConcurrentShutdownWinsBeforeHomeMutationAndProcessStart(t *testing.T) {
	tests := []struct {
		name          string
		install       func(*Config, func(context.Context) error)
		wantHomeEmpty bool
	}{
		{
			name: "after CODEX_HOME lease before mutation",
			install: func(config *Config, hook func(context.Context) error) {
				config.testAfterCodexHomeAuthorityAcquired = hook
			},
			wantHomeEmpty: true,
		},
		{
			name: "after provider mutation before process",
			install: func(config *Config, hook func(context.Context) error) {
				config.testBeforeProcessStart = hook
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			manager, config := newFEAT137AuthorityRuntimeManager(t, "feat137_authority")
			entered := make(chan struct{})
			test.install(&config, func(ctx context.Context) error {
				close(entered)
				<-ctx.Done()
				return ctx.Err()
			})
			manager = NewManager(config, nil)
			if err := manager.SetCommandApprovalHandler(feat137LifecycleApprovalHandler{}); err != nil {
				t.Fatal(err)
			}
			startResult := make(chan error, 1)
			go func() { startResult <- manager.Start(context.Background()) }()
			select {
			case <-entered:
			case <-time.After(5 * time.Second):
				t.Fatal("Start did not reach deterministic lifecycle boundary")
			}
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := manager.Shutdown(shutdownCtx); err != nil {
				t.Fatal(err)
			}
			if err := <-startResult; !errors.Is(err, context.Canceled) {
				t.Fatalf("losing Start did not return cancellation: %v", err)
			}
			manager.mu.Lock()
			process := manager.cmd
			manager.mu.Unlock()
			if process != nil {
				t.Fatal("Shutdown-losing Start launched a Runtime process")
			}
			if test.wantHomeEmpty {
				if entries, err := os.ReadDir(config.CodexHome); err != nil || len(entries) != 0 {
					t.Fatalf("Shutdown-losing Start mutated CODEX_HOME: entries=%v err=%v", entries, err)
				}
			}
			if status := manager.Snapshot(); status.State != StateStopped || status.FailureCode != "" {
				t.Fatalf("concurrent Shutdown did not win final state: %+v", status)
			}
			assertFEAT137RuleAbsentAndLeaseReleased(t, config.CodexHome)
		})
	}
}

func TestFEAT137AuthorityAcquisitionUsesClosedProviderFailureCode(t *testing.T) {
	manager, config := newFEAT137AuthorityRuntimeManager(t, "feat137_authority")
	lease, err := acquireManagedCodexHomeAuthority(config.CodexHome)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := lease.Close(); err != nil {
			t.Error(err)
		}
	}()
	if err := manager.Start(context.Background()); err == nil {
		t.Fatal("manager unexpectedly acquired an already-leased CODEX_HOME")
	}
	if status := manager.Snapshot(); status.State != StateFailed || status.FailureCode != "provider_config_failed" {
		t.Fatalf("authority detail escaped the closed Runtime status enum: %+v", status)
	}
}

func TestFEAT137ShutdownSurfacesExactRuleCleanupFailure(t *testing.T) {
	home := canonicalOwnedTempDir(t)
	rulesDirectory := filepath.Join(home, managedRulesDirectory)
	if err := os.Mkdir(rulesDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	rulePath := filepath.Join(rulesDirectory, managedFEAT137RulesFile)
	if err := os.WriteFile(rulePath, []byte(managedFEAT137ExecPolicy+"# benign ownership drift\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	authority, err := acquireManagedCodexHomeAuthority(home)
	if err != nil {
		t.Fatal(err)
	}
	manager := NewManager(DefaultConfig(), nil)
	manager.mu.Lock()
	manager.started = true
	manager.status.State = StateFailed
	manager.codexHomeAuthority = authority
	manager.mu.Unlock()
	if err := manager.Shutdown(context.Background()); err == nil {
		t.Fatal("Shutdown hid exact-owned rule cleanup failure")
	}
	if status := manager.Snapshot(); status.State != StateFailed || status.FailureCode != "provider_config_failed" {
		t.Fatalf("cleanup detail escaped the closed Runtime status enum: %+v", status)
	}
	content, err := os.ReadFile(rulePath)
	if err != nil || !strings.Contains(string(content), "benign ownership drift") {
		t.Fatalf("failed cleanup mutated drifted rule: content=%q err=%v", content, err)
	}
}

func TestFEAT137DirectorySyncFailureIsVisibleAfterNormalRuntimeExit(t *testing.T) {
	manager, config := newFEAT137AuthorityRuntimeManager(t, "feat137_authority")
	if err := manager.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	manager.mu.Lock()
	authority := manager.codexHomeAuthority
	manager.mu.Unlock()
	if authority == nil {
		t.Fatal("ready Runtime did not retain CODEX_HOME authority")
	}
	authority.mu.Lock()
	authority.testSyncDirectory = func(*os.File) error {
		return errors.New("deterministic directory sync failure")
	}
	authority.mu.Unlock()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := manager.Shutdown(shutdownCtx); err == nil || !strings.Contains(err.Error(), "directory sync failure") {
		t.Fatalf("normal Runtime exit hid directory sync failure: %v", err)
	}
	if status := manager.Snapshot(); status.State != StateFailed || status.FailureCode != "provider_config_failed" {
		t.Fatalf("directory sync failure escaped the closed failure mapping: %+v", status)
	}
	if _, err := os.Lstat(filepath.Join(config.CodexHome, managedRulesDirectory, managedFEAT137RulesFile)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("unlink did not complete before surfaced sync failure: %v", err)
	}
	reacquired, err := acquireManagedCodexHomeAuthority(config.CodexHome)
	if err != nil {
		t.Fatalf("normal Runtime exit did not release lease after sync failure: %v", err)
	}
	if err := reacquired.Close(); err != nil {
		t.Fatal(err)
	}
}

func assertFEAT137ExactManagedRule(t *testing.T, path string) {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil || string(content) != managedFEAT137ExecPolicy {
		t.Fatalf("FEAT-137 managed rule is not exact: content=%q err=%v", content, err)
	}
}

func assertFEAT137RuleAbsentAndLeaseReleased(t *testing.T, home string) {
	t.Helper()
	rulePath := filepath.Join(home, managedRulesDirectory, managedFEAT137RulesFile)
	if _, err := os.Lstat(rulePath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("FEAT-137 rule remained after Runtime exit: %v", err)
	}
	authority, err := acquireManagedCodexHomeAuthority(home)
	if err != nil {
		t.Fatalf("CODEX_HOME lease remained after Runtime exit: %v", err)
	}
	if err := authority.Close(); err != nil {
		t.Fatal(err)
	}
}
