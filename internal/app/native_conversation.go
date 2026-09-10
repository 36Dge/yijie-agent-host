package app

import (
	"context"
	"errors"
	native "github.com/36Dge/yijie-agent-host/internal/contracts/nativeconversation"
	nativev2 "github.com/36Dge/yijie-agent-host/internal/contracts/nativeconversationv2"
	"github.com/36Dge/yijie-agent-host/internal/session"
	"net/http"
)

type nativeConversationService interface {
	ReadNativeThread(context.Context, string) (native.NativeThreadSnapshot, error)
	SubscribeNativeEvents(string, string, uint64) (string, []session.Event, <-chan session.Event, func(), error)
}

func registerNativeConversation(mux *http.ServeMux, h *sessionHandler) {
	service, ok := h.service.(nativeConversationService)
	if !ok {
		return
	}
	mux.Handle("GET /v1/agent-sessions/{agent_session_id}/native-thread", h.authorize(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		value, err := service.ReadNativeThread(r.Context(), r.PathValue("agent_session_id"))
		if err != nil {
			if errors.Is(err, session.ErrNotFound) {
				writeNestedError(w, 404, "session_not_found", "session not found")
			} else {
				writeNestedError(w, 503, "native_history_unavailable", "native history is unavailable")
			}
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		writeJSON(w, http.StatusOK, value)
	})))
	if current, ok := h.service.(interface {
		ReadNativeThreadV2(context.Context, string) (nativev2.NativeThreadSnapshot, error)
	}); ok {
		mux.Handle("GET /v2/agent-sessions/{agent_session_id}/native-thread", h.authorize(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			value, err := current.ReadNativeThreadV2(r.Context(), r.PathValue("agent_session_id"))
			if err != nil {
				writeSessionError(w, err)
				return
			}
			w.Header().Set("Cache-Control", "no-store")
			writeJSON(w, http.StatusOK, value)
		})))
		mux.Handle("GET /v8/agent-sessions/{agent_session_id}/events", h.authorize(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("X-Yijie-Event-Schema-Version", "8")
			h.streamEvents(w, r, service.SubscribeNativeEvents)
		})))
	}
	mux.Handle("GET /v7/agent-sessions/{agent_session_id}/events", h.authorize(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Yijie-Event-Schema-Version", "7")
		h.streamEvents(w, r, service.SubscribeNativeEvents)
	})))
}
