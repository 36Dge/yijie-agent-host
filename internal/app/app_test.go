package app

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

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
		"YIJIE_AGENT_HOST_V2_CLEANUP_ENABLED",
	} {
		t.Setenv(key, "")
	}
	runID := "019fbd88-cbc3-7bf1-934d-7b05cd693f80"
	hostHome := filepath.Join(t.TempDir(), "host-home")
	codexHome := filepath.Join(t.TempDir(), "codex-home")
	t.Setenv("YIJIE_FEAT126_S10_TEST_PROFILE_ENABLED", "true")
	t.Setenv("YIJIE_FEAT126_S10_RUN_ID", runID)
	t.Setenv("YIJIE_FEAT126_FAKE_RESPONSES_BASE_URL", codex.FEAT126FakeBaseURL)
	logDirectory := t.TempDir()
	if err := os.Chmod(logDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	processManifest := filepath.Join(logDirectory, "process.json")
	t.Setenv("YIJIE_FEAT126_S10_PARENT_PID", fmt.Sprint(os.Getppid()))
	t.Setenv("YIJIE_FEAT126_S10_HOST_LOG_DIR", logDirectory)
	t.Setenv("YIJIE_FEAT126_S10_PROCESS_MANIFEST", processManifest)
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
		config.Runtime.MiniMax.Enabled || !config.RawReasoningV2Enabled || !config.CleanupV2Enabled || config.TitleV2Enabled ||
		config.FEAT126TestParentPID != os.Getppid() {
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

func (appFakeRuntime) InterruptTurn(context.Context, string, string) error { return nil }
func (appFakeRuntime) DeleteThread(context.Context, string) error          { return nil }
func (appFakeRuntime) GenerateTitle(context.Context, string) (string, error) {
	return "设计本地聊天安全删除流程", nil
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

func TestLoadConfigKeepsV2DraftCapabilitiesOffAndLocalOnly(t *testing.T) {
	for _, key := range []string{
		"YIJIE_AGENT_HOST_V2_RAW_REASONING_ENABLED", "YIJIE_AGENT_HOST_V2_TITLE_ENABLED", "YIJIE_AGENT_HOST_V2_CLEANUP_ENABLED",
		"YIJIE_MODEL_PROVIDER", "YIJIE_MINIMAX_API_KEY", "YIJIE_MINIMAX_API_KEY_FILE", "YIJIE_AGENT_HOST_HOME", "YIJIE_ENV",
		"YIJIE_AGENT_HOST_INSTANCE_NONCE",
	} {
		t.Setenv(key, "")
	}
	config, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if config.RawReasoningV2Enabled || config.TitleV2Enabled || config.CleanupV2Enabled {
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
	t.Setenv("YIJIE_AGENT_HOST_V2_TITLE_ENABLED", "true")
	t.Setenv("YIJIE_ENV", "local")
	t.Setenv("YIJIE_MODEL_PROVIDER", "minimax")
	t.Setenv("YIJIE_MINIMAX_API_KEY", "synthetic-test-key")
	t.Setenv("YIJIE_CODEX_HOME", filepath.Join(t.TempDir(), "codex-home"))
	if _, err := LoadConfig(); err == nil || !strings.Contains(err.Error(), "cannot capability-disable tools") {
		t.Fatalf("title flag was not held closed for the pinned Runtime: %v", err)
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
