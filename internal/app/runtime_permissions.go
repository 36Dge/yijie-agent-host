package app

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/36Dge/yijie-agent-host/internal/codex"
	agenthostcontract "github.com/36Dge/yijie-agent-host/internal/contracts"
	contract "github.com/36Dge/yijie-agent-host/internal/contracts/runtimepermissions"
	"github.com/36Dge/yijie-agent-host/internal/session"
	"github.com/google/uuid"
)

type permissionSessionService interface {
	StartTurnV2(context.Context, session.StartTurnV2Input) (codex.TurnInfo, error)
	ListRuntimeApprovals(string) ([]codex.RuntimeApproval, error)
	DecideRuntimeApproval(context.Context, string, string, string) (codex.RuntimeApproval, error)
}

func registerRuntimePermissions(mux *http.ServeMux, h *sessionHandler, enabled bool) {
	if !enabled {
		return
	}
	service, ok := h.service.(permissionSessionService)
	if !ok {
		return
	}
	register := func(pattern string, handler http.HandlerFunc) { mux.Handle(pattern, h.authorize(handler)) }
	register("POST /v1/agent-sessions/{agent_session_id}/permission-turns", func(w http.ResponseWriter, r *http.Request) {
		var request contract.PermissionTurnRequest
		if err := decodeRequestWithLimit(w, r, &request, 16<<20); err != nil || !codex.PermissionMode(request.PermissionMode).Valid() {
			writeSessionError(w, session.ErrInvalidArgument)
			return
		}
		// Both generated representations reference the SAME StartTurnV2Request
		// authority. Convert at this version boundary, then reuse its validator.
		encoded, err := json.Marshal(request.Turn)
		var turn agenthostcontract.StartTurnV2Request
		if err != nil || json.Unmarshal(encoded, &turn) != nil {
			writeSessionError(w, session.ErrInvalidArgument)
			return
		}
		blocks, err := mapTurnV2ContentBlocks(turn.ContentBlocks)
		if err != nil {
			writeSessionError(w, session.ErrInvalidArgument)
			return
		}
		result, err := service.StartTurnV2(r.Context(), session.StartTurnV2Input{AgentSessionID: r.PathValue("agent_session_id"), OperationID: turn.OperationId.String(), ContentBlocks: blocks, ReasoningEffort: reasoningEffortV2(turn.ReasoningEffort), Trace: traceContext(turn.TraceId, turn.RequestId, turn.TenantId, turn.UserId), PermissionMode: codex.PermissionMode(request.PermissionMode)})
		if err != nil {
			writeSessionError(w, err)
			return
		}
		id, err := uuid.Parse(result.ID)
		if err != nil {
			writeSessionError(w, session.ErrRuntimeRequest)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		writeJSON(w, http.StatusAccepted, agenthostcontract.StartTurnResponse{TurnId: id})
	})
	register("GET /v1/agent-sessions/{agent_session_id}/runtime-approvals", func(w http.ResponseWriter, r *http.Request) {
		values, err := service.ListRuntimeApprovals(r.PathValue("agent_session_id"))
		if err != nil {
			writeSessionError(w, err)
			return
		}
		response := contract.RuntimeApprovalSnapshot{Requests: make([]contract.RuntimeApproval, 0, len(values))}
		for _, v := range values {
			response.Requests = append(response.Requests, approvalResponse(v))
		}
		w.Header().Set("Cache-Control", "no-store")
		writeJSON(w, http.StatusOK, response)
	})
	register("POST /v1/agent-sessions/{agent_session_id}/runtime-approvals/{approval_id}", func(w http.ResponseWriter, r *http.Request) {
		var request contract.RuntimeApprovalDecision
		if err := decodeRequest(w, r, &request); err != nil || (request.Decision != "approve_once" && request.Decision != "reject") {
			writeSessionError(w, session.ErrInvalidArgument)
			return
		}
		value, err := service.DecideRuntimeApproval(r.Context(), r.PathValue("agent_session_id"), r.PathValue("approval_id"), string(request.Decision))
		if err != nil {
			writeSessionError(w, err)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		writeJSON(w, http.StatusOK, approvalResponse(value))
	})
}

func approvalResponse(value codex.RuntimeApproval) contract.RuntimeApproval {
	id, _ := uuid.Parse(value.ID)
	return contract.RuntimeApproval{Id: id, Kind: contract.RuntimeApprovalKind(value.Kind), Summary: value.Summary, Scope: value.Scope, Reason: value.Reason, Status: contract.RuntimeApprovalStatus(value.Status)}
}
