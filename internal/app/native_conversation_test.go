package app

import (
	"context"
	"encoding/json"
	native "github.com/36Dge/yijie-agent-host/internal/contracts/nativeconversationv6"
	"github.com/36Dge/yijie-agent-host/internal/session"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type nativeTraceRouteService struct {
	SessionService
	reads int
}

func (s *nativeTraceRouteService) ReadNativeThreadV6(_ context.Context, id string) (native.NativeThreadSnapshot, error) {
	s.reads++
	if id != "known-session" {
		return native.NativeThreadSnapshot{}, session.ErrNotFound
	}
	return native.NativeThreadSnapshot{SchemaVersion: 6, Source: native.RuntimeRead, ThreadId: "thread", Turns: []native.NativeTurn{}, Availability: native.NativeThreadSnapshotAvailabilityPartial}, nil
}
func (s *nativeTraceRouteService) SubscribeNativeEvents(string, string, uint64) (string, []session.Event, <-chan session.Event, func(), error) {
	s.reads++
	updates := make(chan session.Event)
	close(updates)
	return "native-stream", []session.Event{{SchemaVersion: 13, EventType: "native.notification", Payload: session.EventPayload{Native: &native.NativeNotification{Item: &native.NativeItem{Id: "web-record", Type: native.NativeItemTypeWebSearch, Availability: native.NativeItemAvailabilityAvailable}}}}}, updates, func() {}, nil
}
func TestNativeTraceRoutesAndRetirement(t *testing.T) {
	service := &nativeTraceRouteService{}
	mux := http.NewServeMux()
	const token = "ordinary-local-test-token"
	registerNativeConversation(mux, &sessionHandler{service: service, apiToken: token})
	for _, path := range []string{"/v6/agent-sessions/known-session/native-thread", "/v13/agent-sessions/known-session/events"} {
		denied := httptest.NewRecorder()
		mux.ServeHTTP(denied, httptest.NewRequest(http.MethodGet, path, nil))
		if denied.Code != 401 {
			t.Fatal("missing bearer accepted")
		}
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		response := httptest.NewRecorder()
		mux.ServeHTTP(response, req)
		if response.Code != 200 || response.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("read failed %d", response.Code)
		}
		if strings.HasSuffix(path, "/events") {
			if !strings.Contains(response.Body.String(), `"schema_version":13`) {
				t.Fatal("wrong stream version")
			}
		} else {
			var view native.NativeThreadSnapshot
			if json.Unmarshal(response.Body.Bytes(), &view) != nil || view.SchemaVersion != 6 {
				t.Fatal("wrong snapshot")
			}
		}
	}
	before := service.reads
	for _, path := range []string{"/v4/agent-sessions/known-session/native-thread", "/v11/agent-sessions/known-session/events", "/v3/agent-sessions/known-session/native-thread", "/v10/agent-sessions/known-session/events", "/v1/agent-sessions/known-session/native-thread", "/v7/agent-sessions/known-session/events"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		response := httptest.NewRecorder()
		mux.ServeHTTP(response, req)
		if response.Code != 404 {
			t.Fatalf("retired route reachable: %s", path)
		}
	}
	if service.reads != before {
		t.Fatal("retired route called service")
	}
}
