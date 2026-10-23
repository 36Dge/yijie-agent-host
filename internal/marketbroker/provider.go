package marketbroker

import (
	"context"
	"encoding/json"
	"reflect"

	provider "github.com/36Dge/yijie-agent-host/internal/contracts/marketprovider"
)

type ProviderError struct{ code provider.ErrorCode }

func (e *ProviderError) Error() string            { return "market provider control rejected: " + string(e.code) }
func (e *ProviderError) Code() provider.ErrorCode { return e.code }

func providerResponse[T any](ctx context.Context, owner *Owner, request validatedRequest, requestID provider.CanonicalId, matches func(T) bool) (T, error) {
	var response T
	if err := ctx.Err(); err != nil {
		return response, err
	}
	if request.Validate() != nil {
		return response, ErrProtocol
	}
	if err := owner.acquireControl(ctx); err != nil {
		return response, err
	}
	defer func() { <-owner.gate }()
	frame, err := json.Marshal(request)
	if err != nil {
		return response, ErrProtocol
	}
	// Authorization is deliberately available before Broker Initialize. This
	// flag permits owner transport only; the worker still rejects data calls.
	encoded, err := owner.exchange(ctx, frame, true)
	if err != nil {
		return response, err
	}
	var envelope map[string]json.RawMessage
	if json.Unmarshal(encoded, &envelope) != nil {
		owner.retire(ErrProtocol)
		return response, ErrProtocol
	}
	if _, failure := envelope["code"]; failure {
		var remote provider.Error
		if _, both := envelope["data"]; both || json.Unmarshal(encoded, &remote) != nil || remote.RequestId == nil || *remote.RequestId != requestID {
			owner.retire(ErrProtocol)
			return response, ErrProtocol
		}
		return response, &ProviderError{code: remote.Code}
	}
	var echoed provider.CanonicalId
	if json.Unmarshal(envelope["requestId"], &echoed) != nil || echoed != requestID || json.Unmarshal(encoded, &response) != nil || !matches(response) {
		owner.retire(ErrBinding)
		return response, ErrBinding
	}
	return response, nil
}

func providerMatches(result provider.ProviderStatus, request provider.ProviderPayload) bool {
	return result.OperationId == request.OperationId && result.HostInstanceId == request.HostInstanceId && reflect.DeepEqual(result.Scope, request.Scope) && result.Binding == request.Binding
}
func (o *Owner) AuthBegin(ctx context.Context, request provider.AuthBeginRequest) (provider.AuthBeginResponse, error) {
	return providerResponse(ctx, o, request, request.RequestId, func(r provider.AuthBeginResponse) bool { return providerMatches(r.Data, request.Payload) })
}
func (o *Owner) AuthPoll(ctx context.Context, request provider.AuthPollRequest) (provider.AuthPollResponse, error) {
	return providerResponse(ctx, o, request, request.RequestId, func(r provider.AuthPollResponse) bool { return providerMatches(r.Data, request.Payload) })
}
func (o *Owner) AuthCancel(ctx context.Context, request provider.AuthCancelRequest) (provider.AuthCancelResponse, error) {
	return providerResponse(ctx, o, request, request.RequestId, func(r provider.AuthCancelResponse) bool { return providerMatches(r.Data, request.Payload) })
}
func (o *Owner) Forget(ctx context.Context, request provider.ForgetRequest) (provider.ForgetResponse, error) {
	return providerResponse(ctx, o, request, request.RequestId, func(r provider.ForgetResponse) bool { return providerMatches(r.Data, request.Payload) })
}
func (o *Owner) Probe(ctx context.Context, request provider.ProbeRequest) (provider.ProbeResponse, error) {
	return providerResponse(ctx, o, request, request.RequestId, func(r provider.ProbeResponse) bool { return providerMatches(r.Data, request.Payload) })
}

func (o *Owner) Suspend(ctx context.Context, request provider.SuspendRequest) (provider.SuspendResponse, error) {
	return providerResponse(ctx, o, request, request.RequestId, func(r provider.SuspendResponse) bool { return providerMatches(r.Data, request.Payload) })
}
