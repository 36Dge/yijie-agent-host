package marketbroker

import (
	"context"
	"encoding/json"
	"reflect"

	wire "github.com/36Dge/yijie-agent-host/internal/contracts/marketbrokercontrol"
)

// RemoteError exposes only the generated safe code. Raw frames, URLs and child
// diagnostic output are never formatted into an application error.
type RemoteError struct{ code wire.ErrorCode }

func (e *RemoteError) Error() string {
	return "market worker rejected control request: " + string(e.code)
}
func (e *RemoteError) Code() wire.ErrorCode { return e.code }

type validatedRequest interface{ Validate() error }

func processKey(binding wire.ProcessBinding) string {
	return string(binding.HostInstanceId) + "\n" + string(binding.RuntimeGeneration)
}

func requestResponse[T any](ctx context.Context, owner *Owner, request validatedRequest, requestID wire.CanonicalId, process wire.ProcessBinding, initializing bool, matches func(T) bool) (T, error) {
	var response T
	if err := ctx.Err(); err != nil {
		return response, err
	}
	if err := request.Validate(); err != nil {
		return response, ErrProtocol
	}
	if err := owner.acquireControl(ctx); err != nil {
		return response, err
	}
	defer func() { <-owner.gate }()
	owner.mu.Lock()
	matched := owner.processKey == processKey(process)
	owner.mu.Unlock()
	if !matched {
		return response, ErrBinding
	}
	frame, err := json.Marshal(request)
	if err != nil {
		return response, ErrProtocol
	}
	encoded, err := owner.exchange(ctx, frame, initializing)
	if err != nil {
		return response, err
	}
	var envelope map[string]json.RawMessage
	if json.Unmarshal(encoded, &envelope) != nil {
		owner.retire(ErrProtocol)
		return response, ErrProtocol
	}
	if _, failure := envelope["code"]; failure {
		var remote wire.Error
		if _, alsoData := envelope["data"]; alsoData || json.Unmarshal(encoded, &remote) != nil || remote.RequestId == nil || *remote.RequestId != requestID {
			owner.retire(ErrProtocol)
			return response, ErrProtocol
		}
		retireTerminationAtCapacity(owner, request, remote.Code)
		return response, &RemoteError{code: remote.Code}
	}
	var echoed wire.CanonicalId
	if json.Unmarshal(envelope["requestId"], &echoed) != nil || echoed != requestID || json.Unmarshal(encoded, &response) != nil {
		owner.retire(ErrProtocol)
		return response, ErrProtocol
	}
	if !matches(response) {
		return mismatched[T](owner)
	}
	return response, nil
}

// A full immutable receipt ledger cannot acknowledge a new revoke/shutdown.
// Stop the whole owned generation via normal EOF while the control gate is
// still held. Preserve the typed error: EOF is not an external completion fact.
// Other rejected mutations must not silently acquire this lifecycle effect.
func retireTerminationAtCapacity(owner *Owner, request validatedRequest, code wire.ErrorCode) {
	if code != wire.ErrorCodeCapacityExceeded {
		return
	}
	switch request.(type) {
	case wire.RevokeRequest, wire.ShutdownRequest:
		owner.retire(ErrClosed)
	}
}

func mismatched[T any](owner *Owner) (T, error) {
	owner.retire(ErrBinding)
	var zero T
	return zero, ErrBinding
}

// Initialize binds this one worker to the assembly-owned Host instance and
// Runtime generation. A successful structural scope is not a Native grant.
func (o *Owner) Initialize(ctx context.Context, request wire.InitializeRequest) (wire.InitializeResponse, error) {
	if err := ctx.Err(); err != nil {
		return wire.InitializeResponse{}, err
	}
	if request.Validate() != nil {
		return wire.InitializeResponse{}, ErrProtocol
	}
	if err := o.StartControl(ctx); err != nil {
		return wire.InitializeResponse{}, err
	}
	if err := o.bindProcess(request.Payload.Process); err != nil {
		return wire.InitializeResponse{}, err
	}
	response, err := requestResponse[wire.InitializeResponse](ctx, o, request, request.RequestId, request.Payload.Process, true, func(response wire.InitializeResponse) bool {
		o.mu.Lock()
		external := o.externalCallsEnabled
		o.mu.Unlock()
		return response.Data.Process == request.Payload.Process && response.Data.ExternalCallsEnabled == external
	})
	if err != nil {
		o.retire(ErrProtocol)
		return response, err
	}
	o.mu.Lock()
	if o.closed {
		o.mu.Unlock()
		return wire.InitializeResponse{}, ErrClosed
	}
	o.initialized = true
	o.mu.Unlock()
	return response, nil
}

// Prepare accepts only already authoritative snapshots from internal callers.
// No HTTP/Native grant carrier is registered in this candidate. Real nonempty
// provider selections remain not_qualified in the worker.
func (o *Owner) Prepare(ctx context.Context, request wire.PrepareRequest) (wire.PrepareResponse, error) {
	return requestResponse[wire.PrepareResponse](ctx, o, request, request.RequestId, request.Payload.Context.Process, false, func(response wire.PrepareResponse) bool {
		return reflect.DeepEqual(response.Data.Binding.Context, request.Payload.Context) && reflect.DeepEqual(response.Data.Snapshot, request.Payload.Snapshot)
	})
}

func (o *Owner) BindTurn(ctx context.Context, request wire.BindTurnRequest) (wire.BindTurnResponse, error) {
	return requestResponse[wire.BindTurnResponse](ctx, o, request, request.RequestId, request.Payload.Binding.Context.Process, false, func(response wire.BindTurnResponse) bool {
		return reflect.DeepEqual(response.Data.Binding, request.Payload.Binding) && response.Data.BoundNativeThreadId != nil && *response.Data.BoundNativeThreadId == request.Payload.NativeThreadId && response.Data.NativeTurnId != nil && *response.Data.NativeTurnId == request.Payload.NativeTurnId
	})
}

func (o *Owner) Revoke(ctx context.Context, request wire.RevokeRequest) (wire.RevokeResponse, error) {
	return requestResponse[wire.RevokeResponse](ctx, o, request, request.RequestId, request.Payload.Binding.Context.Process, false, func(response wire.RevokeResponse) bool {
		return reflect.DeepEqual(response.Data.Binding, request.Payload.Binding)
	})
}

func (o *Owner) Status(ctx context.Context, request wire.StatusRequest) (wire.StatusResponse, error) {
	return requestResponse[wire.StatusResponse](ctx, o, request, request.RequestId, request.Payload.Binding.Context.Process, false, func(response wire.StatusResponse) bool {
		return reflect.DeepEqual(response.Data.Binding, request.Payload.Binding)
	})
}

func (o *Owner) PendingCall(ctx context.Context, request wire.PendingCallRequest) (wire.PendingCallResponse, error) {
	return requestResponse[wire.PendingCallResponse](ctx, o, request, request.RequestId, request.Payload.Binding.Context.Process, false, func(response wire.PendingCallResponse) bool {
		return reflect.DeepEqual(response.Data.Identity.Binding, request.Payload.Binding) && response.Data.Identity.NativeThreadId == request.Payload.NativeThreadId && response.Data.Identity.NativeTurnId == request.Payload.NativeTurnId && response.Data.Identity.CallRef == request.Payload.CallRef
	})
}

func (o *Owner) DecideCall(ctx context.Context, request wire.DecideCallRequest) (wire.DecideCallResponse, error) {
	return requestResponse[wire.DecideCallResponse](ctx, o, request, request.RequestId, request.Payload.Identity.Binding.Context.Process, false, func(response wire.DecideCallResponse) bool {
		return reflect.DeepEqual(response.Data.Identity, request.Payload.Identity) && response.Data.DecisionId != nil && *response.Data.DecisionId == request.Payload.DecisionId && response.Data.Decision != nil && *response.Data.Decision == request.Payload.Decision
	})
}

// Shutdown's response acknowledges admission stopping only. Close must still
// observe normal child exit; callers must preserve STOP_PENDING if it cannot.
func (o *Owner) Shutdown(ctx context.Context, request wire.ShutdownRequest) (wire.ShutdownResponse, error) {
	return requestResponse[wire.ShutdownResponse](ctx, o, request, request.RequestId, request.Payload.Process, false, func(wire.ShutdownResponse) bool { o.retire(ErrClosed); return true })
}
