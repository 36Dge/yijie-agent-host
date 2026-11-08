package app

import (
	"context"
	"errors"
	"github.com/36Dge/yijie-agent-host/internal/codex"
	wire "github.com/36Dge/yijie-agent-host/internal/contracts/userinput"
	"github.com/36Dge/yijie-agent-host/internal/session"
	"io"
	"net/http"
	"time"
)

type userInputService interface {
	UserInputSnapshot(string) (wire.UserInputSnapshot, error)
	GetUserInput(string, string) (wire.UserInputRequest, error)
	TouchUserInput(string, string) (wire.UserInputRequest, error)
	AnswerUserInput(context.Context, string, string, wire.UserInputAnswerRequest) (wire.UserInputRequest, error)
}

func registerUserInput(mux *http.ServeMux, h *sessionHandler, enabled bool) {
	if !enabled {
		return
	}
	s, ok := h.service.(userInputService)
	if !ok {
		return
	}
	register := func(pattern string, fn http.HandlerFunc) {
		auth := h.authorize(fn)
		mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Cache-Control", "no-store")
			auth.ServeHTTP(w, r)
		})
	}
	register("GET /v1/agent-sessions/{agent_session_id}/user-input", func(w http.ResponseWriter, r *http.Request) {
		result, err := s.UserInputSnapshot(r.PathValue("agent_session_id"))
		writeUserInput(w, "UserInputSnapshot", result, err)
	})
	register("GET /v1/agent-sessions/{agent_session_id}/user-input/{request_id}", func(w http.ResponseWriter, r *http.Request) {
		result, err := s.GetUserInput(r.PathValue("agent_session_id"), r.PathValue("request_id"))
		writeUserInput(w, "UserInputRequest", result, err)
	})
	register("POST /v1/agent-sessions/{agent_session_id}/user-input/{request_id}/touch", func(w http.ResponseWriter, r *http.Request) {
		result, err := s.TouchUserInput(r.PathValue("agent_session_id"), r.PathValue("request_id"))
		writeUserInput(w, "UserInputRequest", result, err)
	})
	register("POST /v1/agent-sessions/{agent_session_id}/user-input/{request_id}/answer", func(w http.ResponseWriter, r *http.Request) {
		bytes, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 256<<10))
		var answer wire.UserInputAnswerRequest
		if err != nil || wire.Decode("UserInputAnswerRequest", bytes, &answer) != nil {
			writeUserInput(w, "", nil, codex.ErrUserInputInvalid)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()
		result, err := s.AnswerUserInput(ctx, r.PathValue("agent_session_id"), r.PathValue("request_id"), answer)
		writeUserInput(w, "UserInputRequest", result, err)
	})
}
func writeUserInput(w http.ResponseWriter, name string, value any, err error) {
	if err == nil && wire.Validate(name, value) != nil {
		err = session.ErrRuntimeRequest
	}
	if err == nil {
		writeJSON(w, http.StatusOK, value)
		return
	}
	status, code := http.StatusServiceUnavailable, "unavailable"
	switch {
	case errors.Is(err, codex.ErrUserInputNotFound), errors.Is(err, session.ErrNotFound):
		status, code = http.StatusNotFound, "not_found"
	case errors.Is(err, codex.ErrUserInputInvalid), errors.Is(err, session.ErrInvalidArgument):
		status, code = http.StatusBadRequest, "invalid_request"
	case errors.Is(err, codex.ErrUserInputConflict):
		status, code = http.StatusConflict, "conflict"
	}
	writeNestedError(w, status, code, "User input request unavailable")
}
