package app

import (
	"errors"
	"net/http"
	"os"

	recovery "github.com/36Dge/yijie-agent-host/internal/contracts/scheduledrecovery"
	"github.com/36Dge/yijie-agent-host/internal/session"
	"github.com/google/uuid"
)

type scheduledRecoveryService interface {
	LookupSessionMapping(string) (session.Record, error)
	LookupTurnOperation(string, string) (session.TurnOperation, error)
}

func scheduledRecoveryEnabled() bool {
	return os.Getenv("YIJIE_ENV") == "local" && os.Getenv("YIJIE_LOCAL_PROFILE") == "demo_fast"
}

func registerScheduledRecovery(mux *http.ServeMux, h *sessionHandler, nonce string) {
	service, ok := h.service.(scheduledRecoveryService)
	if !ok {
		return
	}
	var responder *uuid.UUID
	if isCanonicalUUID(nonce) {
		id := uuid.MustParse(nonce)
		responder = &id
	}
	register := func(route string, handler http.HandlerFunc) {
		protected := h.authorize(handler)
		mux.Handle(route, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Cache-Control", "no-store")
			protected.ServeHTTP(w, r)
		}))
	}
	register("GET /v1/tasks/{task_id}/agent-session-mapping", func(w http.ResponseWriter, r *http.Request) {
		taskID := r.PathValue("task_id")
		if !validRecoveryRequest(r, taskID) {
			writeRecoveryError(w, session.ErrInvalidArgument)
			return
		}
		record, err := service.LookupSessionMapping(taskID)
		if err != nil {
			writeRecoveryError(w, err)
			return
		}
		if record.TaskID != taskID || !isCanonicalUUID(record.AgentSessionID) || (record.CodexThreadID != "" && !isCanonicalUUID(record.CodexThreadID)) {
			writeRecoveryError(w, errRecoveryProjection)
			return
		}
		value := recovery.SessionMapping{
			TaskId: uuid.MustParse(taskID), AgentSessionId: uuid.MustParse(record.AgentSessionID),
			MappingState: recovery.Reserved, RespondingHostInstanceId: responder,
		}
		if record.CodexThreadID != "" {
			id := uuid.MustParse(record.CodexThreadID)
			value.CodexThreadId, value.MappingState = &id, recovery.Bound
		}
		writeJSON(w, http.StatusOK, value)
	})
	register("GET /v1/agent-sessions/{agent_session_id}/turn-operations/{operation_id}", func(w http.ResponseWriter, r *http.Request) {
		sessionID, operationID := r.PathValue("agent_session_id"), r.PathValue("operation_id")
		if !validRecoveryRequest(r, sessionID, operationID) {
			writeRecoveryError(w, session.ErrInvalidArgument)
			return
		}
		operation, err := service.LookupTurnOperation(sessionID, operationID)
		if err != nil {
			writeRecoveryError(w, err)
			return
		}
		state := recovery.OperationState(operation.State)
		if operation.SessionID != sessionID || operation.OperationID != operationID || !state.Valid() ||
			(state == recovery.Accepted && !isCanonicalUUID(operation.TurnID)) ||
			(state != recovery.Accepted && operation.TurnID != "") {
			writeRecoveryError(w, errRecoveryProjection)
			return
		}
		value := recovery.TurnOperationResult{
			AgentSessionId: uuid.MustParse(sessionID), OperationId: uuid.MustParse(operationID),
			State: state, RespondingHostInstanceId: responder,
		}
		if state == recovery.Accepted {
			id := uuid.MustParse(operation.TurnID)
			value.TurnId = &id
		}
		writeJSON(w, http.StatusOK, value)
	})
}

var errRecoveryProjection = errors.New("recovery projection unavailable")

func validRecoveryRequest(r *http.Request, ids ...string) bool {
	if r.URL.RawQuery != "" || r.URL.ForceQuery || r.ContentLength != 0 || len(r.TransferEncoding) != 0 {
		return false
	}
	for _, id := range ids {
		if !isCanonicalUUID(id) {
			return false
		}
	}
	return true
}

func writeRecoveryError(w http.ResponseWriter, err error) {
	status, code, message := http.StatusServiceUnavailable, recovery.RecoveryUnavailable, "recovery facts are unavailable"
	if errors.Is(err, session.ErrInvalidArgument) {
		status, code, message = http.StatusBadRequest, recovery.InvalidRequest, "query requires canonical IDs and no query or body parameters"
	} else if errors.Is(err, session.ErrNotFound) {
		status, code, message = http.StatusNotFound, recovery.RecoveryRecordNotFound, "no current recovery record; prior execution remains unknown"
	}
	var value recovery.RecoveryError
	value.Error.Code, value.Error.Message = code, message
	writeJSON(w, status, value)
}
