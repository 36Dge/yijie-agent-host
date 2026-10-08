package session

import (
	"context"

	wire "github.com/36Dge/yijie-agent-host/internal/contracts/markethost"
	provider "github.com/36Dge/yijie-agent-host/internal/contracts/marketprovider"
	"github.com/36Dge/yijie-agent-host/internal/marketbroker"
)

type marketOwnerRuntime interface {
	MarketOwner() (*marketbroker.Owner, error)
}

// Provider controls are callable only from the owned Native adapter. A platform
// credential is never a parameter or a return value in this API.
func (s *Service) MarketProviderControl(ctx context.Context, method string, requestID provider.CanonicalId, payload provider.ProviderPayload) (provider.ProviderStatus, error) {
	if payload.Validate() != nil || requestID.Validate() != nil {
		return provider.ProviderStatus{}, marketFail(wire.ErrorCodeInvalidRequest)
	}
	current := method == "provider_auth_begin" || method == "provider_probe"
	if err := s.ValidateMarketScope(payload.HostInstanceId, payload.Scope, current); err != nil {
		return provider.ProviderStatus{}, err
	}
	runtime, ok := s.runtime.(marketOwnerRuntime)
	if !ok {
		return provider.ProviderStatus{}, marketFail(wire.ErrorCodeNotReady)
	}
	owner, err := runtime.MarketOwner()
	if err != nil {
		return provider.ProviderStatus{}, marketFail(wire.ErrorCodeNotReady)
	}
	switch method {
	case "provider_auth_begin":
		r, err := owner.AuthBegin(ctx, provider.AuthBeginRequest{SchemaVersion: 1, RequestId: requestID, Method: method, Payload: payload})
		return r.Data, err
	case "provider_auth_poll":
		r, err := owner.AuthPoll(ctx, provider.AuthPollRequest{SchemaVersion: 1, RequestId: requestID, Method: method, Payload: payload})
		return r.Data, err
	case "provider_auth_cancel":
		r, err := owner.AuthCancel(ctx, provider.AuthCancelRequest{SchemaVersion: 1, RequestId: requestID, Method: method, Payload: payload})
		return r.Data, err
	case "provider_forget":
		installation := payload.Binding.Reference.InstallationId
		result, e := s.RevokeMarketAuthority(ctx, wire.RevokeAuthorityPayload{OperationId: payload.OperationId, HostInstanceId: payload.HostInstanceId, Scope: payload.Scope, InstallationId: &installation, Reason: "installation_changed"})
		if e != nil {
			return provider.ProviderStatus{}, e
		}
		if result.Outcome != "admission_revoked" {
			return provider.ProviderStatus{}, marketFail(wire.ErrorCodeCleanupPending)
		}
		r, err := owner.Forget(ctx, provider.ForgetRequest{SchemaVersion: 1, RequestId: requestID, Method: method, Payload: payload})
		return r.Data, err
	case "provider_probe":
		r, err := owner.Probe(ctx, provider.ProbeRequest{SchemaVersion: 1, RequestId: requestID, Method: method, Payload: payload})
		return r.Data, err
	default:
		return provider.ProviderStatus{}, marketFail(wire.ErrorCodeInvalidRequest)
	}
}
