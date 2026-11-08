package app

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	wire "github.com/36Dge/yijie-agent-host/internal/contracts/contextwindow"
)

type contextWindowRouteService struct {
	SessionService
	session string
	history bool
	calls   int
}

func (s *contextWindowRouteService) ReadContextWindow(_ context.Context, id string, history bool) (wire.ContextWindowSnapshot, error) {
	s.session, s.history = id, history
	s.calls++
	return wire.ContextWindowSnapshot{SchemaVersion: 1, Compactions: []wire.ContextCompaction{}}, nil
}
func TestContextWindowReadRouteUsesExistingAuthorization(t *testing.T) {
	s := &contextWindowRouteService{}
	mux := http.NewServeMux()
	registerContextWindow(mux, &sessionHandler{service: s, apiToken: "ordinary-owner-token"})
	for _, authorized := range []bool{false, true} {
		r := httptest.NewRequest("GET", "/v1/agent-sessions/10000000-0000-4000-8000-000000000001/context-window?include_history=true", nil)
		if authorized {
			r.Header.Set("Authorization", "Bearer ordinary-owner-token")
		}
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		if !authorized {
			if w.Code != 401 || s.calls != 0 {
				t.Fatal("read lacked authorization")
			}
			continue
		}
		var value wire.ContextWindowSnapshot
		if w.Code != 200 || wire.Decode("ContextWindowSnapshot", w.Body.Bytes(), &value) != nil || !s.history || s.calls != 1 || w.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("read route failed", w.Code)
		}
	}
}
