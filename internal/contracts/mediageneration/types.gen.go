// Generated from media-generation/v1; DO NOT EDIT.
package mediageneration

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"unicode/utf8"
)

type GenerateInput struct {
	Prompt              string `json:"prompt"`
	UseCurrentReference bool   `json:"use_current_reference"`
}

func (v GenerateInput) Validate() error {
	if utf8.RuneCountInString(string(v.Prompt)) < 1 {
		return errors.New("invalid broker text")
	}
	if utf8.RuneCountInString(string(v.Prompt)) > 1500 {
		return errors.New("invalid broker text")
	}
	if !utf8.ValidString(v.Prompt) || strings.TrimSpace(v.Prompt) == "" || strings.ContainsRune(v.Prompt, 0) {
		return errors.New("invalid image prompt")
	}
	return nil
}
func (v *GenerateInput) UnmarshalJSON(b []byte) error {
	type wire GenerateInput
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil || raw == nil {
		return errors.New("invalid broker object")
	}
	if _, ok := raw["prompt"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["prompt"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["use_current_reference"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["use_current_reference"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
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
	x := GenerateInput(w)
	if err := x.Validate(); err != nil {
		return err
	}
	*v = x
	return nil
}

type GenerateRequest struct {
	ThreadId string        `json:"thread_id"`
	TurnId   string        `json:"turn_id"`
	Input    GenerateInput `json:"input"`
}

func (v GenerateRequest) Validate() error {
	if utf8.RuneCountInString(string(v.ThreadId)) < 36 {
		return errors.New("invalid broker text")
	}
	if utf8.RuneCountInString(string(v.ThreadId)) > 36 {
		return errors.New("invalid broker text")
	}
	if utf8.RuneCountInString(string(v.TurnId)) < 36 {
		return errors.New("invalid broker text")
	}
	if utf8.RuneCountInString(string(v.TurnId)) > 36 {
		return errors.New("invalid broker text")
	}
	if err := v.Input.Validate(); err != nil {
		return err
	}
	return nil
}
func (v *GenerateRequest) UnmarshalJSON(b []byte) error {
	type wire GenerateRequest
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil || raw == nil {
		return errors.New("invalid broker object")
	}
	if _, ok := raw["thread_id"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["thread_id"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["turn_id"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["turn_id"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["input"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["input"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
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
	x := GenerateRequest(w)
	if err := x.Validate(); err != nil {
		return err
	}
	*v = x
	return nil
}

type GenerateStatus string

const (
	GenerateStatusStaged    GenerateStatus = "staged"
	GenerateStatusCommitted GenerateStatus = "committed"
	GenerateStatusBlocked   GenerateStatus = "blocked"
	GenerateStatusFailed    GenerateStatus = "failed"
	GenerateStatusUnknown   GenerateStatus = "unknown"
)

func (v GenerateStatus) Validate() error {
	switch v {
	case GenerateStatusStaged, GenerateStatusCommitted, GenerateStatusBlocked, GenerateStatusFailed, GenerateStatusUnknown:
		return nil
	default:
		return errors.New("invalid broker enum")
	}
}
func (v *GenerateStatus) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return errors.New("invalid broker enum")
	}
	x := GenerateStatus(s)
	if err := x.Validate(); err != nil {
		return err
	}
	*v = x
	return nil
}

type GenerateCode string

const (
	GenerateCodeOk                 GenerateCode = "ok"
	GenerateCodeExpertRequired     GenerateCode = "expert_required"
	GenerateCodeStatePending       GenerateCode = "state_pending"
	GenerateCodeOutcomeUnknown     GenerateCode = "outcome_unknown"
	GenerateCodeCancelled          GenerateCode = "cancelled"
	GenerateCodeServiceUnavailable GenerateCode = "service_unavailable"
	GenerateCodeGenerationFailed   GenerateCode = "generation_failed"
	GenerateCodeInputUnsupported   GenerateCode = "input_unsupported"
	GenerateCodeOperationConflict  GenerateCode = "operation_conflict"
	GenerateCodeContentRejected    GenerateCode = "content_rejected"
)

func (v GenerateCode) Validate() error {
	switch v {
	case GenerateCodeOk, GenerateCodeExpertRequired, GenerateCodeStatePending, GenerateCodeOutcomeUnknown, GenerateCodeCancelled, GenerateCodeServiceUnavailable, GenerateCodeGenerationFailed, GenerateCodeInputUnsupported, GenerateCodeOperationConflict, GenerateCodeContentRejected:
		return nil
	default:
		return errors.New("invalid broker enum")
	}
}
func (v *GenerateCode) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return errors.New("invalid broker enum")
	}
	x := GenerateCode(s)
	if err := x.Validate(); err != nil {
		return err
	}
	*v = x
	return nil
}

type NextAction string

const (
	NextActionCheckArtifact NextAction = "check_artifact"
	NextActionUseExpert     NextAction = "use_expert"
	NextActionReportFailure NextAction = "report_failure"
)

func (v NextAction) Validate() error {
	switch v {
	case NextActionCheckArtifact, NextActionUseExpert, NextActionReportFailure:
		return nil
	default:
		return errors.New("invalid broker enum")
	}
}
func (v *NextAction) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return errors.New("invalid broker enum")
	}
	x := NextAction(s)
	if err := x.Validate(); err != nil {
		return err
	}
	*v = x
	return nil
}

type GenerateResult struct {
	Status     GenerateStatus `json:"status"`
	Code       GenerateCode   `json:"code"`
	NextAction NextAction     `json:"next_action"`
	Source     string         `json:"source"`
	ArtifactId *string        `json:"artifact_id,omitempty"`
}

func (v GenerateResult) Validate() error {
	if err := v.Status.Validate(); err != nil {
		return err
	}
	if err := v.Code.Validate(); err != nil {
		return err
	}
	if err := v.NextAction.Validate(); err != nil {
		return err
	}
	if string(v.Source) != "builtin" {
		return errors.New("invalid broker constant")
	}
	if v.ArtifactId != nil {
		if utf8.RuneCountInString(string((*v.ArtifactId))) < 36 {
			return errors.New("invalid broker text")
		}
		if utf8.RuneCountInString(string((*v.ArtifactId))) > 36 {
			return errors.New("invalid broker text")
		}
	}
	ready := v.Status == GenerateStatusStaged || v.Status == GenerateStatusCommitted
	if ready != (v.Code == GenerateCodeOk) || ready != (v.ArtifactId != nil) || (ready && v.NextAction != NextActionCheckArtifact) {
		return errors.New("inconsistent image result")
	}
	return nil
}
func (v *GenerateResult) UnmarshalJSON(b []byte) error {
	type wire GenerateResult
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil || raw == nil {
		return errors.New("invalid broker object")
	}
	if _, ok := raw["status"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["status"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["code"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["code"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["next_action"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["next_action"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["source"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["source"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if x, ok := raw["artifact_id"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
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
	x := GenerateResult(w)
	if err := x.Validate(); err != nil {
		return err
	}
	*v = x
	return nil
}

const ToolName = "generate_image"
const ToolDescription = "易界内置图片生成 / Seedream image generation. For conversations with no selected expert, generate one image explicitly requested by the user, optionally using their single current-turn reference. A selected expert never falls back to this tool, including unavailable, failed, unsupported or unknown outcomes. Do not generate for image analysis. A blocked/unknown result does not authorize retry or a different provider. Images use the desktop Artifact viewer; staged is not yet committed. Discover this tool through native tool search; do not use an image CLI or ask for provider keys."
const PrivatePath = "/internal/media-generation/v1/generate-image"
const ToolApprovalMode = "approve"
const BindingWaitMilliseconds = 5000
const ToolTimeoutSeconds = 150

type ExpertImageRoute struct{ Route, ExpertID, Version, RoleSHA256, Service, Proxy, Method string }

var ExpertImageRoutes = [...]ExpertImageRoute{{"linkfox-mannequin-v1", "linkfox-expert-aigc-imagegen-mannequin-to-model", "0.1.0", "042a9acb59429f68adccd0a925fa9d83ea8ba1890b2a1b3f23ecd9049703b80c", "linkfox", "proxy_call", "aigc/imageGenAsync"}}
