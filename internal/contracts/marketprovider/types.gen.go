// Generated from market-provider source; DO NOT EDIT.
package marketprovider

import (
	"bytes"
	"encoding/json"
	"errors"
	broker "github.com/36Dge/yijie-agent-host/internal/contracts/marketbrokercontrol"
	market "github.com/36Dge/yijie-agent-host/internal/contracts/marketconnectors"
	"io"
	"regexp"
	"unicode/utf8"
)

var wirePattern0 = regexp.MustCompile("^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$")
var wirePattern1 = regexp.MustCompile("^[A-Za-z0-9][A-Za-z0-9_-]{0,127}$")
var wirePattern2 = regexp.MustCompile("^[0-9]{6}\\.(SH|SZ|BJ)$")
var wirePattern3 = regexp.MustCompile("^(?:(?:[0-9]{3}[1-9]|[0-9]{2}[1-9][0-9]|[0-9][1-9][0-9]{2}|[1-9][0-9]{3})(?:(?:01|03|05|07|08|10|12)(?:0[1-9]|[12][0-9]|3[01])|(?:04|06|09|11)(?:0[1-9]|[12][0-9]|30)|02(?:0[1-9]|1[0-9]|2[0-8]))|(?:[0-9]{2}(?:0[48]|[2468][048]|[13579][26])|(?:0[48]|[2468][048]|[13579][26])00)0229)$")

type CanonicalId = market.CanonicalId
type SelectionRef = market.SelectionRef
type ServiceId = market.ServiceId
type ScopeBinding = broker.ScopeBinding
type ProviderBinding struct {
	Reference     SelectionRef `json:"reference"`
	ServiceId     ServiceId    `json:"serviceId"`
	CredentialRef CanonicalId  `json:"credentialRef"`
}

func (v ProviderBinding) Validate() error {
	if err := v.Reference.Validate(); err != nil {
		return err
	}
	if err := v.ServiceId.Validate(); err != nil {
		return err
	}
	if err := v.CredentialRef.Validate(); err != nil {
		return err
	}
	return nil
}
func (v *ProviderBinding) UnmarshalJSON(b []byte) error {
	type wire ProviderBinding
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil || raw == nil {
		return errors.New("invalid broker object")
	}
	if _, ok := raw["reference"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["reference"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["serviceId"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["serviceId"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["credentialRef"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["credentialRef"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
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
	x := ProviderBinding(w)
	if err := x.Validate(); err != nil {
		return err
	}
	*v = x
	return nil
}

type ProviderPayload struct {
	OperationId    CanonicalId     `json:"operationId"`
	HostInstanceId CanonicalId     `json:"hostInstanceId"`
	Scope          ScopeBinding    `json:"scope"`
	Binding        ProviderBinding `json:"binding"`
}

func (v ProviderPayload) Validate() error {
	if err := v.OperationId.Validate(); err != nil {
		return err
	}
	if err := v.HostInstanceId.Validate(); err != nil {
		return err
	}
	if err := v.Scope.Validate(); err != nil {
		return err
	}
	if err := v.Binding.Validate(); err != nil {
		return err
	}
	return nil
}
func (v *ProviderPayload) UnmarshalJSON(b []byte) error {
	type wire ProviderPayload
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
	if _, ok := raw["hostInstanceId"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["hostInstanceId"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["scope"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["scope"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
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
	x := ProviderPayload(w)
	if err := x.Validate(); err != nil {
		return err
	}
	*v = x
	return nil
}

type OperationState string

const (
	OperationStateStarting     OperationState = "starting"
	OperationStateAwaitingUser OperationState = "awaiting_user"
	OperationStateSucceeded    OperationState = "succeeded"
	OperationStateFailed       OperationState = "failed"
	OperationStateCancelled    OperationState = "cancelled"
)

func (v OperationState) Validate() error {
	switch v {
	case OperationStateStarting, OperationStateAwaitingUser, OperationStateSucceeded, OperationStateFailed, OperationStateCancelled:
		return nil
	default:
		return errors.New("invalid broker enum")
	}
}
func (v *OperationState) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return errors.New("invalid broker enum")
	}
	x := OperationState(s)
	if err := x.Validate(); err != nil {
		return err
	}
	*v = x
	return nil
}

type AuthorizationStatus string

const (
	AuthorizationStatusUnknown      AuthorizationStatus = "unknown"
	AuthorizationStatusUnauthorized AuthorizationStatus = "unauthorized"
	AuthorizationStatusAuthorizing  AuthorizationStatus = "authorizing"
	AuthorizationStatusAuthorized   AuthorizationStatus = "authorized"
	AuthorizationStatusError        AuthorizationStatus = "error"
)

func (v AuthorizationStatus) Validate() error {
	switch v {
	case AuthorizationStatusUnknown, AuthorizationStatusUnauthorized, AuthorizationStatusAuthorizing, AuthorizationStatusAuthorized, AuthorizationStatusError:
		return nil
	default:
		return errors.New("invalid broker enum")
	}
}
func (v *AuthorizationStatus) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return errors.New("invalid broker enum")
	}
	x := AuthorizationStatus(s)
	if err := x.Validate(); err != nil {
		return err
	}
	*v = x
	return nil
}

type ConnectionStatus string

const (
	ConnectionStatusDisconnected ConnectionStatus = "disconnected"
	ConnectionStatusConnecting   ConnectionStatus = "connecting"
	ConnectionStatusConnected    ConnectionStatus = "connected"
	ConnectionStatusError        ConnectionStatus = "error"
)

func (v ConnectionStatus) Validate() error {
	switch v {
	case ConnectionStatusDisconnected, ConnectionStatusConnecting, ConnectionStatusConnected, ConnectionStatusError:
		return nil
	default:
		return errors.New("invalid broker enum")
	}
}
func (v *ConnectionStatus) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return errors.New("invalid broker enum")
	}
	x := ConnectionStatus(s)
	if err := x.Validate(); err != nil {
		return err
	}
	*v = x
	return nil
}

type Qualification string

const (
	QualificationNotQualified Qualification = "not_qualified"
	QualificationQualified    Qualification = "qualified"
)

func (v Qualification) Validate() error {
	switch v {
	case QualificationNotQualified, QualificationQualified:
		return nil
	default:
		return errors.New("invalid broker enum")
	}
}
func (v *Qualification) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return errors.New("invalid broker enum")
	}
	x := Qualification(s)
	if err := x.Validate(); err != nil {
		return err
	}
	*v = x
	return nil
}

type ErrorCode string

const (
	ErrorCodeInvalidRequest         ErrorCode = "invalid_request"
	ErrorCodeAuthorityMismatch      ErrorCode = "authority_mismatch"
	ErrorCodeScopeExpired           ErrorCode = "scope_expired"
	ErrorCodeBindingMismatch        ErrorCode = "binding_mismatch"
	ErrorCodeNotFound               ErrorCode = "not_found"
	ErrorCodeRequestConflict        ErrorCode = "request_conflict"
	ErrorCodeCapacityExceeded       ErrorCode = "capacity_exceeded"
	ErrorCodeUnknownService         ErrorCode = "unknown_service"
	ErrorCodeUnsupportedAuth        ErrorCode = "unsupported_auth"
	ErrorCodeKeyringUnavailable     ErrorCode = "keyring_unavailable"
	ErrorCodeMetadataUnavailable    ErrorCode = "metadata_unavailable"
	ErrorCodeAuthorizationRejected  ErrorCode = "authorization_rejected"
	ErrorCodeAuthorizationTimeout   ErrorCode = "authorization_timeout"
	ErrorCodeNotQualified           ErrorCode = "not_qualified"
	ErrorCodeCleanupPending         ErrorCode = "cleanup_pending"
	ErrorCodeTemporarilyUnavailable ErrorCode = "temporarily_unavailable"
)

func (v ErrorCode) Validate() error {
	switch v {
	case ErrorCodeInvalidRequest, ErrorCodeAuthorityMismatch, ErrorCodeScopeExpired, ErrorCodeBindingMismatch, ErrorCodeNotFound, ErrorCodeRequestConflict, ErrorCodeCapacityExceeded, ErrorCodeUnknownService, ErrorCodeUnsupportedAuth, ErrorCodeKeyringUnavailable, ErrorCodeMetadataUnavailable, ErrorCodeAuthorizationRejected, ErrorCodeAuthorizationTimeout, ErrorCodeNotQualified, ErrorCodeCleanupPending, ErrorCodeTemporarilyUnavailable:
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

type ProviderStatus struct {
	OperationId         CanonicalId         `json:"operationId"`
	HostInstanceId      CanonicalId         `json:"hostInstanceId"`
	Scope               ScopeBinding        `json:"scope"`
	Binding             ProviderBinding     `json:"binding"`
	OperationState      OperationState      `json:"operationState"`
	AuthorizationStatus AuthorizationStatus `json:"authorizationStatus"`
	ConnectionStatus    ConnectionStatus    `json:"connectionStatus"`
	Qualification       Qualification       `json:"qualification"`
	ExecutionAvailable  bool                `json:"executionAvailable"`
	AuthorizationUrl    *string             `json:"authorizationUrl,omitempty"`
	ErrorCode           *ErrorCode          `json:"errorCode,omitempty"`
}

func (v ProviderStatus) Validate() error {
	if err := v.OperationId.Validate(); err != nil {
		return err
	}
	if err := v.HostInstanceId.Validate(); err != nil {
		return err
	}
	if err := v.Scope.Validate(); err != nil {
		return err
	}
	if err := v.Binding.Validate(); err != nil {
		return err
	}
	if err := v.OperationState.Validate(); err != nil {
		return err
	}
	if err := v.AuthorizationStatus.Validate(); err != nil {
		return err
	}
	if err := v.ConnectionStatus.Validate(); err != nil {
		return err
	}
	if err := v.Qualification.Validate(); err != nil {
		return err
	}
	if v.AuthorizationUrl != nil {
		if utf8.RuneCountInString(string((*v.AuthorizationUrl))) < 1 {
			return errors.New("invalid broker text")
		}
		if utf8.RuneCountInString(string((*v.AuthorizationUrl))) > 4096 {
			return errors.New("invalid broker text")
		}
	}
	if v.ErrorCode != nil {
		if err := (*v.ErrorCode).Validate(); err != nil {
			return err
		}
	}
	if v.AuthorizationUrl != nil && v.OperationState != OperationStateAwaitingUser {
		return errors.New("authorization URL outside pending state")
	}
	if v.ExecutionAvailable && (v.Qualification != QualificationQualified || v.AuthorizationStatus != AuthorizationStatusAuthorized || v.ConnectionStatus != ConnectionStatusConnected) {
		return errors.New("provider readiness lacks facts")
	}
	return nil
}
func (v *ProviderStatus) UnmarshalJSON(b []byte) error {
	type wire ProviderStatus
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
	if _, ok := raw["hostInstanceId"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["hostInstanceId"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["scope"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["scope"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["binding"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["binding"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["operationState"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["operationState"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["authorizationStatus"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["authorizationStatus"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["connectionStatus"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["connectionStatus"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["qualification"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["qualification"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["executionAvailable"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["executionAvailable"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if x, ok := raw["authorizationUrl"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if x, ok := raw["errorCode"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
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
	x := ProviderStatus(w)
	if err := x.Validate(); err != nil {
		return err
	}
	*v = x
	return nil
}

type AuthBeginRequest struct {
	SchemaVersion int64           `json:"schemaVersion"`
	RequestId     CanonicalId     `json:"requestId"`
	Method        string          `json:"method"`
	Payload       ProviderPayload `json:"payload"`
}

func (v AuthBeginRequest) Validate() error {
	if v.SchemaVersion != 1 {
		return errors.New("invalid broker constant")
	}
	if err := v.RequestId.Validate(); err != nil {
		return err
	}
	if string(v.Method) != "provider_auth_begin" {
		return errors.New("invalid broker constant")
	}
	if err := v.Payload.Validate(); err != nil {
		return err
	}
	return nil
}
func (v *AuthBeginRequest) UnmarshalJSON(b []byte) error {
	type wire AuthBeginRequest
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
	x := AuthBeginRequest(w)
	if err := x.Validate(); err != nil {
		return err
	}
	*v = x
	return nil
}

type AuthBeginResponse struct {
	SchemaVersion int64          `json:"schemaVersion"`
	RequestId     CanonicalId    `json:"requestId"`
	Data          ProviderStatus `json:"data"`
}

func (v AuthBeginResponse) Validate() error {
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
func (v *AuthBeginResponse) UnmarshalJSON(b []byte) error {
	type wire AuthBeginResponse
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
	x := AuthBeginResponse(w)
	if err := x.Validate(); err != nil {
		return err
	}
	*v = x
	return nil
}

type AuthPollRequest struct {
	SchemaVersion int64           `json:"schemaVersion"`
	RequestId     CanonicalId     `json:"requestId"`
	Method        string          `json:"method"`
	Payload       ProviderPayload `json:"payload"`
}

func (v AuthPollRequest) Validate() error {
	if v.SchemaVersion != 1 {
		return errors.New("invalid broker constant")
	}
	if err := v.RequestId.Validate(); err != nil {
		return err
	}
	if string(v.Method) != "provider_auth_poll" {
		return errors.New("invalid broker constant")
	}
	if err := v.Payload.Validate(); err != nil {
		return err
	}
	return nil
}
func (v *AuthPollRequest) UnmarshalJSON(b []byte) error {
	type wire AuthPollRequest
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
	x := AuthPollRequest(w)
	if err := x.Validate(); err != nil {
		return err
	}
	*v = x
	return nil
}

type AuthPollResponse struct {
	SchemaVersion int64          `json:"schemaVersion"`
	RequestId     CanonicalId    `json:"requestId"`
	Data          ProviderStatus `json:"data"`
}

func (v AuthPollResponse) Validate() error {
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
func (v *AuthPollResponse) UnmarshalJSON(b []byte) error {
	type wire AuthPollResponse
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
	x := AuthPollResponse(w)
	if err := x.Validate(); err != nil {
		return err
	}
	*v = x
	return nil
}

type AuthCancelRequest struct {
	SchemaVersion int64           `json:"schemaVersion"`
	RequestId     CanonicalId     `json:"requestId"`
	Method        string          `json:"method"`
	Payload       ProviderPayload `json:"payload"`
}

func (v AuthCancelRequest) Validate() error {
	if v.SchemaVersion != 1 {
		return errors.New("invalid broker constant")
	}
	if err := v.RequestId.Validate(); err != nil {
		return err
	}
	if string(v.Method) != "provider_auth_cancel" {
		return errors.New("invalid broker constant")
	}
	if err := v.Payload.Validate(); err != nil {
		return err
	}
	return nil
}
func (v *AuthCancelRequest) UnmarshalJSON(b []byte) error {
	type wire AuthCancelRequest
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
	x := AuthCancelRequest(w)
	if err := x.Validate(); err != nil {
		return err
	}
	*v = x
	return nil
}

type AuthCancelResponse struct {
	SchemaVersion int64          `json:"schemaVersion"`
	RequestId     CanonicalId    `json:"requestId"`
	Data          ProviderStatus `json:"data"`
}

func (v AuthCancelResponse) Validate() error {
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
func (v *AuthCancelResponse) UnmarshalJSON(b []byte) error {
	type wire AuthCancelResponse
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
	x := AuthCancelResponse(w)
	if err := x.Validate(); err != nil {
		return err
	}
	*v = x
	return nil
}

type ForgetRequest struct {
	SchemaVersion int64           `json:"schemaVersion"`
	RequestId     CanonicalId     `json:"requestId"`
	Method        string          `json:"method"`
	Payload       ProviderPayload `json:"payload"`
}

func (v ForgetRequest) Validate() error {
	if v.SchemaVersion != 1 {
		return errors.New("invalid broker constant")
	}
	if err := v.RequestId.Validate(); err != nil {
		return err
	}
	if string(v.Method) != "provider_forget" {
		return errors.New("invalid broker constant")
	}
	if err := v.Payload.Validate(); err != nil {
		return err
	}
	return nil
}
func (v *ForgetRequest) UnmarshalJSON(b []byte) error {
	type wire ForgetRequest
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
	x := ForgetRequest(w)
	if err := x.Validate(); err != nil {
		return err
	}
	*v = x
	return nil
}

type ForgetResponse struct {
	SchemaVersion int64          `json:"schemaVersion"`
	RequestId     CanonicalId    `json:"requestId"`
	Data          ProviderStatus `json:"data"`
}

func (v ForgetResponse) Validate() error {
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
func (v *ForgetResponse) UnmarshalJSON(b []byte) error {
	type wire ForgetResponse
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
	x := ForgetResponse(w)
	if err := x.Validate(); err != nil {
		return err
	}
	*v = x
	return nil
}

type ProbeRequest struct {
	SchemaVersion int64           `json:"schemaVersion"`
	RequestId     CanonicalId     `json:"requestId"`
	Method        string          `json:"method"`
	Payload       ProviderPayload `json:"payload"`
}

func (v ProbeRequest) Validate() error {
	if v.SchemaVersion != 1 {
		return errors.New("invalid broker constant")
	}
	if err := v.RequestId.Validate(); err != nil {
		return err
	}
	if string(v.Method) != "provider_probe" {
		return errors.New("invalid broker constant")
	}
	if err := v.Payload.Validate(); err != nil {
		return err
	}
	return nil
}
func (v *ProbeRequest) UnmarshalJSON(b []byte) error {
	type wire ProbeRequest
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
	x := ProbeRequest(w)
	if err := x.Validate(); err != nil {
		return err
	}
	*v = x
	return nil
}

type ProbeResponse struct {
	SchemaVersion int64          `json:"schemaVersion"`
	RequestId     CanonicalId    `json:"requestId"`
	Data          ProviderStatus `json:"data"`
}

func (v ProbeResponse) Validate() error {
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
func (v *ProbeResponse) UnmarshalJSON(b []byte) error {
	type wire ProbeResponse
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
	x := ProbeResponse(w)
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

type TushareDailyArguments struct {
	TsCode    string `json:"ts_code"`
	TradeDate string `json:"trade_date"`
}

func (v TushareDailyArguments) Validate() error {
	if utf8.RuneCountInString(string(v.TsCode)) < 9 {
		return errors.New("invalid broker text")
	}
	if utf8.RuneCountInString(string(v.TsCode)) > 9 {
		return errors.New("invalid broker text")
	}
	if !wirePattern2.MatchString(string(v.TsCode)) {
		return errors.New("invalid broker pattern")
	}
	if utf8.RuneCountInString(string(v.TradeDate)) < 8 {
		return errors.New("invalid broker text")
	}
	if utf8.RuneCountInString(string(v.TradeDate)) > 8 {
		return errors.New("invalid broker text")
	}
	if !wirePattern3.MatchString(string(v.TradeDate)) {
		return errors.New("invalid broker pattern")
	}
	return nil
}
func (v *TushareDailyArguments) UnmarshalJSON(b []byte) error {
	type wire TushareDailyArguments
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil || raw == nil {
		return errors.New("invalid broker object")
	}
	if _, ok := raw["ts_code"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["ts_code"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["trade_date"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["trade_date"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
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
	x := TushareDailyArguments(w)
	if err := x.Validate(); err != nil {
		return err
	}
	*v = x
	return nil
}

type SuspendRequest struct {
	SchemaVersion int64           `json:"schemaVersion"`
	RequestId     CanonicalId     `json:"requestId"`
	Method        string          `json:"method"`
	Payload       ProviderPayload `json:"payload"`
}

func (v SuspendRequest) Validate() error {
	if v.SchemaVersion != 1 {
		return errors.New("invalid broker constant")
	}
	if err := v.RequestId.Validate(); err != nil {
		return err
	}
	if string(v.Method) != "provider_suspend" {
		return errors.New("invalid broker constant")
	}
	if err := v.Payload.Validate(); err != nil {
		return err
	}
	return nil
}
func (v *SuspendRequest) UnmarshalJSON(b []byte) error {
	type wire SuspendRequest
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
	x := SuspendRequest(w)
	if err := x.Validate(); err != nil {
		return err
	}
	*v = x
	return nil
}

type SuspendResponse struct {
	SchemaVersion int64          `json:"schemaVersion"`
	RequestId     CanonicalId    `json:"requestId"`
	Data          ProviderStatus `json:"data"`
}

func (v SuspendResponse) Validate() error {
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
func (v *SuspendResponse) UnmarshalJSON(b []byte) error {
	type wire SuspendResponse
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
	x := SuspendResponse(w)
	if err := x.Validate(); err != nil {
		return err
	}
	*v = x
	return nil
}

const MaxFrameBytes = 65536
const MaxOperations = 64
const MaxOAuthTtlMs = 300000
const TushareDailyProfile = "tushare-daily-v1"
const TushareDailyToolName = "tushare_daily"
const TushareDailyUpstreamToolName = "daily"
const TushareDailyUpstreamInputSchemaSha256 = "ec10409543d1ae690de2b5da5893a0e69387cdd5a398f976fb04d187fc632ae1"
const TushareDailyRisk = "read"
const TushareDailyPermissionMode = "ask"
const TushareDailyResultRowsField = "rows"
const TushareDailyInputSchemaJson = "{\"type\":\"object\",\"additionalProperties\":false,\"required\":[\"ts_code\",\"trade_date\"],\"properties\":{\"ts_code\":{\"type\":\"string\",\"minLength\":9,\"maxLength\":9,\"pattern\":\"^[0-9]{6}\\\\.(SH|SZ|BJ)$\",\"description\":\"One six-digit A-share instrument code with SH, SZ or BJ suffix. This checks syntax, not current listing existence. No lists, separators or whitespace.\"},\"trade_date\":{\"type\":\"string\",\"minLength\":8,\"maxLength\":8,\"pattern\":\"^(?:(?:[0-9]{3}[1-9]|[0-9]{2}[1-9][0-9]|[0-9][1-9][0-9]{2}|[1-9][0-9]{3})(?:(?:01|03|05|07|08|10|12)(?:0[1-9]|[12][0-9]|3[01])|(?:04|06|09|11)(?:0[1-9]|[12][0-9]|30)|02(?:0[1-9]|1[0-9]|2[0-8]))|(?:[0-9]{2}(?:0[48]|[2468][048]|[13579][26])|(?:0[48]|[2468][048]|[13579][26])00)0229)$\",\"description\":\"Exactly one valid Gregorian date in YYYYMMDD, years 0001 through 9999, including leap-year validation. No date range.\"}},\"description\":\"Yijie-owned normalized input for the tushare-daily-v1 Gateway tool tushare_daily. Only these two strings are forwarded to exact upstream daily after validation and one-call approval; no fields, range, limit or other provider arguments are accepted. This is not the full upstream input schema.\"}"
const TushareDailyOutputSchemaJson = "{\"type\":\"object\",\"additionalProperties\":false,\"required\":[\"rows\"],\"properties\":{\"rows\":{\"type\":\"array\",\"maxItems\":1,\"items\":{\"type\":\"object\",\"additionalProperties\":false,\"required\":[\"ts_code\",\"trade_date\",\"close\"],\"properties\":{\"ts_code\":{\"type\":\"string\",\"minLength\":9,\"maxLength\":9,\"pattern\":\"^[0-9]{6}\\\\.(SH|SZ|BJ)$\",\"description\":\"One six-digit A-share instrument code with SH, SZ or BJ suffix. This checks syntax, not current listing existence. No lists, separators or whitespace.\"},\"trade_date\":{\"type\":\"string\",\"minLength\":8,\"maxLength\":8,\"pattern\":\"^(?:(?:[0-9]{3}[1-9]|[0-9]{2}[1-9][0-9]|[0-9][1-9][0-9]{2}|[1-9][0-9]{3})(?:(?:01|03|05|07|08|10|12)(?:0[1-9]|[12][0-9]|3[01])|(?:04|06|09|11)(?:0[1-9]|[12][0-9]|30)|02(?:0[1-9]|1[0-9]|2[0-8]))|(?:[0-9]{2}(?:0[48]|[2468][048]|[13579][26])|(?:0[48]|[2468][048]|[13579][26])00)0229)$\",\"description\":\"Exactly one valid Gregorian date in YYYYMMDD, years 0001 through 9999, including leap-year validation. No date range.\"},\"open\":{\"type\":\"number\"},\"high\":{\"type\":\"number\"},\"low\":{\"type\":\"number\"},\"close\":{\"type\":\"number\"},\"pre_close\":{\"type\":\"number\"},\"change\":{\"type\":\"number\"},\"pct_chg\":{\"type\":\"number\"},\"vol\":{\"type\":\"number\"},\"amount\":{\"type\":\"number\"}}}}}}"
const TushareDailyMaxRows = 1
const TushareDailyMaxCallsPerApproval = 1

var TushareDailyResultIdentityFields = [...]string{"ts_code", "trade_date"}
var TushareDailyResultNumericFields = [...]string{"open", "high", "low", "close", "pre_close", "change", "pct_chg", "vol", "amount"}
var TushareDailyResultRequiredNumericFields = [...]string{"close"}
