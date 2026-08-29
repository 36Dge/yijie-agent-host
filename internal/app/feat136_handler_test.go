package app

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/36Dge/yijie-agent-host/internal/codex"
	"github.com/36Dge/yijie-agent-host/internal/session"
)

func TestFEAT136V5RouteRequiresDependentGateAndExactNegotiation(t *testing.T) {
	store, err := session.OpenStore(filepath.Join(t.TempDir(), "host-home"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	const (
		taskID    = "019c0123-4567-7abc-8123-456789abcdfa"
		sessionID = "019c0123-4567-7abc-8123-456789abcdfb"
		threadID  = "019c0123-4567-7abc-8123-456789abcdfc"
	)
	if err := store.Reserve(session.Record{TaskID: taskID, AgentSessionID: sessionID, Cwd: t.TempDir()}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.BindThread(sessionID, threadID, "runtime-session", codex.MiniMaxModel, codex.MiniMaxProviderID); err != nil {
		t.Fatal(err)
	}
	v4 := session.NewEventHubVersion(session.EventSchemaVersionV4, 8, 2)
	v5 := session.NewEventHubVersion(session.EventSchemaVersionV5, 8, 2)
	service := session.NewService(
		appFakeRuntime{}, store, session.NewEventHub(8, 2), nil,
		session.WithV4Events(v4), session.WithV5Events(v5),
	)
	warning := "safe"
	willRetry := false
	published, err := v5.Publish(session.Event{
		TaskID: taskID, AgentSessionID: sessionID, CodexThreadID: threadID,
		EventType: session.EventWarning,
		Payload:   session.EventPayload{Message: &warning, WillRetry: &willRetry},
	})
	if err != nil {
		t.Fatal(err)
	}

	for name, config := range map[string]Config{
		"FEAT-136 only": {Environment: "local", FEAT136CommandToolItemsEnabled: true},
		"FEAT-134 only": {Environment: "local", FEAT134StreamingEnabled: true},
	} {
		handler := NewHandler(config, staticRuntimeStatus{}, service, "api-token")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, authorizedRequest(http.MethodGet, "/v5/agent-sessions/"+sessionID+"/events?event_schema_version=5", ""))
		if response.Code != http.StatusNotFound {
			t.Fatalf("v5 route enabled with %s: %d", name, response.Code)
		}
	}

	enabled := NewHandler(Config{
		Environment:                    "local",
		FEAT134StreamingEnabled:        true,
		FEAT136CommandToolItemsEnabled: true,
	}, staticRuntimeStatus{}, service, "api-token")
	for _, target := range []string{
		"/v5/agent-sessions/" + sessionID + "/events",
		"/v5/agent-sessions/" + sessionID + "/events?event_schema_version=4",
		"/v5/agent-sessions/" + sessionID + "/events?event_schema_version=5&event_schema_version=5",
		"/v5/agent-sessions/" + sessionID + "/events?event_schema_version=5&after_sequence=0",
	} {
		response := httptest.NewRecorder()
		enabled.ServeHTTP(response, authorizedRequest(http.MethodGet, target, ""))
		if response.Code != http.StatusBadRequest {
			t.Fatalf("v5 negotiation accepted %q: %d %s", target, response.Code, response.Body.String())
		}
	}

	request := authorizedRequest(http.MethodGet, "/v5/agent-sessions/"+sessionID+"/events?event_schema_version=5", "")
	ctx, cancel := context.WithCancel(request.Context())
	cancel()
	response := httptest.NewRecorder()
	enabled.ServeHTTP(response, request.WithContext(ctx))
	body := response.Body.String()
	if response.Code != http.StatusOK || response.Header().Get("X-Yijie-Event-Schema-Version") != "5" ||
		!strings.Contains(body, `"schema_version":5`) || !strings.Contains(body, `"event_id":"`+published.EventID+`"`) ||
		!strings.Contains(body, "id: "+published.StreamID+":1") {
		t.Fatalf("unexpected negotiated v5 stream: %d headers=%v body=%s", response.Code, response.Header(), body)
	}
}
