package codex

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestRuntimeHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_CODEX_HELPER") != "1" {
		return
	}
	args := helperArgs(os.Args)
	if len(args) == 1 && args[0] == "--version" {
		if os.Getenv("YIJIE_FAKE_MODE") == "bad_version" {
			fmt.Println("codex-cli 9.9.9")
		} else {
			fmt.Println(ExpectedReportedVersion)
		}
		os.Exit(0)
	}

	expectedArgs := []string{
		"app-server",
		"--listen", "stdio://",
		"--strict-config",
	}
	if !reflect.DeepEqual(args, expectedArgs) {
		fmt.Fprintln(os.Stderr, "unexpected app-server arguments")
		os.Exit(20)
	}
	for _, key := range []string{
		"OPENAI_API_KEY",
		"CODEX_API_KEY",
		"CODEX_ACCESS_TOKEN",
		"CHATGPT_ACCESS_TOKEN",
	} {
		if os.Getenv(key) != "" {
			fmt.Fprintln(os.Stderr, "credential leaked into Runtime environment")
			os.Exit(21)
		}
	}

	mode := os.Getenv("YIJIE_FAKE_MODE")
	if mode == "baseline2" || mode == "title" {
		expectedKey := "test-minimax-key"
		if mode == "title" {
			expectedKey = "synthetic-test-key"
		}
		if os.Getenv(MiniMaxRuntimeEnvKey) != expectedKey {
			fmt.Fprintln(os.Stderr, "scoped MiniMax credential missing")
			os.Exit(27)
		}
	} else if os.Getenv(MiniMaxRuntimeEnvKey) != "" {
		fmt.Fprintln(os.Stderr, "MiniMax credential leaked into credential-free Runtime")
		os.Exit(28)
	}
	scanner := bufio.NewScanner(os.Stdin)
	encoder := json.NewEncoder(os.Stdout)
	for scanner.Scan() {
		var message struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
			Params map[string]any  `json:"params"`
			Error  map[string]any  `json:"error"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &message); err != nil {
			os.Exit(22)
		}
		switch message.Method {
		case "initialize":
			capabilities, ok := message.Params["capabilities"].(map[string]any)
			if !ok || capabilities["experimentalApi"] != false {
				os.Exit(23)
			}
			if mode == "timeout" {
				if resultFile := os.Getenv("YIJIE_FAKE_RESULT_FILE"); resultFile != "" {
					_ = os.WriteFile(resultFile, []byte("initialize_received"), 0o600)
				}
				for {
					time.Sleep(time.Hour)
				}
			}
			if mode == "bad_json" {
				fmt.Println("{not-json")
				continue
			}
			if mode == "oversize" {
				fmt.Println(strings.Repeat("x", 4096))
				continue
			}
			codexHome := os.Getenv("CODEX_HOME")
			if mode == "bad_home" {
				codexHome = filepath.Dir(codexHome)
			}
			_ = encoder.Encode(map[string]any{
				"id": message.ID,
				"result": map[string]any{
					"userAgent":      ExpectedReportedVersion,
					"codexHome":      codexHome,
					"platformFamily": "unix",
					"platformOs":     "macos",
				},
			})
		case "initialized":
			if mode == "crash" {
				os.Exit(24)
			}
			if mode == "reverse_request" {
				_ = encoder.Encode(map[string]any{
					"id":     "server-request-1",
					"method": "item/tool/requestApproval",
					"params": map[string]any{},
				})
			}
		case "thread/start":
			if mode != "baseline2" && mode != "title" && mode != "feat126_fake" {
				os.Exit(29)
			}
			if message.Params["model"] != MiniMaxModel || message.Params["modelProvider"] != MiniMaxProviderID ||
				message.Params["approvalPolicy"] != "never" || message.Params["sandbox"] != "read-only" {
				os.Exit(30)
			}
			if mode == "title" {
				if message.Params["ephemeral"] != true {
					os.Exit(31)
				}
				cwd, hasCwd := message.Params["cwd"].(string)
				info, statErr := os.Stat(cwd)
				if !hasCwd || statErr != nil || !info.IsDir() || info.Mode().Perm() != 0o700 {
					os.Exit(32)
				}
			}
			thread := map[string]any{
				"id":        "019c0123-4567-7abc-8123-456789abcdec",
				"sessionId": "019c0123-4567-7abc-8123-456789abcdea",
				"turns":     []any{},
			}
			if mode == "title" {
				thread["ephemeral"] = true
				thread["path"] = nil
			}
			_ = encoder.Encode(map[string]any{
				"id": message.ID,
				"result": map[string]any{
					"thread": thread, "model": MiniMaxModel, "modelProvider": MiniMaxProviderID,
				},
			})
			_ = encoder.Encode(map[string]any{
				"method": "thread/started", "params": map[string]any{"thread": thread},
			})
		case "thread/resume":
			thread := map[string]any{
				"id":        "019c0123-4567-7abc-8123-456789abcdec",
				"sessionId": "019c0123-4567-7abc-8123-456789abcdea",
				"turns": []any{map[string]any{
					"id": "019c0123-4567-7abc-8123-456789abcded", "status": "completed",
				}},
			}
			_ = encoder.Encode(map[string]any{
				"id": message.ID,
				"result": map[string]any{
					"thread": thread, "model": MiniMaxModel, "modelProvider": MiniMaxProviderID,
				},
			})
		case "turn/start":
			if mode == "title" {
				if message.Params["effort"] != "none" || message.Params["outputSchema"] == nil {
					os.Exit(33)
				}
				turn := map[string]any{"id": "019c0123-4567-7abc-8123-456789abcded", "status": "inProgress"}
				_ = encoder.Encode(map[string]any{"id": message.ID, "result": map[string]any{"turn": turn}})
				titleJSON := `{"title":"设计本地聊天安全删除流程"}`
				_ = encoder.Encode(map[string]any{"method": "item/agentMessage/delta", "params": map[string]any{
					"threadId": "019c0123-4567-7abc-8123-456789abcdec", "turnId": turn["id"], "itemId": "title-item", "delta": titleJSON,
				}})
				_ = encoder.Encode(map[string]any{"method": "item/completed", "params": map[string]any{
					"threadId": "019c0123-4567-7abc-8123-456789abcdec", "turnId": turn["id"],
					"item": map[string]any{"id": "title-item", "type": "agentMessage", "text": titleJSON},
				}})
				turn["status"] = "completed"
				_ = encoder.Encode(map[string]any{"method": "turn/completed", "params": map[string]any{
					"threadId": "019c0123-4567-7abc-8123-456789abcdec", "turn": turn,
				}})
				continue
			}
			turn := map[string]any{
				"id": "019c0123-4567-7abc-8123-456789abcded", "status": "inProgress",
			}
			_ = encoder.Encode(map[string]any{"id": message.ID, "result": map[string]any{"turn": turn}})
			_ = encoder.Encode(map[string]any{
				"method": "turn/started", "params": map[string]any{
					"threadId": "019c0123-4567-7abc-8123-456789abcdec", "turn": turn,
				},
			})
			_ = encoder.Encode(map[string]any{
				"method": "item/started", "params": map[string]any{
					"threadId": "019c0123-4567-7abc-8123-456789abcdec",
					"turnId":   "019c0123-4567-7abc-8123-456789abcded",
					"item":     map[string]any{"id": "item-1", "type": "agentMessage", "text": ""},
				},
			})
			_ = encoder.Encode(map[string]any{
				"method": "item/agentMessage/delta", "params": map[string]any{
					"threadId": "019c0123-4567-7abc-8123-456789abcdec",
					"turnId":   "019c0123-4567-7abc-8123-456789abcded",
					"itemId":   "item-1", "delta": "OK",
				},
			})
			_ = encoder.Encode(map[string]any{
				"method": "item/completed", "params": map[string]any{
					"threadId": "019c0123-4567-7abc-8123-456789abcdec",
					"turnId":   "019c0123-4567-7abc-8123-456789abcded",
					"item":     map[string]any{"id": "item-1", "type": "agentMessage", "text": "OK"},
				},
			})
			turn["status"] = "completed"
			_ = encoder.Encode(map[string]any{
				"method": "turn/completed", "params": map[string]any{
					"threadId": "019c0123-4567-7abc-8123-456789abcdec", "turn": turn,
				},
			})
		case "turn/interrupt":
			_ = encoder.Encode(map[string]any{"id": message.ID, "result": map[string]any{}})
		case "thread/delete":
			_ = encoder.Encode(map[string]any{"id": message.ID, "result": map[string]any{}})
			_ = encoder.Encode(map[string]any{
				"method": RuntimeNotificationThreadDeleted,
				"params": map[string]any{"threadId": "019c0123-4567-7abc-8123-456789abcdec"},
			})
		case "thread/unsubscribe":
			_ = encoder.Encode(map[string]any{"id": message.ID, "result": map[string]any{"status": "unsubscribed"}})
		default:
			if string(message.ID) == `"server-request-1"` {
				if int(message.Error["code"].(float64)) != methodNotFoundCode {
					os.Exit(25)
				}
				if resultFile := os.Getenv("YIJIE_FAKE_RESULT_FILE"); resultFile != "" {
					_ = os.WriteFile(resultFile, []byte("rejected"), 0o600)
				}
			}
		}
	}
	if err := scanner.Err(); err != nil {
		os.Exit(26)
	}
	os.Exit(0)
}

func TestVerifyArtifactAcceptsPinnedBaseline(t *testing.T) {
	config := newRuntimeFixture(t)
	info, err := verifyArtifactWithPolicy(
		context.Background(),
		config.BinaryPath,
		config.ManifestPath,
		5*time.Second,
		*config.testArtifactPolicy,
	)
	if err != nil {
		t.Fatalf("verify artifact: %v", err)
	}
	if info.RuntimeVersion != ExpectedRuntimeVersion || info.UpstreamCommit != ExpectedUpstreamCommit {
		t.Fatalf("unexpected artifact info: %+v", info)
	}
}

func TestVerifyArtifactRejectsVersionMismatch(t *testing.T) {
	t.Setenv("YIJIE_FAKE_MODE", "bad_version")
	config := newRuntimeFixture(t)
	if _, err := verifyArtifactWithPolicy(
		context.Background(),
		config.BinaryPath,
		config.ManifestPath,
		5*time.Second,
		*config.testArtifactPolicy,
	); err == nil {
		t.Fatal("expected incompatible runtime version to fail")
	}
}

func TestVerifyArtifactRejectsTamperedBinary(t *testing.T) {
	config := newRuntimeFixture(t)
	file, err := os.OpenFile(config.BinaryPath, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString("\n# tampered\n"); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := verifyArtifactWithPolicy(
		context.Background(),
		config.BinaryPath,
		config.ManifestPath,
		5*time.Second,
		*config.testArtifactPolicy,
	); err == nil {
		t.Fatal("expected tampered runtime binary to fail")
	}
}

func TestPinnedPolicyRejectsSelfConsistentAlternativeArtifact(t *testing.T) {
	config := newRuntimeFixture(t)
	manifest, err := readManifest(config.ManifestPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := validateManifest(manifest, runtimeBaseline0Policy); err == nil {
		t.Fatal("expected production policy to reject an alternative binary and manifest pair")
	}
}

func TestVerifyArtifactRejectsUnexpectedRuntimePatchAuthority(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Manifest)
	}{
		{
			name: "path",
			mutate: func(manifest *Manifest) {
				manifest.Patches[0].Path = ".yijie/patches/unreviewed.patch"
			},
		},
		{
			name: "digest",
			mutate: func(manifest *Manifest) {
				manifest.Patches[0].SHA256 = strings.Repeat("0", 64)
			},
		},
		{
			name: "count",
			mutate: func(manifest *Manifest) {
				manifest.Patches = nil
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			config := newRuntimeFixture(t)
			manifest, err := readManifest(config.ManifestPath)
			if err != nil {
				t.Fatal(err)
			}
			test.mutate(&manifest)
			manifestBytes, err := json.Marshal(manifest)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(config.ManifestPath, manifestBytes, 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := verifyArtifactWithPolicy(
				context.Background(),
				config.BinaryPath,
				config.ManifestPath,
				5*time.Second,
				*config.testArtifactPolicy,
			); err == nil {
				t.Fatal("expected unexpected runtime patch authority to fail")
			}
		})
	}
}

func TestManagerHandshakeAndGracefulShutdown(t *testing.T) {
	setCredentialFixtures(t)
	config := newRuntimeFixture(t)
	manager := NewManager(config, nil)
	if err := manager.Start(context.Background()); err != nil {
		t.Fatalf("start runtime: %v", err)
	}
	status := manager.Snapshot()
	if !status.Ready || status.State != StateReady {
		t.Fatalf("expected ready runtime, got %+v", status)
	}
	if status.RuntimeVersion != ExpectedRuntimeVersion || status.UpstreamCommit != ExpectedUpstreamCommit {
		t.Fatalf("runtime identity missing from status: %+v", status)
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := manager.Shutdown(shutdownCtx); err != nil {
		t.Fatalf("shutdown runtime: %v", err)
	}
	if status := manager.Snapshot(); status.State != StateStopped || status.Ready {
		t.Fatalf("expected stopped runtime, got %+v", status)
	}
}

func TestManagerRejectsUnknownReverseRequests(t *testing.T) {
	resultFile := filepath.Join(t.TempDir(), "reverse-request-result")
	t.Setenv("YIJIE_FAKE_MODE", "reverse_request")
	t.Setenv("YIJIE_FAKE_RESULT_FILE", resultFile)
	manager := NewManager(newRuntimeFixture(t), nil)
	if err := manager.Start(context.Background()); err != nil {
		t.Fatalf("start runtime: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = manager.Shutdown(ctx)
	})

	waitFor(t, 5*time.Second, func() bool {
		content, err := os.ReadFile(resultFile)
		return err == nil && string(content) == "rejected"
	})
}

func TestManagerInitializationFailures(t *testing.T) {
	tests := []struct {
		name            string
		mode            string
		maxMessageBytes int
		wantFailure     string
	}{
		{name: "request timeout", mode: "timeout", wantFailure: "initialize_failed"},
		{name: "malformed JSON", mode: "bad_json", wantFailure: "protocol_failure"},
		{name: "oversized message", mode: "oversize", maxMessageBytes: 1024, wantFailure: "protocol_failure"},
		{name: "wrong CODEX_HOME", mode: "bad_home", wantFailure: "initialize_response_invalid"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("YIJIE_FAKE_MODE", test.mode)
			config := newRuntimeFixture(t)
			config.StartupTimeout = 10 * time.Second
			config.RequestTimeout = 500 * time.Millisecond
			if test.maxMessageBytes != 0 {
				config.MaxMessageBytes = test.maxMessageBytes
			}
			manager := NewManager(config, nil)
			if err := manager.Start(context.Background()); err == nil {
				t.Fatal("expected initialization to fail")
			}
			status := manager.Snapshot()
			if status.Ready || status.State != StateFailed || status.FailureCode != test.wantFailure {
				t.Fatalf("unexpected failed status: %+v", status)
			}
		})
	}
}

func TestRuntimeEvidenceRequiresCanonicalRFC4122UUIDv4RunAndNonce(t *testing.T) {
	const (
		validRunID = "123e4567-e89b-42d3-a456-426614174000"
		validNonce = "123e4567-e89b-42d3-a456-426614174001"
		profile    = "feat-126-s10-local-lab"
	)
	manager := &Manager{}
	if _, err := manager.RuntimeEvidence(validRunID, validNonce, profile); err == nil || !strings.Contains(err.Error(), "process evidence is unavailable") {
		t.Fatalf("valid authority did not reach process evidence boundary: %v", err)
	}
	invalid := []struct {
		name  string
		value string
	}{
		{name: "malformed", value: "not-a-uuid"},
		{name: "uppercase", value: "123E4567-E89B-42D3-A456-426614174003"},
		{name: "uuid-v1", value: "6ba7b810-9dad-11d1-80b4-00c04fd430c8"},
		{name: "uuid-v7", value: "019fbd88-cbc3-7bf1-934d-7b05cd693f80"},
		{name: "non-rfc4122-variant", value: "123e4567-e89b-42d3-4456-426614174003"},
		{name: "surrounding-whitespace", value: " 123e4567-e89b-42d3-a456-426614174003 "},
	}
	for _, identity := range []struct {
		name string
		call func(string) error
	}{
		{name: "run-id", call: func(value string) error {
			_, err := manager.RuntimeEvidence(value, validNonce, profile)
			return err
		}},
		{name: "nonce", call: func(value string) error {
			_, err := manager.RuntimeEvidence(validRunID, value, profile)
			return err
		}},
	} {
		for _, test := range invalid {
			t.Run(identity.name+"/"+test.name, func(t *testing.T) {
				err := identity.call(test.value)
				if err == nil || !strings.Contains(err.Error(), "authority is invalid") {
					t.Fatalf("accepted invalid %s %q: %v", identity.name, test.value, err)
				}
			})
		}
	}
}

func TestManagerPropagatesStartupCancellation(t *testing.T) {
	resultFile := filepath.Join(t.TempDir(), "initialize-marker")
	t.Setenv("YIJIE_FAKE_MODE", "timeout")
	t.Setenv("YIJIE_FAKE_RESULT_FILE", resultFile)
	config := newRuntimeFixture(t)
	config.StartupTimeout = 5 * time.Second
	config.RequestTimeout = 5 * time.Second
	manager := NewManager(config, nil)
	ctx, cancel := context.WithCancel(context.Background())
	startResult := make(chan error, 1)
	go func() {
		startResult <- manager.Start(ctx)
	}()
	waitFor(t, 5*time.Second, func() bool {
		content, err := os.ReadFile(resultFile)
		return err == nil && string(content) == "initialize_received"
	})
	cancel()
	select {
	case err := <-startResult:
		if err == nil {
			t.Fatal("expected canceled startup to fail")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("canceled startup did not return")
	}
	if status := manager.Snapshot(); status.Ready || status.State != StateFailed {
		t.Fatalf("expected failed runtime after cancellation, got %+v", status)
	}
}

func TestManagerDetectsRuntimeExit(t *testing.T) {
	t.Setenv("YIJIE_FAKE_MODE", "crash")
	manager := NewManager(newRuntimeFixture(t), nil)
	_ = manager.Start(context.Background())
	waitFor(t, 5*time.Second, func() bool {
		status := manager.Snapshot()
		return status.State == StateFailed && !status.Ready
	})
}

func TestRuntimeEnvironmentRemovesBaselineCredentials(t *testing.T) {
	environment := runtimeEnvironment([]string{
		"PATH=/bin",
		"OPENAI_API_KEY=secret",
		"CODEX_API_KEY=secret",
		"CODEX_ACCESS_TOKEN=secret",
		"CHATGPT_ACCESS_TOKEN=secret",
		"MINIMAX_API_KEY=secret",
		"MINIMAX_API_KEY_FILE=/ambient-secret",
		"YIJIE_MINIMAX_API_KEY=secret",
		"YIJIE_MINIMAX_API_KEY_FILE=/secret",
		"CODEX_HOME=/old",
	}, "/new", "")
	joined := strings.Join(environment, "\n")
	for _, secret := range []string{
		"OPENAI_API_KEY", "CODEX_API_KEY", "CODEX_ACCESS_TOKEN", "CHATGPT_ACCESS_TOKEN",
		"MINIMAX_API_KEY", "MINIMAX_API_KEY_FILE", "YIJIE_MINIMAX_API_KEY", "YIJIE_MINIMAX_API_KEY_FILE",
	} {
		if strings.Contains(joined, secret) {
			t.Fatalf("credential %s remained in Runtime environment", secret)
		}
	}
	if !strings.Contains(joined, "CODEX_HOME=/new") {
		t.Fatalf("new CODEX_HOME missing from Runtime environment: %v", environment)
	}
}

func TestManagerBaseline2ThreadTurnMethods(t *testing.T) {
	t.Setenv("YIJIE_FAKE_MODE", "baseline2")
	config := newRuntimeFixture(t)
	config.MiniMax = MiniMaxConfig{Enabled: true, APIKey: "test-minimax-key"}
	manager := NewManager(config, nil)
	notifications := make(chan string, 16)
	if err := manager.SetNotificationHandler(func(method string, _ json.RawMessage) {
		notifications <- method
	}); err != nil {
		t.Fatal(err)
	}
	if err := manager.Start(context.Background()); err != nil {
		t.Fatalf("start Runtime: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = manager.Shutdown(ctx)
	})
	thread, err := manager.StartThread(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if thread.ID != "019c0123-4567-7abc-8123-456789abcdec" || thread.Model != MiniMaxModel {
		t.Fatalf("unexpected thread response: %+v", thread)
	}
	turn, err := manager.StartTurn(context.Background(), thread.ID, "reply OK", "none")
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.InterruptTurn(context.Background(), thread.ID, turn.ID); err != nil {
		t.Fatal(err)
	}
	resumed, err := manager.ResumeThread(context.Background(), thread.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(resumed.Turns) != 1 || resumed.Turns[0].Status != "completed" {
		t.Fatalf("unexpected resumed thread: %+v", resumed)
	}
	want := []string{
		"thread/started", "turn/started", "item/started", "item/agentMessage/delta", "item/completed", "turn/completed",
	}
	for _, method := range want {
		select {
		case received := <-notifications:
			if received != method {
				t.Fatalf("expected notification %s, got %s", method, received)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("timed out waiting for %s", method)
		}
	}
	if err := manager.DeleteThread(context.Background(), thread.ID); err != nil {
		t.Fatalf("delete thread with confirmation: %v", err)
	}
	select {
	case method := <-notifications:
		if method != RuntimeNotificationThreadDeleted {
			t.Fatalf("expected delete notification, got %s", method)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for delete notification forwarding")
	}
}

func TestManagerFEAT126FakeProviderKeepsWireIdentityWithoutCredential(t *testing.T) {
	t.Setenv("YIJIE_FAKE_MODE", "feat126_fake")
	config := newRuntimeFixture(t)
	canonicalHome, err := filepath.EvalSymlinks(config.CodexHome)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(canonicalHome, 0o700); err != nil {
		t.Fatal(err)
	}
	config.CodexHome = canonicalHome
	config.FakeResponses = FakeResponsesConfig{
		Enabled: true, BaseURL: FEAT126FakeBaseURL,
		RunID: "123e4567-e89b-42d3-a456-426614174000", FixtureID: FEAT126FakeFixtureID,
	}
	manager := NewManager(config, nil)
	if err := manager.Start(context.Background()); err != nil {
		t.Fatalf("start Runtime with FEAT-126 fake provider: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = manager.Shutdown(ctx)
	})
	thread, err := manager.StartThread(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if thread.Model != MiniMaxModel || thread.ModelProvider != MiniMaxProviderID {
		t.Fatalf("fake transport changed on-wire identity: %#v", thread)
	}
	if _, err := manager.StartTurn(context.Background(), thread.ID, "synthetic input", "high"); err != nil {
		t.Fatal(err)
	}
}

func TestManagerGeneratesTitleWithPathlessEphemeralStrictFixture(t *testing.T) {
	t.Setenv("YIJIE_FAKE_MODE", "title")
	config := newRuntimeFixture(t)
	config.MiniMax = MiniMaxConfig{Enabled: true, APIKey: "synthetic-test-key"}
	manager := NewManager(config, nil)
	notifications := make(chan string, 1)
	if err := manager.SetNotificationHandler(func(method string, _ json.RawMessage) {
		notifications <- method
	}); err != nil {
		t.Fatal(err)
	}
	if err := manager.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = manager.Shutdown(ctx)
	})
	title, err := manager.GenerateTitle(context.Background(), "为新的本地聊天任务设计安全的删除流程")
	if err != nil {
		t.Fatal(err)
	}
	if title != "设计本地聊天安全删除流程" {
		t.Fatalf("unexpected generated title %q", title)
	}
	select {
	case method := <-notifications:
		t.Fatalf("title notification leaked into the main session handler: %s", method)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestTailBufferIsBounded(t *testing.T) {
	buffer := newTailBuffer(5)
	_, _ = buffer.Write([]byte("1234"))
	_, _ = buffer.Write([]byte("5678"))
	if got := buffer.String(); got != "45678" {
		t.Fatalf("expected bounded tail, got %q", got)
	}
}

func newRuntimeFixture(t *testing.T) Config {
	t.Helper()
	tempDir := t.TempDir()
	binaryPath := filepath.Join(tempDir, "codex")
	testBinary, err := os.Executable()
	if err != nil {
		t.Fatalf("resolve test executable: %v", err)
	}
	wrapper := fmt.Sprintf(
		"#!/bin/sh\nGO_WANT_CODEX_HELPER=1 exec %s -test.run='^TestRuntimeHelperProcess$' -- \"$@\"\n",
		shellQuote(testBinary),
	)
	if err := os.WriteFile(binaryPath, []byte(wrapper), 0o700); err != nil {
		t.Fatalf("write fake runtime: %v", err)
	}
	digest, err := fileSHA256(binaryPath)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(binaryPath)
	if err != nil {
		t.Fatal(err)
	}

	manifest := Manifest{
		SchemaVersion: ExpectedSchemaVersion,
		Baseline:      BaselineName,
		RustToolchain: ExpectedRustToolchain,
		Upstream: ManifestUpstream{
			URL:    ExpectedUpstreamURL,
			Tag:    ExpectedUpstreamTag,
			Commit: ExpectedUpstreamCommit,
		},
		Runtime: ManifestRuntime{
			Binary:          "codex",
			ReportedVersion: ExpectedReportedVersion,
			SHA256:          digest,
			SizeBytes:       info.Size(),
			Target:          ExpectedTarget,
			Version:         ExpectedRuntimeVersion,
		},
		AppServer: ManifestAppServer{
			ExperimentalAPI:  false,
			SchemaFileCount:  267,
			SchemaTreeSHA256: ExpectedSchemaTreeSHA256,
			Transport:        ExpectedTransport,
		},
		Patches: []ManifestPatch{{
			Path:   ExpectedRuntimePatchPath,
			SHA256: ExpectedRuntimePatchSHA256,
		}},
		BuildLock: ManifestBuildLock{
			FromVersion:            "0.0.0",
			NormalizedPackageCount: 132,
			Policy:                 "local-workspace-version-normalization-only",
			ResolvedLockSHA256:     ExpectedResolvedLockSHA256,
			SchemaVersion:          1,
			ToVersion:              ExpectedRuntimeVersion,
			UpstreamLockSHA256:     ExpectedUpstreamLockSHA256,
		},
	}
	manifestBytes, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	manifestPath := filepath.Join(tempDir, "runtime-manifest.json")
	if err := os.WriteFile(manifestPath, manifestBytes, 0o600); err != nil {
		t.Fatalf("write fake runtime manifest: %v", err)
	}
	codexHome := filepath.Join(tempDir, "codex-home")
	if err := os.Mkdir(codexHome, 0o700); err != nil {
		t.Fatalf("create fake CODEX_HOME: %v", err)
	}

	config := DefaultConfig()
	config.BinaryPath = binaryPath
	config.ManifestPath = manifestPath
	config.CodexHome = codexHome
	config.StartupTimeout = 10 * time.Second
	config.RequestTimeout = 5 * time.Second
	config.ShutdownTimeout = 5 * time.Second
	config.testArtifactPolicy = &artifactPolicy{
		runtimeSHA256: digest,
		runtimeSize:   info.Size(),
	}
	return config
}

func helperArgs(args []string) []string {
	for index, arg := range args {
		if arg == "--" {
			return args[index+1:]
		}
	}
	return nil
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'"'"'`) + "'"
}

func setCredentialFixtures(t *testing.T) {
	t.Helper()
	for _, key := range []string{
		"OPENAI_API_KEY",
		"CODEX_API_KEY",
		"CODEX_ACCESS_TOKEN",
		"CHATGPT_ACCESS_TOKEN",
	} {
		t.Setenv(key, "must-not-reach-runtime")
	}
}

func waitFor(t *testing.T, timeout time.Duration, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("condition was not met before timeout")
}
