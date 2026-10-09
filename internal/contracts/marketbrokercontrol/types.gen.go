// Generated from market-broker-control source and declared borrowed authorities; DO NOT EDIT.
package marketbrokercontrol

import (
	"bytes"
	"encoding/json"
	"errors"
	market "github.com/36Dge/yijie-agent-host/internal/contracts/marketconnectors"
	selection "github.com/36Dge/yijie-agent-host/internal/contracts/marketselection"
	"io"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

var wirePattern0 = regexp.MustCompile("^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$")
var wirePattern1 = regexp.MustCompile("^[0-9a-f]{64}$")
var wirePattern2 = regexp.MustCompile("^[A-Za-z0-9][A-Za-z0-9_-]{0,127}$")
var wirePattern3 = regexp.MustCompile("^http://127\\.0\\.0\\.1:([1-9][0-9]{0,3}|[1-5][0-9]{4}|6[0-4][0-9]{3}|65[0-4][0-9]{2}|655[0-2][0-9]|6553[0-5])/mcp$")

type CanonicalId = market.CanonicalId
type SelectionRef = market.SelectionRef
type SelectionSnapshot = selection.SelectionSnapshot
type SelectionDigest = selection.SelectionDigestValue
type ServiceId = market.ServiceId
type Revision = market.Revision
type Milliseconds int64

func (v Milliseconds) Validate() error {
	if v < 0 {
		return errors.New("invalid broker number")
	}
	if v > 300000 {
		return errors.New("invalid broker number")
	}
	return nil
}

type RequestedTtlMs int64

func (v RequestedTtlMs) Validate() error {
	if v < 1 {
		return errors.New("invalid broker number")
	}
	if v > 300000 {
		return errors.New("invalid broker number")
	}
	return nil
}

type UnixMilliseconds int64

func (v UnixMilliseconds) Validate() error {
	if v < 1 {
		return errors.New("invalid broker number")
	}
	if v > 9007199254740991 {
		return errors.New("invalid broker number")
	}
	return nil
}

type NativeId string

func (v NativeId) Validate() error {
	if utf8.RuneCountInString(string(v)) < 1 {
		return errors.New("invalid broker text")
	}
	if utf8.RuneCountInString(string(v)) > 256 {
		return errors.New("invalid broker text")
	}
	return nil
}

type ToolName string

func (v ToolName) Validate() error {
	if utf8.RuneCountInString(string(v)) < 1 {
		return errors.New("invalid broker text")
	}
	if utf8.RuneCountInString(string(v)) > 128 {
		return errors.New("invalid broker text")
	}
	return nil
}

type ArgsDigest string

func (v ArgsDigest) Validate() error {
	if !wirePattern1.MatchString(string(v)) {
		return errors.New("invalid broker pattern")
	}
	return nil
}

type GatewayUrl string

func (v GatewayUrl) Validate() error {
	if utf8.RuneCountInString(string(v)) < 22 {
		return errors.New("invalid broker text")
	}
	if utf8.RuneCountInString(string(v)) > 32 {
		return errors.New("invalid broker text")
	}
	if !wirePattern3.MatchString(string(v)) {
		return errors.New("invalid broker pattern")
	}
	port := strings.TrimSuffix(strings.TrimPrefix(string(v), "http://127.0.0.1:"), "/mcp")
	n, err := strconv.ParseUint(port, 10, 16)
	if err != nil || n == 0 {
		return errors.New("invalid broker gateway port")
	}
	return nil
}

type LeaseState string

const (
	LeaseStatePrepared LeaseState = "prepared"
	LeaseStateBound    LeaseState = "bound"
	LeaseStateRevoked  LeaseState = "revoked"
	LeaseStateExpired  LeaseState = "expired"
)

func (v LeaseState) Validate() error {
	switch v {
	case LeaseStatePrepared, LeaseStateBound, LeaseStateRevoked, LeaseStateExpired:
		return nil
	default:
		return errors.New("invalid broker enum")
	}
}
func (v *LeaseState) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return errors.New("invalid broker enum")
	}
	x := LeaseState(s)
	if err := x.Validate(); err != nil {
		return err
	}
	*v = x
	return nil
}

type CallState string

const (
	CallStatePending   CallState = "pending"
	CallStateApproved  CallState = "approved"
	CallStateRejected  CallState = "rejected"
	CallStateCancelled CallState = "cancelled"
	CallStateConsumed  CallState = "consumed"
	CallStateExpired   CallState = "expired"
	CallStateRevoked   CallState = "revoked"
)

func (v CallState) Validate() error {
	switch v {
	case CallStatePending, CallStateApproved, CallStateRejected, CallStateCancelled, CallStateConsumed, CallStateExpired, CallStateRevoked:
		return nil
	default:
		return errors.New("invalid broker enum")
	}
}
func (v *CallState) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return errors.New("invalid broker enum")
	}
	x := CallState(s)
	if err := x.Validate(); err != nil {
		return err
	}
	*v = x
	return nil
}

type Decision string

const (
	DecisionApproveOnce Decision = "approve_once"
	DecisionReject      Decision = "reject"
	DecisionCancel      Decision = "cancel"
)

func (v Decision) Validate() error {
	switch v {
	case DecisionApproveOnce, DecisionReject, DecisionCancel:
		return nil
	default:
		return errors.New("invalid broker enum")
	}
}
func (v *Decision) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return errors.New("invalid broker enum")
	}
	x := Decision(s)
	if err := x.Validate(); err != nil {
		return err
	}
	*v = x
	return nil
}

type RevokeReason string

const (
	RevokeReasonTurnTerminal         RevokeReason = "turn_terminal"
	RevokeReasonUserCancelled        RevokeReason = "user_cancelled"
	RevokeReasonSelectionChanged     RevokeReason = "selection_changed"
	RevokeReasonAuthorizationChanged RevokeReason = "authorization_changed"
	RevokeReasonGenerationClosed     RevokeReason = "generation_closed"
	RevokeReasonOwnerShutdown        RevokeReason = "owner_shutdown"
)

func (v RevokeReason) Validate() error {
	switch v {
	case RevokeReasonTurnTerminal, RevokeReasonUserCancelled, RevokeReasonSelectionChanged, RevokeReasonAuthorizationChanged, RevokeReasonGenerationClosed, RevokeReasonOwnerShutdown:
		return nil
	default:
		return errors.New("invalid broker enum")
	}
}
func (v *RevokeReason) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return errors.New("invalid broker enum")
	}
	x := RevokeReason(s)
	if err := x.Validate(); err != nil {
		return err
	}
	*v = x
	return nil
}

type ShutdownReason string

const (
	ShutdownReasonOwnerShutdown    ShutdownReason = "owner_shutdown"
	ShutdownReasonGenerationClosed ShutdownReason = "generation_closed"
)

func (v ShutdownReason) Validate() error {
	switch v {
	case ShutdownReasonOwnerShutdown, ShutdownReasonGenerationClosed:
		return nil
	default:
		return errors.New("invalid broker enum")
	}
}
func (v *ShutdownReason) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return errors.New("invalid broker enum")
	}
	x := ShutdownReason(s)
	if err := x.Validate(); err != nil {
		return err
	}
	*v = x
	return nil
}

type ErrorCode string

const (
	ErrorCodeInvalidRequest         ErrorCode = "invalid_request"
	ErrorCodeNotInitialized         ErrorCode = "not_initialized"
	ErrorCodeAlreadyInitialized     ErrorCode = "already_initialized"
	ErrorCodeBindingMismatch        ErrorCode = "binding_mismatch"
	ErrorCodeNotFound               ErrorCode = "not_found"
	ErrorCodeRequestConflict        ErrorCode = "request_conflict"
	ErrorCodeRevisionConflict       ErrorCode = "revision_conflict"
	ErrorCodeScopeExpired           ErrorCode = "scope_expired"
	ErrorCodeLeaseExpired           ErrorCode = "lease_expired"
	ErrorCodeLeaseRevoked           ErrorCode = "lease_revoked"
	ErrorCodeTurnMismatch           ErrorCode = "turn_mismatch"
	ErrorCodeNotBound               ErrorCode = "not_bound"
	ErrorCodeNotQualified           ErrorCode = "not_qualified"
	ErrorCodeCapacityExceeded       ErrorCode = "capacity_exceeded"
	ErrorCodeCallExpired            ErrorCode = "call_expired"
	ErrorCodeCallConsumed           ErrorCode = "call_consumed"
	ErrorCodeApprovalInvalid        ErrorCode = "approval_invalid"
	ErrorCodeStopping               ErrorCode = "stopping"
	ErrorCodeTemporarilyUnavailable ErrorCode = "temporarily_unavailable"
)

func (v ErrorCode) Validate() error {
	switch v {
	case ErrorCodeInvalidRequest, ErrorCodeNotInitialized, ErrorCodeAlreadyInitialized, ErrorCodeBindingMismatch, ErrorCodeNotFound, ErrorCodeRequestConflict, ErrorCodeRevisionConflict, ErrorCodeScopeExpired, ErrorCodeLeaseExpired, ErrorCodeLeaseRevoked, ErrorCodeTurnMismatch, ErrorCodeNotBound, ErrorCodeNotQualified, ErrorCodeCapacityExceeded, ErrorCodeCallExpired, ErrorCodeCallConsumed, ErrorCodeApprovalInvalid, ErrorCodeStopping, ErrorCodeTemporarilyUnavailable:
		return nil
	default:
		return errors.New("invalid broker enum")
	}
}
func (v *ErrorCode) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return errors.New("invalid broker enum")
	}
	x := ErrorCode(s)
	if err := x.Validate(); err != nil {
		return err
	}
	*v = x
	return nil
}

type ProcessBinding struct {
	HostInstanceId    CanonicalId `json:"hostInstanceId"`
	RuntimeGeneration CanonicalId `json:"runtimeGeneration"`
}

func (v ProcessBinding) Validate() error {
	if err := v.HostInstanceId.Validate(); err != nil {
		return err
	}
	if err := v.RuntimeGeneration.Validate(); err != nil {
		return err
	}
	return nil
}
func (v *ProcessBinding) UnmarshalJSON(b []byte) error {
	type wire ProcessBinding
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil || raw == nil {
		return errors.New("invalid broker object")
	}
	if _, ok := raw["hostInstanceId"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["hostInstanceId"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["runtimeGeneration"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["runtimeGeneration"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	var w wire
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if err := d.Decode(&w); err != nil {
		return errors.New("invalid broker object")
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return errors.New("invalid broker object")
	}
	x := ProcessBinding(w)
	if err := x.Validate(); err != nil {
		return err
	}
	*v = x
	return nil
}

type ScopeBinding struct {
	OwnerUserId                  CanonicalId      `json:"ownerUserId"`
	TenantId                     CanonicalId      `json:"tenantId"`
	NativeProcessEpoch           CanonicalId      `json:"nativeProcessEpoch"`
	AuthorizationRevision        Revision         `json:"authorizationRevision"`
	AuthorizationExpiresAtUnixMs UnixMilliseconds `json:"authorizationExpiresAtUnixMs"`
}

func (v ScopeBinding) Validate() error {
	if err := v.OwnerUserId.Validate(); err != nil {
		return err
	}
	if err := v.TenantId.Validate(); err != nil {
		return err
	}
	if err := v.NativeProcessEpoch.Validate(); err != nil {
		return err
	}
	if err := v.AuthorizationRevision.Validate(); err != nil {
		return err
	}
	if err := v.AuthorizationExpiresAtUnixMs.Validate(); err != nil {
		return err
	}
	return nil
}
func (v *ScopeBinding) UnmarshalJSON(b []byte) error {
	type wire ScopeBinding
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil || raw == nil {
		return errors.New("invalid broker object")
	}
	if _, ok := raw["ownerUserId"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["ownerUserId"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["tenantId"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["tenantId"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["nativeProcessEpoch"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["nativeProcessEpoch"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["authorizationRevision"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["authorizationRevision"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["authorizationExpiresAtUnixMs"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["authorizationExpiresAtUnixMs"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	var w wire
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if err := d.Decode(&w); err != nil {
		return errors.New("invalid broker object")
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return errors.New("invalid broker object")
	}
	x := ScopeBinding(w)
	if err := x.Validate(); err != nil {
		return err
	}
	*v = x
	return nil
}

type ContextBinding struct {
	Process        ProcessBinding `json:"process"`
	Scope          ScopeBinding   `json:"scope"`
	AgentSessionId CanonicalId    `json:"agentSessionId"`
	NativeThreadId *NativeId      `json:"nativeThreadId"`
}

func (v ContextBinding) Validate() error {
	if err := v.Process.Validate(); err != nil {
		return err
	}
	if err := v.Scope.Validate(); err != nil {
		return err
	}
	if err := v.AgentSessionId.Validate(); err != nil {
		return err
	}
	if v.NativeThreadId != nil {
		if err := (*v.NativeThreadId).Validate(); err != nil {
			return err
		}
	}
	return nil
}
func (v *ContextBinding) UnmarshalJSON(b []byte) error {
	type wire ContextBinding
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil || raw == nil {
		return errors.New("invalid broker object")
	}
	if _, ok := raw["process"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["process"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["scope"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["scope"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["agentSessionId"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["agentSessionId"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["nativeThreadId"]; !ok {
		return errors.New("missing broker field")
	}
	var w wire
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if err := d.Decode(&w); err != nil {
		return errors.New("invalid broker object")
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return errors.New("invalid broker object")
	}
	x := ContextBinding(w)
	if err := x.Validate(); err != nil {
		return err
	}
	*v = x
	return nil
}

type CapabilityBinding struct {
	Context         ContextBinding  `json:"context"`
	CapabilityRef   CanonicalId     `json:"capabilityRef"`
	TurnOperationId CanonicalId     `json:"turnOperationId"`
	SelectionDigest SelectionDigest `json:"selectionDigest"`
}

func (v CapabilityBinding) Validate() error {
	if err := v.Context.Validate(); err != nil {
		return err
	}
	if err := v.CapabilityRef.Validate(); err != nil {
		return err
	}
	if err := v.TurnOperationId.Validate(); err != nil {
		return err
	}
	if err := v.SelectionDigest.Validate(); err != nil {
		return err
	}
	return nil
}
func (v *CapabilityBinding) UnmarshalJSON(b []byte) error {
	type wire CapabilityBinding
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil || raw == nil {
		return errors.New("invalid broker object")
	}
	if _, ok := raw["context"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["context"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["capabilityRef"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["capabilityRef"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["turnOperationId"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["turnOperationId"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["selectionDigest"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["selectionDigest"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	var w wire
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if err := d.Decode(&w); err != nil {
		return errors.New("invalid broker object")
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return errors.New("invalid broker object")
	}
	x := CapabilityBinding(w)
	if err := x.Validate(); err != nil {
		return err
	}
	*v = x
	return nil
}

type Lease struct {
	Binding             CapabilityBinding `json:"binding"`
	Snapshot            SelectionSnapshot `json:"snapshot"`
	Revision            Revision          `json:"revision"`
	State               LeaseState        `json:"state"`
	RemainingTtlMs      Milliseconds      `json:"remainingTtlMs"`
	NativeTurnId        *NativeId         `json:"nativeTurnId,omitempty"`
	BoundNativeThreadId *NativeId         `json:"boundNativeThreadId,omitempty"`
}

func (v Lease) Validate() error {
	if err := v.Binding.Validate(); err != nil {
		return err
	}
	if err := v.Snapshot.Validate(); err != nil {
		return err
	}
	if err := v.Revision.Validate(); err != nil {
		return err
	}
	if err := v.State.Validate(); err != nil {
		return err
	}
	if err := v.RemainingTtlMs.Validate(); err != nil {
		return err
	}
	if v.NativeTurnId != nil {
		if err := (*v.NativeTurnId).Validate(); err != nil {
			return err
		}
	}
	if v.BoundNativeThreadId != nil {
		if err := (*v.BoundNativeThreadId).Validate(); err != nil {
			return err
		}
	}
	if v.Snapshot.TurnOperationId != v.Binding.TurnOperationId || string(v.Snapshot.SelectionDigest) != string(v.Binding.SelectionDigest) {
		return errors.New("lease snapshot binding mismatch")
	}
	if (v.NativeTurnId == nil) != (v.BoundNativeThreadId == nil) || (v.State == LeaseStatePrepared && v.NativeTurnId != nil) || (v.State == LeaseStateBound && v.NativeTurnId == nil) {
		return errors.New("invalid lease turn binding")
	}
	if v.Binding.Context.NativeThreadId != nil && v.BoundNativeThreadId != nil && *v.Binding.Context.NativeThreadId != *v.BoundNativeThreadId {
		return errors.New("lease thread binding mismatch")
	}
	return nil
}
func (v *Lease) UnmarshalJSON(b []byte) error {
	type wire Lease
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil || raw == nil {
		return errors.New("invalid broker object")
	}
	if _, ok := raw["binding"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["binding"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["snapshot"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["snapshot"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["revision"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["revision"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["state"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["state"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["remainingTtlMs"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["remainingTtlMs"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if x, ok := raw["nativeTurnId"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if x, ok := raw["boundNativeThreadId"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	var w wire
	d := json.NewDecoder(bytes.NewReader(b))
	if err := d.Decode(&w); err != nil {
		return errors.New("invalid broker object")
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return errors.New("invalid broker object")
	}
	x := Lease(w)
	if err := x.Validate(); err != nil {
		return err
	}
	*v = x
	return nil
}

type ReviewProjection struct {
	Title         string  `json:"title"`
	Summary       string  `json:"summary"`
	Risk          string  `json:"risk"`
	ArgumentsJson *string `json:"argumentsJson,omitempty"`
	SchemaDigest  *string `json:"schemaDigest,omitempty"`
}

func (v ReviewProjection) Validate() error {
	if utf8.RuneCountInString(string(v.Title)) < 1 {
		return errors.New("invalid broker text")
	}
	if utf8.RuneCountInString(string(v.Title)) > 160 {
		return errors.New("invalid broker text")
	}
	if utf8.RuneCountInString(string(v.Summary)) < 1 {
		return errors.New("invalid broker text")
	}
	if utf8.RuneCountInString(string(v.Summary)) > 2048 {
		return errors.New("invalid broker text")
	}
	switch string(v.Risk) {
	case "read", "write":
	default:
		return errors.New("invalid broker enum")
	}
	if v.ArgumentsJson != nil {
		if utf8.RuneCountInString(string((*v.ArgumentsJson))) < 2 {
			return errors.New("invalid broker text")
		}
		if utf8.RuneCountInString(string((*v.ArgumentsJson))) > 16384 {
			return errors.New("invalid broker text")
		}
	}
	if v.SchemaDigest != nil {
		if !wirePattern1.MatchString(string((*v.SchemaDigest))) {
			return errors.New("invalid broker pattern")
		}
	}
	return nil
}
func (v *ReviewProjection) UnmarshalJSON(b []byte) error {
	type wire ReviewProjection
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil || raw == nil {
		return errors.New("invalid broker object")
	}
	if _, ok := raw["title"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["title"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["summary"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["summary"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["risk"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["risk"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if x, ok := raw["argumentsJson"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if x, ok := raw["schemaDigest"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	var w wire
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if err := d.Decode(&w); err != nil {
		return errors.New("invalid broker object")
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return errors.New("invalid broker object")
	}
	x := ReviewProjection(w)
	if err := x.Validate(); err != nil {
		return err
	}
	*v = x
	return nil
}

type CallIdentity struct {
	Binding        CapabilityBinding `json:"binding"`
	NativeTurnId   NativeId          `json:"nativeTurnId"`
	CallRef        CanonicalId       `json:"callRef"`
	ServiceId      ServiceId         `json:"serviceId"`
	Reference      SelectionRef      `json:"reference"`
	ToolName       ToolName          `json:"toolName"`
	ArgsDigest     ArgsDigest        `json:"argsDigest"`
	ArgsEncoding   string            `json:"argsEncoding"`
	NativeThreadId NativeId          `json:"nativeThreadId"`
}

func (v CallIdentity) Validate() error {
	if err := v.Binding.Validate(); err != nil {
		return err
	}
	if err := v.NativeTurnId.Validate(); err != nil {
		return err
	}
	if err := v.CallRef.Validate(); err != nil {
		return err
	}
	if err := v.ServiceId.Validate(); err != nil {
		return err
	}
	if err := v.Reference.Validate(); err != nil {
		return err
	}
	if err := v.ToolName.Validate(); err != nil {
		return err
	}
	if err := v.ArgsDigest.Validate(); err != nil {
		return err
	}
	if string(v.ArgsEncoding) != "worker-json-v1" {
		return errors.New("invalid broker constant")
	}
	if err := v.NativeThreadId.Validate(); err != nil {
		return err
	}
	return nil
}
func (v *CallIdentity) UnmarshalJSON(b []byte) error {
	type wire CallIdentity
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil || raw == nil {
		return errors.New("invalid broker object")
	}
	if _, ok := raw["binding"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["binding"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["nativeTurnId"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["nativeTurnId"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["callRef"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["callRef"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["serviceId"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["serviceId"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["reference"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["reference"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["toolName"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["toolName"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["argsDigest"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["argsDigest"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["argsEncoding"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["argsEncoding"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["nativeThreadId"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["nativeThreadId"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	var w wire
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if err := d.Decode(&w); err != nil {
		return errors.New("invalid broker object")
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return errors.New("invalid broker object")
	}
	x := CallIdentity(w)
	if err := x.Validate(); err != nil {
		return err
	}
	*v = x
	return nil
}

type PendingCall struct {
	Identity       CallIdentity     `json:"identity"`
	Revision       Revision         `json:"revision"`
	State          CallState        `json:"state"`
	RemainingTtlMs Milliseconds     `json:"remainingTtlMs"`
	Review         ReviewProjection `json:"review"`
	DecisionId     *CanonicalId     `json:"decisionId,omitempty"`
	Decision       *Decision        `json:"decision,omitempty"`
}

func (v PendingCall) Validate() error {
	if err := v.Identity.Validate(); err != nil {
		return err
	}
	if err := v.Revision.Validate(); err != nil {
		return err
	}
	if err := v.State.Validate(); err != nil {
		return err
	}
	if err := v.RemainingTtlMs.Validate(); err != nil {
		return err
	}
	if err := v.Review.Validate(); err != nil {
		return err
	}
	if v.DecisionId != nil {
		if err := (*v.DecisionId).Validate(); err != nil {
			return err
		}
	}
	if v.Decision != nil {
		if err := (*v.Decision).Validate(); err != nil {
			return err
		}
	}
	return nil
}
func (v *PendingCall) UnmarshalJSON(b []byte) error {
	type wire PendingCall
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil || raw == nil {
		return errors.New("invalid broker object")
	}
	if _, ok := raw["identity"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["identity"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["revision"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["revision"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["state"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["state"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["remainingTtlMs"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["remainingTtlMs"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["review"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["review"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if x, ok := raw["decisionId"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if x, ok := raw["decision"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	var w wire
	d := json.NewDecoder(bytes.NewReader(b))
	if err := d.Decode(&w); err != nil {
		return errors.New("invalid broker object")
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return errors.New("invalid broker object")
	}
	x := PendingCall(w)
	if err := x.Validate(); err != nil {
		return err
	}
	*v = x
	return nil
}

type InitializePayload struct {
	OperationId CanonicalId    `json:"operationId"`
	Process     ProcessBinding `json:"process"`
}

func (v InitializePayload) Validate() error {
	if err := v.OperationId.Validate(); err != nil {
		return err
	}
	if err := v.Process.Validate(); err != nil {
		return err
	}
	return nil
}
func (v *InitializePayload) UnmarshalJSON(b []byte) error {
	type wire InitializePayload
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil || raw == nil {
		return errors.New("invalid broker object")
	}
	if _, ok := raw["operationId"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["operationId"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["process"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["process"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	var w wire
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if err := d.Decode(&w); err != nil {
		return errors.New("invalid broker object")
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return errors.New("invalid broker object")
	}
	x := InitializePayload(w)
	if err := x.Validate(); err != nil {
		return err
	}
	*v = x
	return nil
}

type InitializeRequest struct {
	SchemaVersion int64             `json:"schemaVersion"`
	RequestId     CanonicalId       `json:"requestId"`
	Method        string            `json:"method"`
	Payload       InitializePayload `json:"payload"`
}

func (v InitializeRequest) Validate() error {
	if v.SchemaVersion != 1 {
		return errors.New("invalid broker constant")
	}
	if err := v.RequestId.Validate(); err != nil {
		return err
	}
	if string(v.Method) != "initialize" {
		return errors.New("invalid broker constant")
	}
	if err := v.Payload.Validate(); err != nil {
		return err
	}
	return nil
}
func (v *InitializeRequest) UnmarshalJSON(b []byte) error {
	type wire InitializeRequest
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil || raw == nil {
		return errors.New("invalid broker object")
	}
	if _, ok := raw["schemaVersion"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["schemaVersion"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["requestId"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["requestId"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["method"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["method"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["payload"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["payload"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	var w wire
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if err := d.Decode(&w); err != nil {
		return errors.New("invalid broker object")
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return errors.New("invalid broker object")
	}
	x := InitializeRequest(w)
	if err := x.Validate(); err != nil {
		return err
	}
	*v = x
	return nil
}

type InitializeResult struct {
	WorkerInstanceId     CanonicalId    `json:"workerInstanceId"`
	Process              ProcessBinding `json:"process"`
	GatewayUrl           GatewayUrl     `json:"gatewayUrl"`
	ExternalCallsEnabled bool           `json:"externalCallsEnabled"`
}

func (v InitializeResult) Validate() error {
	if err := v.WorkerInstanceId.Validate(); err != nil {
		return err
	}
	if err := v.Process.Validate(); err != nil {
		return err
	}
	if err := v.GatewayUrl.Validate(); err != nil {
		return err
	}
	return nil
}
func (v *InitializeResult) UnmarshalJSON(b []byte) error {
	type wire InitializeResult
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil || raw == nil {
		return errors.New("invalid broker object")
	}
	if _, ok := raw["workerInstanceId"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["workerInstanceId"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["process"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["process"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["gatewayUrl"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["gatewayUrl"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["externalCallsEnabled"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["externalCallsEnabled"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	var w wire
	d := json.NewDecoder(bytes.NewReader(b))
	if err := d.Decode(&w); err != nil {
		return errors.New("invalid broker object")
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return errors.New("invalid broker object")
	}
	x := InitializeResult(w)
	if err := x.Validate(); err != nil {
		return err
	}
	*v = x
	return nil
}

type InitializeResponse struct {
	SchemaVersion int64            `json:"schemaVersion"`
	RequestId     CanonicalId      `json:"requestId"`
	Data          InitializeResult `json:"data"`
}

func (v InitializeResponse) Validate() error {
	if v.SchemaVersion != 1 {
		return errors.New("invalid broker constant")
	}
	if err := v.RequestId.Validate(); err != nil {
		return err
	}
	if err := v.Data.Validate(); err != nil {
		return err
	}
	return nil
}
func (v *InitializeResponse) UnmarshalJSON(b []byte) error {
	type wire InitializeResponse
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil || raw == nil {
		return errors.New("invalid broker object")
	}
	if _, ok := raw["schemaVersion"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["schemaVersion"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["requestId"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["requestId"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["data"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["data"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	var w wire
	d := json.NewDecoder(bytes.NewReader(b))
	if err := d.Decode(&w); err != nil {
		return errors.New("invalid broker object")
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return errors.New("invalid broker object")
	}
	x := InitializeResponse(w)
	if err := x.Validate(); err != nil {
		return err
	}
	*v = x
	return nil
}

type PreparePayload struct {
	OperationId CanonicalId       `json:"operationId"`
	Context     ContextBinding    `json:"context"`
	Snapshot    SelectionSnapshot `json:"snapshot"`
	TtlMs       RequestedTtlMs    `json:"ttlMs"`
}

func (v PreparePayload) Validate() error {
	if err := v.OperationId.Validate(); err != nil {
		return err
	}
	if err := v.Context.Validate(); err != nil {
		return err
	}
	if err := v.Snapshot.Validate(); err != nil {
		return err
	}
	if err := v.TtlMs.Validate(); err != nil {
		return err
	}
	return nil
}
func (v *PreparePayload) UnmarshalJSON(b []byte) error {
	type wire PreparePayload
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil || raw == nil {
		return errors.New("invalid broker object")
	}
	if _, ok := raw["operationId"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["operationId"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["context"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["context"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["snapshot"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["snapshot"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["ttlMs"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["ttlMs"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	var w wire
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if err := d.Decode(&w); err != nil {
		return errors.New("invalid broker object")
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return errors.New("invalid broker object")
	}
	x := PreparePayload(w)
	if err := x.Validate(); err != nil {
		return err
	}
	*v = x
	return nil
}

type PrepareRequest struct {
	SchemaVersion int64          `json:"schemaVersion"`
	RequestId     CanonicalId    `json:"requestId"`
	Method        string         `json:"method"`
	Payload       PreparePayload `json:"payload"`
}

func (v PrepareRequest) Validate() error {
	if v.SchemaVersion != 1 {
		return errors.New("invalid broker constant")
	}
	if err := v.RequestId.Validate(); err != nil {
		return err
	}
	if string(v.Method) != "prepare" {
		return errors.New("invalid broker constant")
	}
	if err := v.Payload.Validate(); err != nil {
		return err
	}
	return nil
}
func (v *PrepareRequest) UnmarshalJSON(b []byte) error {
	type wire PrepareRequest
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil || raw == nil {
		return errors.New("invalid broker object")
	}
	if _, ok := raw["schemaVersion"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["schemaVersion"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["requestId"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["requestId"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["method"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["method"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["payload"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["payload"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	var w wire
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if err := d.Decode(&w); err != nil {
		return errors.New("invalid broker object")
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return errors.New("invalid broker object")
	}
	x := PrepareRequest(w)
	if err := x.Validate(); err != nil {
		return err
	}
	*v = x
	return nil
}

type PrepareResponse struct {
	SchemaVersion int64       `json:"schemaVersion"`
	RequestId     CanonicalId `json:"requestId"`
	Data          Lease       `json:"data"`
}

func (v PrepareResponse) Validate() error {
	if v.SchemaVersion != 1 {
		return errors.New("invalid broker constant")
	}
	if err := v.RequestId.Validate(); err != nil {
		return err
	}
	if err := v.Data.Validate(); err != nil {
		return err
	}
	return nil
}
func (v *PrepareResponse) UnmarshalJSON(b []byte) error {
	type wire PrepareResponse
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil || raw == nil {
		return errors.New("invalid broker object")
	}
	if _, ok := raw["schemaVersion"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["schemaVersion"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["requestId"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["requestId"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["data"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["data"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	var w wire
	d := json.NewDecoder(bytes.NewReader(b))
	if err := d.Decode(&w); err != nil {
		return errors.New("invalid broker object")
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return errors.New("invalid broker object")
	}
	x := PrepareResponse(w)
	if err := x.Validate(); err != nil {
		return err
	}
	*v = x
	return nil
}

type BindTurnPayload struct {
	OperationId      CanonicalId       `json:"operationId"`
	Binding          CapabilityBinding `json:"binding"`
	ExpectedRevision Revision          `json:"expectedRevision"`
	NativeTurnId     NativeId          `json:"nativeTurnId"`
	NativeThreadId   NativeId          `json:"nativeThreadId"`
}

func (v BindTurnPayload) Validate() error {
	if err := v.OperationId.Validate(); err != nil {
		return err
	}
	if err := v.Binding.Validate(); err != nil {
		return err
	}
	if err := v.ExpectedRevision.Validate(); err != nil {
		return err
	}
	if err := v.NativeTurnId.Validate(); err != nil {
		return err
	}
	if err := v.NativeThreadId.Validate(); err != nil {
		return err
	}
	return nil
}
func (v *BindTurnPayload) UnmarshalJSON(b []byte) error {
	type wire BindTurnPayload
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil || raw == nil {
		return errors.New("invalid broker object")
	}
	if _, ok := raw["operationId"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["operationId"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["binding"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["binding"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["expectedRevision"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["expectedRevision"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["nativeTurnId"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["nativeTurnId"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["nativeThreadId"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["nativeThreadId"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	var w wire
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if err := d.Decode(&w); err != nil {
		return errors.New("invalid broker object")
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return errors.New("invalid broker object")
	}
	x := BindTurnPayload(w)
	if err := x.Validate(); err != nil {
		return err
	}
	*v = x
	return nil
}

type BindTurnRequest struct {
	SchemaVersion int64           `json:"schemaVersion"`
	RequestId     CanonicalId     `json:"requestId"`
	Method        string          `json:"method"`
	Payload       BindTurnPayload `json:"payload"`
}

func (v BindTurnRequest) Validate() error {
	if v.SchemaVersion != 1 {
		return errors.New("invalid broker constant")
	}
	if err := v.RequestId.Validate(); err != nil {
		return err
	}
	if string(v.Method) != "bind_turn" {
		return errors.New("invalid broker constant")
	}
	if err := v.Payload.Validate(); err != nil {
		return err
	}
	return nil
}
func (v *BindTurnRequest) UnmarshalJSON(b []byte) error {
	type wire BindTurnRequest
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil || raw == nil {
		return errors.New("invalid broker object")
	}
	if _, ok := raw["schemaVersion"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["schemaVersion"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["requestId"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["requestId"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["method"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["method"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["payload"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["payload"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	var w wire
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if err := d.Decode(&w); err != nil {
		return errors.New("invalid broker object")
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return errors.New("invalid broker object")
	}
	x := BindTurnRequest(w)
	if err := x.Validate(); err != nil {
		return err
	}
	*v = x
	return nil
}

type BindTurnResponse struct {
	SchemaVersion int64       `json:"schemaVersion"`
	RequestId     CanonicalId `json:"requestId"`
	Data          Lease       `json:"data"`
}

func (v BindTurnResponse) Validate() error {
	if v.SchemaVersion != 1 {
		return errors.New("invalid broker constant")
	}
	if err := v.RequestId.Validate(); err != nil {
		return err
	}
	if err := v.Data.Validate(); err != nil {
		return err
	}
	return nil
}
func (v *BindTurnResponse) UnmarshalJSON(b []byte) error {
	type wire BindTurnResponse
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil || raw == nil {
		return errors.New("invalid broker object")
	}
	if _, ok := raw["schemaVersion"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["schemaVersion"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["requestId"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["requestId"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["data"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["data"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	var w wire
	d := json.NewDecoder(bytes.NewReader(b))
	if err := d.Decode(&w); err != nil {
		return errors.New("invalid broker object")
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return errors.New("invalid broker object")
	}
	x := BindTurnResponse(w)
	if err := x.Validate(); err != nil {
		return err
	}
	*v = x
	return nil
}

type RevokePayload struct {
	OperationId      CanonicalId       `json:"operationId"`
	Binding          CapabilityBinding `json:"binding"`
	ExpectedRevision Revision          `json:"expectedRevision"`
	Reason           RevokeReason      `json:"reason"`
}

func (v RevokePayload) Validate() error {
	if err := v.OperationId.Validate(); err != nil {
		return err
	}
	if err := v.Binding.Validate(); err != nil {
		return err
	}
	if err := v.ExpectedRevision.Validate(); err != nil {
		return err
	}
	if err := v.Reason.Validate(); err != nil {
		return err
	}
	return nil
}
func (v *RevokePayload) UnmarshalJSON(b []byte) error {
	type wire RevokePayload
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil || raw == nil {
		return errors.New("invalid broker object")
	}
	if _, ok := raw["operationId"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["operationId"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["binding"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["binding"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["expectedRevision"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["expectedRevision"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["reason"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["reason"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	var w wire
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if err := d.Decode(&w); err != nil {
		return errors.New("invalid broker object")
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return errors.New("invalid broker object")
	}
	x := RevokePayload(w)
	if err := x.Validate(); err != nil {
		return err
	}
	*v = x
	return nil
}

type RevokeRequest struct {
	SchemaVersion int64         `json:"schemaVersion"`
	RequestId     CanonicalId   `json:"requestId"`
	Method        string        `json:"method"`
	Payload       RevokePayload `json:"payload"`
}

func (v RevokeRequest) Validate() error {
	if v.SchemaVersion != 1 {
		return errors.New("invalid broker constant")
	}
	if err := v.RequestId.Validate(); err != nil {
		return err
	}
	if string(v.Method) != "revoke" {
		return errors.New("invalid broker constant")
	}
	if err := v.Payload.Validate(); err != nil {
		return err
	}
	return nil
}
func (v *RevokeRequest) UnmarshalJSON(b []byte) error {
	type wire RevokeRequest
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil || raw == nil {
		return errors.New("invalid broker object")
	}
	if _, ok := raw["schemaVersion"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["schemaVersion"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["requestId"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["requestId"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["method"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["method"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["payload"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["payload"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	var w wire
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if err := d.Decode(&w); err != nil {
		return errors.New("invalid broker object")
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return errors.New("invalid broker object")
	}
	x := RevokeRequest(w)
	if err := x.Validate(); err != nil {
		return err
	}
	*v = x
	return nil
}

type RevokeResponse struct {
	SchemaVersion int64       `json:"schemaVersion"`
	RequestId     CanonicalId `json:"requestId"`
	Data          Lease       `json:"data"`
}

func (v RevokeResponse) Validate() error {
	if v.SchemaVersion != 1 {
		return errors.New("invalid broker constant")
	}
	if err := v.RequestId.Validate(); err != nil {
		return err
	}
	if err := v.Data.Validate(); err != nil {
		return err
	}
	return nil
}
func (v *RevokeResponse) UnmarshalJSON(b []byte) error {
	type wire RevokeResponse
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil || raw == nil {
		return errors.New("invalid broker object")
	}
	if _, ok := raw["schemaVersion"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["schemaVersion"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["requestId"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["requestId"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["data"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["data"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	var w wire
	d := json.NewDecoder(bytes.NewReader(b))
	if err := d.Decode(&w); err != nil {
		return errors.New("invalid broker object")
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return errors.New("invalid broker object")
	}
	x := RevokeResponse(w)
	if err := x.Validate(); err != nil {
		return err
	}
	*v = x
	return nil
}

type StatusPayload struct {
	Binding CapabilityBinding `json:"binding"`
}

func (v StatusPayload) Validate() error {
	if err := v.Binding.Validate(); err != nil {
		return err
	}
	return nil
}
func (v *StatusPayload) UnmarshalJSON(b []byte) error {
	type wire StatusPayload
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil || raw == nil {
		return errors.New("invalid broker object")
	}
	if _, ok := raw["binding"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["binding"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	var w wire
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if err := d.Decode(&w); err != nil {
		return errors.New("invalid broker object")
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return errors.New("invalid broker object")
	}
	x := StatusPayload(w)
	if err := x.Validate(); err != nil {
		return err
	}
	*v = x
	return nil
}

type StatusRequest struct {
	SchemaVersion int64         `json:"schemaVersion"`
	RequestId     CanonicalId   `json:"requestId"`
	Method        string        `json:"method"`
	Payload       StatusPayload `json:"payload"`
}

func (v StatusRequest) Validate() error {
	if v.SchemaVersion != 1 {
		return errors.New("invalid broker constant")
	}
	if err := v.RequestId.Validate(); err != nil {
		return err
	}
	if string(v.Method) != "status" {
		return errors.New("invalid broker constant")
	}
	if err := v.Payload.Validate(); err != nil {
		return err
	}
	return nil
}
func (v *StatusRequest) UnmarshalJSON(b []byte) error {
	type wire StatusRequest
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil || raw == nil {
		return errors.New("invalid broker object")
	}
	if _, ok := raw["schemaVersion"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["schemaVersion"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["requestId"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["requestId"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["method"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["method"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["payload"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["payload"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	var w wire
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if err := d.Decode(&w); err != nil {
		return errors.New("invalid broker object")
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return errors.New("invalid broker object")
	}
	x := StatusRequest(w)
	if err := x.Validate(); err != nil {
		return err
	}
	*v = x
	return nil
}

type StatusResponse struct {
	SchemaVersion int64       `json:"schemaVersion"`
	RequestId     CanonicalId `json:"requestId"`
	Data          Lease       `json:"data"`
}

func (v StatusResponse) Validate() error {
	if v.SchemaVersion != 1 {
		return errors.New("invalid broker constant")
	}
	if err := v.RequestId.Validate(); err != nil {
		return err
	}
	if err := v.Data.Validate(); err != nil {
		return err
	}
	return nil
}
func (v *StatusResponse) UnmarshalJSON(b []byte) error {
	type wire StatusResponse
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil || raw == nil {
		return errors.New("invalid broker object")
	}
	if _, ok := raw["schemaVersion"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["schemaVersion"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["requestId"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["requestId"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["data"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["data"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	var w wire
	d := json.NewDecoder(bytes.NewReader(b))
	if err := d.Decode(&w); err != nil {
		return errors.New("invalid broker object")
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return errors.New("invalid broker object")
	}
	x := StatusResponse(w)
	if err := x.Validate(); err != nil {
		return err
	}
	*v = x
	return nil
}

type PendingCallPayload struct {
	Binding        CapabilityBinding `json:"binding"`
	NativeTurnId   NativeId          `json:"nativeTurnId"`
	CallRef        CanonicalId       `json:"callRef"`
	NativeThreadId NativeId          `json:"nativeThreadId"`
}

func (v PendingCallPayload) Validate() error {
	if err := v.Binding.Validate(); err != nil {
		return err
	}
	if err := v.NativeTurnId.Validate(); err != nil {
		return err
	}
	if err := v.CallRef.Validate(); err != nil {
		return err
	}
	if err := v.NativeThreadId.Validate(); err != nil {
		return err
	}
	return nil
}
func (v *PendingCallPayload) UnmarshalJSON(b []byte) error {
	type wire PendingCallPayload
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil || raw == nil {
		return errors.New("invalid broker object")
	}
	if _, ok := raw["binding"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["binding"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["nativeTurnId"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["nativeTurnId"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["callRef"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["callRef"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["nativeThreadId"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["nativeThreadId"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	var w wire
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if err := d.Decode(&w); err != nil {
		return errors.New("invalid broker object")
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return errors.New("invalid broker object")
	}
	x := PendingCallPayload(w)
	if err := x.Validate(); err != nil {
		return err
	}
	*v = x
	return nil
}

type PendingCallRequest struct {
	SchemaVersion int64              `json:"schemaVersion"`
	RequestId     CanonicalId        `json:"requestId"`
	Method        string             `json:"method"`
	Payload       PendingCallPayload `json:"payload"`
}

func (v PendingCallRequest) Validate() error {
	if v.SchemaVersion != 1 {
		return errors.New("invalid broker constant")
	}
	if err := v.RequestId.Validate(); err != nil {
		return err
	}
	if string(v.Method) != "pending_call" {
		return errors.New("invalid broker constant")
	}
	if err := v.Payload.Validate(); err != nil {
		return err
	}
	return nil
}
func (v *PendingCallRequest) UnmarshalJSON(b []byte) error {
	type wire PendingCallRequest
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil || raw == nil {
		return errors.New("invalid broker object")
	}
	if _, ok := raw["schemaVersion"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["schemaVersion"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["requestId"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["requestId"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["method"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["method"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["payload"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["payload"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	var w wire
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if err := d.Decode(&w); err != nil {
		return errors.New("invalid broker object")
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return errors.New("invalid broker object")
	}
	x := PendingCallRequest(w)
	if err := x.Validate(); err != nil {
		return err
	}
	*v = x
	return nil
}

type PendingCallResponse struct {
	SchemaVersion int64       `json:"schemaVersion"`
	RequestId     CanonicalId `json:"requestId"`
	Data          PendingCall `json:"data"`
}

func (v PendingCallResponse) Validate() error {
	if v.SchemaVersion != 1 {
		return errors.New("invalid broker constant")
	}
	if err := v.RequestId.Validate(); err != nil {
		return err
	}
	if err := v.Data.Validate(); err != nil {
		return err
	}
	return nil
}
func (v *PendingCallResponse) UnmarshalJSON(b []byte) error {
	type wire PendingCallResponse
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil || raw == nil {
		return errors.New("invalid broker object")
	}
	if _, ok := raw["schemaVersion"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["schemaVersion"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["requestId"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["requestId"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["data"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["data"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	var w wire
	d := json.NewDecoder(bytes.NewReader(b))
	if err := d.Decode(&w); err != nil {
		return errors.New("invalid broker object")
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return errors.New("invalid broker object")
	}
	x := PendingCallResponse(w)
	if err := x.Validate(); err != nil {
		return err
	}
	*v = x
	return nil
}

type DecideCallPayload struct {
	OperationId      CanonicalId  `json:"operationId"`
	Identity         CallIdentity `json:"identity"`
	ExpectedRevision Revision     `json:"expectedRevision"`
	ApprovalRef      CanonicalId  `json:"approvalRef"`
	DecisionId       CanonicalId  `json:"decisionId"`
	Decision         Decision     `json:"decision"`
}

func (v DecideCallPayload) Validate() error {
	if err := v.OperationId.Validate(); err != nil {
		return err
	}
	if err := v.Identity.Validate(); err != nil {
		return err
	}
	if err := v.ExpectedRevision.Validate(); err != nil {
		return err
	}
	if err := v.ApprovalRef.Validate(); err != nil {
		return err
	}
	if err := v.DecisionId.Validate(); err != nil {
		return err
	}
	if err := v.Decision.Validate(); err != nil {
		return err
	}
	return nil
}
func (v *DecideCallPayload) UnmarshalJSON(b []byte) error {
	type wire DecideCallPayload
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil || raw == nil {
		return errors.New("invalid broker object")
	}
	if _, ok := raw["operationId"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["operationId"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["identity"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["identity"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["expectedRevision"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["expectedRevision"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["approvalRef"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["approvalRef"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["decisionId"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["decisionId"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["decision"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["decision"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	var w wire
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if err := d.Decode(&w); err != nil {
		return errors.New("invalid broker object")
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return errors.New("invalid broker object")
	}
	x := DecideCallPayload(w)
	if err := x.Validate(); err != nil {
		return err
	}
	*v = x
	return nil
}

type DecideCallRequest struct {
	SchemaVersion int64             `json:"schemaVersion"`
	RequestId     CanonicalId       `json:"requestId"`
	Method        string            `json:"method"`
	Payload       DecideCallPayload `json:"payload"`
}

func (v DecideCallRequest) Validate() error {
	if v.SchemaVersion != 1 {
		return errors.New("invalid broker constant")
	}
	if err := v.RequestId.Validate(); err != nil {
		return err
	}
	if string(v.Method) != "decide_call" {
		return errors.New("invalid broker constant")
	}
	if err := v.Payload.Validate(); err != nil {
		return err
	}
	return nil
}
func (v *DecideCallRequest) UnmarshalJSON(b []byte) error {
	type wire DecideCallRequest
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil || raw == nil {
		return errors.New("invalid broker object")
	}
	if _, ok := raw["schemaVersion"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["schemaVersion"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["requestId"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["requestId"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["method"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["method"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["payload"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["payload"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	var w wire
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if err := d.Decode(&w); err != nil {
		return errors.New("invalid broker object")
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return errors.New("invalid broker object")
	}
	x := DecideCallRequest(w)
	if err := x.Validate(); err != nil {
		return err
	}
	*v = x
	return nil
}

type DecideCallResponse struct {
	SchemaVersion int64       `json:"schemaVersion"`
	RequestId     CanonicalId `json:"requestId"`
	Data          PendingCall `json:"data"`
}

func (v DecideCallResponse) Validate() error {
	if v.SchemaVersion != 1 {
		return errors.New("invalid broker constant")
	}
	if err := v.RequestId.Validate(); err != nil {
		return err
	}
	if err := v.Data.Validate(); err != nil {
		return err
	}
	return nil
}
func (v *DecideCallResponse) UnmarshalJSON(b []byte) error {
	type wire DecideCallResponse
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil || raw == nil {
		return errors.New("invalid broker object")
	}
	if _, ok := raw["schemaVersion"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["schemaVersion"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["requestId"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["requestId"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["data"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["data"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	var w wire
	d := json.NewDecoder(bytes.NewReader(b))
	if err := d.Decode(&w); err != nil {
		return errors.New("invalid broker object")
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return errors.New("invalid broker object")
	}
	x := DecideCallResponse(w)
	if err := x.Validate(); err != nil {
		return err
	}
	*v = x
	return nil
}

type ShutdownPayload struct {
	OperationId CanonicalId    `json:"operationId"`
	Process     ProcessBinding `json:"process"`
	Reason      ShutdownReason `json:"reason"`
}

func (v ShutdownPayload) Validate() error {
	if err := v.OperationId.Validate(); err != nil {
		return err
	}
	if err := v.Process.Validate(); err != nil {
		return err
	}
	if err := v.Reason.Validate(); err != nil {
		return err
	}
	return nil
}
func (v *ShutdownPayload) UnmarshalJSON(b []byte) error {
	type wire ShutdownPayload
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil || raw == nil {
		return errors.New("invalid broker object")
	}
	if _, ok := raw["operationId"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["operationId"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["process"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["process"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["reason"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["reason"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	var w wire
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if err := d.Decode(&w); err != nil {
		return errors.New("invalid broker object")
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return errors.New("invalid broker object")
	}
	x := ShutdownPayload(w)
	if err := x.Validate(); err != nil {
		return err
	}
	*v = x
	return nil
}

type ShutdownRequest struct {
	SchemaVersion int64           `json:"schemaVersion"`
	RequestId     CanonicalId     `json:"requestId"`
	Method        string          `json:"method"`
	Payload       ShutdownPayload `json:"payload"`
}

func (v ShutdownRequest) Validate() error {
	if v.SchemaVersion != 1 {
		return errors.New("invalid broker constant")
	}
	if err := v.RequestId.Validate(); err != nil {
		return err
	}
	if string(v.Method) != "shutdown" {
		return errors.New("invalid broker constant")
	}
	if err := v.Payload.Validate(); err != nil {
		return err
	}
	return nil
}
func (v *ShutdownRequest) UnmarshalJSON(b []byte) error {
	type wire ShutdownRequest
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil || raw == nil {
		return errors.New("invalid broker object")
	}
	if _, ok := raw["schemaVersion"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["schemaVersion"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["requestId"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["requestId"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["method"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["method"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["payload"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["payload"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	var w wire
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if err := d.Decode(&w); err != nil {
		return errors.New("invalid broker object")
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return errors.New("invalid broker object")
	}
	x := ShutdownRequest(w)
	if err := x.Validate(); err != nil {
		return err
	}
	*v = x
	return nil
}

type ShutdownResult struct {
	State             string `json:"state"`
	StoppedAdmissions bool   `json:"stoppedAdmissions"`
	ExternalOutcomes  string `json:"externalOutcomes"`
}

func (v ShutdownResult) Validate() error {
	if string(v.State) != "stopping" {
		return errors.New("invalid broker constant")
	}
	if v.StoppedAdmissions != true {
		return errors.New("invalid broker constant")
	}
	if string(v.ExternalOutcomes) != "not_asserted" {
		return errors.New("invalid broker constant")
	}
	return nil
}
func (v *ShutdownResult) UnmarshalJSON(b []byte) error {
	type wire ShutdownResult
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil || raw == nil {
		return errors.New("invalid broker object")
	}
	if _, ok := raw["state"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["state"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["stoppedAdmissions"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["stoppedAdmissions"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["externalOutcomes"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["externalOutcomes"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	var w wire
	d := json.NewDecoder(bytes.NewReader(b))
	if err := d.Decode(&w); err != nil {
		return errors.New("invalid broker object")
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return errors.New("invalid broker object")
	}
	x := ShutdownResult(w)
	if err := x.Validate(); err != nil {
		return err
	}
	*v = x
	return nil
}

type ShutdownResponse struct {
	SchemaVersion int64          `json:"schemaVersion"`
	RequestId     CanonicalId    `json:"requestId"`
	Data          ShutdownResult `json:"data"`
}

func (v ShutdownResponse) Validate() error {
	if v.SchemaVersion != 1 {
		return errors.New("invalid broker constant")
	}
	if err := v.RequestId.Validate(); err != nil {
		return err
	}
	if err := v.Data.Validate(); err != nil {
		return err
	}
	return nil
}
func (v *ShutdownResponse) UnmarshalJSON(b []byte) error {
	type wire ShutdownResponse
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil || raw == nil {
		return errors.New("invalid broker object")
	}
	if _, ok := raw["schemaVersion"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["schemaVersion"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["requestId"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["requestId"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["data"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["data"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	var w wire
	d := json.NewDecoder(bytes.NewReader(b))
	if err := d.Decode(&w); err != nil {
		return errors.New("invalid broker object")
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return errors.New("invalid broker object")
	}
	x := ShutdownResponse(w)
	if err := x.Validate(); err != nil {
		return err
	}
	*v = x
	return nil
}

type Error struct {
	SchemaVersion int64        `json:"schemaVersion"`
	RequestId     *CanonicalId `json:"requestId,omitempty"`
	Code          ErrorCode    `json:"code"`
	Retryable     bool         `json:"retryable"`
}

func (v Error) Validate() error {
	if v.SchemaVersion != 1 {
		return errors.New("invalid broker constant")
	}
	if v.RequestId != nil {
		if err := (*v.RequestId).Validate(); err != nil {
			return err
		}
	}
	if err := v.Code.Validate(); err != nil {
		return err
	}
	if v.Retryable != false {
		return errors.New("invalid broker constant")
	}
	return nil
}
func (v *Error) UnmarshalJSON(b []byte) error {
	type wire Error
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil || raw == nil {
		return errors.New("invalid broker object")
	}
	if _, ok := raw["schemaVersion"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["schemaVersion"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if x, ok := raw["requestId"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["code"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["code"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["retryable"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["retryable"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	var w wire
	d := json.NewDecoder(bytes.NewReader(b))
	if err := d.Decode(&w); err != nil {
		return errors.New("invalid broker object")
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return errors.New("invalid broker object")
	}
	x := Error(w)
	if err := x.Validate(); err != nil {
		return err
	}
	*v = x
	return nil
}

type ElicitationMetadata struct {
	YijieMarketCallRef CanonicalId `json:"yijieMarketCallRef"`
	YijieKind          string      `json:"yijieKind"`
}

func (v ElicitationMetadata) Validate() error {
	if err := v.YijieMarketCallRef.Validate(); err != nil {
		return err
	}
	if string(v.YijieKind) != "gateway_call_approval" {
		return errors.New("invalid broker constant")
	}
	return nil
}
func (v *ElicitationMetadata) UnmarshalJSON(b []byte) error {
	type wire ElicitationMetadata
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil || raw == nil {
		return errors.New("invalid broker object")
	}
	if _, ok := raw["yijieMarketCallRef"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["yijieMarketCallRef"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["yijieKind"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["yijieKind"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	var w wire
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if err := d.Decode(&w); err != nil {
		return errors.New("invalid broker object")
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return errors.New("invalid broker object")
	}
	x := ElicitationMetadata(w)
	if err := x.Validate(); err != nil {
		return err
	}
	*v = x
	return nil
}

// DecodeRequest chooses only a declared method, then runs its closed generated decoder.
func DecodeRequest(b []byte) (any, error) {
	var h struct {
		Method string `json:"method"`
	}
	if err := json.Unmarshal(b, &h); err != nil {
		return nil, errors.New("invalid broker request")
	}
	switch h.Method {
	case "initialize":
		var v InitializeRequest
		if err := json.Unmarshal(b, &v); err != nil {
			return nil, err
		}
		return v, nil
	case "prepare":
		var v PrepareRequest
		if err := json.Unmarshal(b, &v); err != nil {
			return nil, err
		}
		return v, nil
	case "bind_turn":
		var v BindTurnRequest
		if err := json.Unmarshal(b, &v); err != nil {
			return nil, err
		}
		return v, nil
	case "revoke":
		var v RevokeRequest
		if err := json.Unmarshal(b, &v); err != nil {
			return nil, err
		}
		return v, nil
	case "status":
		var v StatusRequest
		if err := json.Unmarshal(b, &v); err != nil {
			return nil, err
		}
		return v, nil
	case "pending_call":
		var v PendingCallRequest
		if err := json.Unmarshal(b, &v); err != nil {
			return nil, err
		}
		return v, nil
	case "decide_call":
		var v DecideCallRequest
		if err := json.Unmarshal(b, &v); err != nil {
			return nil, err
		}
		return v, nil
	case "shutdown":
		var v ShutdownRequest
		if err := json.Unmarshal(b, &v); err != nil {
			return nil, err
		}
		return v, nil
	default:
		return nil, errors.New("unknown broker method")
	}
}

const GenericMaxToolsPerService = 512
const GenericMaxToolsPerSelection = 512
const GenericMaxSchemaBytes = 65536
const GenericMaxSchemasBytes = 2097152
const GenericMaxReviewArgumentsBytes = 16384
const GenericMaxResultBytes = 65536
const GenericMaxDepth = 16
const MaxFrameBytes = 65536
const MaxLeaseTtlMs = 300000
const MaxUnboundWaitMs = 5000
const MaxApprovalTtlMs = 60000
const MaxRetainedLeases = 64
const MaxPendingCalls = 128
const MaxMutationReceipts = 1024
const MaxArgumentsBytes = 32768
const MaxArgumentsDepth = 16
const GatewayErrorOwnerRejectedOutcome = "not_sent"
const GatewayErrorOwnerRejectedCode = "approval_invalid"
const GatewayErrorOwnerRejectedMessage = "用户已拒绝本次连接器调用，本次外部请求未发送。不会自动重试。"
const GatewayErrorAdmissionStoppedOutcome = "not_sent"
const GatewayErrorAdmissionStoppedCode = "approval_invalid"
const GatewayErrorAdmissionStoppedMessage = "本次连接器调用在进入外部执行前已停止，外部请求未发送。不会自动重试。"
const GatewayErrorExecutionUnverifiedOutcome = "unknown"
const GatewayErrorExecutionUnverifiedCode = "temporarily_unavailable"
const GatewayErrorExecutionUnverifiedMessage = "本次连接器调用未取得可验证的结果，无法确认外部请求是否已发送或完成。不能据此判断没有数据或已执行；不会自动重试。"
