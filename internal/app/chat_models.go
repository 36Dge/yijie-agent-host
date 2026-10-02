package app

import (
	"context"
	"errors"
	"github.com/36Dge/yijie-agent-host/internal/codex"
	wire "github.com/36Dge/yijie-agent-host/internal/contracts/chatmodels"
	"github.com/36Dge/yijie-agent-host/internal/session"
	"io"
	"net/http"
	"strings"
)

type chatModelService interface {
	ChatModelsEnabled() bool
	ChatModelCatalog() wire.Catalog
	ReadChatModel(string) (wire.Selection, error)
	SelectChatModel(context.Context, string, wire.SelectRequest) (wire.Selection, error)
}

func modelReply(w http.ResponseWriter, name string, value any, err error) {
	w.Header().Set("Cache-Control", "no-store")
	if err == nil && wire.Validate(name, value) == nil {
		writeJSON(w, 200, value)
		return
	}
	status, code := 503, "runtime_unavailable"
	switch {
	case errors.Is(err, session.ErrInvalidArgument):
		status, code = 400, "invalid_request"
	case errors.Is(err, session.ErrNotFound):
		status, code = 404, "not_found"
	case errors.Is(err, session.ErrTurnActive):
		status, code = 409, "busy"
	case errors.Is(err, session.ErrModelRevision):
		status, code = 409, "revision_conflict"
	case errors.Is(err, session.ErrTurnOperationConflict):
		status, code = 409, "request_conflict"
	case errors.Is(err, session.ErrModelUnavailable):
		code = "model_unavailable"
	case errors.Is(err, session.ErrModelUnknown):
		code = "selection_unknown"
	}
	writeJSON(w, status, wire.Error{SchemaVersion: 1, Code: wire.ErrorCode(code)})
}
func registerChatModels(mux *http.ServeMux, h *sessionHandler, enabled bool) {
	s, ok := h.service.(chatModelService)
	if !enabled || !ok {
		return
	}
	register := func(p string, fn http.HandlerFunc) { mux.Handle(p, h.authorize(fn)) }
	register("GET /v1/model-chat/catalog", func(w http.ResponseWriter, r *http.Request) {
		if !validRecoveryRequest(r) {
			modelReply(w, "Catalog", nil, session.ErrInvalidArgument)
			return
		}
		modelReply(w, "Catalog", s.ChatModelCatalog(), nil)
	})
	register("GET /v1/model-chat/agent-sessions/{agent_session_id}/model", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("agent_session_id")
		if !validRecoveryRequest(r, id) {
			modelReply(w, "Selection", nil, session.ErrInvalidArgument)
			return
		}
		v, e := s.ReadChatModel(id)
		modelReply(w, "Selection", v, e)
	})
	register("POST /v1/model-chat/agent-sessions/{agent_session_id}/model", func(w http.ResponseWriter, r *http.Request) {
		var input wire.SelectRequest
		b, e := io.ReadAll(http.MaxBytesReader(w, r.Body, 4096))
		if e != nil || r.URL.RawQuery != "" || wire.Decode("SelectRequest", b, &input) != nil {
			modelReply(w, "Selection", nil, session.ErrInvalidArgument)
			return
		}
		v, e := s.SelectChatModel(r.Context(), r.PathValue("agent_session_id"), input)
		modelReply(w, "Selection", v, e)
	})
	// Source payloads, authentication, scopes and validators stay with the
	// existing handlers. Only these exact opt-in routes inject a model profile.
	for _, route := range []struct{ method, path, legacy string }{
		{"POST", "/v1/model-chat/tasks/{task_id}/agent-sessions", "/v1/tasks/{task_id}/agent-sessions"},
		{"POST", "/v1/model-chat/agent-sessions/{agent_session_id}/resume", "/v1/agent-sessions/{agent_session_id}/resume"},
		{"GET", "/v1/model-chat/agent-sessions/{agent_session_id}", "/v1/agent-sessions/{agent_session_id}"},
		{"POST", "/v1/model-chat/agent-sessions/{agent_session_id}/turns", "/v2/agent-sessions/{agent_session_id}/turns"},
		{"POST", "/v1/model-chat/agent-sessions/{agent_session_id}/permission-turns", "/v1/agent-sessions/{agent_session_id}/permission-turns"},
		{"POST", "/v1/model-chat/scheduled-plan-draft-sessions", "/v1/scheduled-plan-draft-sessions"},
		{"POST", "/v1/model-chat/scheduled-plan-draft-sessions/{agent_session_id}/resume", "/v1/scheduled-plan-draft-sessions/{agent_session_id}/resume"},
		{"POST", "/v1/model-chat/scheduled-plan-draft-sessions/{agent_session_id}/turns", "/v1/scheduled-plan-draft-sessions/{agent_session_id}/turns"},
	} {
		register(route.method+" "+route.path, func(w http.ResponseWriter, r *http.Request) {
			id := r.Header.Get("X-Yijie-Model-Profile")
			if route.method == "POST" {
				if _, ok := codex.ModelProfile(id); !ok || !s.ChatModelsEnabled() {
					modelReply(w, "Error", nil, session.ErrModelUnavailable)
					return
				}
			}
			if r.URL.RawQuery != "" {
				modelReply(w, "Error", nil, session.ErrInvalidArgument)
				return
			}
			clone := r.Clone(codex.WithModelProfile(r.Context(), id))
			target := route.legacy
			for _, param := range []string{"task_id", "agent_session_id"} {
				target = strings.ReplaceAll(target, "{"+param+"}", r.PathValue(param))
			}
			clone.URL.Path = target
			clone.URL.RawPath = ""
			mux.ServeHTTP(w, clone)
		})
	}
}
