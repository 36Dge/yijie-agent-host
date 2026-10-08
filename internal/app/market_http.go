package app

import (
	"context"
	"encoding/json"
	"io"
	"mime"
	"net/http"

	wire "github.com/36Dge/yijie-agent-host/internal/contracts/markethost"
	"github.com/36Dge/yijie-agent-host/internal/security"
	"github.com/36Dge/yijie-agent-host/internal/session"
)

type marketHTTPService interface {
	MarketEnabled() bool
	SubmitMarket(context.Context, wire.SubmitPayload) (wire.SubmissionReceipt, error)
	ReadMarketOperation(string, string) (wire.SubmissionReceipt, error)
	ReadMarketApprovals(context.Context, string) (wire.ApprovalSnapshot, error)
	ReadMarketTools(context.Context, string, string) (wire.ToolSnapshot, error)
}

type marketValidated interface{ Validate() error }

func marketErrorReply(w http.ResponseWriter, requestID *wire.CanonicalId, code wire.ErrorCode) {
	status := http.StatusServiceUnavailable
	switch code {
	case wire.ErrorCodeInvalidRequest:
		status = http.StatusBadRequest
	case wire.ErrorCodePermissionDenied, wire.ErrorCodeAuthorityMismatch, wire.ErrorCodeScopeExpired:
		status = http.StatusForbidden
	case wire.ErrorCodeNotFound, wire.ErrorCodeSessionNotFound, wire.ErrorCodeApprovalNotFound, wire.ErrorCodeGrantNotFound:
		status = http.StatusNotFound
	case wire.ErrorCodeRequestConflict, wire.ErrorCodeRevisionConflict, wire.ErrorCodeSelectionStale, wire.ErrorCodeBusy, wire.ErrorCodeApprovalStale, wire.ErrorCodeApprovalResolved, wire.ErrorCodePermissionModeUnavailable:
		status = http.StatusConflict
	case wire.ErrorCodeGrantExpired, wire.ErrorCodeApprovalExpired:
		status = http.StatusGone
	}
	writeJSON(w, status, wire.Error{SchemaVersion: 1, RequestId: requestID, Code: code, Retryable: false})
}
func marketReply(w http.ResponseWriter, requestID wire.CanonicalId, status int, value marketValidated, err error) {
	if err != nil {
		marketErrorReply(w, &requestID, session.MarketFailureCode(err))
		return
	}
	if value.Validate() != nil {
		marketErrorReply(w, &requestID, wire.ErrorCodeTemporarilyUnavailable)
		return
	}
	writeJSON(w, status, value)
}
func marketReadID(r *http.Request, ids ...string) (wire.CanonicalId, bool) {
	headers := r.Header.Values("X-Yijie-Market-Request-Id")
	if len(headers) != 1 || !validRecoveryRequest(r, ids...) {
		return "", false
	}
	id := wire.CanonicalId(headers[0])
	return id, id.Validate() == nil
}
func registerMarketRoutes(mux *http.ServeMux, candidate any, apiToken, nonce string) {
	service, ok := candidate.(marketHTTPService)
	if !ok || !service.MarketEnabled() {
		return
	}
	register := func(route string, next http.HandlerFunc) {
		mux.HandleFunc(route, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Cache-Control", "no-store")
			setInstanceNonceHeader(w, nonce)
			if !security.TokenMatches(apiToken, r.Header.Get("Authorization")) {
				// The existing bearer only authorizes access to this local HTTP
				// surface. It cannot register a grant or decide an approval.
				writeJSON(w, http.StatusUnauthorized, wire.Error{SchemaVersion: 1, Code: wire.ErrorCodePermissionDenied, Retryable: false})
				return
			}
			next(w, r)
		})
	}
	register("POST /v1/market-chat/submissions", func(w http.ResponseWriter, r *http.Request) {
		var request wire.SubmitRequest
		contentTypes := r.Header.Values("Content-Type")
		contentType := ""
		var err error
		if len(contentTypes) == 1 {
			contentType, _, err = mime.ParseMediaType(contentTypes[0])
		}
		if len(contentTypes) != 1 || err != nil || contentType != "application/json" || r.URL.RawQuery != "" || r.URL.ForceQuery {
			marketErrorReply(w, nil, wire.ErrorCodeInvalidRequest)
			return
		}
		bytes, err := io.ReadAll(http.MaxBytesReader(w, r.Body, wire.MaxHttpTriggerBytes))
		if err != nil || json.Unmarshal(bytes, &request) != nil {
			marketErrorReply(w, nil, wire.ErrorCodeInvalidRequest)
			return
		}
		data, err := service.SubmitMarket(r.Context(), request.Payload)
		marketReply(w, request.RequestId, http.StatusAccepted, wire.SubmitResponse{SchemaVersion: 1, RequestId: request.RequestId, Data: data}, err)
	})
	register("GET /v1/market-chat/tasks/{taskId}/operations/{turnOperationId}", func(w http.ResponseWriter, r *http.Request) {
		task, operation := r.PathValue("taskId"), r.PathValue("turnOperationId")
		requestID, ok := marketReadID(r, task, operation)
		if !ok {
			marketErrorReply(w, nil, wire.ErrorCodeInvalidRequest)
			return
		}
		data, err := service.ReadMarketOperation(task, operation)
		marketReply(w, requestID, http.StatusOK, wire.OperationResponse{SchemaVersion: 1, RequestId: requestID, Data: data}, err)
	})
	register("GET /v1/market-chat/agent-sessions/{agentSessionId}/approvals", func(w http.ResponseWriter, r *http.Request) {
		agent := r.PathValue("agentSessionId")
		requestID, ok := marketReadID(r, agent)
		if !ok {
			marketErrorReply(w, nil, wire.ErrorCodeInvalidRequest)
			return
		}
		data, err := service.ReadMarketApprovals(r.Context(), agent)
		marketReply(w, requestID, http.StatusOK, wire.ApprovalsResponse{SchemaVersion: 1, RequestId: requestID, Data: data}, err)
	})
	register("GET /v1/market-chat/agent-sessions/{agentSessionId}/turns/{nativeTurnId}/tools", func(w http.ResponseWriter, r *http.Request) {
		agent, turn := r.PathValue("agentSessionId"), r.PathValue("nativeTurnId")
		requestID, ok := marketReadID(r, agent, turn)
		if !ok {
			marketErrorReply(w, nil, wire.ErrorCodeInvalidRequest)
			return
		}
		data, err := service.ReadMarketTools(r.Context(), agent, turn)
		marketReply(w, requestID, http.StatusOK, wire.ToolsResponse{SchemaVersion: 1, RequestId: requestID, Data: data}, err)
	})
}
