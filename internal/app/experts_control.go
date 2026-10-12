package app

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	expert "github.com/36Dge/yijie-agent-host/internal/contracts/experts"
	market "github.com/36Dge/yijie-agent-host/internal/contracts/markethost"
)

// Uses only the already owned Native pipe and its market hello/scope authority.
// There is no HTTP route, renderer prompt field or parallel grant lifecycle.
type expertControlService interface {
	ApplyExpertBinding(context.Context, expert.ApplyPayload) (expert.ApplyReceipt, error)
	RecoverExpertBinding(context.Context, expert.PrivateBindingRecoverPayload) (expert.ApplyReceipt, error)
	ReadExpertOperation(expert.PrivateOperationReadPayload) (expert.ApplyReceipt, error)
	RegisterExpertGrant(expert.PrivateGrantPayload) (expert.PrivateGrant, error)
	SubmitExpert(context.Context, market.SubmitPayload) (expert.PrivateReceipt, error)
	ReadExpertSubmission(expert.SubmissionReadPayload) (expert.PrivateReceipt, error)
}

func expertControlMethod(frame []byte) string {
	var route struct {
		Method string `json:"method"`
	}
	if json.Unmarshal(frame, &route) != nil {
		return ""
	}
	return route.Method
}
func expertControlTimeout(frame []byte) time.Duration {
	if ms, ok := expert.MethodTimeoutMs[expertControlMethod(frame)]; ok {
		return time.Duration(ms) * time.Millisecond
	}
	return 5 * time.Second
}
func isExpertControlFrame(frame []byte) bool {
	return strings.HasPrefix(expertControlMethod(frame), "expert_")
}
func routeExpertControl(ctx context.Context, service MarketControlService, frame []byte, method string, id *market.CanonicalId) (marketValidated, *market.CanonicalId, error) {
	if method == "expert_status" || strings.HasPrefix(method, "expert_resources_") {
		return routeExpertResourcesControl(ctx, service, frame, method, id)
	}
	svc, ok := service.(expertControlService)
	if !ok {
		return nil, id, errMarketControlRequest
	}
	switch method {
	case "expert_binding_recover":
		var request expert.PrivateBindingRecoverRequest
		if json.Unmarshal(frame, &request) != nil {
			return nil, id, errMarketControlRequest
		}
		data, e := svc.RecoverExpertBinding(ctx, request.Payload)
		return expert.PrivateBindingRecoverResponse{SchemaVersion: 1, RequestId: request.RequestId, Data: data}, id, e
	case "expert_apply_binding":
		var request expert.ApplyRequest
		if json.Unmarshal(frame, &request) != nil {
			return nil, id, errMarketControlRequest
		}
		data, e := svc.ApplyExpertBinding(ctx, request.Payload)
		return expert.ApplyResponse{SchemaVersion: 1, RequestId: request.RequestId, Data: data}, id, e
	case "expert_operation_read":
		var request expert.PrivateOperationReadRequest
		if json.Unmarshal(frame, &request) != nil {
			return nil, id, errMarketControlRequest
		}
		data, e := svc.ReadExpertOperation(request.Payload)
		return expert.PrivateOperationReadResponse{SchemaVersion: 1, RequestId: request.RequestId, Data: data}, id, e
	case "expert_grant_register":
		var request expert.PrivateGrantRequest
		if json.Unmarshal(frame, &request) != nil {
			return nil, id, errMarketControlRequest
		}
		data, e := svc.RegisterExpertGrant(request.Payload)
		return expert.PrivateGrantResponse{SchemaVersion: 1, RequestId: request.RequestId, Data: data}, id, e
	case "expert_submit":
		var request expert.PrivateSubmitRequest
		if json.Unmarshal(frame, &request) != nil {
			return nil, id, errMarketControlRequest
		}
		data, e := svc.SubmitExpert(ctx, request.Payload)
		return expert.PrivateSubmitResponse{SchemaVersion: 1, RequestId: request.RequestId, Data: data}, id, e
	case "expert_submission_read":
		var request expert.SubmissionReadRequest
		if json.Unmarshal(frame, &request) != nil {
			return nil, id, errMarketControlRequest
		}
		data, e := svc.ReadExpertSubmission(request.Payload)
		return expert.SubmissionReadResponse{SchemaVersion: 1, RequestId: request.RequestId, Data: data}, id, e
	default:
		return nil, id, errMarketControlRequest
	}
}

type expertResourceControlService interface {
	ExpertStatus(context.Context, expert.PrivateStatusPayload) (expert.PrivateStatus, error)
	PrepareExpertResources(context.Context, expert.PrivateResourcePayload) (expert.ResourceOperation, error)
	RemoveExpertResources(context.Context, expert.PrivateResourcePayload) (expert.ResourceOperation, error)
	ReadExpertResourceOperation(expert.PrivateResourceReadPayload) (expert.ResourceOperation, error)
	RecoverExpertResources(context.Context, expert.PrivateResourceRecoverPayload) (expert.ResourceOperation, error)
}

func routeExpertResourcesControl(ctx context.Context, service MarketControlService, frame []byte, method string, id *market.CanonicalId) (marketValidated, *market.CanonicalId, error) {
	svc, ok := service.(expertResourceControlService)
	if !ok {
		return nil, id, errMarketControlRequest
	}
	switch method {
	case "expert_resources_recover":
		var request expert.PrivateResourceRecoverRequest
		if json.Unmarshal(frame, &request) != nil {
			return nil, id, errMarketControlRequest
		}
		data, e := svc.RecoverExpertResources(ctx, request.Payload)
		return expert.PrivateResourceRecoverResponse{SchemaVersion: 1, RequestId: request.RequestId, Data: data}, id, e
	case "expert_status":
		var request expert.PrivateStatusRequest
		if json.Unmarshal(frame, &request) != nil {
			return nil, id, errMarketControlRequest
		}
		data, e := svc.ExpertStatus(ctx, request.Payload)
		return expert.PrivateStatusResponse{SchemaVersion: 1, RequestId: request.RequestId, Data: data}, id, e
	case "expert_resources_prepare":
		var request expert.PrivateResourcePrepareRequest
		if json.Unmarshal(frame, &request) != nil {
			return nil, id, errMarketControlRequest
		}
		data, e := svc.PrepareExpertResources(ctx, request.Payload)
		return expert.PrivateResourcePrepareResponse{SchemaVersion: 1, RequestId: request.RequestId, Data: data}, id, e
	case "expert_resources_remove":
		var request expert.PrivateResourceRemoveRequest
		if json.Unmarshal(frame, &request) != nil {
			return nil, id, errMarketControlRequest
		}
		data, e := svc.RemoveExpertResources(ctx, request.Payload)
		return expert.PrivateResourceRemoveResponse{SchemaVersion: 1, RequestId: request.RequestId, Data: data}, id, e
	case "expert_resources_operation_read":
		var request expert.PrivateResourceReadRequest
		if json.Unmarshal(frame, &request) != nil {
			return nil, id, errMarketControlRequest
		}
		data, e := svc.ReadExpertResourceOperation(request.Payload)
		return expert.PrivateResourceReadResponse{SchemaVersion: 1, RequestId: request.RequestId, Data: data}, id, e
	default:
		return nil, id, errMarketControlRequest
	}
}
