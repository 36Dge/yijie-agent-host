package app

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	native "github.com/36Dge/yijie-agent-host/internal/contracts/nativeconversation"
	"github.com/36Dge/yijie-agent-host/internal/session"
)

// This mock never starts a process or calls a model.
type nativeRouteService struct {
	SessionService
	reads int
}

func (s *nativeRouteService) ReadNativeThread(_ context.Context, id string) (native.NativeThreadSnapshot, error) {
	s.reads++
	if id != "known-session" {
		return native.NativeThreadSnapshot{}, session.ErrNotFound
	}
	return native.NativeThreadSnapshot{SchemaVersion: 1, Source: native.RuntimeRead, ThreadId: "native-thread", Turns: []native.NativeTurn{}, Availability: native.NativeThreadSnapshotAvailabilityPartial}, nil
}

func (s *nativeRouteService) SubscribeNativeEvents(string, string, uint64) (string, []session.Event, <-chan session.Event, func(), error) {
	return "", nil, nil, nil, session.ErrNotFound
}

func TestFEAT132NativeRoutesKeepBearerAuthorityAndNoStore(t *testing.T) {
	service := &nativeRouteService{}
	mux := http.NewServeMux()
	const token = "native-test-token-no-sensitive-value"
	registerNativeConversation(mux, &sessionHandler{service: service, apiToken: token})
	for _, target := range []string{"/v1/agent-sessions/known-session/native-thread", "/v7/agent-sessions/known-session/events"} {
		response := httptest.NewRecorder()
		mux.ServeHTTP(response, httptest.NewRequest(http.MethodGet, target, nil))
		if response.Code != http.StatusUnauthorized {
			t.Fatal("native route bypassed bearer authority")
		}
	}
	if service.reads != 0 {
		t.Fatal("unauthorized read reached Runtime adapter")
	}
	for _, value := range []struct {
		id     string
		status int
	}{{"known-session", 200}, {"missing-session", 404}} {
		request := httptest.NewRequest(http.MethodGet, "/v1/agent-sessions/"+value.id+"/native-thread", nil)
		request.Header.Set("Authorization", "Bearer "+token)
		response := httptest.NewRecorder()
		mux.ServeHTTP(response, request)
		if response.Code != value.status {
			t.Fatalf("expected %d, got %d", value.status, response.Code)
		}
		if value.status == 200 && response.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("native history may be cached")
		}
	}
}
