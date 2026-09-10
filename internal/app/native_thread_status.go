package app

import (
	"context"
	"net/http"

	native "github.com/36Dge/yijie-agent-host/internal/contracts/nativeconversationv2"
)

func registerNativeThreadStatus(mux *http.ServeMux, h *sessionHandler) {
	service, ok := h.service.(interface {
		ReadNativeThreadStatusV2(context.Context, string) (native.NativeThreadStatusSnapshot, error)
	})
	if !ok {
		return
	}
	handler := h.authorize(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		value, err := service.ReadNativeThreadStatusV2(r.Context(), r.PathValue("agent_session_id"))
		if err != nil {
			writeSessionError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, value)
	}))
	mux.Handle("GET /v2/agent-sessions/{agent_session_id}/native-thread-status", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		handler.ServeHTTP(w, r)
	}))
}
