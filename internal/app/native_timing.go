package app

import (
	"context"
	"errors"
	"net/http"

	timing "github.com/36Dge/yijie-agent-host/internal/contracts/nativetiming"
	"github.com/36Dge/yijie-agent-host/internal/session"
)

func registerNativeTiming(mux *http.ServeMux, h *sessionHandler) {
	service, ok := h.service.(interface {
		ReadNativeTurnTiming(context.Context, string, string) (timing.NativeTurnTiming, error)
	})
	if !ok {
		return
	}
	protected := h.authorize(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sid, tid := r.PathValue("agent_session_id"), r.PathValue("runtime_turn_id")
		if !validRecoveryRequest(r, sid, tid) {
			writeTimingError(w, timing.InvalidRequest)
			return
		}
		result, err := service.ReadNativeTurnTiming(r.Context(), sid, tid)
		if err != nil {
			code := timing.NativeTimingUnavailable
			var typed session.NativeTimingError
			if errors.As(err, &typed) && typed.Code.Valid() {
				code = typed.Code
			}
			writeTimingError(w, code)
			return
		}
		writeJSON(w, http.StatusOK, result)
	}))
	mux.Handle("GET /v1/agent-sessions/{agent_session_id}/turns/{runtime_turn_id}/timing", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		protected.ServeHTTP(w, r)
	}))
}

func writeTimingError(w http.ResponseWriter, code timing.TimingErrorCode) {
	status := http.StatusServiceUnavailable
	if code == timing.InvalidRequest {
		status = http.StatusBadRequest
	} else if code == timing.SessionNotFound || code == timing.TurnNotFound {
		status = http.StatusNotFound
	}
	var value timing.TimingError
	value.Error.Code, value.Error.Message = code, "native turn timing is unavailable; no execution inference"
	writeJSON(w, status, value)
}
