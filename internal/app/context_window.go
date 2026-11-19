package app

import (
	"context"
	"errors"
	"net/http"
	"time"

	wire "github.com/36Dge/yijie-agent-host/internal/contracts/contextwindow"
	"github.com/36Dge/yijie-agent-host/internal/session"
)

type contextWindowService interface {
	ReadContextWindow(context.Context, string, bool, bool) (wire.ContextWindowSnapshot, error)
}

func registerContextWindow(mux *http.ServeMux, h *sessionHandler) {
	s, ok := h.service.(contextWindowService)
	if !ok {
		return
	}
	mux.Handle("GET /v1/agent-sessions/{agent_session_id}/context-window", h.authorize(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		query := r.URL.Query()
		history, last := query.Get("include_history"), query.Get("include_last_observation")
		if (history != "" && history != "true" && history != "false") || (last != "" && last != "true" && last != "false") {
			writeNestedError(w, http.StatusBadRequest, "invalid_request", "Context window unavailable")
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()
		result, err := s.ReadContextWindow(ctx, r.PathValue("agent_session_id"), history == "true", last == "true")
		if err == nil {
			err = wire.Validate(result)
		}
		if err == nil {
			writeJSON(w, http.StatusOK, result)
			return
		}
		status, code := http.StatusServiceUnavailable, "unavailable"
		if errors.Is(err, session.ErrNotFound) {
			status, code = http.StatusNotFound, "not_found"
		}
		if errors.Is(err, session.ErrInvalidArgument) {
			status, code = http.StatusBadRequest, "invalid_request"
		}
		writeNestedError(w, status, code, "Context window unavailable")
	})))
}
