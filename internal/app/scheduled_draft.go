package app

import (
	"context"
	"errors"
	"github.com/36Dge/yijie-agent-host/internal/security"
	"io"
	"net/http"

	wire "github.com/36Dge/yijie-agent-host/internal/contracts/scheduledraft"
	"github.com/36Dge/yijie-agent-host/internal/session"
)

type scheduledDraftService interface {
	ScheduledDraftCapability() wire.Capability
	ReadScheduledDraftMapping(string) (wire.RecoveryMapping, error)
	CreateScheduledDraft(context.Context, wire.CreateRequest) (wire.SessionReceipt, error)
	ResumeScheduledDraft(context.Context, string, wire.ResumeRequest) (wire.SessionReceipt, error)
	StartScheduledDraftTurn(context.Context, string, wire.TurnRequest) (wire.TurnReceipt, error)
}

func draftInput[T any](w http.ResponseWriter, r *http.Request, name string) (T, error) {
	var value T
	if r.URL.RawQuery != "" || r.URL.ForceQuery {
		return value, session.ErrInvalidArgument
	}
	body, e := io.ReadAll(http.MaxBytesReader(w, r.Body, 65536))
	if e != nil || wire.Decode(name, body, &value) != nil {
		return value, session.ErrInvalidArgument
	}
	return value, nil
}
func draftReply(w http.ResponseWriter, name string, value any, err error) {
	if err == nil && wire.Validate(name, value) != nil {
		err = session.ErrSessionNotUsable
	}
	if err == nil {
		writeJSON(w, http.StatusOK, value)
		return
	}
	status, code := http.StatusServiceUnavailable, "storage_unavailable"
	switch {
	case errors.Is(err, session.ErrInvalidArgument):
		status, code = http.StatusBadRequest, "invalid_request"
	case errors.Is(err, session.ErrNotFound):
		status, code = http.StatusNotFound, "not_found"
	case errors.Is(err, session.ErrTurnOperationConflict) || errors.Is(err, session.ErrTaskExists):
		status, code = http.StatusConflict, "request_conflict"
	case errors.Is(err, session.ErrDraftPurpose):
		status, code = http.StatusConflict, "purpose_conflict"
	case errors.Is(err, session.ErrDraftStorageDisabled):
		code = "storage_disabled"
	case errors.Is(err, session.ErrDraftPolicy):
		code = "policy_unqualified"
	case errors.Is(err, session.ErrTurnActive):
		status, code = http.StatusConflict, "busy"
	case errors.Is(err, session.ErrTurnOperationPending) || errors.Is(err, session.ErrRuntimeRequest):
		code = "operation_unknown"
	}
	writeJSON(w, status, wire.Error{SchemaVersion: 1, Code: wire.ErrorCode(code)})
}
func registerScheduledDraft(mux *http.ServeMux, h *sessionHandler, nonce string) {
	s, ok := h.service.(scheduledDraftService)
	if !ok {
		return
	}
	register := func(route string, handler http.HandlerFunc) {
		mux.Handle(route, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Cache-Control", "no-store")
			if !security.TokenMatches(h.apiToken, r.Header.Get("Authorization")) {
				writeJSON(w, http.StatusUnauthorized, wire.Error{SchemaVersion: 1, Code: "unauthorized"})
				return
			}
			handler.ServeHTTP(w, r)
		}))
	}
	register("GET /v1/scheduled-plan-draft-session-mappings/{task_id}", func(w http.ResponseWriter, r *http.Request) {
		task := r.PathValue("task_id")
		if !validRecoveryRequest(r, task) {
			draftReply(w, "RecoveryMapping", nil, session.ErrInvalidArgument)
			return
		}
		value, err := s.ReadScheduledDraftMapping(task)
		if isCanonicalUUID(nonce) {
			value.RespondingHostInstanceId = &nonce
		}
		draftReply(w, "RecoveryMapping", value, err)
	})
	register("GET /v1/scheduled-plan-draft-capability", func(w http.ResponseWriter, r *http.Request) {
		if !validRecoveryRequest(r) {
			draftReply(w, "Capability", nil, session.ErrInvalidArgument)
			return
		}
		draftReply(w, "Capability", s.ScheduledDraftCapability(), nil)
	})
	register("POST /v1/scheduled-plan-draft-sessions", func(w http.ResponseWriter, r *http.Request) {
		v, e := draftInput[wire.CreateRequest](w, r, "CreateRequest")
		if e != nil {
			draftReply(w, "SessionReceipt", nil, e)
			return
		}
		out, e := s.CreateScheduledDraft(r.Context(), v)
		draftReply(w, "SessionReceipt", out, e)
	})
	register("POST /v1/scheduled-plan-draft-sessions/{agent_session_id}/resume", func(w http.ResponseWriter, r *http.Request) {
		v, e := draftInput[wire.ResumeRequest](w, r, "ResumeRequest")
		if e != nil {
			draftReply(w, "SessionReceipt", nil, e)
			return
		}
		out, e := s.ResumeScheduledDraft(r.Context(), r.PathValue("agent_session_id"), v)
		draftReply(w, "SessionReceipt", out, e)
	})
	register("POST /v1/scheduled-plan-draft-sessions/{agent_session_id}/turns", func(w http.ResponseWriter, r *http.Request) {
		v, e := draftInput[wire.TurnRequest](w, r, "TurnRequest")
		if e != nil {
			draftReply(w, "TurnReceipt", nil, e)
			return
		}
		out, e := s.StartScheduledDraftTurn(r.Context(), r.PathValue("agent_session_id"), v)
		draftReply(w, "TurnReceipt", out, e)
	})
}
