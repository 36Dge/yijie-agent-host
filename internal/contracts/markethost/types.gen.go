// Generated from market-host source and referenced authorities; DO NOT EDIT.
package markethost

import (
	"bytes"
	"encoding/json"
	"errors"
	agenthost "github.com/36Dge/yijie-agent-host/internal/contracts"
	models "github.com/36Dge/yijie-agent-host/internal/contracts/chatmodels"
	broker "github.com/36Dge/yijie-agent-host/internal/contracts/marketbrokercontrol"
	market "github.com/36Dge/yijie-agent-host/internal/contracts/marketconnectors"
	provider "github.com/36Dge/yijie-agent-host/internal/contracts/marketprovider"
	selection "github.com/36Dge/yijie-agent-host/internal/contracts/marketselection"
	"io"
	"regexp"
	"unicode/utf8"
)

var wirePattern0 = regexp.MustCompile("^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$")
var wirePattern1 = regexp.MustCompile("^[A-Za-z0-9][A-Za-z0-9_-]{0,127}$")
var wirePattern2 = regexp.MustCompile("^[0-9a-f]{64}$")
var wirePattern3 = regexp.MustCompile("\\S")
var wirePattern4 = regexp.MustCompile("^[a-f0-9]{64}$")
var wirePattern5 = regexp.MustCompile("^data:image/(jpeg|png|webp|gif);base64,[A-Za-z0-9+/]+={0,2}$")
var wirePattern6 = regexp.MustCompile("^[^/\\\\\\x00-\\x1F\\x7F\\s]([^/\\\\\\x00-\\x1F\\x7F]*[^/\\\\\\x00-\\x1F\\x7F\\s])?$")

type CanonicalId = market.CanonicalId
type SelectionRef = market.SelectionRef
type ServiceId = market.ServiceId
type SelectionSnapshot = selection.SelectionSnapshot
type SelectionDigest = selection.SelectionDigestValue
type ModelIntent struct {
	ProfileId        ProfileId     `json:"profileId"`
	ExpectedRevision ModelRevision `json:"expectedRevision"`
}

func (v ModelIntent) Validate() error {
	switch string(v.ProfileId) {
	case "kimi-k3-max-v1", "minimax-m3-high-v1":
	default:
		return errors.New("invalid model profile")
	}
	if v.ExpectedRevision < 0 || v.ExpectedRevision > 9007199254740991 {
		return errors.New("invalid model revision")
	}
	return nil
}
func (v *ModelIntent) UnmarshalJSON(b []byte) error {
	type wire ModelIntent
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil || raw == nil {
		return errors.New("invalid broker object")
	}
	if _, ok := raw["profileId"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["profileId"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["expectedRevision"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["expectedRevision"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
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
	x := ModelIntent(w)
	if err := x.Validate(); err != nil {
		return err
	}
	*v = x
	return nil
}

type ProfileId = models.ProfileId
type ModelRevision = models.Revision
type ScopeBinding = broker.ScopeBinding
type CallIdentity = broker.CallIdentity
type ReviewProjection = broker.ReviewProjection
type Decision = broker.Decision
type NativeId = broker.NativeId
type Revision = market.Revision
type PermissionMode string

const (
	PermissionModeAsk  PermissionMode = "ask"
	PermissionModeAuto PermissionMode = "auto"
	PermissionModeFull PermissionMode = "full"
)

func (v PermissionMode) Validate() error {
	switch v {
	case PermissionModeAsk, PermissionModeAuto, PermissionModeFull:
		return nil
	default:
		return errors.New("invalid broker enum")
	}
}
func (v *PermissionMode) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return errors.New("invalid broker enum")
	}
	x := PermissionMode(s)
	if err := x.Validate(); err != nil {
		return err
	}
	*v = x
	return nil
}

type ContentBlock = agenthost.StartTurnV2ContentBlock
type ServiceBinding = provider.ProviderBinding
type Submission struct {
	TaskId                CanonicalId       `json:"taskId"`
	LocalSessionId        CanonicalId       `json:"localSessionId"`
	AgentSessionId        *CanonicalId      `json:"agentSessionId"`
	SubmissionOperationId CanonicalId       `json:"submissionOperationId"`
	Cwd                   string            `json:"cwd"`
	ContentBlocks         []ContentBlock    `json:"contentBlocks"`
	Intent                ModelIntent       `json:"intent"`
	PermissionMode        PermissionMode    `json:"permissionMode"`
	Snapshot              SelectionSnapshot `json:"snapshot"`
	Services              []ServiceBinding  `json:"services"`
}

func (v Submission) Validate() error {
	if err := v.TaskId.Validate(); err != nil {
		return err
	}
	if err := v.LocalSessionId.Validate(); err != nil {
		return err
	}
	if v.AgentSessionId != nil {
		if err := (*v.AgentSessionId).Validate(); err != nil {
			return err
		}
	}
	if err := v.SubmissionOperationId.Validate(); err != nil {
		return err
	}
	if utf8.RuneCountInString(string(v.Cwd)) < 1 {
		return errors.New("invalid broker text")
	}
	if utf8.RuneCountInString(string(v.Cwd)) > 4096 {
		return errors.New("invalid broker text")
	}
	if v.ContentBlocks == nil {
		return errors.New("null broker collection")
	}
	if len(v.ContentBlocks) < 1 {
		return errors.New("invalid broker collection")
	}
	if len(v.ContentBlocks) > 16 {
		return errors.New("invalid broker collection")
	}
	for _, item := range v.ContentBlocks {
		if err := validateContentBlock(item); err != nil {
			return err
		}
	}
	if err := v.Intent.Validate(); err != nil {
		return err
	}
	if err := v.PermissionMode.Validate(); err != nil {
		return err
	}
	if err := v.Snapshot.Validate(); err != nil {
		return err
	}
	if v.Services == nil {
		return errors.New("null broker collection")
	}
	if len(v.Services) > 58 {
		return errors.New("invalid broker collection")
	}
	for _, item := range v.Services {
		if err := item.Validate(); err != nil {
			return err
		}
	}
	if len(v.Services) != len(v.Snapshot.Selection) {
		return errors.New("service snapshot mismatch")
	}
	for i, s := range v.Services {
		if s.Reference != v.Snapshot.Selection[i] {
			return errors.New("service snapshot mismatch")
		}
	}
	if len(v.Snapshot.Selection) > 0 && v.PermissionMode != PermissionModeAsk {
		return errors.New("permission_mode_unavailable")
	}
	if v.AgentSessionId == nil && v.Intent.ExpectedRevision != 0 {
		return errors.New("invalid new session revision")
	}
	return nil
}
func (v *Submission) UnmarshalJSON(b []byte) error {
	type wire Submission
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil || raw == nil {
		return errors.New("invalid broker object")
	}
	if _, ok := raw["taskId"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["taskId"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["localSessionId"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["localSessionId"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["agentSessionId"]; !ok {
		return errors.New("missing broker field")
	}
	if _, ok := raw["submissionOperationId"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["submissionOperationId"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["cwd"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["cwd"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["contentBlocks"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["contentBlocks"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["intent"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["intent"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["permissionMode"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["permissionMode"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["snapshot"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["snapshot"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["services"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["services"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
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
	x := Submission(w)
	if err := x.Validate(); err != nil {
		return err
	}
	*v = x
	return nil
}

type HelloPayload struct {
	HostInstanceId     CanonicalId `json:"hostInstanceId"`
	NativeProcessEpoch CanonicalId `json:"nativeProcessEpoch"`
	OwnerUserId        CanonicalId `json:"ownerUserId"`
	TenantId           CanonicalId `json:"tenantId"`
}

func (v HelloPayload) Validate() error {
	if err := v.HostInstanceId.Validate(); err != nil {
		return err
	}
	if err := v.NativeProcessEpoch.Validate(); err != nil {
		return err
	}
	if err := v.OwnerUserId.Validate(); err != nil {
		return err
	}
	if err := v.TenantId.Validate(); err != nil {
		return err
	}
	return nil
}
func (v *HelloPayload) UnmarshalJSON(b []byte) error {
	type wire HelloPayload
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
	if _, ok := raw["nativeProcessEpoch"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["nativeProcessEpoch"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
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
	var w wire
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if err := d.Decode(&w); err != nil {
		return errors.New("invalid broker object")
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return errors.New("invalid broker object")
	}
	x := HelloPayload(w)
	if err := x.Validate(); err != nil {
		return err
	}
	*v = x
	return nil
}

type HelloResult struct {
	HostInstanceId     CanonicalId `json:"hostInstanceId"`
	NativeProcessEpoch CanonicalId `json:"nativeProcessEpoch"`
	OwnerUserId        CanonicalId `json:"ownerUserId"`
	TenantId           CanonicalId `json:"tenantId"`
	Ready              bool        `json:"ready"`
}

func (v HelloResult) Validate() error {
	if err := v.HostInstanceId.Validate(); err != nil {
		return err
	}
	if err := v.NativeProcessEpoch.Validate(); err != nil {
		return err
	}
	if err := v.OwnerUserId.Validate(); err != nil {
		return err
	}
	if err := v.TenantId.Validate(); err != nil {
		return err
	}
	if v.Ready != true {
		return errors.New("invalid broker constant")
	}
	return nil
}
func (v *HelloResult) UnmarshalJSON(b []byte) error {
	type wire HelloResult
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
	if _, ok := raw["nativeProcessEpoch"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["nativeProcessEpoch"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
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
	if _, ok := raw["ready"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["ready"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
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
	x := HelloResult(w)
	if err := x.Validate(); err != nil {
		return err
	}
	*v = x
	return nil
}

type GrantRegisterPayload struct {
	OperationId    CanonicalId  `json:"operationId"`
	HostInstanceId CanonicalId  `json:"hostInstanceId"`
	Scope          ScopeBinding `json:"scope"`
	Submission     Submission   `json:"submission"`
}

func (v GrantRegisterPayload) Validate() error {
	if err := v.OperationId.Validate(); err != nil {
		return err
	}
	if err := v.HostInstanceId.Validate(); err != nil {
		return err
	}
	if err := v.Scope.Validate(); err != nil {
		return err
	}
	if err := v.Submission.Validate(); err != nil {
		return err
	}
	return nil
}
func (v *GrantRegisterPayload) UnmarshalJSON(b []byte) error {
	type wire GrantRegisterPayload
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
	if _, ok := raw["submission"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["submission"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
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
	x := GrantRegisterPayload(w)
	if err := x.Validate(); err != nil {
		return err
	}
	*v = x
	return nil
}

type GrantRegistration struct {
	GrantRef           CanonicalId     `json:"grantRef"`
	HostInstanceId     CanonicalId     `json:"hostInstanceId"`
	NativeProcessEpoch CanonicalId     `json:"nativeProcessEpoch"`
	TurnOperationId    CanonicalId     `json:"turnOperationId"`
	SelectionDigest    SelectionDigest `json:"selectionDigest"`
	RemainingTtlMs     int64           `json:"remainingTtlMs"`
}

func (v GrantRegistration) Validate() error {
	if err := v.GrantRef.Validate(); err != nil {
		return err
	}
	if err := v.HostInstanceId.Validate(); err != nil {
		return err
	}
	if err := v.NativeProcessEpoch.Validate(); err != nil {
		return err
	}
	if err := v.TurnOperationId.Validate(); err != nil {
		return err
	}
	if err := v.SelectionDigest.Validate(); err != nil {
		return err
	}
	if v.RemainingTtlMs < 0 {
		return errors.New("invalid broker number")
	}
	if v.RemainingTtlMs > 300000 {
		return errors.New("invalid broker number")
	}
	return nil
}
func (v *GrantRegistration) UnmarshalJSON(b []byte) error {
	type wire GrantRegistration
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil || raw == nil {
		return errors.New("invalid broker object")
	}
	if _, ok := raw["grantRef"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["grantRef"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["hostInstanceId"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["hostInstanceId"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["nativeProcessEpoch"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["nativeProcessEpoch"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
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
	if _, ok := raw["remainingTtlMs"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["remainingTtlMs"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
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
	x := GrantRegistration(w)
	if err := x.Validate(); err != nil {
		return err
	}
	*v = x
	return nil
}

type RevokeAuthorityPayload struct {
	OperationId    CanonicalId  `json:"operationId"`
	HostInstanceId CanonicalId  `json:"hostInstanceId"`
	Scope          ScopeBinding `json:"scope"`
	InstallationId *CanonicalId `json:"installationId,omitempty"`
	Reason         string       `json:"reason"`
}

func (v RevokeAuthorityPayload) Validate() error {
	if err := v.OperationId.Validate(); err != nil {
		return err
	}
	if err := v.HostInstanceId.Validate(); err != nil {
		return err
	}
	if err := v.Scope.Validate(); err != nil {
		return err
	}
	if v.InstallationId != nil {
		if err := (*v.InstallationId).Validate(); err != nil {
			return err
		}
	}
	switch string(v.Reason) {
	case "authorization_changed", "installation_changed", "native_shutdown":
	default:
		return errors.New("invalid broker enum")
	}
	return nil
}
func (v *RevokeAuthorityPayload) UnmarshalJSON(b []byte) error {
	type wire RevokeAuthorityPayload
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
	if x, ok := raw["installationId"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
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
	x := RevokeAuthorityPayload(w)
	if err := x.Validate(); err != nil {
		return err
	}
	*v = x
	return nil
}

type RevokeAuthorityResult struct {
	Outcome          string `json:"outcome"`
	ExternalOutcomes string `json:"externalOutcomes"`
}

func (v RevokeAuthorityResult) Validate() error {
	switch string(v.Outcome) {
	case "admission_revoked", "retirement_pending":
	default:
		return errors.New("invalid broker enum")
	}
	if string(v.ExternalOutcomes) != "not_asserted" {
		return errors.New("invalid broker constant")
	}
	return nil
}
func (v *RevokeAuthorityResult) UnmarshalJSON(b []byte) error {
	type wire RevokeAuthorityResult
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil || raw == nil {
		return errors.New("invalid broker object")
	}
	if _, ok := raw["outcome"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["outcome"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
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
	x := RevokeAuthorityResult(w)
	if err := x.Validate(); err != nil {
		return err
	}
	*v = x
	return nil
}

type SubmissionState string

const (
	SubmissionStatePending   SubmissionState = "pending"
	SubmissionStateAccepted  SubmissionState = "accepted"
	SubmissionStateUncertain SubmissionState = "uncertain"
)

func (v SubmissionState) Validate() error {
	switch v {
	case SubmissionStatePending, SubmissionStateAccepted, SubmissionStateUncertain:
		return nil
	default:
		return errors.New("invalid broker enum")
	}
}
func (v *SubmissionState) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return errors.New("invalid broker enum")
	}
	x := SubmissionState(s)
	if err := x.Validate(); err != nil {
		return err
	}
	*v = x
	return nil
}

type SubmissionReceipt struct {
	HostInstanceId        CanonicalId     `json:"hostInstanceId"`
	RuntimeGeneration     *CanonicalId    `json:"runtimeGeneration"`
	TaskId                CanonicalId     `json:"taskId"`
	LocalSessionId        CanonicalId     `json:"localSessionId"`
	SubmissionOperationId CanonicalId     `json:"submissionOperationId"`
	TurnOperationId       CanonicalId     `json:"turnOperationId"`
	SelectionDigest       SelectionDigest `json:"selectionDigest"`
	State                 SubmissionState `json:"state"`
	AgentSessionId        *CanonicalId    `json:"agentSessionId"`
	NativeThreadId        *NativeId       `json:"nativeThreadId"`
	NativeTurnId          *NativeId       `json:"nativeTurnId"`
	ProfileId             ProfileId       `json:"profileId"`
	ModelRevision         ModelRevision   `json:"modelRevision"`
}

func (v SubmissionReceipt) Validate() error {
	if err := v.HostInstanceId.Validate(); err != nil {
		return err
	}
	if v.RuntimeGeneration != nil {
		if err := (*v.RuntimeGeneration).Validate(); err != nil {
			return err
		}
	}
	if err := v.TaskId.Validate(); err != nil {
		return err
	}
	if err := v.LocalSessionId.Validate(); err != nil {
		return err
	}
	if err := v.SubmissionOperationId.Validate(); err != nil {
		return err
	}
	if err := v.TurnOperationId.Validate(); err != nil {
		return err
	}
	if err := v.SelectionDigest.Validate(); err != nil {
		return err
	}
	if err := v.State.Validate(); err != nil {
		return err
	}
	if v.AgentSessionId != nil {
		if err := (*v.AgentSessionId).Validate(); err != nil {
			return err
		}
	}
	if v.NativeThreadId != nil {
		if err := (*v.NativeThreadId).Validate(); err != nil {
			return err
		}
	}
	if v.NativeTurnId != nil {
		if err := (*v.NativeTurnId).Validate(); err != nil {
			return err
		}
	}
	switch string(v.ProfileId) {
	case "kimi-k3-max-v1", "minimax-m3-high-v1":
	default:
		return errors.New("invalid model profile")
	}
	if v.ModelRevision < 0 || v.ModelRevision > 9007199254740991 {
		return errors.New("invalid model revision")
	}
	if v.State == SubmissionStateAccepted && (v.AgentSessionId == nil || v.NativeThreadId == nil || v.NativeTurnId == nil || v.RuntimeGeneration == nil) {
		return errors.New("accepted receipt missing native fact")
	}
	return nil
}
func (v *SubmissionReceipt) UnmarshalJSON(b []byte) error {
	type wire SubmissionReceipt
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
	if _, ok := raw["taskId"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["taskId"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["localSessionId"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["localSessionId"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["submissionOperationId"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["submissionOperationId"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
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
	if _, ok := raw["state"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["state"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["agentSessionId"]; !ok {
		return errors.New("missing broker field")
	}
	if _, ok := raw["nativeThreadId"]; !ok {
		return errors.New("missing broker field")
	}
	if _, ok := raw["nativeTurnId"]; !ok {
		return errors.New("missing broker field")
	}
	if _, ok := raw["profileId"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["profileId"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["modelRevision"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["modelRevision"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
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
	x := SubmissionReceipt(w)
	if err := x.Validate(); err != nil {
		return err
	}
	*v = x
	return nil
}

type SubmitPayload struct {
	GrantRef        CanonicalId `json:"grantRef"`
	TurnOperationId CanonicalId `json:"turnOperationId"`
}

func (v SubmitPayload) Validate() error {
	if err := v.GrantRef.Validate(); err != nil {
		return err
	}
	if err := v.TurnOperationId.Validate(); err != nil {
		return err
	}
	return nil
}
func (v *SubmitPayload) UnmarshalJSON(b []byte) error {
	type wire SubmitPayload
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil || raw == nil {
		return errors.New("invalid broker object")
	}
	if _, ok := raw["grantRef"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["grantRef"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["turnOperationId"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["turnOperationId"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
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
	x := SubmitPayload(w)
	if err := x.Validate(); err != nil {
		return err
	}
	*v = x
	return nil
}

type ApprovalState string

const (
	ApprovalStatePending     ApprovalState = "pending"
	ApprovalStateApproved    ApprovalState = "approved"
	ApprovalStateRejected    ApprovalState = "rejected"
	ApprovalStateCancelled   ApprovalState = "cancelled"
	ApprovalStateExpired     ApprovalState = "expired"
	ApprovalStateUnavailable ApprovalState = "unavailable"
)

func (v ApprovalState) Validate() error {
	switch v {
	case ApprovalStatePending, ApprovalStateApproved, ApprovalStateRejected, ApprovalStateCancelled, ApprovalStateExpired, ApprovalStateUnavailable:
		return nil
	default:
		return errors.New("invalid broker enum")
	}
}
func (v *ApprovalState) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return errors.New("invalid broker enum")
	}
	x := ApprovalState(s)
	if err := x.Validate(); err != nil {
		return err
	}
	*v = x
	return nil
}

type MarketApproval struct {
	ApprovalId      CanonicalId      `json:"approvalId"`
	AgentSessionId  CanonicalId      `json:"agentSessionId"`
	Kind            string           `json:"kind"`
	Revision        Revision         `json:"revision"`
	State           ApprovalState    `json:"state"`
	Identity        CallIdentity     `json:"identity"`
	Review          ReviewProjection `json:"review"`
	ExpiresAtUnixMs int64            `json:"expiresAtUnixMs"`
	DecisionId      *CanonicalId     `json:"decisionId,omitempty"`
	Decision        *Decision        `json:"decision,omitempty"`
}

func (v MarketApproval) Validate() error {
	if err := v.ApprovalId.Validate(); err != nil {
		return err
	}
	if err := v.AgentSessionId.Validate(); err != nil {
		return err
	}
	if string(v.Kind) != "mcp_market" {
		return errors.New("invalid broker constant")
	}
	if err := v.Revision.Validate(); err != nil {
		return err
	}
	if err := v.State.Validate(); err != nil {
		return err
	}
	if err := v.Identity.Validate(); err != nil {
		return err
	}
	if err := v.Review.Validate(); err != nil {
		return err
	}
	if v.ExpiresAtUnixMs < 1 {
		return errors.New("invalid broker number")
	}
	if v.ExpiresAtUnixMs > 9007199254740991 {
		return errors.New("invalid broker number")
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
func (v *MarketApproval) UnmarshalJSON(b []byte) error {
	type wire MarketApproval
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil || raw == nil {
		return errors.New("invalid broker object")
	}
	if _, ok := raw["approvalId"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["approvalId"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["agentSessionId"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["agentSessionId"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["kind"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["kind"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
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
	if _, ok := raw["identity"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["identity"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["review"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["review"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["expiresAtUnixMs"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["expiresAtUnixMs"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
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
	x := MarketApproval(w)
	if err := x.Validate(); err != nil {
		return err
	}
	*v = x
	return nil
}

type ApprovalDecidePayload struct {
	OperationId      CanonicalId  `json:"operationId"`
	HostInstanceId   CanonicalId  `json:"hostInstanceId"`
	Scope            ScopeBinding `json:"scope"`
	AgentSessionId   CanonicalId  `json:"agentSessionId"`
	ApprovalId       CanonicalId  `json:"approvalId"`
	CallRef          CanonicalId  `json:"callRef"`
	ExpectedRevision Revision     `json:"expectedRevision"`
	DecisionId       CanonicalId  `json:"decisionId"`
	Decision         Decision     `json:"decision"`
}

func (v ApprovalDecidePayload) Validate() error {
	if err := v.OperationId.Validate(); err != nil {
		return err
	}
	if err := v.HostInstanceId.Validate(); err != nil {
		return err
	}
	if err := v.Scope.Validate(); err != nil {
		return err
	}
	if err := v.AgentSessionId.Validate(); err != nil {
		return err
	}
	if err := v.ApprovalId.Validate(); err != nil {
		return err
	}
	if err := v.CallRef.Validate(); err != nil {
		return err
	}
	if err := v.ExpectedRevision.Validate(); err != nil {
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
func (v *ApprovalDecidePayload) UnmarshalJSON(b []byte) error {
	type wire ApprovalDecidePayload
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
	if _, ok := raw["agentSessionId"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["agentSessionId"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["approvalId"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["approvalId"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["callRef"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["callRef"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["expectedRevision"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["expectedRevision"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
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
	x := ApprovalDecidePayload(w)
	if err := x.Validate(); err != nil {
		return err
	}
	*v = x
	return nil
}

type ApprovalSnapshot struct {
	AgentSessionId CanonicalId      `json:"agentSessionId"`
	Requests       []MarketApproval `json:"requests"`
}

func (v ApprovalSnapshot) Validate() error {
	if err := v.AgentSessionId.Validate(); err != nil {
		return err
	}
	if v.Requests == nil {
		return errors.New("null broker collection")
	}
	if len(v.Requests) > 128 {
		return errors.New("invalid broker collection")
	}
	for _, item := range v.Requests {
		if err := item.Validate(); err != nil {
			return err
		}
	}
	return nil
}
func (v *ApprovalSnapshot) UnmarshalJSON(b []byte) error {
	type wire ApprovalSnapshot
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil || raw == nil {
		return errors.New("invalid broker object")
	}
	if _, ok := raw["agentSessionId"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["agentSessionId"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["requests"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["requests"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
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
	x := ApprovalSnapshot(w)
	if err := x.Validate(); err != nil {
		return err
	}
	*v = x
	return nil
}

type ToolObservation struct {
	NativeItemId   NativeId        `json:"nativeItemId"`
	NativeThreadId NativeId        `json:"nativeThreadId"`
	NativeTurnId   NativeId        `json:"nativeTurnId"`
	ServerName     string          `json:"serverName"`
	ToolName       string          `json:"toolName"`
	State          string          `json:"state"`
	Service        *ServiceBinding `json:"service,omitempty"`
	CallRef        *CanonicalId    `json:"callRef,omitempty"`
	ResultText     *string         `json:"resultText,omitempty"`
	ErrorCode      *string         `json:"errorCode,omitempty"`
	Truncated      bool            `json:"truncated"`
}

func (v ToolObservation) Validate() error {
	if err := v.NativeItemId.Validate(); err != nil {
		return err
	}
	if err := v.NativeThreadId.Validate(); err != nil {
		return err
	}
	if err := v.NativeTurnId.Validate(); err != nil {
		return err
	}
	if utf8.RuneCountInString(string(v.ServerName)) < 1 {
		return errors.New("invalid broker text")
	}
	if utf8.RuneCountInString(string(v.ServerName)) > 128 {
		return errors.New("invalid broker text")
	}
	if utf8.RuneCountInString(string(v.ToolName)) < 1 {
		return errors.New("invalid broker text")
	}
	if utf8.RuneCountInString(string(v.ToolName)) > 128 {
		return errors.New("invalid broker text")
	}
	switch string(v.State) {
	case "in_progress", "completed", "failed":
	default:
		return errors.New("invalid broker enum")
	}
	if v.Service != nil {
		if err := (*v.Service).Validate(); err != nil {
			return err
		}
	}
	if v.CallRef != nil {
		if err := (*v.CallRef).Validate(); err != nil {
			return err
		}
	}
	if v.ResultText != nil {
		if utf8.RuneCountInString(string((*v.ResultText))) > 65536 {
			return errors.New("invalid broker text")
		}
	}
	if v.ErrorCode != nil {
		switch string((*v.ErrorCode)) {
		case "tool_failed", "cancelled", "runtime_unavailable", "unsupported_result":
		default:
			return errors.New("invalid broker enum")
		}
	}
	return nil
}
func (v *ToolObservation) UnmarshalJSON(b []byte) error {
	type wire ToolObservation
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil || raw == nil {
		return errors.New("invalid broker object")
	}
	if _, ok := raw["nativeItemId"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["nativeItemId"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["nativeThreadId"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["nativeThreadId"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["nativeTurnId"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["nativeTurnId"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["serverName"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["serverName"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["toolName"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["toolName"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["state"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["state"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if x, ok := raw["service"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if x, ok := raw["callRef"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if x, ok := raw["resultText"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if x, ok := raw["errorCode"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["truncated"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["truncated"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
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
	x := ToolObservation(w)
	if err := x.Validate(); err != nil {
		return err
	}
	*v = x
	return nil
}

type ToolSnapshot struct {
	AgentSessionId CanonicalId       `json:"agentSessionId"`
	NativeTurnId   NativeId          `json:"nativeTurnId"`
	Items          []ToolObservation `json:"items"`
	Truncated      bool              `json:"truncated"`
}

func (v ToolSnapshot) Validate() error {
	if err := v.AgentSessionId.Validate(); err != nil {
		return err
	}
	if err := v.NativeTurnId.Validate(); err != nil {
		return err
	}
	if v.Items == nil {
		return errors.New("null broker collection")
	}
	if len(v.Items) > 128 {
		return errors.New("invalid broker collection")
	}
	for _, item := range v.Items {
		if err := item.Validate(); err != nil {
			return err
		}
	}
	return nil
}
func (v *ToolSnapshot) UnmarshalJSON(b []byte) error {
	type wire ToolSnapshot
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil || raw == nil {
		return errors.New("invalid broker object")
	}
	if _, ok := raw["agentSessionId"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["agentSessionId"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["nativeTurnId"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["nativeTurnId"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["items"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["items"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["truncated"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["truncated"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
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
	x := ToolSnapshot(w)
	if err := x.Validate(); err != nil {
		return err
	}
	*v = x
	return nil
}

type ErrorCode string

const (
	ErrorCodeInvalidRequest            ErrorCode = "invalid_request"
	ErrorCodeNotReady                  ErrorCode = "not_ready"
	ErrorCodeAuthorityMismatch         ErrorCode = "authority_mismatch"
	ErrorCodeScopeExpired              ErrorCode = "scope_expired"
	ErrorCodePermissionDenied          ErrorCode = "permission_denied"
	ErrorCodeGrantNotFound             ErrorCode = "grant_not_found"
	ErrorCodeGrantExpired              ErrorCode = "grant_expired"
	ErrorCodeCapacityExceeded          ErrorCode = "capacity_exceeded"
	ErrorCodeRequestConflict           ErrorCode = "request_conflict"
	ErrorCodeRevisionConflict          ErrorCode = "revision_conflict"
	ErrorCodeSelectionStale            ErrorCode = "selection_stale"
	ErrorCodeExecutionUnavailable      ErrorCode = "execution_unavailable"
	ErrorCodeModelUnavailable          ErrorCode = "model_unavailable"
	ErrorCodePermissionModeUnavailable ErrorCode = "permission_mode_unavailable"
	ErrorCodeSessionNotFound           ErrorCode = "session_not_found"
	ErrorCodeBusy                      ErrorCode = "busy"
	ErrorCodeOperationUncertain        ErrorCode = "operation_uncertain"
	ErrorCodeApprovalNotFound          ErrorCode = "approval_not_found"
	ErrorCodeApprovalStale             ErrorCode = "approval_stale"
	ErrorCodeApprovalExpired           ErrorCode = "approval_expired"
	ErrorCodeApprovalResolved          ErrorCode = "approval_resolved"
	ErrorCodeCleanupPending            ErrorCode = "cleanup_pending"
	ErrorCodeTemporarilyUnavailable    ErrorCode = "temporarily_unavailable"
	ErrorCodeContextInvalid            ErrorCode = "context_invalid"
	ErrorCodeBindingMismatch           ErrorCode = "binding_mismatch"
	ErrorCodeNotFound                  ErrorCode = "not_found"
	ErrorCodeUnknownService            ErrorCode = "unknown_service"
	ErrorCodeUnsupportedAuth           ErrorCode = "unsupported_auth"
	ErrorCodeKeyringUnavailable        ErrorCode = "keyring_unavailable"
	ErrorCodeMetadataUnavailable       ErrorCode = "metadata_unavailable"
	ErrorCodeAuthorizationRejected     ErrorCode = "authorization_rejected"
	ErrorCodeAuthorizationTimeout      ErrorCode = "authorization_timeout"
	ErrorCodeNotQualified              ErrorCode = "not_qualified"
)

func (v ErrorCode) Validate() error {
	switch v {
	case ErrorCodeInvalidRequest, ErrorCodeNotReady, ErrorCodeAuthorityMismatch, ErrorCodeScopeExpired, ErrorCodePermissionDenied, ErrorCodeGrantNotFound, ErrorCodeGrantExpired, ErrorCodeCapacityExceeded, ErrorCodeRequestConflict, ErrorCodeRevisionConflict, ErrorCodeSelectionStale, ErrorCodeExecutionUnavailable, ErrorCodeModelUnavailable, ErrorCodePermissionModeUnavailable, ErrorCodeSessionNotFound, ErrorCodeBusy, ErrorCodeOperationUncertain, ErrorCodeApprovalNotFound, ErrorCodeApprovalStale, ErrorCodeApprovalExpired, ErrorCodeApprovalResolved, ErrorCodeCleanupPending, ErrorCodeTemporarilyUnavailable, ErrorCodeContextInvalid, ErrorCodeBindingMismatch, ErrorCodeNotFound, ErrorCodeUnknownService, ErrorCodeUnsupportedAuth, ErrorCodeKeyringUnavailable, ErrorCodeMetadataUnavailable, ErrorCodeAuthorizationRejected, ErrorCodeAuthorizationTimeout, ErrorCodeNotQualified:
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

type HelloRequest struct {
	SchemaVersion int64        `json:"schemaVersion"`
	RequestId     CanonicalId  `json:"requestId"`
	Method        string       `json:"method"`
	Payload       HelloPayload `json:"payload"`
}

func (v HelloRequest) Validate() error {
	if v.SchemaVersion != 1 {
		return errors.New("invalid broker constant")
	}
	if err := v.RequestId.Validate(); err != nil {
		return err
	}
	if string(v.Method) != "hello" {
		return errors.New("invalid broker constant")
	}
	if err := v.Payload.Validate(); err != nil {
		return err
	}
	return nil
}
func (v *HelloRequest) UnmarshalJSON(b []byte) error {
	type wire HelloRequest
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
	x := HelloRequest(w)
	if err := x.Validate(); err != nil {
		return err
	}
	*v = x
	return nil
}

type HelloResponse struct {
	SchemaVersion int64       `json:"schemaVersion"`
	RequestId     CanonicalId `json:"requestId"`
	Data          HelloResult `json:"data"`
}

func (v HelloResponse) Validate() error {
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
func (v *HelloResponse) UnmarshalJSON(b []byte) error {
	type wire HelloResponse
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
	x := HelloResponse(w)
	if err := x.Validate(); err != nil {
		return err
	}
	*v = x
	return nil
}

type GrantRegisterRequest struct {
	SchemaVersion int64                `json:"schemaVersion"`
	RequestId     CanonicalId          `json:"requestId"`
	Method        string               `json:"method"`
	Payload       GrantRegisterPayload `json:"payload"`
}

func (v GrantRegisterRequest) Validate() error {
	if v.SchemaVersion != 1 {
		return errors.New("invalid broker constant")
	}
	if err := v.RequestId.Validate(); err != nil {
		return err
	}
	if string(v.Method) != "grant_register" {
		return errors.New("invalid broker constant")
	}
	if err := v.Payload.Validate(); err != nil {
		return err
	}
	return nil
}
func (v *GrantRegisterRequest) UnmarshalJSON(b []byte) error {
	type wire GrantRegisterRequest
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
	x := GrantRegisterRequest(w)
	if err := x.Validate(); err != nil {
		return err
	}
	*v = x
	return nil
}

type GrantRegisterResponse struct {
	SchemaVersion int64             `json:"schemaVersion"`
	RequestId     CanonicalId       `json:"requestId"`
	Data          GrantRegistration `json:"data"`
}

func (v GrantRegisterResponse) Validate() error {
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
func (v *GrantRegisterResponse) UnmarshalJSON(b []byte) error {
	type wire GrantRegisterResponse
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
	x := GrantRegisterResponse(w)
	if err := x.Validate(); err != nil {
		return err
	}
	*v = x
	return nil
}

type RevokeAuthorityRequest struct {
	SchemaVersion int64                  `json:"schemaVersion"`
	RequestId     CanonicalId            `json:"requestId"`
	Method        string                 `json:"method"`
	Payload       RevokeAuthorityPayload `json:"payload"`
}

func (v RevokeAuthorityRequest) Validate() error {
	if v.SchemaVersion != 1 {
		return errors.New("invalid broker constant")
	}
	if err := v.RequestId.Validate(); err != nil {
		return err
	}
	if string(v.Method) != "revoke_authority" {
		return errors.New("invalid broker constant")
	}
	if err := v.Payload.Validate(); err != nil {
		return err
	}
	return nil
}
func (v *RevokeAuthorityRequest) UnmarshalJSON(b []byte) error {
	type wire RevokeAuthorityRequest
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
	x := RevokeAuthorityRequest(w)
	if err := x.Validate(); err != nil {
		return err
	}
	*v = x
	return nil
}

type RevokeAuthorityResponse struct {
	SchemaVersion int64                 `json:"schemaVersion"`
	RequestId     CanonicalId           `json:"requestId"`
	Data          RevokeAuthorityResult `json:"data"`
}

func (v RevokeAuthorityResponse) Validate() error {
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
func (v *RevokeAuthorityResponse) UnmarshalJSON(b []byte) error {
	type wire RevokeAuthorityResponse
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
	x := RevokeAuthorityResponse(w)
	if err := x.Validate(); err != nil {
		return err
	}
	*v = x
	return nil
}

type ApprovalDecideRequest struct {
	SchemaVersion int64                 `json:"schemaVersion"`
	RequestId     CanonicalId           `json:"requestId"`
	Method        string                `json:"method"`
	Payload       ApprovalDecidePayload `json:"payload"`
}

func (v ApprovalDecideRequest) Validate() error {
	if v.SchemaVersion != 1 {
		return errors.New("invalid broker constant")
	}
	if err := v.RequestId.Validate(); err != nil {
		return err
	}
	if string(v.Method) != "approval_decide" {
		return errors.New("invalid broker constant")
	}
	if err := v.Payload.Validate(); err != nil {
		return err
	}
	return nil
}
func (v *ApprovalDecideRequest) UnmarshalJSON(b []byte) error {
	type wire ApprovalDecideRequest
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
	x := ApprovalDecideRequest(w)
	if err := x.Validate(); err != nil {
		return err
	}
	*v = x
	return nil
}

type ApprovalDecideResponse struct {
	SchemaVersion int64          `json:"schemaVersion"`
	RequestId     CanonicalId    `json:"requestId"`
	Data          MarketApproval `json:"data"`
}

func (v ApprovalDecideResponse) Validate() error {
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
func (v *ApprovalDecideResponse) UnmarshalJSON(b []byte) error {
	type wire ApprovalDecideResponse
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
	x := ApprovalDecideResponse(w)
	if err := x.Validate(); err != nil {
		return err
	}
	*v = x
	return nil
}

type SubmitRequest struct {
	SchemaVersion int64         `json:"schemaVersion"`
	RequestId     CanonicalId   `json:"requestId"`
	Payload       SubmitPayload `json:"payload"`
}

func (v SubmitRequest) Validate() error {
	if v.SchemaVersion != 1 {
		return errors.New("invalid broker constant")
	}
	if err := v.RequestId.Validate(); err != nil {
		return err
	}
	if err := v.Payload.Validate(); err != nil {
		return err
	}
	return nil
}
func (v *SubmitRequest) UnmarshalJSON(b []byte) error {
	type wire SubmitRequest
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
	x := SubmitRequest(w)
	if err := x.Validate(); err != nil {
		return err
	}
	*v = x
	return nil
}

type SubmitResponse struct {
	SchemaVersion int64             `json:"schemaVersion"`
	RequestId     CanonicalId       `json:"requestId"`
	Data          SubmissionReceipt `json:"data"`
}

func (v SubmitResponse) Validate() error {
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
func (v *SubmitResponse) UnmarshalJSON(b []byte) error {
	type wire SubmitResponse
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
	x := SubmitResponse(w)
	if err := x.Validate(); err != nil {
		return err
	}
	*v = x
	return nil
}

type OperationResponse struct {
	SchemaVersion int64             `json:"schemaVersion"`
	RequestId     CanonicalId       `json:"requestId"`
	Data          SubmissionReceipt `json:"data"`
}

func (v OperationResponse) Validate() error {
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
func (v *OperationResponse) UnmarshalJSON(b []byte) error {
	type wire OperationResponse
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
	x := OperationResponse(w)
	if err := x.Validate(); err != nil {
		return err
	}
	*v = x
	return nil
}

type ApprovalsResponse struct {
	SchemaVersion int64            `json:"schemaVersion"`
	RequestId     CanonicalId      `json:"requestId"`
	Data          ApprovalSnapshot `json:"data"`
}

func (v ApprovalsResponse) Validate() error {
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
func (v *ApprovalsResponse) UnmarshalJSON(b []byte) error {
	type wire ApprovalsResponse
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
	x := ApprovalsResponse(w)
	if err := x.Validate(); err != nil {
		return err
	}
	*v = x
	return nil
}

type ToolsResponse struct {
	SchemaVersion int64        `json:"schemaVersion"`
	RequestId     CanonicalId  `json:"requestId"`
	Data          ToolSnapshot `json:"data"`
}

func (v ToolsResponse) Validate() error {
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
func (v *ToolsResponse) UnmarshalJSON(b []byte) error {
	type wire ToolsResponse
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
	x := ToolsResponse(w)
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

type ProviderPayload = provider.ProviderPayload
type ProviderStatus = provider.ProviderStatus
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

type SelectionDisplay = market.SelectionDisplay
type NativeObservePayload struct {
	SessionId    CanonicalId `json:"sessionId"`
	NativeTurnId *NativeId   `json:"nativeTurnId,omitempty"`
}

func (v NativeObservePayload) Validate() error {
	if err := v.SessionId.Validate(); err != nil {
		return err
	}
	if v.NativeTurnId != nil {
		if err := (*v.NativeTurnId).Validate(); err != nil {
			return err
		}
	}
	return nil
}
func (v *NativeObservePayload) UnmarshalJSON(b []byte) error {
	type wire NativeObservePayload
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil || raw == nil {
		return errors.New("invalid broker object")
	}
	if _, ok := raw["sessionId"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["sessionId"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if x, ok := raw["nativeTurnId"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
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
	x := NativeObservePayload(w)
	if err := x.Validate(); err != nil {
		return err
	}
	*v = x
	return nil
}

type NativeObservation struct {
	Managed          bool               `json:"managed"`
	SelectionDisplay []SelectionDisplay `json:"selectionDisplay"`
	Approvals        *ApprovalSnapshot  `json:"approvals,omitempty"`
	Tools            *ToolSnapshot      `json:"tools,omitempty"`
	AvailableTurns   *[]ObservedTurn    `json:"availableTurns,omitempty"`
	TurnsTruncated   *bool              `json:"turnsTruncated,omitempty"`
}

func (v NativeObservation) Validate() error {
	if v.SelectionDisplay == nil {
		return errors.New("null broker collection")
	}
	if len(v.SelectionDisplay) > 58 {
		return errors.New("invalid broker collection")
	}
	for _, item := range v.SelectionDisplay {
		if err := item.Validate(); err != nil {
			return err
		}
	}
	if v.Approvals != nil {
		if err := (*v.Approvals).Validate(); err != nil {
			return err
		}
	}
	if v.Tools != nil {
		if err := (*v.Tools).Validate(); err != nil {
			return err
		}
	}
	if v.AvailableTurns != nil {
		if (*v.AvailableTurns) == nil {
			return errors.New("null broker collection")
		}
		if len((*v.AvailableTurns)) > 128 {
			return errors.New("invalid broker collection")
		}
		for _, item := range *v.AvailableTurns {
			if err := item.Validate(); err != nil {
				return err
			}
		}
	}
	if v.TurnsTruncated != nil {
	}
	return nil
}
func (v *NativeObservation) UnmarshalJSON(b []byte) error {
	type wire NativeObservation
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil || raw == nil {
		return errors.New("invalid broker object")
	}
	if _, ok := raw["managed"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["managed"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["selectionDisplay"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["selectionDisplay"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if x, ok := raw["approvals"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if x, ok := raw["tools"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if x, ok := raw["availableTurns"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if x, ok := raw["turnsTruncated"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
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
	x := NativeObservation(w)
	if err := x.Validate(); err != nil {
		return err
	}
	*v = x
	return nil
}

type NativeApprovalDecidePayload struct {
	SessionId        CanonicalId `json:"sessionId"`
	ApprovalId       CanonicalId `json:"approvalId"`
	DecisionId       CanonicalId `json:"decisionId"`
	ExpectedRevision Revision    `json:"expectedRevision"`
	Decision         Decision    `json:"decision"`
}

func (v NativeApprovalDecidePayload) Validate() error {
	if err := v.SessionId.Validate(); err != nil {
		return err
	}
	if err := v.ApprovalId.Validate(); err != nil {
		return err
	}
	if err := v.DecisionId.Validate(); err != nil {
		return err
	}
	if err := v.ExpectedRevision.Validate(); err != nil {
		return err
	}
	if err := v.Decision.Validate(); err != nil {
		return err
	}
	return nil
}
func (v *NativeApprovalDecidePayload) UnmarshalJSON(b []byte) error {
	type wire NativeApprovalDecidePayload
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil || raw == nil {
		return errors.New("invalid broker object")
	}
	if _, ok := raw["sessionId"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["sessionId"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["approvalId"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["approvalId"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["decisionId"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["decisionId"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["expectedRevision"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["expectedRevision"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
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
	x := NativeApprovalDecidePayload(w)
	if err := x.Validate(); err != nil {
		return err
	}
	*v = x
	return nil
}

type NativeObserveRequest struct {
	SchemaVersion int64                `json:"schemaVersion"`
	RequestId     CanonicalId          `json:"requestId"`
	ContextId     CanonicalId          `json:"contextId"`
	Payload       NativeObservePayload `json:"payload"`
}

func (v NativeObserveRequest) Validate() error {
	if v.SchemaVersion != 1 {
		return errors.New("invalid broker constant")
	}
	if err := v.RequestId.Validate(); err != nil {
		return err
	}
	if err := v.ContextId.Validate(); err != nil {
		return err
	}
	if err := v.Payload.Validate(); err != nil {
		return err
	}
	return nil
}
func (v *NativeObserveRequest) UnmarshalJSON(b []byte) error {
	type wire NativeObserveRequest
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
	if _, ok := raw["contextId"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["contextId"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
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
	x := NativeObserveRequest(w)
	if err := x.Validate(); err != nil {
		return err
	}
	*v = x
	return nil
}

type NativeObserveResponse struct {
	SchemaVersion int64             `json:"schemaVersion"`
	RequestId     CanonicalId       `json:"requestId"`
	Data          NativeObservation `json:"data"`
}

func (v NativeObserveResponse) Validate() error {
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
func (v *NativeObserveResponse) UnmarshalJSON(b []byte) error {
	type wire NativeObserveResponse
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
	x := NativeObserveResponse(w)
	if err := x.Validate(); err != nil {
		return err
	}
	*v = x
	return nil
}

type NativeApprovalDecideRequest struct {
	SchemaVersion int64                       `json:"schemaVersion"`
	RequestId     CanonicalId                 `json:"requestId"`
	ContextId     CanonicalId                 `json:"contextId"`
	Payload       NativeApprovalDecidePayload `json:"payload"`
}

func (v NativeApprovalDecideRequest) Validate() error {
	if v.SchemaVersion != 1 {
		return errors.New("invalid broker constant")
	}
	if err := v.RequestId.Validate(); err != nil {
		return err
	}
	if err := v.ContextId.Validate(); err != nil {
		return err
	}
	if err := v.Payload.Validate(); err != nil {
		return err
	}
	return nil
}
func (v *NativeApprovalDecideRequest) UnmarshalJSON(b []byte) error {
	type wire NativeApprovalDecideRequest
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
	if _, ok := raw["contextId"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["contextId"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
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
	x := NativeApprovalDecideRequest(w)
	if err := x.Validate(); err != nil {
		return err
	}
	*v = x
	return nil
}

type NativeApprovalDecideResponse struct {
	SchemaVersion int64          `json:"schemaVersion"`
	RequestId     CanonicalId    `json:"requestId"`
	Data          MarketApproval `json:"data"`
}

func (v NativeApprovalDecideResponse) Validate() error {
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
func (v *NativeApprovalDecideResponse) UnmarshalJSON(b []byte) error {
	type wire NativeApprovalDecideResponse
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
	x := NativeApprovalDecideResponse(w)
	if err := x.Validate(); err != nil {
		return err
	}
	*v = x
	return nil
}

type NativeError = Error
type ObservedTurn struct {
	NativeTurnId     NativeId           `json:"nativeTurnId"`
	SelectionDisplay []SelectionDisplay `json:"selectionDisplay"`
}

func (v ObservedTurn) Validate() error {
	if err := v.NativeTurnId.Validate(); err != nil {
		return err
	}
	if v.SelectionDisplay == nil {
		return errors.New("null broker collection")
	}
	if len(v.SelectionDisplay) > 58 {
		return errors.New("invalid broker collection")
	}
	for _, item := range v.SelectionDisplay {
		if err := item.Validate(); err != nil {
			return err
		}
	}
	return nil
}
func (v *ObservedTurn) UnmarshalJSON(b []byte) error {
	type wire ObservedTurn
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil || raw == nil {
		return errors.New("invalid broker object")
	}
	if _, ok := raw["nativeTurnId"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["nativeTurnId"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["selectionDisplay"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["selectionDisplay"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
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
	x := ObservedTurn(w)
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

type StartTurnV2TextBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

func (v StartTurnV2TextBlock) Validate() error {
	switch string(v.Type) {
	case "text":
	default:
		return errors.New("invalid broker enum")
	}
	if utf8.RuneCountInString(string(v.Text)) < 1 {
		return errors.New("invalid broker text")
	}
	if utf8.RuneCountInString(string(v.Text)) > 1048576 {
		return errors.New("invalid broker text")
	}
	if !wirePattern3.MatchString(string(v.Text)) {
		return errors.New("invalid broker pattern")
	}
	return nil
}
func (v *StartTurnV2TextBlock) UnmarshalJSON(b []byte) error {
	type wire StartTurnV2TextBlock
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil || raw == nil {
		return errors.New("invalid broker object")
	}
	if _, ok := raw["type"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["type"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["text"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["text"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
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
	x := StartTurnV2TextBlock(w)
	if err := x.Validate(); err != nil {
		return err
	}
	*v = x
	return nil
}

type StartTurnV2ImageBlock struct {
	Type         string `json:"type"`
	AttachmentId string `json:"attachment_id"`
	MediaType    string `json:"media_type"`
	SizeBytes    int64  `json:"size_bytes"`
	Sha256       string `json:"sha256"`
	DataUrl      string `json:"data_url"`
}

func (v StartTurnV2ImageBlock) Validate() error {
	switch string(v.Type) {
	case "image":
	default:
		return errors.New("invalid broker enum")
	}
	if err := market.CanonicalId(v.AttachmentId).Validate(); err != nil {
		return err
	}
	switch string(v.MediaType) {
	case "image/jpeg", "image/png", "image/webp", "image/gif":
	default:
		return errors.New("invalid broker enum")
	}
	if v.SizeBytes < 1 {
		return errors.New("invalid broker number")
	}
	if v.SizeBytes > 10485760 {
		return errors.New("invalid broker number")
	}
	if !wirePattern4.MatchString(string(v.Sha256)) {
		return errors.New("invalid broker pattern")
	}
	if utf8.RuneCountInString(string(v.DataUrl)) < 23 {
		return errors.New("invalid broker text")
	}
	if utf8.RuneCountInString(string(v.DataUrl)) > 13981039 {
		return errors.New("invalid broker text")
	}
	if !wirePattern5.MatchString(string(v.DataUrl)) {
		return errors.New("invalid broker pattern")
	}
	return nil
}
func (v *StartTurnV2ImageBlock) UnmarshalJSON(b []byte) error {
	type wire StartTurnV2ImageBlock
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil || raw == nil {
		return errors.New("invalid broker object")
	}
	if _, ok := raw["type"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["type"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["attachment_id"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["attachment_id"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["media_type"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["media_type"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["size_bytes"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["size_bytes"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["sha256"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["sha256"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["data_url"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["data_url"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
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
	x := StartTurnV2ImageBlock(w)
	if err := x.Validate(); err != nil {
		return err
	}
	*v = x
	return nil
}

type StartTurnV2FileBlock struct {
	Type          string                   `json:"type"`
	AttachmentId  string                   `json:"attachment_id"`
	Name          string                   `json:"name"`
	MediaType     StartTurnV2FileMediaType `json:"media_type"`
	SizeBytes     int64                    `json:"size_bytes"`
	Sha256        string                   `json:"sha256"`
	ContextChunks []string                 `json:"context_chunks"`
}

func (v StartTurnV2FileBlock) Validate() error {
	switch string(v.Type) {
	case "file":
	default:
		return errors.New("invalid broker enum")
	}
	if err := market.CanonicalId(v.AttachmentId).Validate(); err != nil {
		return err
	}
	if utf8.RuneCountInString(string(v.Name)) < 1 {
		return errors.New("invalid broker text")
	}
	if utf8.RuneCountInString(string(v.Name)) > 255 {
		return errors.New("invalid broker text")
	}
	if !wirePattern6.MatchString(string(v.Name)) {
		return errors.New("invalid broker pattern")
	}
	switch string(v.Name) {
	case ".", "..":
		return errors.New("invalid excluded value")
	}
	if err := v.MediaType.Validate(); err != nil {
		return err
	}
	if v.SizeBytes < 1 {
		return errors.New("invalid broker number")
	}
	if v.SizeBytes > 10485760 {
		return errors.New("invalid broker number")
	}
	if !wirePattern4.MatchString(string(v.Sha256)) {
		return errors.New("invalid broker pattern")
	}
	if v.ContextChunks == nil {
		return errors.New("null broker collection")
	}
	if len(v.ContextChunks) < 1 {
		return errors.New("invalid broker collection")
	}
	if len(v.ContextChunks) > 32 {
		return errors.New("invalid broker collection")
	}
	for _, item := range v.ContextChunks {
		if utf8.RuneCountInString(string(item)) < 1 {
			return errors.New("invalid broker text")
		}
		if utf8.RuneCountInString(string(item)) > 16384 {
			return errors.New("invalid broker text")
		}
		if !wirePattern3.MatchString(string(item)) {
			return errors.New("invalid broker pattern")
		}
	}
	return nil
}
func (v *StartTurnV2FileBlock) UnmarshalJSON(b []byte) error {
	type wire StartTurnV2FileBlock
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil || raw == nil {
		return errors.New("invalid broker object")
	}
	if _, ok := raw["type"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["type"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["attachment_id"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["attachment_id"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["name"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["name"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["media_type"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["media_type"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["size_bytes"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["size_bytes"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["sha256"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["sha256"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["context_chunks"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["context_chunks"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
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
	x := StartTurnV2FileBlock(w)
	if err := x.Validate(); err != nil {
		return err
	}
	*v = x
	return nil
}

type StartTurnV2FileMediaType string

const (
	StartTurnV2FileMediaTypeApplicationPdf                                                       StartTurnV2FileMediaType = "application/pdf"
	StartTurnV2FileMediaTypeTextPlain                                                            StartTurnV2FileMediaType = "text/plain"
	StartTurnV2FileMediaTypeTextMarkdown                                                         StartTurnV2FileMediaType = "text/markdown"
	StartTurnV2FileMediaTypeTextCsv                                                              StartTurnV2FileMediaType = "text/csv"
	StartTurnV2FileMediaTypeApplicationJson                                                      StartTurnV2FileMediaType = "application/json"
	StartTurnV2FileMediaTypeApplicationYaml                                                      StartTurnV2FileMediaType = "application/yaml"
	StartTurnV2FileMediaTypeApplicationXml                                                       StartTurnV2FileMediaType = "application/xml"
	StartTurnV2FileMediaTypeTextHtml                                                             StartTurnV2FileMediaType = "text/html"
	StartTurnV2FileMediaTypeApplicationRtf                                                       StartTurnV2FileMediaType = "application/rtf"
	StartTurnV2FileMediaTypeApplicationVndOpenxmlformatsOfficedocumentWordprocessingmlDocument   StartTurnV2FileMediaType = "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
	StartTurnV2FileMediaTypeApplicationVndOpenxmlformatsOfficedocumentSpreadsheetmlSheet         StartTurnV2FileMediaType = "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
	StartTurnV2FileMediaTypeApplicationVndOpenxmlformatsOfficedocumentPresentationmlPresentation StartTurnV2FileMediaType = "application/vnd.openxmlformats-officedocument.presentationml.presentation"
)

func (v StartTurnV2FileMediaType) Validate() error {
	switch v {
	case StartTurnV2FileMediaTypeApplicationPdf, StartTurnV2FileMediaTypeTextPlain, StartTurnV2FileMediaTypeTextMarkdown, StartTurnV2FileMediaTypeTextCsv, StartTurnV2FileMediaTypeApplicationJson, StartTurnV2FileMediaTypeApplicationYaml, StartTurnV2FileMediaTypeApplicationXml, StartTurnV2FileMediaTypeTextHtml, StartTurnV2FileMediaTypeApplicationRtf, StartTurnV2FileMediaTypeApplicationVndOpenxmlformatsOfficedocumentWordprocessingmlDocument, StartTurnV2FileMediaTypeApplicationVndOpenxmlformatsOfficedocumentSpreadsheetmlSheet, StartTurnV2FileMediaTypeApplicationVndOpenxmlformatsOfficedocumentPresentationmlPresentation:
		return nil
	default:
		return errors.New("invalid broker enum")
	}
}
func (v *StartTurnV2FileMediaType) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return errors.New("invalid broker enum")
	}
	x := StartTurnV2FileMediaType(s)
	if err := x.Validate(); err != nil {
		return err
	}
	*v = x
	return nil
}
func validateContentBlock(v ContentBlock) error {
	b, err := v.MarshalJSON()
	if err != nil {
		return errors.New("invalid content block")
	}
	var tag struct {
		Type string `json:"type"`
	}
	if json.Unmarshal(b, &tag) != nil {
		return errors.New("invalid content block")
	}
	switch tag.Type {
	case "text":
		var value StartTurnV2TextBlock
		return json.Unmarshal(b, &value)
	case "image":
		var value StartTurnV2ImageBlock
		return json.Unmarshal(b, &value)
	case "file":
		var value StartTurnV2FileBlock
		return json.Unmarshal(b, &value)
	default:
		return errors.New("unsupported content block")
	}
}

const MaxControlFrameBytes = 16842752
const MaxSmallControlFrameBytes = 65536
const MaxHttpTriggerBytes = 4096
const MaxPendingGrants = 8
const MaxGrantBytes = 67108864
const MaxGrantTtlMs = 300000
const MaxControlReceipts = 1024
const MaxApprovalRecords = 128
const MaxToolItems = 128
const MaxResultTextBytes = 65536
