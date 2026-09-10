package app

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	native "github.com/36Dge/yijie-agent-host/internal/contracts/nativeconversationv2"
	"github.com/36Dge/yijie-agent-host/internal/session"
)

type nativeStatusRouteService struct {
	SessionService
	reads int
	err   error
}

func (s *nativeStatusRouteService) ReadNativeThreadStatusV2(_ context.Context, id string) (native.NativeThreadStatusSnapshot, error) {
	s.reads++
	if s.err != nil {
		return native.NativeThreadStatusSnapshot{}, s.err
	}
	return native.NativeThreadStatusSnapshot{SchemaVersion: native.NativeThreadStatusSnapshotSchemaVersionN2, Source: native.NativeThreadStatusSnapshotSourceRuntimeRead, ThreadId: "native-thread", Status: native.NativeThreadStatusSnapshotStatusIdle}, nil
}

func TestFEAT144NativeThreadStatusRoutePreservesAuthorityAndSafeErrors(t *testing.T) {
	service := &nativeStatusRouteService{}
	mux := http.NewServeMux()
	const token = "public-local-status-placeholder"
	registerNativeConversation(mux, &sessionHandler{service: service, apiToken: token})
	const target = "/v2/agent-sessions/known-session/native-thread-status"
	request := func(authorized bool) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodGet, target, nil)
		if authorized {
			r.Header.Set("Authorization", "Bearer "+token)
		}
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		if w.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("native status or error may be cached")
		}
		return w
	}
	if w := request(false); w.Code != http.StatusUnauthorized || service.reads != 0 {
		t.Fatal("status route bypassed owner authority")
	}
	for _, tc := range []struct {
		err    error
		status int
	}{
		{nil, http.StatusOK},
		{session.ErrInvalidArgument, http.StatusBadRequest},
		{session.ErrNotFound, http.StatusNotFound},
		{session.ErrSessionNotUsable, http.StatusConflict},
		{session.ErrRuntimeRequest, http.StatusBadGateway},
		{fmt.Errorf("ordinary internal detail"), http.StatusInternalServerError},
	} {
		service.err = tc.err
		w := request(true)
		if w.Code != tc.status || strings.Contains(w.Body.String(), "detail") {
			t.Fatal("native status error mapping changed or exposed internal detail")
		}
		if tc.err == nil && w.Body.String() != "{\"schema_version\":2,\"source\":\"runtime_read\",\"status\":\"idle\",\"thread_id\":\"native-thread\"}\n" {
			t.Fatal("native status projection emitted unexpected fields")
		}
	}
}
