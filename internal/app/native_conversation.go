package app

import (
	"context"
	native "github.com/36Dge/yijie-agent-host/internal/contracts/nativeconversationv6"
	"github.com/36Dge/yijie-agent-host/internal/session"
	"net/http"
)

type nativeConversationService interface {
	ReadNativeThreadV6(context.Context, string) (native.NativeThreadSnapshot, error)
	SubscribeNativeEvents(string, string, uint64) (string, []session.Event, <-chan session.Event, func(), error)
}

// FEAT-167: the owner retired old native display endpoints. Runtime execution
// and artifact/control-plane endpoints are independent and remain unchanged.
func registerNativeConversation(mux *http.ServeMux, h *sessionHandler) {
	registerNativeThreadStatus(mux, h)
	service, ok := h.service.(nativeConversationService)
	if !ok {
		return
	}
	mux.Handle("GET /v6/agent-sessions/{agent_session_id}/native-thread", h.authorize(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		value, err := service.ReadNativeThreadV6(r.Context(), r.PathValue("agent_session_id"))
		if err != nil {
			writeSessionError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, value)
	})))
	mux.Handle("GET /v13/agent-sessions/{agent_session_id}/events", h.authorize(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Yijie-Event-Schema-Version", "13")
		h.streamEvents(w, r, service.SubscribeNativeEvents)
	})))
}
