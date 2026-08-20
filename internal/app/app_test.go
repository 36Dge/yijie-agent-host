package app

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/36Dge/yijie-agent-host/internal/artifact"
	"github.com/36Dge/yijie-agent-host/internal/codex"
	agenthostcontract "github.com/36Dge/yijie-agent-host/internal/contracts"
	"github.com/36Dge/yijie-agent-host/internal/session"
	"github.com/getkin/kin-openapi/openapi3"
)

type staticRuntimeStatus struct {
	status codex.Status
}

func (s staticRuntimeStatus) Snapshot() codex.Status {
	return s.status
}

func TestHealthzReportsProcessHealth(t *testing.T) {
	const instanceNonce = "019fbd88-cbc3-7bf1-934d-7b05cd693f80"
	request := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	response := httptest.NewRecorder()
	runtime := staticRuntimeStatus{status: codex.Status{State: codex.StateFailed}}

	NewHandler(Config{Environment: "test", Port: "0", InstanceNonce: instanceNonce}, runtime, nil, "").ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", response.Code)
	}
	if response.Header().Get("X-Yijie-Host-Instance-Nonce") != instanceNonce {
		t.Fatalf("health response did not bind the spawned instance: %v", response.Header())
	}
	var payload agenthostcontract.HealthResponse
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode generated health contract: %v", err)
	}
	if payload.Service != agenthostcontract.HealthResponseServiceYijieAgentHost ||
		payload.Status != agenthostcontract.HealthResponseStatusOk {
		t.Fatalf("unexpected health contract response: %+v", payload)
	}
	assertOpenAPIJSON(t, "HealthResponse", response.Body.Bytes())
}

func TestReadyzReflectsRuntimeReadiness(t *testing.T) {
	const instanceNonce = "019fbd88-cbc3-7bf1-934d-7b05cd693f80"
	tests := []struct {
		name       string
		status     codex.Status
		wantCode   int
		wantStatus string
	}{
		{
			name:       "runtime unavailable",
			status:     codex.Status{State: codex.StateFailed},
			wantCode:   http.StatusServiceUnavailable,
			wantStatus: "not_ready",
		},
		{
			name:       "runtime initialized",
			status:     codex.Status{State: codex.StateReady, Ready: true},
			wantCode:   http.StatusOK,
			wantStatus: "ready",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "/readyz", nil)
			response := httptest.NewRecorder()
			NewHandler(
				Config{Environment: "test", Port: "0", InstanceNonce: instanceNonce},
				staticRuntimeStatus{status: test.status},
				nil,
				"",
			).ServeHTTP(response, request)

			if response.Code != test.wantCode {
				t.Fatalf("expected status %d, got %d", test.wantCode, response.Code)
			}
			if response.Header().Get("X-Yijie-Host-Instance-Nonce") != instanceNonce {
				t.Fatalf("ready response did not bind the spawned instance: %v", response.Header())
			}
			var payload struct {
				Status string `json:"status"`
			}
			if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
				t.Fatalf("decode response: %v", err)
			}
			if payload.Status != test.wantStatus {
				t.Fatalf("expected readiness %q, got %q", test.wantStatus, payload.Status)
			}
			schemaName := "NotReadyResponse"
			if test.wantStatus == "ready" {
				schemaName = "ReadyResponse"
			}
			assertOpenAPIJSON(t, schemaName, response.Body.Bytes())
		})
	}
}

func TestStatusIsSanitized(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/v1/status", nil)
	response := httptest.NewRecorder()
	runtime := staticRuntimeStatus{status: codex.Status{
		State:           codex.StateReady,
		Ready:           true,
		RuntimeVersion:  codex.ExpectedRuntimeVersion,
		UpstreamCommit:  codex.ExpectedUpstreamCommit,
		Transport:       codex.ExpectedTransport,
		ExperimentalAPI: false,
	}}

	NewHandler(Config{Environment: "test", Port: "0"}, runtime, nil, "").ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", response.Code)
	}
	body := response.Body.String()
	if strings.Contains(body, "binary") || strings.Contains(body, "manifest") || strings.Contains(body, "codex_home") {
		t.Fatalf("status response leaked a local runtime path field: %s", body)
	}
	if !strings.Contains(body, codex.ExpectedUpstreamCommit) {
		t.Fatalf("status response omitted pinned upstream identity: %s", body)
	}
	var payload agenthostcontract.AgentHostStatus
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode generated status contract: %v", err)
	}
	if payload.Status != agenthostcontract.AgentHostStatusStatusOk ||
		payload.Runtime.UpstreamCommit == nil || string(*payload.Runtime.UpstreamCommit) != codex.ExpectedUpstreamCommit {
		t.Fatalf("unexpected generated status response: %+v", payload)
	}
	assertOpenAPIJSON(t, "AgentHostStatus", response.Body.Bytes())
	if response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("expected no-store status response")
	}
}

func TestRuntimeCompatibilityProjectionMatchesHostAdapter(t *testing.T) {
	encoded, err := os.ReadFile(contractFilePath(t, "compatibility", "agent-host-runtime-v1.json"))
	if err != nil {
		t.Fatalf("read Runtime compatibility contract: %v", err)
	}
	var manifest struct {
		HostProjection struct {
			Transport            string   `json:"transport"`
			Authentication       string   `json:"authentication"`
			Sandbox              string   `json:"sandbox"`
			ApprovalPolicy       string   `json:"approval_policy"`
			RuntimeMethods       []string `json:"runtime_methods"`
			RuntimeNotifications []string `json:"runtime_notifications"`
		} `json:"host_projection"`
	}
	if err := json.Unmarshal(encoded, &manifest); err != nil {
		t.Fatalf("decode Runtime compatibility contract: %v", err)
	}
	projection := manifest.HostProjection
	if projection.Transport != "local-http-sse" || projection.Authentication != "owner-only-bearer" ||
		projection.Sandbox != codex.SessionSandbox ||
		projection.ApprovalPolicy != codex.SessionApprovalPolicy {
		t.Fatalf("Host policy projection drifted from the locked contract: %+v", projection)
	}
	if !reflect.DeepEqual(projection.RuntimeMethods, codex.SupportedSessionRuntimeMethods()) {
		t.Fatalf("Runtime method projection drifted: %v", projection.RuntimeMethods)
	}
	if !reflect.DeepEqual(projection.RuntimeNotifications, session.SupportedRuntimeNotifications()) {
		t.Fatalf("Runtime notification projection drifted: %v", projection.RuntimeNotifications)
	}
}

func TestEndpointsRejectNonGETMethods(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "/healthz", nil)
	response := httptest.NewRecorder()
	NewHandler(
		Config{Environment: "test", Port: "0"},
		staticRuntimeStatus{status: codex.Status{}},
		nil,
		"",
	).ServeHTTP(response, request)

	if response.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected status 405, got %d", response.Code)
	}
}

func TestSessionErrorsHaveStableHTTPMapping(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantStatus int
		wantCode   string
	}{
		{name: "not found", err: session.ErrNotFound, wantStatus: http.StatusNotFound, wantCode: "session_not_found"},
		{name: "task exists", err: session.ErrTaskExists, wantStatus: http.StatusConflict, wantCode: "task_session_exists"},
		{name: "turn active", err: session.ErrTurnActive, wantStatus: http.StatusConflict, wantCode: "turn_active"},
		{name: "turn inactive", err: session.ErrTurnNotActive, wantStatus: http.StatusConflict, wantCode: "turn_not_active"},
		{name: "session unusable", err: session.ErrSessionNotUsable, wantStatus: http.StatusConflict, wantCode: "session_not_usable"},
		{name: "stream changed", err: session.ErrStreamChanged, wantStatus: http.StatusConflict, wantCode: "event_stream_changed"},
		{name: "replay unavailable", err: session.ErrReplayUnavailable, wantStatus: http.StatusConflict, wantCode: "event_replay_unavailable"},
		{name: "invalid sequence", err: session.ErrInvalidSequence, wantStatus: http.StatusBadRequest, wantCode: "invalid_event_cursor"},
		{name: "invalid argument", err: session.ErrInvalidArgument, wantStatus: http.StatusBadRequest, wantCode: "invalid_request"},
		{
			name: "wrapped runtime error", err: fmt.Errorf("request: %w", session.ErrRuntimeRequest),
			wantStatus: http.StatusBadGateway, wantCode: "runtime_request_failed",
		},
		{name: "internal error", err: errors.New("unexpected"), wantStatus: http.StatusInternalServerError, wantCode: "internal_error"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			writeSessionError(response, test.err)
			if response.Code != test.wantStatus {
				t.Fatalf("expected status %d, got %d", test.wantStatus, response.Code)
			}
			var payload agenthostcontract.ErrorResponse
			if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
				t.Fatal(err)
			}
			if string(payload.Error.Code) != test.wantCode || payload.Error.Message == "" || !payload.Error.Code.Valid() {
				t.Fatalf("unexpected stable error response: %s", response.Body.String())
			}
			if response.Header().Get("Content-Type") != "application/json" ||
				response.Header().Get("Cache-Control") != "no-store" {
				t.Fatalf("unexpected error response headers: %v", response.Header())
			}
			assertOpenAPIJSON(t, "ErrorResponse", response.Body.Bytes())
		})
	}
}

func TestEventCursorParsing(t *testing.T) {
	const streamID = "019c0123-4567-7abc-8123-456789abcdef"
	tests := []struct {
		name        string
		target      string
		lastEventID string
		wantStream  string
		wantAfter   uint64
		wantError   bool
	}{
		{name: "new stream", target: "/events"},
		{name: "stream without offset", target: "/events?stream_id=" + streamID, wantStream: streamID},
		{name: "query cursor", target: "/events?stream_id=" + streamID + "&after=42", wantStream: streamID, wantAfter: 42},
		{name: "zero query offset", target: "/events?after=0"},
		{
			name: "last event id takes precedence", target: "/events?stream_id=ignored&after=invalid",
			lastEventID: streamID + ":7", wantStream: streamID, wantAfter: 7,
		},
		{name: "missing stream", target: "/events?after=1", wantError: true},
		{name: "negative offset", target: "/events?stream_id=" + streamID + "&after=-1", wantError: true},
		{name: "invalid offset", target: "/events?stream_id=" + streamID + "&after=one", wantError: true},
		{name: "malformed last event id", target: "/events", lastEventID: streamID, wantError: true},
		{name: "empty last event stream", target: "/events", lastEventID: ":1", wantError: true},
		{name: "empty last event sequence", target: "/events", lastEventID: streamID + ":", wantError: true},
		{name: "zero last event sequence", target: "/events", lastEventID: streamID + ":0", wantError: true},
		{name: "leading-zero last event sequence", target: "/events", lastEventID: streamID + ":01", wantError: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, test.target, nil)
			if test.lastEventID != "" {
				request.Header.Set("Last-Event-ID", test.lastEventID)
			}
			stream, after, err := eventCursor(request)
			if test.wantError {
				if err == nil {
					t.Fatalf("expected cursor rejection, got stream=%q after=%d", stream, after)
				}
				return
			}
			if err != nil || stream != test.wantStream || after != test.wantAfter {
				t.Fatalf("unexpected cursor: stream=%q after=%d err=%v", stream, after, err)
			}
		})
	}
}

func TestWriteSSEEventUsesStableWireFormat(t *testing.T) {
	message := "safe\nwarning"
	willRetry := false
	event := session.Event{
		SchemaVersion:  1,
		EventID:        "019c0123-4567-7abc-8123-456789abcdea",
		StreamID:       "019c0123-4567-7abc-8123-456789abcdeb",
		Sequence:       9,
		OccurredAt:     time.Date(2026, time.July, 23, 12, 0, 0, 0, time.UTC),
		TaskID:         "019c0123-4567-7abc-8123-456789abcdec",
		AgentSessionID: "019c0123-4567-7abc-8123-456789abcded",
		CodexThreadID:  "019c0123-4567-7abc-8123-456789abcdee",
		EventType:      session.EventWarning,
		Payload:        session.EventPayload{Message: &message, WillRetry: &willRetry},
	}
	var output strings.Builder
	if err := writeSSEEvent(&output, event); err != nil {
		t.Fatal(err)
	}
	wantPrefix := "id: " + event.StreamID + ":9\nevent: warning\ndata: "
	if !strings.HasPrefix(output.String(), wantPrefix) || !strings.HasSuffix(output.String(), "\n\n") {
		t.Fatalf("unexpected SSE frame: %q", output.String())
	}
	lines := strings.Split(strings.TrimSuffix(output.String(), "\n\n"), "\n")
	if len(lines) != 3 || !strings.Contains(lines[2], `"message":"safe\nwarning"`) {
		t.Fatalf("SSE data was not encoded as one JSON line: %q", output.String())
	}
}

func TestSSEHeartbeatAndHeaders(t *testing.T) {
	store, err := session.OpenStore(filepath.Join(t.TempDir(), "host-home"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	const (
		taskID    = "019c0123-4567-7abc-8123-456789abcdea"
		sessionID = "019c0123-4567-7abc-8123-456789abcdeb"
		threadID  = "019c0123-4567-7abc-8123-456789abcdec"
	)
	if err := store.Reserve(session.Record{TaskID: taskID, AgentSessionID: sessionID, Cwd: t.TempDir()}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.BindThread(
		sessionID, threadID, "runtime-session", codex.MiniMaxModel, codex.MiniMaxProviderID,
	); err != nil {
		t.Fatal(err)
	}
	service := session.NewService(appFakeRuntime{}, store, session.NewEventHub(8, 2), nil)
	handler := &sessionHandler{service: service, heartbeatInterval: 5 * time.Millisecond}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.SetPathValue("agent_session_id", sessionID)
		handler.events(w, r)
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"/events", nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err := server.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK || response.Header.Get("Content-Type") != "text/event-stream" ||
		response.Header.Get("Cache-Control") != "no-store" || response.Header.Get("X-Accel-Buffering") != "no" ||
		response.Header.Get("X-Yijie-Event-Stream-ID") == "" {
		t.Fatalf("unexpected SSE response: status=%d headers=%v", response.StatusCode, response.Header)
	}
	reader := bufio.NewReader(response.Body)
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatal(err)
		}
		if line == ": heartbeat\n" {
			break
		}
	}
}

func TestLoadConfigRejectsInvalidDuration(t *testing.T) {
	t.Setenv("YIJIE_CODEX_STARTUP_TIMEOUT", "not-a-duration")
	if _, err := LoadConfig(); err == nil {
		t.Fatal("expected invalid startup timeout to fail")
	}
}

func TestLoadConfigRequiresExplicitMiniMaxProviderAndProtectsKeyFile(t *testing.T) {
	for _, key := range []string{
		"YIJIE_MODEL_PROVIDER", "YIJIE_MINIMAX_API_KEY", "YIJIE_MINIMAX_API_KEY_FILE", "YIJIE_AGENT_HOST_HOME",
	} {
		t.Setenv(key, "")
	}
	t.Setenv("YIJIE_MINIMAX_API_KEY", "secret")
	if _, err := LoadConfig(); err == nil {
		t.Fatal("expected implicit provider selection to fail")
	}
	t.Setenv("YIJIE_MINIMAX_API_KEY", "")
	keyPath := filepath.Join(t.TempDir(), "minimax-key")
	if err := os.WriteFile(keyPath, []byte("secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	hostHome := filepath.Join(t.TempDir(), "host-home")
	t.Setenv("YIJIE_MODEL_PROVIDER", codex.MiniMaxProviderID)
	t.Setenv("YIJIE_MINIMAX_API_KEY_FILE", keyPath)
	t.Setenv("YIJIE_AGENT_HOST_HOME", hostHome)
	config, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if !config.Runtime.MiniMax.Enabled || config.Runtime.MiniMax.APIKey != "secret" || config.HostHome != hostHome {
		t.Fatal("MiniMax configuration was not loaded from the protected file")
	}
	if err := os.Chmod(keyPath, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadConfig(); err == nil {
		t.Fatal("expected loosely protected key file to fail")
	}
}

func TestLoadConfigRejectsSharedRuntimeAndHostHome(t *testing.T) {
	for _, key := range []string{
		"YIJIE_MODEL_PROVIDER", "YIJIE_MINIMAX_API_KEY", "YIJIE_MINIMAX_API_KEY_FILE",
		"YIJIE_AGENT_HOST_HOME", "YIJIE_CODEX_HOME",
	} {
		t.Setenv(key, "")
	}
	shared := filepath.Join(t.TempDir(), "shared-home")
	t.Setenv("YIJIE_MODEL_PROVIDER", codex.MiniMaxProviderID)
	t.Setenv("YIJIE_MINIMAX_API_KEY", "secret")
	t.Setenv("YIJIE_AGENT_HOST_HOME", shared)
	t.Setenv("YIJIE_CODEX_HOME", shared)
	if _, err := LoadConfig(); err == nil {
		t.Fatal("expected shared Runtime and Host home to fail")
	}
}

func TestLoadConfigAcceptsOnlyExactFEAT126FakeProfile(t *testing.T) {
	for _, key := range []string{
		"YIJIE_FEAT126_S10_TEST_PROFILE_ENABLED", "YIJIE_FEAT126_S10_RUN_ID",
		"YIJIE_FEAT126_FAKE_RESPONSES_BASE_URL", "YIJIE_MODEL_PROVIDER",
		"YIJIE_MINIMAX_API_KEY", "YIJIE_MINIMAX_API_KEY_FILE", "YIJIE_ENV",
		"YIJIE_AGENT_HOST_HOME", "YIJIE_CODEX_HOME",
		"YIJIE_FEAT126_S10_PARENT_PID", "YIJIE_FEAT126_S10_HOST_LOG_DIR",
		"YIJIE_FEAT126_S10_PROCESS_MANIFEST",
		"YIJIE_AGENT_HOST_V2_RAW_REASONING_ENABLED", "YIJIE_AGENT_HOST_V2_TITLE_ENABLED",
		"YIJIE_AGENT_HOST_V2_CLEANUP_ENABLED", "YIJIE_AGENT_HOST_V2_MULTIMODAL_TURNS_ENABLED",
		"YIJIE_AGENT_HOST_INSTANCE_NONCE",
	} {
		t.Setenv(key, "")
	}
	runID := "123e4567-e89b-42d3-a456-426614174000"
	instanceNonce := "123e4567-e89b-42d3-a456-426614174001"
	tempRoot, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	runRoot := filepath.Join(tempRoot, runID)
	if err := os.Mkdir(runRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	hostHome := filepath.Join(runRoot, "host-home")
	codexHome := filepath.Join(runRoot, "codex-home")
	projectDirectory := filepath.Join(runRoot, "project")
	hostEvidenceRoot := filepath.Join(runRoot, "host")
	logDirectory := filepath.Join(runRoot, "host", instanceNonce)
	for _, directory := range []string{hostHome, codexHome, projectDirectory, hostEvidenceRoot, logDirectory} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("YIJIE_FEAT126_S10_TEST_PROFILE_ENABLED", "true")
	t.Setenv("YIJIE_FEAT126_S10_RUN_ID", runID)
	t.Setenv("YIJIE_FEAT126_FAKE_RESPONSES_BASE_URL", codex.FEAT126FakeBaseURL)
	processManifest := filepath.Join(logDirectory, "process.json")
	t.Setenv("YIJIE_FEAT126_S10_PARENT_PID", fmt.Sprint(os.Getppid()))
	t.Setenv("YIJIE_FEAT126_S10_HOST_LOG_DIR", logDirectory)
	t.Setenv("YIJIE_FEAT126_S10_PROCESS_MANIFEST", processManifest)
	t.Setenv("YIJIE_AGENT_HOST_INSTANCE_NONCE", instanceNonce)
	t.Setenv("YIJIE_ENV", "local")
	t.Setenv("YIJIE_AGENT_HOST_HOME", hostHome)
	t.Setenv("YIJIE_CODEX_HOME", codexHome)
	t.Setenv("YIJIE_AGENT_HOST_V2_RAW_REASONING_ENABLED", "true")
	t.Setenv("YIJIE_AGENT_HOST_V2_TITLE_ENABLED", "false")
	t.Setenv("YIJIE_AGENT_HOST_V2_CLEANUP_ENABLED", "true")
	config, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if !config.Runtime.FakeResponses.Enabled || config.Runtime.FakeResponses.RunID != runID ||
		config.Runtime.MiniMax.Enabled || !config.RawReasoningV2Enabled || !config.CleanupV2Enabled ||
		config.TitleV2Enabled || config.MultimodalV2Enabled ||
		config.FEAT126TestParentPID != os.Getppid() || config.FEAT126ProjectDir != projectDirectory {
		t.Fatalf("unexpected FEAT-126 fake profile: %#v", config)
	}

	t.Setenv("YIJIE_MINIMAX_API_KEY", "must-not-be-read")
	if _, err := LoadConfig(); err == nil {
		t.Fatal("fake profile accepted a MiniMax key")
	}
	t.Setenv("YIJIE_MINIMAX_API_KEY", "")
	for _, baseURL := range []string{
		"http://localhost:18082/v1", "http://127.0.0.1:18083/v1", "http://127.0.0.1:18082/v1?x=1", "https://127.0.0.1:18082/v1",
	} {
		t.Setenv("YIJIE_FEAT126_FAKE_RESPONSES_BASE_URL", baseURL)
		if _, err := LoadConfig(); err == nil {
			t.Fatalf("fake profile accepted unsafe endpoint %q", baseURL)
		}
	}
	t.Setenv("YIJIE_FEAT126_FAKE_RESPONSES_BASE_URL", codex.FEAT126FakeBaseURL)
	t.Setenv("YIJIE_FEAT126_S10_TEST_PROFILE_ENABLED", "false")
	if _, err := LoadConfig(); err == nil {
		t.Fatal("disabled master accepted subordinate fake profile settings")
	}
}

func TestLoadConfigRejectsFEAT126ProjectAuthorityMismatch(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*testing.T, feat126AuthorityFixture)
	}{
		{
			name: "host home outside run",
			mutate: func(t *testing.T, _ feat126AuthorityFixture) {
				t.Setenv("YIJIE_AGENT_HOST_HOME", filepath.Join(t.TempDir(), "host-home"))
			},
		},
		{
			name: "Runtime home outside run",
			mutate: func(t *testing.T, _ feat126AuthorityFixture) {
				t.Setenv("YIJIE_CODEX_HOME", filepath.Join(t.TempDir(), "codex-home"))
			},
		},
		{
			name: "evidence nonce mismatch",
			mutate: func(t *testing.T, fixture feat126AuthorityFixture) {
				other := filepath.Join(fixture.runRoot, "host", "123e4567-e89b-42d3-a456-426614174002")
				if err := os.Mkdir(other, 0o700); err != nil {
					t.Fatal(err)
				}
				t.Setenv("YIJIE_FEAT126_S10_HOST_LOG_DIR", other)
				t.Setenv("YIJIE_FEAT126_S10_PROCESS_MANIFEST", filepath.Join(other, "process.json"))
			},
		},
		{
			name: "project is not owner-only",
			mutate: func(t *testing.T, fixture feat126AuthorityFixture) {
				if err := os.Chmod(fixture.projectDirectory, 0o755); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "run root is not owner-only",
			mutate: func(t *testing.T, fixture feat126AuthorityFixture) {
				if err := os.Chmod(fixture.runRoot, 0o755); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "Host evidence root is not owner-only",
			mutate: func(t *testing.T, fixture feat126AuthorityFixture) {
				if err := os.Chmod(fixture.hostEvidenceRoot, 0o755); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "Host home is missing",
			mutate: func(t *testing.T, fixture feat126AuthorityFixture) {
				if err := os.Remove(fixture.hostHome); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "Host home is not owner-only",
			mutate: func(t *testing.T, fixture feat126AuthorityFixture) {
				if err := os.Chmod(fixture.hostHome, 0o755); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "Runtime home is not owner-only",
			mutate: func(t *testing.T, fixture feat126AuthorityFixture) {
				if err := os.Chmod(fixture.codexHome, 0o755); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "current nonce directory is not owner-only",
			mutate: func(t *testing.T, fixture feat126AuthorityFixture) {
				if err := os.Chmod(fixture.logDirectory, 0o755); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "Runtime home is a symlink",
			mutate: func(t *testing.T, fixture feat126AuthorityFixture) {
				if err := os.Remove(fixture.codexHome); err != nil {
					t.Fatal(err)
				}
				target := t.TempDir()
				if err := os.Chmod(target, 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(target, fixture.codexHome); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "Host home path is not canonical",
			mutate: func(t *testing.T, fixture feat126AuthorityFixture) {
				t.Setenv("YIJIE_AGENT_HOST_HOME", fixture.runRoot+"/project/../host-home")
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := configureExactFEAT126Authority(t)
			test.mutate(t, fixture)
			if _, err := LoadConfig(); err == nil {
				t.Fatal("mismatched FEAT-126 project authority was accepted")
			}
		})
	}
}

func TestLoadConfigRequiresCanonicalRFC4122UUIDv4ForExactFEAT126Identity(t *testing.T) {
	invalid := []struct {
		name  string
		value string
	}{
		{name: "missing", value: ""},
		{name: "malformed", value: "not-a-uuid"},
		{name: "uppercase", value: "123E4567-E89B-42D3-A456-426614174003"},
		{name: "uuid v1", value: "6ba7b810-9dad-11d1-80b4-00c04fd430c8"},
		{name: "uuid v7", value: "019fbd88-cbc3-7bf1-934d-7b05cd693f80"},
		{name: "non RFC4122 variant", value: "123e4567-e89b-42d3-4456-426614174003"},
		{name: "surrounding whitespace", value: " 123e4567-e89b-42d3-a456-426614174003 "},
	}
	for _, identity := range []struct {
		name string
		key  string
	}{
		{name: "run id", key: "YIJIE_FEAT126_S10_RUN_ID"},
		{name: "instance nonce", key: "YIJIE_AGENT_HOST_INSTANCE_NONCE"},
	} {
		for _, test := range invalid {
			t.Run(identity.name+"/"+test.name, func(t *testing.T) {
				configureExactFEAT126Authority(t)
				t.Setenv(identity.key, test.value)
				if _, err := LoadConfig(); err == nil {
					t.Fatalf("exact FEAT-126 profile accepted %s %q", identity.name, test.value)
				}
			})
		}
	}

	const canonicalV7 = "019fbd88-cbc3-7bf1-934d-7b05cd693f80"
	if !isCanonicalUUID(canonicalV7) || isCanonicalRFC4122UUIDv4(canonicalV7) {
		t.Fatal("default UUID compatibility and exact FEAT-126 UUIDv4 authority were not separated")
	}
}

func TestFEAT126DirectoryAuthorityMatrixRejectsThroughStartupBeforeMutation(t *testing.T) {
	roles := []struct {
		name      string
		authority string
		path      func(feat126AuthorityFixture) string
	}{
		{
			name: "run-root", authority: "FEAT-126 run root",
			path: func(f feat126AuthorityFixture) string { return f.runRoot },
		},
		{
			name: "project", authority: "FEAT-126 project directory",
			path: func(f feat126AuthorityFixture) string { return f.projectDirectory },
		},
		{
			name: "host-root", authority: "FEAT-126 Host evidence root",
			path: func(f feat126AuthorityFixture) string { return f.hostEvidenceRoot },
		},
		{
			name: "current-nonce", authority: "YIJIE_FEAT126_S10_HOST_LOG_DIR",
			path: func(f feat126AuthorityFixture) string { return f.logDirectory },
		},
		{
			name: "host-home", authority: "FEAT-126 Host home",
			path: func(f feat126AuthorityFixture) string { return f.hostHome },
		},
		{
			name: "codex-home", authority: "FEAT-126 Runtime home",
			path: func(f feat126AuthorityFixture) string { return f.codexHome },
		},
	}
	conditions := []struct {
		name      string
		candidate func(*testing.T, string) (string, uint32)
	}{
		{
			name: "missing",
			candidate: func(t *testing.T, _ string) (string, uint32) {
				root, err := filepath.EvalSymlinks(t.TempDir())
				if err != nil {
					t.Fatal(err)
				}
				return filepath.Join(root, "missing"), uint32(os.Geteuid())
			},
		},
		{
			name: "symlink",
			candidate: func(t *testing.T, _ string) (string, uint32) {
				root, err := filepath.EvalSymlinks(t.TempDir())
				if err != nil {
					t.Fatal(err)
				}
				target := filepath.Join(root, "target")
				candidate := filepath.Join(root, "candidate")
				if err := os.Mkdir(target, 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(target, candidate); err != nil {
					t.Fatal(err)
				}
				return candidate, uint32(os.Geteuid())
			},
		},
		{
			name: "wrong-owner",
			candidate: func(_ *testing.T, path string) (string, uint32) {
				return path, uint32(os.Geteuid() + 1)
			},
		},
		{
			name: "0755",
			candidate: func(t *testing.T, _ string) (string, uint32) {
				root, err := filepath.EvalSymlinks(t.TempDir())
				if err != nil {
					t.Fatal(err)
				}
				candidate := filepath.Join(root, "candidate")
				if err := os.Mkdir(candidate, 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.Chmod(candidate, 0o755); err != nil {
					t.Fatal(err)
				}
				return candidate, uint32(os.Geteuid())
			},
		},
		{
			name: "noncanonical",
			candidate: func(_ *testing.T, path string) (string, uint32) {
				return path + string(os.PathSeparator) + ".", uint32(os.Geteuid())
			},
		},
	}

	for _, role := range roles {
		for _, condition := range conditions {
			t.Run(role.name+"/"+condition.name, func(t *testing.T) {
				fixture := configureExactFEAT126Authority(t)
				expectedPath := role.path(fixture)
				if err := os.WriteFile(
					filepath.Join(fixture.projectDirectory, ".authority-canary"),
					[]byte("unchanged"),
					0o600,
				); err != nil {
					t.Fatal(err)
				}
				candidate, expectedUID := condition.candidate(t, expectedPath)
				snapshotRoot := filepath.Dir(fixture.runRoot)
				before := snapshotAuthorityTree(t, snapshotRoot)
				calls := 0
				validateDirectory := func(value, authority string) (string, error) {
					if authority != role.authority {
						return validateOwnerOnlyDirectoryAuthority(value, authority)
					}
					calls++
					if value != expectedPath {
						t.Fatalf("startup wired %s to %q instead of %q", role.name, value, expectedPath)
					}
					return validateOwnerOnlyDirectoryAuthorityForUID(candidate, authority, expectedUID)
				}
				_, err := loadConfigWithDirectoryAuthority(validateDirectory)
				if err == nil {
					t.Fatalf("accepted invalid %s authority", role.name)
				}
				if calls != 1 || !strings.Contains(err.Error(), role.authority) {
					t.Fatalf("startup did not reject the targeted %s authority: calls=%d err=%v", role.name, calls, err)
				}
				after := snapshotAuthorityTree(t, snapshotRoot)
				if !reflect.DeepEqual(before, after) {
					t.Fatalf("rejected %s/%s authority mutated files or metadata: before=%v after=%v", role.name, condition.name, before, after)
				}
			})
		}
	}
}

func snapshotAuthorityTree(t *testing.T, root string) map[string]string {
	t.Helper()
	snapshot := make(map[string]string)
	if err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		value := fmt.Sprintf("%s|%04o|%d", info.Mode().Type(), info.Mode().Perm(), info.Size())
		if stat, ok := info.Sys().(*syscall.Stat_t); ok {
			value += fmt.Sprintf("|%d|%d|%d", stat.Uid, stat.Gid, stat.Nlink)
		}
		switch {
		case info.Mode()&os.ModeSymlink != 0:
			target, readErr := os.Readlink(path)
			if readErr != nil {
				return readErr
			}
			value += "|" + target
		case info.Mode().IsRegular():
			content, readErr := os.ReadFile(path)
			if readErr != nil {
				return readErr
			}
			value += fmt.Sprintf("|%x", content)
		}
		snapshot[relative] = value
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return snapshot
}

type feat126AuthorityFixture struct {
	runRoot          string
	projectDirectory string
	hostEvidenceRoot string
	hostHome         string
	codexHome        string
	logDirectory     string
	runID            string
	instanceNonce    string
}

func configureExactFEAT126Authority(t *testing.T) feat126AuthorityFixture {
	t.Helper()
	const (
		runID         = "123e4567-e89b-42d3-a456-426614174000"
		instanceNonce = "123e4567-e89b-42d3-a456-426614174001"
	)
	for _, key := range []string{
		"YIJIE_MODEL_PROVIDER", "YIJIE_MINIMAX_API_KEY", "YIJIE_MINIMAX_API_KEY_FILE",
		"YIJIE_FEAT126_S10_TEST_PROFILE_ENABLED", "YIJIE_FEAT126_S10_RUN_ID",
		"YIJIE_FEAT126_FAKE_RESPONSES_BASE_URL", "YIJIE_FEAT126_S10_PARENT_PID",
		"YIJIE_FEAT126_S10_HOST_LOG_DIR", "YIJIE_FEAT126_S10_PROCESS_MANIFEST",
		"YIJIE_AGENT_HOST_HOME", "YIJIE_CODEX_HOME", "YIJIE_AGENT_HOST_INSTANCE_NONCE",
		"YIJIE_AGENT_HOST_V2_RAW_REASONING_ENABLED", "YIJIE_AGENT_HOST_V2_TITLE_ENABLED",
		"YIJIE_AGENT_HOST_V2_CLEANUP_ENABLED", "YIJIE_AGENT_HOST_V2_MULTIMODAL_TURNS_ENABLED",
	} {
		t.Setenv(key, "")
	}
	tempRoot, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	runRoot := filepath.Join(tempRoot, runID)
	projectDirectory := filepath.Join(runRoot, "project")
	hostHome := filepath.Join(runRoot, "host-home")
	codexHome := filepath.Join(runRoot, "codex-home")
	hostEvidenceRoot := filepath.Join(runRoot, "host")
	logDirectory := filepath.Join(runRoot, "host", instanceNonce)
	for _, directory := range []string{runRoot, projectDirectory, hostHome, codexHome, hostEvidenceRoot, logDirectory} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("YIJIE_ENV", "local")
	t.Setenv("YIJIE_FEAT126_S10_TEST_PROFILE_ENABLED", "true")
	t.Setenv("YIJIE_FEAT126_S10_RUN_ID", runID)
	t.Setenv("YIJIE_FEAT126_FAKE_RESPONSES_BASE_URL", codex.FEAT126FakeBaseURL)
	t.Setenv("YIJIE_FEAT126_S10_PARENT_PID", fmt.Sprint(os.Getppid()))
	t.Setenv("YIJIE_FEAT126_S10_HOST_LOG_DIR", logDirectory)
	t.Setenv("YIJIE_FEAT126_S10_PROCESS_MANIFEST", filepath.Join(logDirectory, "process.json"))
	t.Setenv("YIJIE_AGENT_HOST_HOME", hostHome)
	t.Setenv("YIJIE_CODEX_HOME", codexHome)
	t.Setenv("YIJIE_AGENT_HOST_INSTANCE_NONCE", instanceNonce)
	t.Setenv("YIJIE_AGENT_HOST_V2_RAW_REASONING_ENABLED", "true")
	t.Setenv("YIJIE_AGENT_HOST_V2_TITLE_ENABLED", "false")
	t.Setenv("YIJIE_AGENT_HOST_V2_CLEANUP_ENABLED", "true")
	return feat126AuthorityFixture{
		runRoot: runRoot, projectDirectory: projectDirectory, hostEvidenceRoot: hostEvidenceRoot,
		hostHome: hostHome, codexHome: codexHome, logDirectory: logDirectory,
		runID: runID, instanceNonce: instanceNonce,
	}
}

func TestLoadConfigAllowsFreshNonceRestartWithinTheSameFEAT126Run(t *testing.T) {
	fixture := configureExactFEAT126Authority(t)
	first, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	store, err := session.OpenStore(
		first.HostHome,
		session.WithFEAT126Authority(first.FEAT126ProjectDir, first.FEAT126TestRunID),
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	const secondNonce = "123e4567-e89b-42d3-a456-426614174002"
	secondLogDirectory := filepath.Join(fixture.hostEvidenceRoot, secondNonce)
	if err := os.Mkdir(secondLogDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("YIJIE_AGENT_HOST_INSTANCE_NONCE", secondNonce)
	t.Setenv("YIJIE_FEAT126_S10_HOST_LOG_DIR", secondLogDirectory)
	t.Setenv("YIJIE_FEAT126_S10_PROCESS_MANIFEST", filepath.Join(secondLogDirectory, "process.json"))
	second, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if first.FEAT126TestRunID != second.FEAT126TestRunID || first.HostHome != second.HostHome ||
		first.FEAT126ProjectDir != second.FEAT126ProjectDir || first.InstanceNonce == second.InstanceNonce {
		t.Fatalf("fresh-nonce restart authority drift: first=%#v second=%#v", first, second)
	}
	reopened, err := session.OpenStore(
		second.HostHome,
		session.WithFEAT126Authority(second.FEAT126ProjectDir, second.FEAT126TestRunID),
	)
	if err != nil {
		t.Fatalf("fresh-nonce restart could not reopen same run-bound store: %v", err)
	}
	if err := reopened.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestFEAT126DirectoryAuthorityRejectsWrongOwnerIdentity(t *testing.T) {
	if hasExactOwnerDirectoryIdentity(os.ModeDir|0o700, 501, 502) {
		t.Fatal("exact directory authority accepted a different owner UID")
	}
}

type appFakeRuntime struct{}

func (appFakeRuntime) StartThread(context.Context, string) (codex.ThreadInfo, error) {
	return codex.ThreadInfo{
		ID: "019c0123-4567-7abc-8123-456789abcdec", RuntimeSession: "019c0123-4567-7abc-8123-456789abcdef",
		Model: codex.MiniMaxModel, ModelProvider: codex.MiniMaxProviderID,
	}, nil
}

func (appFakeRuntime) ResumeThread(context.Context, string) (codex.ThreadInfo, error) {
	return codex.ThreadInfo{
		ID: "019c0123-4567-7abc-8123-456789abcdec", RuntimeSession: "019c0123-4567-7abc-8123-456789abcdef",
		Model: codex.MiniMaxModel, ModelProvider: codex.MiniMaxProviderID,
	}, nil
}

func (appFakeRuntime) StartTurn(context.Context, string, string, string) (codex.TurnInfo, error) {
	return codex.TurnInfo{ID: "019c0123-4567-7abc-8123-456789abcded", Status: "inProgress"}, nil
}

func (appFakeRuntime) StartTurnV2(context.Context, string, []codex.UserInput, string) (codex.TurnInfo, error) {
	return codex.TurnInfo{ID: "019c0123-4567-7abc-8123-456789abcded", Status: "inProgress"}, nil
}

func (appFakeRuntime) InterruptTurn(context.Context, string, string) error { return nil }
func (appFakeRuntime) DeleteThread(context.Context, string) error          { return nil }
func (appFakeRuntime) GenerateTitle(context.Context, string) (string, error) {
	return "设计本地聊天安全删除流程", nil
}

type appCountingMultimodalRuntime struct {
	appFakeRuntime
	calls   int
	entered chan struct{}
	release chan struct{}
}

func (runtime *appCountingMultimodalRuntime) StartTurnV2(
	context.Context,
	string,
	[]codex.UserInput,
	string,
) (codex.TurnInfo, error) {
	runtime.calls++
	if runtime.entered != nil {
		close(runtime.entered)
		<-runtime.release
	}
	return codex.TurnInfo{ID: "019c0123-4567-7abc-8123-456789abcded", Status: "inProgress"}, nil
}

func TestV2DraftRoutesAreFlaggedAndMatchTitleCleanupContracts(t *testing.T) {
	home := filepath.Join(t.TempDir(), "host-home")
	store, err := session.OpenStore(home)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	const (
		taskID      = "019c0123-4567-7abc-8123-456789abcdea"
		sessionID   = "019c0123-4567-7abc-8123-456789abcdeb"
		threadID    = "019c0123-4567-7abc-8123-456789abcdec"
		operationID = "019fbd88-cbc3-7bf1-934d-7b05cd693f60"
	)
	if err := store.Reserve(session.Record{TaskID: taskID, AgentSessionID: sessionID, Cwd: t.TempDir()}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.BindThread(sessionID, threadID, "runtime-session", codex.MiniMaxModel, codex.MiniMaxProviderID); err != nil {
		t.Fatal(err)
	}
	runtime := appFakeRuntime{}
	service := session.NewService(runtime, store, session.NewEventHub(16, 8), nil,
		session.WithV2Events(session.NewEventHubVersion(session.EventSchemaVersionV2, 16, 8)),
		session.WithTitleGenerator(runtime),
	)
	disabled := NewHandler(Config{Environment: "local"}, staticRuntimeStatus{}, service, "api-token")
	disabledResponse := httptest.NewRecorder()
	disabled.ServeHTTP(disabledResponse, authorizedRequest(http.MethodPost, "/v2/agent-sessions/"+sessionID+"/title-generations", `{}`))
	if disabledResponse.Code != http.StatusNotFound {
		t.Fatalf("v2 draft route enabled by default: %d", disabledResponse.Code)
	}

	enabled := NewHandler(Config{Environment: "local", RawReasoningV2Enabled: true, TitleV2Enabled: true, CleanupV2Enabled: true}, staticRuntimeStatus{}, service, "api-token")
	titleRequest := authorizedRequest(http.MethodPost, "/v2/agent-sessions/"+sessionID+"/title-generations",
		`{"operation_id":"`+operationID+`","input":"为新的本地聊天任务设计安全的删除流程"}`)
	titleResponse := httptest.NewRecorder()
	enabled.ServeHTTP(titleResponse, titleRequest)
	if titleResponse.Code != http.StatusOK || !strings.Contains(titleResponse.Body.String(), `"title":"设计本地聊天安全删除流程"`) {
		t.Fatalf("unexpected title v2 response: %d %s", titleResponse.Code, titleResponse.Body.String())
	}
	assertOpenAPIJSON(t, "GenerateTitleV2Response", titleResponse.Body.Bytes())

	missingNegotiation := httptest.NewRecorder()
	enabled.ServeHTTP(missingNegotiation, authorizedRequest(http.MethodGet, "/v2/agent-sessions/"+sessionID+"/events", ""))
	if missingNegotiation.Code != http.StatusBadRequest {
		t.Fatalf("v2 event stream accepted missing negotiation: %d", missingNegotiation.Code)
	}
	streamRequest := authorizedRequest(http.MethodGet, "/v2/agent-sessions/"+sessionID+"/events?event_schema_version=2", "")
	streamContext, cancelStream := context.WithCancel(streamRequest.Context())
	cancelStream()
	streamRequest = streamRequest.WithContext(streamContext)
	streamResponse := httptest.NewRecorder()
	enabled.ServeHTTP(streamResponse, streamRequest)
	if streamResponse.Code != http.StatusOK || streamResponse.Header().Get("X-Yijie-Event-Schema-Version") != "2" {
		t.Fatalf("v2 stream omitted schema response header: status=%d headers=%v", streamResponse.Code, streamResponse.Header())
	}

	cleanupOperation := "019fbd88-cbc3-7bf1-934d-7b05cd693f61"
	cleanupResponse := httptest.NewRecorder()
	enabled.ServeHTTP(cleanupResponse, authorizedRequest(http.MethodPost, "/v2/agent-sessions/"+sessionID+"/cleanup-operations", `{"operation_id":"`+cleanupOperation+`"}`))
	if cleanupResponse.Code != http.StatusOK || !strings.Contains(cleanupResponse.Body.String(), `"runtime_thread_tree":"complete"`) {
		t.Fatalf("unexpected cleanup v2 response: %d %s", cleanupResponse.Code, cleanupResponse.Body.String())
	}
	assertOpenAPIJSON(t, "CleanupAgentSessionV2CompletedResponse", cleanupResponse.Body.Bytes())
}

func TestMultimodalTurnV2RouteIsFlaggedStrictAndAttachmentOnlyCapable(t *testing.T) {
	validBody := validTurnV2RequestBody(t, true, true)
	t.Run("disabled by default", func(t *testing.T) {
		handler := newMultimodalTurnHandler(t, false)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, authorizedRequest(
			http.MethodPost, "/v2/agent-sessions/019c0123-4567-7abc-8123-456789abcdeb/turns", validBody,
		))
		if response.Code != http.StatusNotFound {
			t.Fatalf("multimodal v2 route enabled by default: %d", response.Code)
		}
	})

	tests := []struct {
		name       string
		body       string
		wantStatus int
		forbidden  string
	}{
		{name: "ordered mixed content", body: validBody, wantStatus: http.StatusAccepted},
		{name: "attachment only", body: validTurnV2RequestBody(t, false, true), wantStatus: http.StatusAccepted},
		{
			name:       "missing operation id",
			body:       strings.Replace(validBody, `"operation_id":"019c0123-4567-7abc-8123-456789abcdee",`, "", 1),
			wantStatus: http.StatusBadRequest,
		},
		{
			name: "zero operation id",
			body: strings.Replace(
				validBody,
				"019c0123-4567-7abc-8123-456789abcdee",
				"00000000-0000-0000-0000-000000000000",
				1,
			),
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "unknown nested field",
			body:       strings.Replace(validBody, "\"data_url\":", "\"path\":\"/private/SENSITIVE-PATH-CANARY\",\"data_url\":", 1),
			wantStatus: http.StatusBadRequest,
			forbidden:  "/private/SENSITIVE-PATH-CANARY",
		},
		{
			name:       "unknown discriminator",
			body:       "{\"content_blocks\":[{\"type\":\"audio\",\"text\":\"not supported\"}]}",
			wantStatus: http.StatusBadRequest,
		},
		{
			name: "image digest mismatch",
			body: strings.Replace(
				validBody,
				fmt.Sprintf("\"sha256\":\"%x\"", sha256.Sum256(validAppTestPNG())),
				"\"sha256\":\""+strings.Repeat("0", 64)+"\"",
				1,
			),
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "unknown top level field",
			body:       strings.TrimSuffix(validBody, "}") + ",\"local_path\":\"/private/SENSITIVE-PATH-CANARY\"}",
			wantStatus: http.StatusBadRequest,
			forbidden:  "/private/SENSITIVE-PATH-CANARY",
		},
		{
			name:       "body limit",
			body:       "{\"content_blocks\":[{\"type\":\"text\",\"text\":\"" + strings.Repeat("x", (16<<20)+1) + "\"}]}",
			wantStatus: http.StatusBadRequest,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			handler := newMultimodalTurnHandler(t, true)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, authorizedRequest(
				http.MethodPost, "/v2/agent-sessions/019c0123-4567-7abc-8123-456789abcdeb/turns", test.body,
			))
			if response.Code != test.wantStatus {
				t.Fatalf("status=%d want=%d body=%s", response.Code, test.wantStatus, response.Body.String())
			}
			if test.forbidden != "" && strings.Contains(response.Body.String(), test.forbidden) {
				t.Fatalf("sensitive request value entered response: %s", response.Body.String())
			}
			if response.Header().Get("Cache-Control") != "no-store" {
				t.Fatalf("multimodal response is cacheable: %v", response.Header())
			}
			if test.wantStatus == http.StatusAccepted {
				assertOpenAPIJSON(t, "StartTurnResponse", response.Body.Bytes())
			} else {
				assertOpenAPIJSON(t, "ErrorResponse", response.Body.Bytes())
			}
		})
	}
}

func TestMultimodalTurnV2HTTPIdempotencyAndPendingRetry(t *testing.T) {
	t.Run("accepted replay and changed input conflict", func(t *testing.T) {
		runtime := &appCountingMultimodalRuntime{}
		handler := newMultimodalTurnHandlerForRuntime(t, true, runtime)
		body := validTurnV2RequestBody(t, true, true)
		first := httptest.NewRecorder()
		handler.ServeHTTP(first, authorizedRequest(
			http.MethodPost, "/v2/agent-sessions/019c0123-4567-7abc-8123-456789abcdeb/turns", body,
		))
		second := httptest.NewRecorder()
		handler.ServeHTTP(second, authorizedRequest(
			http.MethodPost, "/v2/agent-sessions/019c0123-4567-7abc-8123-456789abcdeb/turns", body,
		))
		if first.Code != http.StatusAccepted || second.Code != http.StatusAccepted ||
			first.Body.String() != second.Body.String() || runtime.calls != 1 {
			t.Fatalf("exact replay mismatch: first=%d %s second=%d %s calls=%d",
				first.Code, first.Body.String(), second.Code, second.Body.String(), runtime.calls)
		}
		changed := strings.Replace(body, "inspect attachments", "changed canonical input", 1)
		conflict := httptest.NewRecorder()
		handler.ServeHTTP(conflict, authorizedRequest(
			http.MethodPost, "/v2/agent-sessions/019c0123-4567-7abc-8123-456789abcdeb/turns", changed,
		))
		if conflict.Code != http.StatusConflict ||
			!strings.Contains(conflict.Body.String(), `"code":"turn_operation_conflict"`) || runtime.calls != 1 {
			t.Fatalf("changed input conflict mismatch: status=%d body=%s calls=%d", conflict.Code, conflict.Body.String(), runtime.calls)
		}
	})

	t.Run("pending retry does not invoke Runtime again", func(t *testing.T) {
		runtime := &appCountingMultimodalRuntime{entered: make(chan struct{}), release: make(chan struct{})}
		handler := newMultimodalTurnHandlerForRuntime(t, true, runtime)
		body := validTurnV2RequestBody(t, true, true)
		firstDone := make(chan *httptest.ResponseRecorder, 1)
		go func() {
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, authorizedRequest(
				http.MethodPost, "/v2/agent-sessions/019c0123-4567-7abc-8123-456789abcdeb/turns", body,
			))
			firstDone <- response
		}()
		<-runtime.entered
		pending := httptest.NewRecorder()
		handler.ServeHTTP(pending, authorizedRequest(
			http.MethodPost, "/v2/agent-sessions/019c0123-4567-7abc-8123-456789abcdeb/turns", body,
		))
		if pending.Code != http.StatusConflict ||
			!strings.Contains(pending.Body.String(), `"code":"session_not_usable"`) || runtime.calls != 1 {
			t.Fatalf("pending retry mismatch: status=%d body=%s calls=%d", pending.Code, pending.Body.String(), runtime.calls)
		}
		close(runtime.release)
		if first := <-firstDone; first.Code != http.StatusAccepted {
			t.Fatalf("first request did not complete after release: %d %s", first.Code, first.Body.String())
		}
	})
}

func newMultimodalTurnHandler(t *testing.T, enabled bool) http.Handler {
	t.Helper()
	return newMultimodalTurnHandlerForRuntime(t, enabled, appFakeRuntime{})
}

func newMultimodalTurnHandlerForRuntime(t *testing.T, enabled bool, runtime session.Runtime) http.Handler {
	t.Helper()
	store, err := session.OpenStore(filepath.Join(t.TempDir(), "host-home"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.Reserve(session.Record{
		TaskID:         "019c0123-4567-7abc-8123-456789abcdea",
		AgentSessionID: "019c0123-4567-7abc-8123-456789abcdeb",
		Cwd:            t.TempDir(),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.BindThread(
		"019c0123-4567-7abc-8123-456789abcdeb",
		"019c0123-4567-7abc-8123-456789abcdec",
		"runtime-session",
		codex.MiniMaxModel,
		codex.MiniMaxProviderID,
	); err != nil {
		t.Fatal(err)
	}
	service := session.NewService(runtime, store, session.NewEventHub(16, 8), nil)
	return NewHandler(
		Config{Environment: "local", MultimodalV2Enabled: enabled},
		staticRuntimeStatus{status: codex.Status{State: codex.StateReady, Ready: true}},
		service,
		"api-token",
	)
}

func validTurnV2RequestBody(t *testing.T, includeText, includeFile bool) string {
	t.Helper()
	content := validAppTestPNG()
	digest := sha256.Sum256(content)
	blocks := make([]map[string]any, 0, 3)
	if includeText {
		blocks = append(blocks, map[string]any{"type": "text", "text": "inspect attachments"})
	}
	blocks = append(blocks, map[string]any{
		"type": "image", "attachment_id": "019c0123-4567-7abc-8123-456789abcdf0",
		"media_type": "image/png", "size_bytes": len(content), "sha256": fmt.Sprintf("%x", digest),
		"data_url": "data:image/png;base64," + base64.StdEncoding.EncodeToString(content),
	})
	if includeFile {
		blocks = append(blocks, map[string]any{
			"type": "file", "attachment_id": "019c0123-4567-7abc-8123-456789abcdf1",
			"name": "report.txt", "media_type": "text/plain", "size_bytes": 42,
			"sha256": strings.Repeat("a", 64), "context_chunks": []string{"bounded context"},
		})
	}
	encoded, err := json.Marshal(map[string]any{
		"operation_id":     "019c0123-4567-7abc-8123-456789abcdee",
		"content_blocks":   blocks,
		"reasoning_effort": "none",
	})
	if err != nil {
		t.Fatal(err)
	}
	return string(encoded)
}

func validAppTestPNG() []byte {
	var output bytes.Buffer
	if err := png.Encode(&output, image.NewNRGBA(image.Rect(0, 0, 1, 1))); err != nil {
		panic(err)
	}
	return output.Bytes()
}

func TestLoadConfigKeepsV2DraftCapabilitiesOffAndLocalOnly(t *testing.T) {
	for _, key := range []string{
		"YIJIE_AGENT_HOST_V2_RAW_REASONING_ENABLED", "YIJIE_AGENT_HOST_V2_TITLE_ENABLED", "YIJIE_AGENT_HOST_V2_CLEANUP_ENABLED",
		"YIJIE_AGENT_HOST_V2_MULTIMODAL_TURNS_ENABLED",
		"YIJIE_MODEL_PROVIDER", "YIJIE_MINIMAX_API_KEY", "YIJIE_MINIMAX_API_KEY_FILE", "YIJIE_AGENT_HOST_HOME", "YIJIE_ENV",
		"YIJIE_AGENT_HOST_INSTANCE_NONCE",
		"YIJIE_AGENT_HOST_V3_ARTIFACTS_ENABLED", "YIJIE_FEAT128_SYNTHETIC_ENABLED", "YIJIE_FEAT128_SYNTHETIC_MANIFEST",
	} {
		t.Setenv(key, "")
	}
	config, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if config.RawReasoningV2Enabled || config.TitleV2Enabled || config.CleanupV2Enabled || config.MultimodalV2Enabled {
		t.Fatalf("v2 draft capability enabled by default: %#v", config)
	}

	home := filepath.Join(t.TempDir(), "host-home")
	t.Setenv("YIJIE_ENV", "local")
	t.Setenv("YIJIE_AGENT_HOST_HOME", home)
	t.Setenv("YIJIE_AGENT_HOST_V2_RAW_REASONING_ENABLED", "true")
	t.Setenv("YIJIE_AGENT_HOST_V2_CLEANUP_ENABLED", "true")
	config, err = LoadConfig()
	if err != nil || !config.RawReasoningV2Enabled || !config.CleanupV2Enabled {
		t.Fatalf("load local v2 flags: %#v err=%v", config, err)
	}
	t.Setenv("YIJIE_ENV", "production")
	if _, err := LoadConfig(); err == nil {
		t.Fatal("v2 draft capabilities must reject production")
	}
	t.Setenv("YIJIE_ENV", "local")
	t.Setenv("YIJIE_AGENT_HOST_V2_MULTIMODAL_TURNS_ENABLED", "true")
	config, err = LoadConfig()
	if err != nil || !config.MultimodalV2Enabled || config.Runtime.MiniMax.Enabled || config.Runtime.FakeResponses.Enabled {
		t.Fatalf("load local multimodal route without a model provider: %#v err=%v", config, err)
	}
	t.Setenv("YIJIE_MODEL_PROVIDER", "minimax")
	t.Setenv("YIJIE_MINIMAX_API_KEY", "synthetic-test-key")
	t.Setenv("YIJIE_CODEX_HOME", filepath.Join(t.TempDir(), "codex-home"))
	config, err = LoadConfig()
	if err != nil || !config.MultimodalV2Enabled {
		t.Fatalf("load local multimodal v2 flag: %#v err=%v", config, err)
	}
	t.Setenv("YIJIE_ENV", "production")
	if _, err := LoadConfig(); err == nil {
		t.Fatal("multimodal v2 capability must reject production")
	}
	t.Setenv("YIJIE_AGENT_HOST_V2_MULTIMODAL_TURNS_ENABLED", "false")
	t.Setenv("YIJIE_AGENT_HOST_V2_TITLE_ENABLED", "true")
	t.Setenv("YIJIE_ENV", "local")
	t.Setenv("YIJIE_MODEL_PROVIDER", "minimax")
	t.Setenv("YIJIE_MINIMAX_API_KEY", "synthetic-test-key")
	t.Setenv("YIJIE_CODEX_HOME", filepath.Join(t.TempDir(), "codex-home"))
	if _, err := LoadConfig(); err == nil || !strings.Contains(err.Error(), "cannot capability-disable tools") {
		t.Fatalf("title flag was not held closed for the pinned Runtime: %v", err)
	}
}

func TestLoadConfigKeepsFEAT128SyntheticExactLocalAndProviderIsolated(t *testing.T) {
	for _, key := range []string{
		"YIJIE_MODEL_PROVIDER", "YIJIE_MINIMAX_API_KEY", "YIJIE_MINIMAX_API_KEY_FILE",
		"YIJIE_FEAT126_S10_TEST_PROFILE_ENABLED", "YIJIE_AGENT_HOST_HOME", "YIJIE_ENV",
		"YIJIE_AGENT_HOST_V3_ARTIFACTS_ENABLED", "YIJIE_FEAT128_SYNTHETIC_ENABLED", "YIJIE_FEAT128_SYNTHETIC_MANIFEST",
	} {
		t.Setenv(key, "")
	}
	config, err := LoadConfig()
	if err != nil || config.ArtifactV3Enabled || config.ArtifactSynthetic {
		t.Fatalf("v3 Artifact capability enabled by default: %#v err=%v", config, err)
	}
	home := filepath.Join(t.TempDir(), "host-home")
	t.Setenv("YIJIE_ENV", "local")
	t.Setenv("YIJIE_AGENT_HOST_HOME", home)
	t.Setenv("YIJIE_AGENT_HOST_V3_ARTIFACTS_ENABLED", "true")
	t.Setenv("YIJIE_FEAT128_SYNTHETIC_ENABLED", "true")
	t.Setenv("YIJIE_FEAT128_SYNTHETIC_MANIFEST", session.SyntheticArtifactManifest)
	config, err = LoadConfig()
	if err != nil || !config.ArtifactV3Enabled || !config.ArtifactSynthetic || config.ArtifactManifest != session.SyntheticArtifactManifest {
		t.Fatalf("exact local synthetic profile rejected: %#v err=%v", config, err)
	}
	t.Setenv("YIJIE_ENV", "production")
	if _, err := LoadConfig(); err == nil {
		t.Fatal("synthetic Artifact profile accepted production")
	}
	t.Setenv("YIJIE_ENV", "local")
	t.Setenv("YIJIE_MODEL_PROVIDER", "minimax")
	t.Setenv("YIJIE_MINIMAX_API_KEY", "synthetic-test-key")
	t.Setenv("YIJIE_CODEX_HOME", filepath.Join(t.TempDir(), "codex-home"))
	if _, err := LoadConfig(); err == nil {
		t.Fatal("synthetic Artifact profile combined with a real provider")
	}
	t.Setenv("YIJIE_MODEL_PROVIDER", "")
	t.Setenv("YIJIE_MINIMAX_API_KEY", "")
	t.Setenv("YIJIE_CODEX_HOME", "")
	t.Setenv("YIJIE_FEAT128_SYNTHETIC_MANIFEST", "floating")
	if _, err := LoadConfig(); err == nil {
		t.Fatal("synthetic Artifact profile accepted a floating manifest")
	}
}

func TestArtifactV3HTTPResourcesRangeAckAndExplicitNegotiation(t *testing.T) {
	const (
		testTaskID    = "019c0123-4567-7abc-8123-456789abcdea"
		testSessionID = "019c0123-4567-7abc-8123-456789abcdeb"
		testThreadID  = "019c0123-4567-7abc-8123-456789abcdec"
	)
	home := filepath.Join(t.TempDir(), "host-home")
	store, err := session.OpenStore(home)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.Reserve(session.Record{TaskID: testTaskID, AgentSessionID: testSessionID, Cwd: t.TempDir()}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.BindThread(testSessionID, testThreadID, "runtime-session", codex.MiniMaxModel, codex.MiniMaxProviderID); err != nil {
		t.Fatal(err)
	}
	spool, err := artifact.OpenStore(filepath.Join(home, "artifact-spool"), artifact.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = spool.Close() })
	service := session.NewService(appFakeRuntime{}, store, session.NewEventHub(16, 8), nil,
		session.WithV3Artifacts(session.NewEventHubVersion(session.EventSchemaVersionV3, 64, 16), spool, true))
	handler := NewHandler(Config{Environment: "local", ArtifactV3Enabled: true, ArtifactSynthetic: true, ArtifactManifest: session.SyntheticArtifactManifest}, staticRuntimeStatus{}, service, "api-token")

	disabled := NewHandler(Config{Environment: "local"}, staticRuntimeStatus{}, service, "api-token")
	disabledResponse := httptest.NewRecorder()
	disabled.ServeHTTP(disabledResponse, authorizedRequest(http.MethodGet, "/v3/agent-sessions/"+testSessionID+"/events?event_schema_version=3", ""))
	if disabledResponse.Code != http.StatusNotFound {
		t.Fatalf("v3 route enabled by default: %d", disabledResponse.Code)
	}

	turnResponse := httptest.NewRecorder()
	handler.ServeHTTP(turnResponse, authorizedRequest(http.MethodPost, "/v1/agent-sessions/"+testSessionID+"/turns", `{"input":"emit local fixtures","reasoning_effort":"none"}`))
	if turnResponse.Code != http.StatusAccepted {
		t.Fatalf("start synthetic turn: %d %s", turnResponse.Code, turnResponse.Body.String())
	}

	missingNegotiation := httptest.NewRecorder()
	handler.ServeHTTP(missingNegotiation, authorizedRequest(http.MethodGet, "/v3/agent-sessions/"+testSessionID+"/events", ""))
	if missingNegotiation.Code != http.StatusBadRequest {
		t.Fatalf("v3 stream accepted missing negotiation: %d", missingNegotiation.Code)
	}
	alias := httptest.NewRecorder()
	handler.ServeHTTP(alias, authorizedRequest(http.MethodGet, "/v3/agent-sessions/"+testSessionID+"/events?event_schema_version=3&after_sequence=1", ""))
	if alias.Code != http.StatusBadRequest {
		t.Fatalf("v3 stream accepted after_sequence alias: %d", alias.Code)
	}

	streamRequest := authorizedRequest(http.MethodGet, "/v3/agent-sessions/"+testSessionID+"/events?event_schema_version=3", "")
	streamContext, cancelStream := context.WithCancel(streamRequest.Context())
	cancelStream()
	streamResponse := httptest.NewRecorder()
	handler.ServeHTTP(streamResponse, streamRequest.WithContext(streamContext))
	if streamResponse.Code != http.StatusOK || streamResponse.Header().Get("X-Yijie-Event-Schema-Version") != "3" {
		t.Fatalf("v3 stream response mismatch: %d %v", streamResponse.Code, streamResponse.Header())
	}
	var completed session.Event
	for _, line := range strings.Split(streamResponse.Body.String(), "\n") {
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		var event session.Event
		if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &event); err != nil {
			t.Fatal(err)
		}
		if event.EventType == session.EventItemArtifactCompleted && event.Payload.Kind == "image" {
			completed = event
		}
	}
	if completed.Payload.ArtifactID == "" || completed.Payload.SizeBytes == nil {
		t.Fatalf("v3 stream omitted completed image: %s", streamResponse.Body.String())
	}
	contentPath := "/v3/agent-sessions/" + testSessionID + "/artifacts/" + completed.Payload.ArtifactID + "/content"
	content := httptest.NewRecorder()
	handler.ServeHTTP(content, authorizedRequest(http.MethodGet, contentPath, ""))
	if content.Code != http.StatusOK || int64(content.Body.Len()) != *completed.Payload.SizeBytes || content.Header().Get("ETag") != `"`+completed.Payload.SHA256+`"` || content.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("content response mismatch: %d %v size=%d", content.Code, content.Header(), content.Body.Len())
	}
	head := httptest.NewRecorder()
	handler.ServeHTTP(head, authorizedRequest(http.MethodHead, contentPath, ""))
	if head.Code != http.StatusOK || head.Body.Len() != 0 || head.Header().Get("Content-Length") == "" {
		t.Fatalf("HEAD response mismatch: %d %v body=%d", head.Code, head.Header(), head.Body.Len())
	}
	rangeRequest := authorizedRequest(http.MethodGet, contentPath, "")
	rangeRequest.Header.Set("Range", "bytes=0-3")
	ranged := httptest.NewRecorder()
	handler.ServeHTTP(ranged, rangeRequest)
	if ranged.Code != http.StatusPartialContent || ranged.Body.Len() != 4 || !strings.HasPrefix(ranged.Header().Get("Content-Range"), "bytes 0-3/") {
		t.Fatalf("range response mismatch: %d %v size=%d", ranged.Code, ranged.Header(), ranged.Body.Len())
	}
	multiRange := authorizedRequest(http.MethodGet, contentPath, "")
	multiRange.Header.Set("Range", "bytes=0-1,3-4")
	multi := httptest.NewRecorder()
	handler.ServeHTTP(multi, multiRange)
	if multi.Code != http.StatusBadRequest || !strings.Contains(multi.Body.String(), `"code":"invalid_range"`) {
		t.Fatalf("multi-range was not rejected: %d %s", multi.Code, multi.Body.String())
	}
	crossSession := strings.Replace(contentPath, testSessionID, "019fbd88-cbc3-7bf1-934d-7b05cd693f54", 1)
	cross := httptest.NewRecorder()
	handler.ServeHTTP(cross, authorizedRequest(http.MethodGet, crossSession, ""))
	if cross.Code != http.StatusNotFound || strings.Contains(cross.Body.String(), completed.Payload.ArtifactID) {
		t.Fatalf("cross-session response leaked artifact: %d %s", cross.Code, cross.Body.String())
	}

	ackID := "019fbd88-cbc3-7bf1-934d-7b05cd693f80"
	ackBody := fmt.Sprintf(`{"ack_id":"%s","size_bytes":%d,"sha256":"%s","local_committed_at":"2026-08-20T02:00:05Z"}`, ackID, *completed.Payload.SizeBytes, completed.Payload.SHA256)
	ack := httptest.NewRecorder()
	handler.ServeHTTP(ack, authorizedRequest(http.MethodPost, strings.TrimSuffix(contentPath, "/content")+"/ack", ackBody))
	if ack.Code != http.StatusOK || !strings.Contains(ack.Body.String(), `"cleanup_status":"completed"`) {
		t.Fatalf("ack response mismatch: %d %s", ack.Code, ack.Body.String())
	}
	assertOpenAPIJSON(t, "ArtifactAcknowledgementV3Response", ack.Body.Bytes())
	replayAck := httptest.NewRecorder()
	handler.ServeHTTP(replayAck, authorizedRequest(http.MethodPost, strings.TrimSuffix(contentPath, "/content")+"/ack", ackBody))
	if replayAck.Code != http.StatusOK || replayAck.Body.String() != ack.Body.String() {
		t.Fatalf("ack replay mismatch: first=%s replay=%s", ack.Body.String(), replayAck.Body.String())
	}
	afterAck := httptest.NewRecorder()
	handler.ServeHTTP(afterAck, authorizedRequest(http.MethodGet, contentPath, ""))
	if afterAck.Code != http.StatusGone || !strings.Contains(afterAck.Body.String(), `"code":"artifact_expired"`) {
		t.Fatalf("acknowledged content remained available: %d %s", afterAck.Code, afterAck.Body.String())
	}
}

func TestSessionHTTPAndSSEVerticalSlice(t *testing.T) {
	store, err := session.OpenStore(filepath.Join(t.TempDir(), "host-home"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	service := session.NewService(appFakeRuntime{}, store, session.NewEventHub(16, 8), nil)
	handler := NewHandler(
		Config{Environment: "test", Port: "0"},
		staticRuntimeStatus{status: codex.Status{State: codex.StateReady, Ready: true}},
		service,
		"api-token",
	)

	unauthorized := httptest.NewRequest(http.MethodPost, "/v1/tasks/019c0123-4567-7abc-8123-456789abcdea/agent-sessions", strings.NewReader(`{"cwd":"/tmp"}`))
	unauthorized.Header.Set("Content-Type", "application/json")
	unauthorizedResponse := httptest.NewRecorder()
	handler.ServeHTTP(unauthorizedResponse, unauthorized)
	if unauthorizedResponse.Code != http.StatusUnauthorized {
		t.Fatalf("expected unauthorized request to fail, got %d", unauthorizedResponse.Code)
	}
	if !strings.Contains(unauthorizedResponse.Body.String(), `"code":"unauthorized"`) {
		t.Fatalf("unexpected unauthorized response: %s", unauthorizedResponse.Body.String())
	}

	workspace := t.TempDir()
	startBody := fmt.Sprintf(`{"trace_id":"trace-1","request_id":"request-1","cwd":%q}`, workspace)
	start := authorizedRequest(http.MethodPost, "/v1/tasks/019c0123-4567-7abc-8123-456789abcdea/agent-sessions", startBody)
	startResponse := httptest.NewRecorder()
	handler.ServeHTTP(startResponse, start)
	if startResponse.Code != http.StatusCreated {
		t.Fatalf("start session failed: %d %s", startResponse.Code, startResponse.Body.String())
	}
	var started agenthostcontract.SessionResponse
	if err := json.Unmarshal(startResponse.Body.Bytes(), &started); err != nil {
		t.Fatal(err)
	}
	sessionID := started.Session.AgentSessionId.String()
	if sessionID == "" {
		t.Fatalf("session response omitted id: %s", startResponse.Body.String())
	}
	assertOpenAPIJSON(t, "SessionResponse", startResponse.Body.Bytes())

	service.HandleNotification("thread/started", json.RawMessage(`{"thread":{"id":"019c0123-4567-7abc-8123-456789abcdec"}}`))
	turn := authorizedRequest(
		http.MethodPost,
		"/v1/agent-sessions/"+sessionID+"/turns",
		`{"trace_id":"trace-2","input":"reply OK","reasoning_effort":"none"}`,
	)
	turnResponse := httptest.NewRecorder()
	handler.ServeHTTP(turnResponse, turn)
	if turnResponse.Code != http.StatusAccepted || !strings.Contains(turnResponse.Body.String(), "019c0123-4567-7abc-8123-456789abcded") {
		t.Fatalf("start turn failed: %d %s", turnResponse.Code, turnResponse.Body.String())
	}
	assertOpenAPIJSON(t, "StartTurnResponse", turnResponse.Body.Bytes())

	server := httptest.NewServer(handler)
	defer server.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"/v1/agent-sessions/"+sessionID+"/events", nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer api-token")
	response, err := server.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK || response.Header.Get("X-Yijie-Event-Stream-ID") == "" {
		t.Fatalf("unexpected SSE response: %d", response.StatusCode)
	}
	reader := bufio.NewReader(response.Body)
	var dataLine string
	for dataLine == "" {
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatal(err)
		}
		if strings.HasPrefix(line, "data: ") {
			dataLine = strings.TrimSpace(strings.TrimPrefix(line, "data: "))
		}
	}
	if !strings.Contains(dataLine, `"event_type":"thread.started"`) || strings.Contains(dataLine, "api-token") {
		t.Fatalf("unexpected SSE event: %s", dataLine)
	}
}

func authorizedRequest(method, path, body string) *http.Request {
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	request.Header.Set("Authorization", "Bearer api-token")
	request.Header.Set("Content-Type", "application/json")
	return request
}

func assertOpenAPIJSON(t *testing.T, schemaName string, encoded []byte) {
	t.Helper()
	contractPath := contractFilePath(t, "openapi", "agent-host.yaml")
	contract, err := openapi3.NewLoader().LoadFromFile(contractPath)
	if err != nil {
		t.Fatalf("load Agent Host OpenAPI: %v", err)
	}
	if err := contract.Validate(context.Background()); err != nil {
		t.Fatalf("validate Agent Host OpenAPI: %v", err)
	}
	schema, ok := contract.Components.Schemas[schemaName]
	if !ok || schema.Value == nil {
		t.Fatalf("OpenAPI schema %q is missing", schemaName)
	}
	var value any
	if err := json.Unmarshal(encoded, &value); err != nil {
		t.Fatalf("decode response for OpenAPI validation: %v", err)
	}
	if err := schema.Value.VisitJSON(value); err != nil {
		t.Fatalf("response violates OpenAPI schema %s: %v\n%s", schemaName, err, encoded)
	}
}

func contractFilePath(t *testing.T, elements ...string) string {
	t.Helper()
	_, sourceFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate app contract test source")
	}
	parts := append([]string{filepath.Dir(sourceFile), "..", "..", "api"}, elements...)
	return filepath.Clean(filepath.Join(parts...))
}
