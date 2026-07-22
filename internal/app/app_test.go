package app

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/36Dge/yijie-agent-host/internal/codex"
	"github.com/36Dge/yijie-agent-host/internal/session"
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
			var payload map[string]string
			if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
				t.Fatalf("decode response: %v", err)
			}
			if payload["status"] != test.wantStatus {
				t.Fatalf("expected readiness %q, got %q", test.wantStatus, payload["status"])
			}
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
	if response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("expected no-store status response")
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

	workspace := t.TempDir()
	startBody := fmt.Sprintf(`{"trace_id":"trace-1","request_id":"request-1","cwd":%q}`, workspace)
	start := authorizedRequest(http.MethodPost, "/v1/tasks/019c0123-4567-7abc-8123-456789abcdea/agent-sessions", startBody)
	startResponse := httptest.NewRecorder()
	handler.ServeHTTP(startResponse, start)
	if startResponse.Code != http.StatusCreated {
		t.Fatalf("start session failed: %d %s", startResponse.Code, startResponse.Body.String())
	}
	var started struct {
		Session map[string]any `json:"session"`
	}
	if err := json.Unmarshal(startResponse.Body.Bytes(), &started); err != nil {
		t.Fatal(err)
	}
	sessionID, _ := started.Session["agent_session_id"].(string)
	if sessionID == "" {
		t.Fatalf("session response omitted id: %s", startResponse.Body.String())
	}

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
