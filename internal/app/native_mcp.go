package app

import (
	"context"
	"errors"
	"net/http"

	"github.com/36Dge/yijie-agent-host/internal/codex"
	contract "github.com/36Dge/yijie-agent-host/internal/contracts/runtimepermissionsv2"
	"github.com/36Dge/yijie-agent-host/internal/session"
	"github.com/google/uuid"
)

func registerNativeMcpPermissions(mux *http.ServeMux, h *sessionHandler, service permissionSessionService) {
	register := func(path string, handler http.HandlerFunc) { mux.Handle(path, h.authorize(handler)) }
	register("GET /v2/agent-sessions/{agent_session_id}/runtime-approvals", func(w http.ResponseWriter, r *http.Request) {
		values, err := service.ListRuntimeApprovals(r.PathValue("agent_session_id"))
		if err != nil {
			writeSessionError(w, err)
			return
		}
		response := contract.RuntimeApprovalSnapshot{Requests: make([]contract.RuntimeApproval, 0, len(values))}
		for _, value := range values {
			response.Requests = append(response.Requests, approvalResponseV2(value))
		}
		w.Header().Set("Cache-Control", "no-store")
		writeJSON(w, http.StatusOK, response)
	})
	register("POST /v2/agent-sessions/{agent_session_id}/runtime-approvals/{approval_id}", func(w http.ResponseWriter, r *http.Request) {
		var request contract.RuntimeApprovalDecision
		if err := decodeRequest(w, r, &request); err != nil || (request.Decision != "approve_once" && request.Decision != "reject" && request.Decision != "cancel") {
			writeSessionError(w, session.ErrInvalidArgument)
			return
		}
		value, err := service.DecideRuntimeApproval(r.Context(), r.PathValue("agent_session_id"), r.PathValue("approval_id"), string(request.Decision))
		if err != nil {
			writeSessionError(w, err)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		writeJSON(w, http.StatusOK, approvalResponseV2(value))
	})
	if prepare, ok := h.service.(interface {
		PrepareMcpPermissionScope(context.Context, codex.PermissionMode) (bool, bool, error)
	}); ok {
		register("POST /v2/runtime-mcp/permission-scope", func(w http.ResponseWriter, r *http.Request) {
			var request contract.McpPermissionScopeRequest
			if err := decodeRequest(w, r, &request); err != nil || !codex.PermissionMode(request.Mode).Valid() {
				writeSessionError(w, session.ErrInvalidArgument)
				return
			}
			active, restarted, err := prepare.PrepareMcpPermissionScope(r.Context(), codex.PermissionMode(request.Mode))
			if err != nil {
				if errors.Is(err, codex.ErrMcpTurnActive) {
					writeNestedError(w, http.StatusConflict, "mcp_turn_active", "请先正常结束活跃任务和待决审批。")
					return
				}
				writeNestedError(w, http.StatusServiceUnavailable, "mcp_scope_unavailable", "请先正常结束活跃任务；无法确认工具停用时，权限模式保持不变。")
				return
			}
			w.Header().Set("Cache-Control", "no-store")
			writeJSON(w, http.StatusOK, contract.McpPermissionScopeResult{Active: active, RuntimeRestarted: restarted})
		})
	}
}

func approvalResponseV2(value codex.RuntimeApproval) contract.RuntimeApproval {
	id, _ := uuid.Parse(value.ID)
	result := contract.RuntimeApproval{Id: id, Kind: contract.RuntimeApprovalKind(value.Kind), Summary: value.Summary, Scope: value.Scope, Reason: value.Reason, Status: contract.RuntimeApprovalStatus(value.Status)}
	if value.Mcp != nil {
		result.Mcp = &contract.McpApprovalScope{Server: contract.McpApprovalScopeServer(value.Mcp.Server), Tool: contract.McpApprovalScopeTool(value.Mcp.Tool), Asin: value.Mcp.ASIN, Marketplace: contract.McpApprovalScopeMarketplace(value.Mcp.Marketplace)}
	}
	return result
}
