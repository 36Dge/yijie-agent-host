package session

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"reflect"
	"sync"
	"time"

	wire "github.com/36Dge/yijie-agent-host/internal/contracts/markethost"
	"github.com/36Dge/yijie-agent-host/internal/marketbroker"
	"github.com/google/uuid"
)

type marketError struct{ code wire.ErrorCode }

func (e *marketError) Error() string       { return "market delivery: " + string(e.code) }
func marketFail(code wire.ErrorCode) error { return &marketError{code: code} }
func MarketFailureCode(err error) wire.ErrorCode {
	var value *marketError
	if errors.As(err, &value) {
		return value.code
	}
	var provider *marketbroker.ProviderError
	if errors.As(err, &provider) {
		code := wire.ErrorCode(provider.Code())
		if code.Validate() == nil {
			return code
		}
	}
	return wire.ErrorCodeTemporarilyUnavailable
}

type marketGrant struct {
	body              *wire.Submission
	identity          wire.Submission // ContentBlocks omitted; never used as a new request.
	scope             wire.ScopeBinding
	digest, scopeHash string
	deadline          time.Time
	bytes             int
	view              wire.GrantRegistration
	revoked           bool
	inFlight          bool
}
type marketControlReceipt struct {
	digest, grant string
	revoke        *wire.RevokeAuthorityResult
}
type marketAuthority struct {
	mu       sync.Mutex
	instance wire.CanonicalId
	hello    *wire.HelloPayload
	closed   bool
	revision int64
	bytes    int
	grants   map[string]*marketGrant
	receipts map[string]marketControlReceipt
}

func WithMarketAuthority(hostInstanceID string) ServiceOption {
	return func(s *Service) {
		if wire.CanonicalId(hostInstanceID).Validate() == nil && s.store.marketWriter {
			s.market = &marketAuthority{instance: wire.CanonicalId(hostInstanceID), grants: make(map[string]*marketGrant), receipts: make(map[string]marketControlReceipt)}
		}
	}
}

func (s *Service) MarketEnabled() bool { return s.market != nil && s.store.marketWriter }

func (s *Service) MarketHello(input wire.HelloPayload) (wire.HelloResult, error) {
	if !s.MarketEnabled() {
		return wire.HelloResult{}, marketFail(wire.ErrorCodeNotReady)
	}
	if input.Validate() != nil {
		return wire.HelloResult{}, marketFail(wire.ErrorCodeInvalidRequest)
	}
	a := s.market
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closed || input.HostInstanceId != a.instance || (a.hello != nil && *a.hello != input) {
		return wire.HelloResult{}, marketFail(wire.ErrorCodeAuthorityMismatch)
	}
	copy := input
	a.hello = &copy
	// This acknowledges only the exclusive Native control identity. Runtime,
	// model, provider and turn readiness are separately checked before execution.
	return wire.HelloResult{HostInstanceId: input.HostInstanceId, NativeProcessEpoch: input.NativeProcessEpoch, OwnerUserId: input.OwnerUserId, TenantId: input.TenantId, Ready: true}, nil
}

func (a *marketAuthority) checkScope(instance wire.CanonicalId, scope wire.ScopeBinding, current bool) error {
	if a.closed || a.hello == nil || instance != a.instance || scope.OwnerUserId != a.hello.OwnerUserId || scope.TenantId != a.hello.TenantId || scope.NativeProcessEpoch != a.hello.NativeProcessEpoch {
		return marketFail(wire.ErrorCodeAuthorityMismatch)
	}
	if current {
		if int64(scope.AuthorizationRevision) < a.revision {
			return marketFail(wire.ErrorCodeAuthorityMismatch)
		}
		if int64(scope.AuthorizationExpiresAtUnixMs) <= time.Now().UnixMilli() {
			return marketFail(wire.ErrorCodeScopeExpired)
		}
		if int64(scope.AuthorizationRevision) > a.revision {
			a.revision = int64(scope.AuthorizationRevision)
		}
	}
	return nil
}

func (s *Service) marketHMAC(domain string, value any) (string, error) {
	b, err := json.Marshal(value)
	if err != nil {
		return "", marketFail(wire.ErrorCodeInvalidRequest)
	}
	h := hmac.New(sha256.New, s.store.receiptKey[:])
	h.Write([]byte(domain))
	h.Write([]byte{'\n'})
	h.Write(b)
	return hex.EncodeToString(h.Sum(nil)), nil
}
func (s *Service) marketScopeHash(scope wire.ScopeBinding) string {
	value, _ := s.marketHMAC("yijie.market-owner/v1", []string{string(scope.OwnerUserId), string(scope.TenantId)})
	return value
}

func (a *marketAuthority) releaseBody(grant *marketGrant) {
	if grant.body != nil && !grant.inFlight {
		a.bytes -= grant.bytes
		grant.body = nil
		grant.bytes = 0
	}
}
func (a *marketAuthority) sweep() {
	for _, grant := range a.grants {
		if grant.revoked || !time.Now().Before(grant.deadline) {
			a.releaseBody(grant)
		}
	}
}

func (s *Service) RegisterMarketGrant(input wire.GrantRegisterPayload) (wire.GrantRegistration, error) {
	if !s.MarketEnabled() {
		return wire.GrantRegistration{}, marketFail(wire.ErrorCodeNotReady)
	}
	if len(input.Submission.Snapshot.Selection) > 0 && input.Submission.PermissionMode != "ask" {
		return wire.GrantRegistration{}, marketFail(wire.ErrorCodePermissionModeUnavailable)
	}
	if input.Validate() != nil {
		return wire.GrantRegistration{}, marketFail(wire.ErrorCodeInvalidRequest)
	}
	intent, err := s.marketHMAC("yijie.market-grant-registration/v1", input)
	if err != nil {
		return wire.GrantRegistration{}, err
	}
	stableOwner := s.marketScopeHash(input.Scope)
	digest, err := s.marketHMAC("yijie.market-submission/v1", []any{stableOwner, input.Submission})
	if err != nil {
		return wire.GrantRegistration{}, err
	}
	encoded, err := json.Marshal(input.Submission)
	if err != nil || len(encoded) > wire.MaxControlFrameBytes {
		return wire.GrantRegistration{}, marketFail(wire.ErrorCodeInvalidRequest)
	}
	a := s.market
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := a.checkScope(input.HostInstanceId, input.Scope, true); err != nil {
		return wire.GrantRegistration{}, err
	}
	a.sweep()
	if original, ok := a.receipts[string(input.OperationId)]; ok {
		if original.digest != intent {
			return wire.GrantRegistration{}, marketFail(wire.ErrorCodeRequestConflict)
		}
		grant := a.grants[original.grant]
		if grant == nil || grant.revoked || !time.Now().Before(grant.deadline) {
			return wire.GrantRegistration{}, marketFail(wire.ErrorCodeGrantExpired)
		}
		view := grant.view
		view.RemainingTtlMs = grant.deadline.Sub(time.Now()).Milliseconds()
		return view, nil
	}
	active := 0
	for _, grant := range a.grants {
		if grant.body != nil {
			active++
		}
	}
	if len(a.receipts) >= wire.MaxControlReceipts || active >= wire.MaxPendingGrants || a.bytes+len(encoded) > wire.MaxGrantBytes {
		return wire.GrantRegistration{}, marketFail(wire.ErrorCodeCapacityExceeded)
	}
	remaining := time.Until(time.UnixMilli(int64(input.Scope.AuthorizationExpiresAtUnixMs)))
	if remaining > time.Duration(wire.MaxGrantTtlMs)*time.Millisecond {
		remaining = time.Duration(wire.MaxGrantTtlMs) * time.Millisecond
	}
	if remaining <= 0 {
		return wire.GrantRegistration{}, marketFail(wire.ErrorCodeScopeExpired)
	}
	var owned wire.Submission
	if json.Unmarshal(encoded, &owned) != nil {
		return wire.GrantRegistration{}, marketFail(wire.ErrorCodeInvalidRequest)
	}
	identity := owned
	identity.ContentBlocks = nil
	id := wire.CanonicalId(uuid.NewString())
	view := wire.GrantRegistration{GrantRef: id, HostInstanceId: a.instance, NativeProcessEpoch: input.Scope.NativeProcessEpoch, TurnOperationId: owned.Snapshot.TurnOperationId, SelectionDigest: owned.Snapshot.SelectionDigest, RemainingTtlMs: remaining.Milliseconds()}
	a.grants[string(id)] = &marketGrant{body: &owned, identity: identity, scope: input.Scope, digest: digest, scopeHash: stableOwner, deadline: time.Now().Add(remaining), bytes: len(encoded), view: view}
	a.bytes += len(encoded)
	a.receipts[string(input.OperationId)] = marketControlReceipt{digest: intent, grant: string(id)}
	return view, nil
}

// This private adapter is also used by provider commands. It does not accept a
// Trace identity or grant any authority from an HTTP bearer.
func (s *Service) ValidateMarketScope(instance wire.CanonicalId, scope wire.ScopeBinding, current bool) error {
	if !s.MarketEnabled() {
		return marketFail(wire.ErrorCodeNotReady)
	}
	a := s.market
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.checkScope(instance, scope, current)
}

func (s *Service) marketGrant(ref, operation string) (*marketGrant, error) {
	if !s.MarketEnabled() {
		return nil, marketFail(wire.ErrorCodeNotReady)
	}
	a := s.market
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closed || a.hello == nil {
		return nil, marketFail(wire.ErrorCodeAuthorityMismatch)
	}
	a.sweep()
	grant := a.grants[ref]
	if grant == nil || string(grant.view.TurnOperationId) != operation {
		return nil, marketFail(wire.ErrorCodeGrantNotFound)
	}
	copy := *grant
	return &copy, nil
}

func (s *Service) consumeMarketGrant(ref string, expected *marketGrant) error {
	a := s.market
	a.mu.Lock()
	defer a.mu.Unlock()
	actual := a.grants[ref]
	if a.closed || actual == nil || actual.revoked || actual.inFlight || actual.body == nil || !time.Now().Before(actual.deadline) || actual.digest != expected.digest || !reflect.DeepEqual(actual.scope, expected.scope) {
		return marketFail(wire.ErrorCodeGrantExpired)
	}
	if err := a.checkScope(a.instance, actual.scope, true); err != nil {
		return err
	}
	actual.inFlight = true
	return nil
}

// Hook used by the owned control reader on normal EOF. Runtime/Broker shutdown
// follows through the existing managed process lifecycle.
func (s *Service) CloseMarketAuthority(_ context.Context) {
	if s.market == nil {
		return
	}
	a := s.market
	a.mu.Lock()
	a.closed = true
	for _, grant := range a.grants {
		grant.revoked = true
		a.releaseBody(grant)
	}
	a.mu.Unlock()
	// EOF revokes Gateway admission immediately, before HTTP draining. The
	// process handle remains retained on timeout for normal shutdown to await.
	if runtime, ok := s.runtime.(interface{ CloseMarketAdmission(context.Context) error }); ok {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := runtime.CloseMarketAdmission(ctx); err != nil && s.logger != nil {
			s.logger.Warn("market admission cleanup pending", "failure_code", "market_cleanup_pending")
		}
	}
}

func (s *Service) finishMarketGrant(ref string) {
	a := s.market
	a.mu.Lock()
	defer a.mu.Unlock()
	if grant := a.grants[ref]; grant != nil {
		grant.inFlight = false
		a.releaseBody(grant)
	}
}
