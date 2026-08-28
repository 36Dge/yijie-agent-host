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

func TestFEAT134V4RouteRequiresExplicitNegotiationAndFeatureGate(t *testing.T) {
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
	if _, err := store.BindThread(sessionID, threadID, "runtime-session", codex.MiniMaxModel, codex.MiniMaxProviderID); err != nil {
		t.Fatal(err)
	}
	v4 := session.NewEventHubVersion(session.EventSchemaVersionV4, 8, 2)
	service := session.NewService(appFakeRuntime{}, store, session.NewEventHub(8, 2), nil, session.WithV4Events(v4))
	warning := "safe"
	willRetry := false
	if _, err := v4.Publish(session.Event{
		TaskID: taskID, AgentSessionID: sessionID, CodexThreadID: threadID,
		EventType: session.EventWarning,
		Payload:   session.EventPayload{Message: &warning, WillRetry: &willRetry},
	}); err != nil {
		t.Fatal(err)
	}

	disabled := NewHandler(Config{Environment: "local"}, staticRuntimeStatus{}, service, "api-token")
	disabledResponse := httptest.NewRecorder()
	disabled.ServeHTTP(disabledResponse, authorizedRequest(http.MethodGet, "/v4/agent-sessions/"+sessionID+"/events?event_schema_version=4", ""))
	if disabledResponse.Code != http.StatusNotFound {
		t.Fatalf("v4 route enabled without FEAT-134 gate: %d", disabledResponse.Code)
	}

	enabled := NewHandler(Config{Environment: "local", FEAT134StreamingEnabled: true}, staticRuntimeStatus{}, service, "api-token")
	for _, target := range []string{
		"/v4/agent-sessions/" + sessionID + "/events",
		"/v4/agent-sessions/" + sessionID + "/events?event_schema_version=3",
		"/v4/agent-sessions/" + sessionID + "/events?event_schema_version=4&after_sequence=0",
	} {
		response := httptest.NewRecorder()
		enabled.ServeHTTP(response, authorizedRequest(http.MethodGet, target, ""))
		if response.Code != http.StatusBadRequest {
			t.Fatalf("v4 negotiation accepted %q: %d %s", target, response.Code, response.Body.String())
		}
	}

	request := authorizedRequest(http.MethodGet, "/v4/agent-sessions/"+sessionID+"/events?event_schema_version=4", "")
	ctx, cancel := context.WithCancel(request.Context())
	cancel()
	response := httptest.NewRecorder()
	enabled.ServeHTTP(response, request.WithContext(ctx))
	if response.Code != http.StatusOK || response.Header().Get("X-Yijie-Event-Schema-Version") != "4" ||
		!strings.Contains(response.Body.String(), `"schema_version":4`) {
		t.Fatalf("unexpected negotiated v4 stream: %d headers=%v body=%s", response.Code, response.Header(), response.Body.String())
	}
}
