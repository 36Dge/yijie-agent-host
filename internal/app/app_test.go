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
	request := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	response := httptest.NewRecorder()
	runtime := staticRuntimeStatus{status: codex.Status{State: codex.StateFailed}}

	NewHandler(Config{Environment: "test", Port: "0"}, runtime, nil, "").ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", response.Code)
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
				Config{Environment: "test", Port: "0"},
				staticRuntimeStatus{status: test.status},
				nil,
				"",
			).ServeHTTP(response, request)

			if response.Code != test.wantCode {
				t.Fatalf("expected status %d, got %d", test.wantCode, response.Code)
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
