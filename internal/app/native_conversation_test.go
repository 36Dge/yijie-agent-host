package app

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	native "github.com/36Dge/yijie-agent-host/internal/contracts/nativeconversation"
	v2 "github.com/36Dge/yijie-agent-host/internal/contracts/nativeconversationv2"
	v3 "github.com/36Dge/yijie-agent-host/internal/contracts/nativeconversationv3"
	v4 "github.com/36Dge/yijie-agent-host/internal/contracts/nativeconversationv4"
	v5 "github.com/36Dge/yijie-agent-host/internal/contracts/nativeconversationv5"
	"github.com/36Dge/yijie-agent-host/internal/session"
)

// This mock never starts a process or calls a model.
type nativeRouteService struct {
	SessionService
	reads int
}

type nativeProcessRouteService struct {
	nativeRouteService
}

func (s *nativeProcessRouteService) ReadNativeThreadV3(_ context.Context, id string) (v3.NativeThreadSnapshot, error) {
	s.reads++
	if id != "known-session" {
		return v3.NativeThreadSnapshot{}, session.ErrNotFound
	}
	return v3.NativeThreadSnapshot{SchemaVersion: 3, Source: v3.RuntimeRead, ThreadId: "native-thread", Turns: []v3.NativeTurn{}, Availability: v3.NativeThreadSnapshotAvailabilityPartial}, nil
}

func (s *nativeProcessRouteService) ReadNativeThreadV4(_ context.Context, id string) (v4.NativeThreadSnapshot, error) {
	s.reads++
	if id != "known-session" {
		return v4.NativeThreadSnapshot{}, session.ErrNotFound
	}
	return v4.NativeThreadSnapshot{SchemaVersion: 4, Source: v4.RuntimeRead, ThreadId: "native-thread", Turns: []v4.NativeTurn{}, Availability: v4.NativeThreadSnapshotAvailabilityPartial}, nil
}

func (s *nativeProcessRouteService) ReadNativeThreadV5(_ context.Context, id string) (v5.NativeThreadSnapshot, error) {
	s.reads++
	if id != "known-session" {
		return v5.NativeThreadSnapshot{}, session.ErrNotFound
	}
	return v5.NativeThreadSnapshot{SchemaVersion: 5, Source: v5.RuntimeRead, ThreadId: "native-thread", Turns: []v5.NativeTurn{}, Availability: v5.NativeThreadSnapshotAvailabilityPartial}, nil
}

func (s *nativeProcessRouteService) ReadNativeThreadV2(_ context.Context, _ string) (v2.NativeThreadSnapshot, error) {
	return v2.NativeThreadSnapshot{}, session.ErrNotFound
}

func (s *nativeProcessRouteService) SubscribeNativeEvents(string, string, uint64) (string, []session.Event, <-chan session.Event, func(), error) {
	s.reads++
	updates := make(chan session.Event)
	close(updates)
	return "native-stream", []session.Event{{SchemaVersion: 12, Payload: session.EventPayload{Native: &v5.NativeNotification{
		Item: &v5.NativeItem{Id: "web-record", Type: v5.WebSearch, Availability: v5.NativeItemAvailabilityAvailable,
			WebSearch: &v5.NativeWebSearchDisplay{Action: v5.NativeWebSearchDisplayActionSearch, Queries: []string{"城市产业"}}},
	}}}}, updates, func() {}, nil
}

func TestNativeProcessRoutesPreserveAuthorityAndClosedVersions(t *testing.T) {
	service := &nativeProcessRouteService{}
	mux := http.NewServeMux()
	const token = "native-process-test-token"
	registerNativeConversation(mux, &sessionHandler{service: service, apiToken: token})
	for _, target := range []string{"/v5/agent-sessions/known-session/native-thread", "/v12/agent-sessions/known-session/events", "/v4/agent-sessions/known-session/native-thread", "/v11/agent-sessions/known-session/events", "/v3/agent-sessions/known-session/native-thread", "/v10/agent-sessions/known-session/events"} {
		response := httptest.NewRecorder()
		mux.ServeHTTP(response, httptest.NewRequest(http.MethodGet, target, nil))
		if response.Code != http.StatusUnauthorized || service.reads != 0 {
			t.Fatal("new route bypassed existing bearer authority")
		}
		request := httptest.NewRequest(http.MethodGet, target, nil)
		request.Header.Set("Authorization", "Bearer "+token)
		response = httptest.NewRecorder()
		mux.ServeHTTP(response, request)
		if response.Code != http.StatusOK || response.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("new native route unavailable or cacheable")
		}
		service.reads = 0
	}
	missing := httptest.NewRequest(http.MethodGet, "/v3/agent-sessions/missing-session/native-thread", nil)
	missing.Header.Set("Authorization", "Bearer "+token)
	missingResponse := httptest.NewRecorder()
	mux.ServeHTTP(missingResponse, missing)
	if missingResponse.Code != http.StatusNotFound || missingResponse.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("native unavailable history may be cached")
	}
	for _, version := range []int{7, 8, 10, 11, 12} {
		request := httptest.NewRequest(http.MethodGet, "/v"+strconv.Itoa(version)+"/agent-sessions/known-session/events", nil)
		request.Header.Set("Authorization", "Bearer "+token)
		response := httptest.NewRecorder()
		mux.ServeHTTP(response, request)
		seen := 0
		for _, line := range strings.Split(response.Body.String(), "\n") {
			if !strings.HasPrefix(line, "data: ") {
				continue
			}
			var event session.Event
			seen++
			if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &event); err != nil {
				t.Fatal(err)
			}
			if event.SchemaVersion != version || event.Payload.Native.Item.Id != "web-record" {
				t.Fatal("stream version or native ID changed")
			}
			if version >= 10 && event.Payload.Native.Item.Type != v5.WebSearch {
				t.Fatal("new search projection lost")
			}
			if version < 10 && (event.Payload.Native.Item.Type != v5.Unknown || event.Payload.Native.Item.WebSearch != nil) {
				t.Fatal("search fields escaped into a closed old contract")
			}
		}
		if response.Code != http.StatusOK || seen != 1 {
			t.Fatal("expected one native event")
		}
	}
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
