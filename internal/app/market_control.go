package app

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"sync"
	"time"

	wire "github.com/36Dge/yijie-agent-host/internal/contracts/markethost"
	provider "github.com/36Dge/yijie-agent-host/internal/contracts/marketprovider"
	"github.com/36Dge/yijie-agent-host/internal/session"
)

// Only cmd/desktop-host passes its original Native-owned stdin/stdout here.
// This interface is deliberately absent from every HTTP route.
type MarketControlService interface {
	MarketEnabled() bool
	MarketHello(wire.HelloPayload) (wire.HelloResult, error)
	RegisterMarketGrant(wire.GrantRegisterPayload) (wire.GrantRegistration, error)
	RevokeMarketAuthority(context.Context, wire.RevokeAuthorityPayload) (wire.RevokeAuthorityResult, error)
	DecideMarketApproval(context.Context, wire.ApprovalDecidePayload) (wire.MarketApproval, error)
	MarketProviderControl(context.Context, string, provider.CanonicalId, provider.ProviderPayload) (provider.ProviderStatus, error)
	CloseMarketAuthority(context.Context)
}

// routeMarketControl reads only method/requestId for routing. Every dispatched
// value is decoded and validated by the generated source-owned request type.
func routeMarketControl(ctx context.Context, service MarketControlService, frame []byte) (marketValidated, *wire.CanonicalId, error) {
	var route map[string]json.RawMessage
	if json.Unmarshal(frame, &route) != nil {
		return nil, nil, errMarketControlRequest
	}
	var method string
	if json.Unmarshal(route["method"], &method) != nil {
		return nil, nil, errMarketControlRequest
	}
	var id wire.CanonicalId
	var requestID *wire.CanonicalId
	if json.Unmarshal(route["requestId"], &id) == nil && id.Validate() == nil {
		requestID = &id
	}
	if method != "grant_register" && len(frame) > wire.MaxSmallControlFrameBytes {
		return nil, requestID, errMarketControlRequest
	}
	switch method {
	case "hello":
		var request wire.HelloRequest
		if json.Unmarshal(frame, &request) != nil {
			return nil, requestID, errMarketControlRequest
		}
		data, err := service.MarketHello(request.Payload)
		return wire.HelloResponse{SchemaVersion: 1, RequestId: request.RequestId, Data: data}, requestID, err
	case "grant_register":
		var request wire.GrantRegisterRequest
		if json.Unmarshal(frame, &request) != nil {
			return nil, requestID, errMarketControlRequest
		}
		data, err := service.RegisterMarketGrant(request.Payload)
		return wire.GrantRegisterResponse{SchemaVersion: 1, RequestId: request.RequestId, Data: data}, requestID, err
	case "revoke_authority":
		var request wire.RevokeAuthorityRequest
		if json.Unmarshal(frame, &request) != nil {
			return nil, requestID, errMarketControlRequest
		}
		data, err := service.RevokeMarketAuthority(ctx, request.Payload)
		return wire.RevokeAuthorityResponse{SchemaVersion: 1, RequestId: request.RequestId, Data: data}, requestID, err
	case "approval_decide":
		var request wire.ApprovalDecideRequest
		if json.Unmarshal(frame, &request) != nil {
			return nil, requestID, errMarketControlRequest
		}
		data, err := service.DecideMarketApproval(ctx, request.Payload)
		return wire.ApprovalDecideResponse{SchemaVersion: 1, RequestId: request.RequestId, Data: data}, requestID, err
	case "provider_auth_begin":
		var request wire.AuthBeginRequest
		if json.Unmarshal(frame, &request) != nil {
			return nil, requestID, errMarketControlRequest
		}
		data, err := service.MarketProviderControl(ctx, request.Method, request.RequestId, request.Payload)
		return wire.AuthBeginResponse{SchemaVersion: 1, RequestId: request.RequestId, Data: data}, requestID, err
	case "provider_auth_poll":
		var request wire.AuthPollRequest
		if json.Unmarshal(frame, &request) != nil {
			return nil, requestID, errMarketControlRequest
		}
		data, err := service.MarketProviderControl(ctx, request.Method, request.RequestId, request.Payload)
		return wire.AuthPollResponse{SchemaVersion: 1, RequestId: request.RequestId, Data: data}, requestID, err
	case "provider_auth_cancel":
		var request wire.AuthCancelRequest
		if json.Unmarshal(frame, &request) != nil {
			return nil, requestID, errMarketControlRequest
		}
		data, err := service.MarketProviderControl(ctx, request.Method, request.RequestId, request.Payload)
		return wire.AuthCancelResponse{SchemaVersion: 1, RequestId: request.RequestId, Data: data}, requestID, err
	case "provider_forget":
		var request wire.ForgetRequest
		if json.Unmarshal(frame, &request) != nil {
			return nil, requestID, errMarketControlRequest
		}
		data, err := service.MarketProviderControl(ctx, request.Method, request.RequestId, request.Payload)
		return wire.ForgetResponse{SchemaVersion: 1, RequestId: request.RequestId, Data: data}, requestID, err
	case "provider_probe":
		var request wire.ProbeRequest
		if json.Unmarshal(frame, &request) != nil {
			return nil, requestID, errMarketControlRequest
		}
		data, err := service.MarketProviderControl(ctx, request.Method, request.RequestId, request.Payload)
		return wire.ProbeResponse{SchemaVersion: 1, RequestId: request.RequestId, Data: data}, requestID, err
	default:
		return nil, requestID, errMarketControlRequest
	}
}

var errMarketControlRequest = errors.New("market control request unavailable")

// A serial bounded JSONL reader holds exclusive response ownership. EOF retires
// authority before cmd closes HTTP and normally shuts down worker/Runtime.
func ServeMarketControl(ctx context.Context, input io.ReadCloser, output io.Writer, service MarketControlService) error {
	if service == nil || !service.MarketEnabled() {
		return errors.New("market control is not configured")
	}
	readerContext, stopReader := context.WithCancel(ctx)
	var closeOnce sync.Once
	closeAuthority := func() { closeOnce.Do(func() { service.CloseMarketAuthority(context.Background()) }) }
	frames := make(chan []byte)
	readerDone := make(chan struct{})
	// Retain ownership until the original pipe reader exits. No detached reader
	// remains after an output failure, parent cancellation or normal EOF.
	defer func() {
		stopReader()
		closeAuthority()
		_ = input.Close()
		<-readerDone
	}()
	var readerError error
	go func() {
		defer func() {
			stopReader()
			closeAuthority()
			close(readerDone)
		}()
		scanner := bufio.NewScanner(input)
		scanner.Buffer(make([]byte, wire.MaxSmallControlFrameBytes), wire.MaxControlFrameBytes+1)
		for scanner.Scan() {
			frame := scanner.Bytes()
			if len(frame) == 0 || len(frame) > wire.MaxControlFrameBytes {
				readerError = errMarketControlRequest
				return
			}
			// At most one additional frame can wait here; no second request is
			// dispatched before the previous response has been written.
			owned := append([]byte(nil), frame...)
			select {
			case frames <- owned:
			case <-readerContext.Done():
				return
			}
		}
		if scanner.Err() != nil {
			readerError = errors.New("market control closed")
		}
	}()
	type controlResult struct {
		value marketValidated
		id    *wire.CanonicalId
		err   error
	}
	for {
		var frame []byte
		select {
		case <-readerDone:
			return readerError
		case <-ctx.Done():
			return ctx.Err()
		case frame = <-frames:
		}
		requestContext, cancel := context.WithTimeout(readerContext, 5*time.Second)
		completed := make(chan controlResult, 1)
		go func() {
			value, id, err := routeMarketControl(requestContext, service, frame)
			completed <- controlResult{value, id, err}
		}()
		var result controlResult
		select {
		case result = <-completed:
			cancel()
		case <-readerDone:
			cancel()
			closeAuthority()
			<-completed // No detached approval can outlive owner retirement.
			return readerError
		case <-requestContext.Done():
			cancel()
			closeAuthority()
			<-completed
			return errors.New("market control request outcome unavailable")
		}
		value, id, err := result.value, result.id, result.err
		if err != nil {
			code := session.MarketFailureCode(err)
			if errors.Is(err, errMarketControlRequest) {
				code = wire.ErrorCodeInvalidRequest
			}
			value = wire.Error{SchemaVersion: 1, RequestId: id, Code: code, Retryable: false}
		} else if value == nil || value.Validate() != nil {
			value = wire.Error{SchemaVersion: 1, RequestId: id, Code: wire.ErrorCodeTemporarilyUnavailable, Retryable: false}
		}
		response, err := json.Marshal(value)
		if err != nil || len(response)+1 > wire.MaxSmallControlFrameBytes {
			return errors.New("market control response unavailable")
		}
		response = append(response, '\n')
		if written, err := output.Write(response); err != nil || written != len(response) {
			return errors.New("market control closed")
		}
	}
}
