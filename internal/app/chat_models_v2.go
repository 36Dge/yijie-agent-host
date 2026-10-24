package app

import (
	"errors"
	"net/http"
	"strings"

	"github.com/36Dge/yijie-agent-host/internal/codex"
	wire "github.com/36Dge/yijie-agent-host/internal/contracts/chatmodelsv2"
	"github.com/36Dge/yijie-agent-host/internal/security"
	"github.com/36Dge/yijie-agent-host/internal/session"
)

type chatModelV2Service interface {
	ChatModelsV2Enabled() bool
	ChatModelCatalogV2() wire.Catalog
	ReadChatModelV2(string) (wire.Selection, error)
}

func modelReplyV2(w http.ResponseWriter, name string, value any, err error) {
	w.Header().Set("Cache-Control", "no-store")
	if err == nil && wire.Validate(name, value) == nil {
		writeJSON(w, 200, value)
		return
	}
	status, code := 503, wire.ErrorCodeRuntimeUnavailable
	switch {
	case errors.Is(err, session.ErrInvalidArgument):
		status, code = 400, wire.ErrorCodeInvalidRequest
	case errors.Is(err, session.ErrNotFound):
		status, code = 404, wire.ErrorCodeNotFound
	case errors.Is(err, session.ErrTurnActive):
		status, code = 409, wire.ErrorCodeBusy
	case errors.Is(err, session.ErrModelRevision):
		status, code = 409, wire.ErrorCodeRevisionConflict
	case errors.Is(err, session.ErrModelUnavailable):
		code = wire.ErrorCodeModelUnavailable
	case errors.Is(err, session.ErrModelUnknown):
		status, code = 409, wire.ErrorCodeSelectionUnknown
	}
	writeJSON(w, status, wire.Error{SchemaVersion: 2, Code: code})
}
func registerChatModelsV2(mux *http.ServeMux, h *sessionHandler) {
	s, ok := h.service.(chatModelV2Service)
	if !ok {
		return
	}
	register := func(path string, fn http.HandlerFunc) { mux.Handle(path, h.authorize(fn)) }
	registerModel := func(path string, fn http.HandlerFunc) {
		mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
			if !security.TokenMatches(h.apiToken, r.Header.Get("Authorization")) {
				w.Header().Set("Cache-Control", "no-store")
				writeJSON(w, 401, wire.Error{SchemaVersion: 2, Code: wire.ErrorCodeUnauthorized})
				return
			}
			fn(w, r)
		})
	}
	registerModel("GET /v2/model-chat/catalog", func(w http.ResponseWriter, r *http.Request) {
		if !validRecoveryRequest(r) {
			modelReplyV2(w, "Catalog", nil, session.ErrInvalidArgument)
			return
		}
		if !s.ChatModelsV2Enabled() {
			modelReplyV2(w, "Catalog", nil, session.ErrModelUnavailable)
			return
		}
		modelReplyV2(w, "Catalog", s.ChatModelCatalogV2(), nil)
	})
	registerModel("GET /v2/model-chat/agent-sessions/{agent_session_id}/model", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("agent_session_id")
		if !validRecoveryRequest(r, id) {
			modelReplyV2(w, "Selection", nil, session.ErrInvalidArgument)
			return
		}
		v, err := s.ReadChatModelV2(id)
		modelReplyV2(w, "Selection", v, err)
	})
	// The walking skeleton exposes ordinary fresh-session routes only. Model
	// mutation/recovery and draft routes remain unavailable until their slices.
	for _, route := range []struct{ method, path, legacy string }{
		{"POST", "/v2/model-chat/tasks/{task_id}/agent-sessions", "/v1/tasks/{task_id}/agent-sessions"},
		{"GET", "/v2/model-chat/agent-sessions/{agent_session_id}", "/v1/agent-sessions/{agent_session_id}"},
		{"POST", "/v2/model-chat/agent-sessions/{agent_session_id}/resume", "/v1/agent-sessions/{agent_session_id}/resume"},
		{"POST", "/v2/model-chat/agent-sessions/{agent_session_id}/turns", "/v2/agent-sessions/{agent_session_id}/turns"},
		{"POST", "/v2/model-chat/agent-sessions/{agent_session_id}/permission-turns", "/v1/agent-sessions/{agent_session_id}/permission-turns"},
	} {
		register(route.method+" "+route.path, func(w http.ResponseWriter, r *http.Request) {
			if r.URL.RawQuery != "" {
				modelReplyV2(w, "Error", nil, session.ErrInvalidArgument)
				return
			}
			id := r.Header.Get("X-Yijie-Model-Profile")
			if route.method == "POST" {
				if !s.ChatModelsV2Enabled() {
					modelReplyV2(w, "Error", nil, session.ErrModelUnavailable)
					return
				}
				if len(r.Header.Values("X-Yijie-Model-Profile")) != 1 {
					modelReplyV2(w, "Error", nil, session.ErrInvalidArgument)
					return
				}
				if id == "kimi-k3-max-v1" {
					w.Header().Set("Cache-Control", "no-store")
					writeJSON(w, 409, wire.Error{SchemaVersion: 2, Code: wire.ErrorCodeModelRetired})
					return
				}
				if wire.Validate("ActiveProfileId", id) != nil {
					modelReplyV2(w, "Error", nil, session.ErrInvalidArgument)
					return
				}
			}
			clone := r.Clone(codex.WithModelProfile(r.Context(), id))
			target := route.legacy
			for _, key := range []string{"task_id", "agent_session_id"} {
				target = strings.ReplaceAll(target, "{"+key+"}", r.PathValue(key))
			}
			clone.URL.Path, clone.URL.RawPath = target, ""
			mux.ServeHTTP(w, clone)
		})
	}
}
