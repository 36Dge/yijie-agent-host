// Generated from video-generation/v1. DO NOT EDIT.
package videogeneration

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"regexp"
	"strings"
	"unicode/utf8"
)

var wirePattern0 = regexp.MustCompile("\\S")
var wirePattern1 = regexp.MustCompile("(^|\\s)--(resolution|rs|ratio|rt|ar|duration|dur|frames|fps|framespersecond|seed|camera_fixed|cf|watermark|wm|output_format|generate_audio|draft|return_last_frame|service_tier|execution_expires_after|priority|tools|callback_url)(\\s|=|$)")
var wirePattern2 = regexp.MustCompile("^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$")

type ImageMode string

const (
	ImageModeNone            ImageMode = "none"
	ImageModeFirstFrame      ImageMode = "first_frame"
	ImageModeFirstLastFrames ImageMode = "first_last_frames"
	ImageModeReferenceImages ImageMode = "reference_images"
)

func (v ImageMode) Validate() error {
	switch v {
	case ImageModeNone, ImageModeFirstFrame, ImageModeFirstLastFrames, ImageModeReferenceImages:
		return nil
	default:
		return errors.New("invalid broker enum")
	}
}
func (v *ImageMode) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return errors.New("invalid broker enum")
	}
	x := ImageMode(s)
	if err := x.Validate(); err != nil {
		return err
	}
	*v = x
	return nil
}

type VideoStatus string

const (
	VideoStatusPending   VideoStatus = "pending"
	VideoStatusStaged    VideoStatus = "staged"
	VideoStatusCommitted VideoStatus = "committed"
	VideoStatusBlocked   VideoStatus = "blocked"
	VideoStatusFailed    VideoStatus = "failed"
	VideoStatusUnknown   VideoStatus = "unknown"
)

func (v VideoStatus) Validate() error {
	switch v {
	case VideoStatusPending, VideoStatusStaged, VideoStatusCommitted, VideoStatusBlocked, VideoStatusFailed, VideoStatusUnknown:
		return nil
	default:
		return errors.New("invalid broker enum")
	}
}
func (v *VideoStatus) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return errors.New("invalid broker enum")
	}
	x := VideoStatus(s)
	if err := x.Validate(); err != nil {
		return err
	}
	*v = x
	return nil
}

type VideoCode string

const (
	VideoCodeOk                  VideoCode = "ok"
	VideoCodeProviderPending     VideoCode = "provider_pending"
	VideoCodeExistingOperation   VideoCode = "existing_operation"
	VideoCodeSelectionRequired   VideoCode = "selection_required"
	VideoCodeOperationNotFound   VideoCode = "operation_not_found"
	VideoCodeExpertRequired      VideoCode = "expert_required"
	VideoCodeInputUnsupported    VideoCode = "input_unsupported"
	VideoCodeOperationConflict   VideoCode = "operation_conflict"
	VideoCodeServiceUnavailable  VideoCode = "service_unavailable"
	VideoCodeOutcomeUnknown      VideoCode = "outcome_unknown"
	VideoCodeProviderFailed      VideoCode = "provider_failed"
	VideoCodeProviderExpired     VideoCode = "provider_expired"
	VideoCodeDeliveryRejected    VideoCode = "delivery_rejected"
	VideoCodeDeliveryUnconfirmed VideoCode = "delivery_unconfirmed"
	VideoCodeLocalCancelled      VideoCode = "local_cancelled"
)

func (v VideoCode) Validate() error {
	switch v {
	case VideoCodeOk, VideoCodeProviderPending, VideoCodeExistingOperation, VideoCodeSelectionRequired, VideoCodeOperationNotFound, VideoCodeExpertRequired, VideoCodeInputUnsupported, VideoCodeOperationConflict, VideoCodeServiceUnavailable, VideoCodeOutcomeUnknown, VideoCodeProviderFailed, VideoCodeProviderExpired, VideoCodeDeliveryRejected, VideoCodeDeliveryUnconfirmed, VideoCodeLocalCancelled:
		return nil
	default:
		return errors.New("invalid broker enum")
	}
}
func (v *VideoCode) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return errors.New("invalid broker enum")
	}
	x := VideoCode(s)
	if err := x.Validate(); err != nil {
		return err
	}
	*v = x
	return nil
}

type NextAction string

const (
	NextActionGetVideoResult  NextAction = "get_video_result"
	NextActionSelectOperation NextAction = "select_operation"
	NextActionCheckArtifact   NextAction = "check_artifact"
	NextActionReportStatus    NextAction = "report_status"
	NextActionUseExpert       NextAction = "use_expert"
)

func (v NextAction) Validate() error {
	switch v {
	case NextActionGetVideoResult, NextActionSelectOperation, NextActionCheckArtifact, NextActionReportStatus, NextActionUseExpert:
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

type ProviderStatus string

const (
	ProviderStatusQueued    ProviderStatus = "queued"
	ProviderStatusRunning   ProviderStatus = "running"
	ProviderStatusSucceeded ProviderStatus = "succeeded"
	ProviderStatusFailed    ProviderStatus = "failed"
	ProviderStatusCancelled ProviderStatus = "cancelled"
	ProviderStatusExpired   ProviderStatus = "expired"
)

func (v ProviderStatus) Validate() error {
	switch v {
	case ProviderStatusQueued, ProviderStatusRunning, ProviderStatusSucceeded, ProviderStatusFailed, ProviderStatusCancelled, ProviderStatusExpired:
		return nil
	default:
		return errors.New("invalid broker enum")
	}
}
func (v *ProviderStatus) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return errors.New("invalid broker enum")
	}
	x := ProviderStatus(s)
	if err := x.Validate(); err != nil {
		return err
	}
	*v = x
	return nil
}

type OperationPhase string

const (
	OperationPhaseSubmitting OperationPhase = "submitting"
	OperationPhasePending    OperationPhase = "pending"
	OperationPhaseSucceeded  OperationPhase = "succeeded"
	OperationPhaseHandedOff  OperationPhase = "handed_off"
	OperationPhaseFailed     OperationPhase = "failed"
	OperationPhaseUnknown    OperationPhase = "unknown"
)

func (v OperationPhase) Validate() error {
	switch v {
	case OperationPhaseSubmitting, OperationPhasePending, OperationPhaseSucceeded, OperationPhaseHandedOff, OperationPhaseFailed, OperationPhaseUnknown:
		return nil
	default:
		return errors.New("invalid broker enum")
	}
}
func (v *OperationPhase) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return errors.New("invalid broker enum")
	}
	x := OperationPhase(s)
	if err := x.Validate(); err != nil {
		return err
	}
	*v = x
	return nil
}

type GenerateInput struct {
	Prompt       string    `json:"prompt"`
	ImageMode    ImageMode `json:"image_mode"`
	ImageIndices []int64   `json:"image_indices"`
}

func (v GenerateInput) Validate() error {
	if utf8.RuneCountInString(string(v.Prompt)) < 1 {
		return errors.New("invalid broker text")
	}
	if utf8.RuneCountInString(string(v.Prompt)) > 4000 {
		return errors.New("invalid broker text")
	}
	if !wirePattern0.MatchString(string(v.Prompt)) {
		return errors.New("invalid broker pattern")
	}
	if err := v.ImageMode.Validate(); err != nil {
		return err
	}
	if v.ImageIndices == nil {
		return errors.New("null broker collection")
	}
	if len(v.ImageIndices) > 10 {
		return errors.New("invalid broker collection")
	}
	for _, item := range v.ImageIndices {
		if item < 1 {
			return errors.New("invalid broker number")
		}
		if item > 10 {
			return errors.New("invalid broker number")
		}
	}
	if !utf8.ValidString(v.Prompt) || strings.TrimSpace(v.Prompt) == "" || strings.ContainsRune(v.Prompt, 0) || forbiddenPromptParameter(v.Prompt) {
		return errors.New("invalid video prompt")
	}
	switch v.ImageMode {
	case ImageModeNone:
		if len(v.ImageIndices) != 0 {
			return errors.New("unexpected images")
		}
	case ImageModeFirstFrame:
		if len(v.ImageIndices) != 1 {
			return errors.New("one first frame required")
		}
	case ImageModeFirstLastFrames:
		if len(v.ImageIndices) != 2 {
			return errors.New("two frame roles required")
		}
	case ImageModeReferenceImages:
		if len(v.ImageIndices) == 0 {
			return errors.New("reference images required")
		}
		for i, n := range v.ImageIndices {
			for _, prior := range v.ImageIndices[:i] {
				if n == prior {
					return errors.New("duplicate reference index")
				}
			}
		}
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
	if _, ok := raw["image_mode"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["image_mode"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["image_indices"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["image_indices"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
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

type GetInput struct {
	VideoOperationId *string `json:"video_operation_id,omitempty"`
}

func (v GetInput) Validate() error {
	if v.VideoOperationId != nil {
		if utf8.RuneCountInString(string((*v.VideoOperationId))) < 36 {
			return errors.New("invalid broker text")
		}
		if utf8.RuneCountInString(string((*v.VideoOperationId))) > 36 {
			return errors.New("invalid broker text")
		}
		if !wirePattern2.MatchString(string((*v.VideoOperationId))) {
			return errors.New("invalid broker pattern")
		}
		if string((*v.VideoOperationId)) == "00000000-0000-0000-0000-000000000000" {
			return errors.New("invalid broker identity")
		}
	}
	return nil
}
func (v *GetInput) UnmarshalJSON(b []byte) error {
	type wire GetInput
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil || raw == nil {
		return errors.New("invalid broker object")
	}
	if x, ok := raw["video_operation_id"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
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
	x := GetInput(w)
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
	if !wirePattern2.MatchString(string(v.ThreadId)) {
		return errors.New("invalid broker pattern")
	}
	if string(v.ThreadId) == "00000000-0000-0000-0000-000000000000" {
		return errors.New("invalid broker identity")
	}
	if utf8.RuneCountInString(string(v.TurnId)) < 36 {
		return errors.New("invalid broker text")
	}
	if utf8.RuneCountInString(string(v.TurnId)) > 36 {
		return errors.New("invalid broker text")
	}
	if !wirePattern2.MatchString(string(v.TurnId)) {
		return errors.New("invalid broker pattern")
	}
	if string(v.TurnId) == "00000000-0000-0000-0000-000000000000" {
		return errors.New("invalid broker identity")
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

type GetRequest struct {
	ThreadId string   `json:"thread_id"`
	TurnId   string   `json:"turn_id"`
	Input    GetInput `json:"input"`
}

func (v GetRequest) Validate() error {
	if utf8.RuneCountInString(string(v.ThreadId)) < 36 {
		return errors.New("invalid broker text")
	}
	if utf8.RuneCountInString(string(v.ThreadId)) > 36 {
		return errors.New("invalid broker text")
	}
	if !wirePattern2.MatchString(string(v.ThreadId)) {
		return errors.New("invalid broker pattern")
	}
	if string(v.ThreadId) == "00000000-0000-0000-0000-000000000000" {
		return errors.New("invalid broker identity")
	}
	if utf8.RuneCountInString(string(v.TurnId)) < 36 {
		return errors.New("invalid broker text")
	}
	if utf8.RuneCountInString(string(v.TurnId)) > 36 {
		return errors.New("invalid broker text")
	}
	if !wirePattern2.MatchString(string(v.TurnId)) {
		return errors.New("invalid broker pattern")
	}
	if string(v.TurnId) == "00000000-0000-0000-0000-000000000000" {
		return errors.New("invalid broker identity")
	}
	if err := v.Input.Validate(); err != nil {
		return err
	}
	return nil
}
func (v *GetRequest) UnmarshalJSON(b []byte) error {
	type wire GetRequest
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
	x := GetRequest(w)
	if err := x.Validate(); err != nil {
		return err
	}
	*v = x
	return nil
}

type OperationCandidate struct {
	VideoOperationId string         `json:"video_operation_id"`
	CreatedAt        int64          `json:"created_at"`
	ImageMode        ImageMode      `json:"image_mode"`
	Phase            OperationPhase `json:"phase"`
}

func (v OperationCandidate) Validate() error {
	if utf8.RuneCountInString(string(v.VideoOperationId)) < 36 {
		return errors.New("invalid broker text")
	}
	if utf8.RuneCountInString(string(v.VideoOperationId)) > 36 {
		return errors.New("invalid broker text")
	}
	if !wirePattern2.MatchString(string(v.VideoOperationId)) {
		return errors.New("invalid broker pattern")
	}
	if string(v.VideoOperationId) == "00000000-0000-0000-0000-000000000000" {
		return errors.New("invalid broker identity")
	}
	if v.CreatedAt < 0 {
		return errors.New("invalid broker number")
	}
	if err := v.ImageMode.Validate(); err != nil {
		return err
	}
	if err := v.Phase.Validate(); err != nil {
		return err
	}
	return nil
}
func (v *OperationCandidate) UnmarshalJSON(b []byte) error {
	type wire OperationCandidate
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil || raw == nil {
		return errors.New("invalid broker object")
	}
	if _, ok := raw["video_operation_id"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["video_operation_id"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["created_at"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["created_at"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["image_mode"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["image_mode"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["phase"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["phase"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
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
	x := OperationCandidate(w)
	if err := x.Validate(); err != nil {
		return err
	}
	*v = x
	return nil
}

type VideoResult struct {
	Status             VideoStatus           `json:"status"`
	Code               VideoCode             `json:"code"`
	NextAction         NextAction            `json:"next_action"`
	Source             string                `json:"source"`
	VideoOperationId   *string               `json:"video_operation_id,omitempty"`
	ProviderStatus     *ProviderStatus       `json:"provider_status,omitempty"`
	ProviderObservedAt *int64                `json:"provider_observed_at,omitempty"`
	ArtifactId         *string               `json:"artifact_id,omitempty"`
	DeliveryTurnId     *string               `json:"delivery_turn_id,omitempty"`
	Candidates         *[]OperationCandidate `json:"candidates,omitempty"`
	CandidatesComplete *bool                 `json:"candidates_complete,omitempty"`
}

func (v VideoResult) Validate() error {
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
	if v.VideoOperationId != nil {
		if utf8.RuneCountInString(string((*v.VideoOperationId))) < 36 {
			return errors.New("invalid broker text")
		}
		if utf8.RuneCountInString(string((*v.VideoOperationId))) > 36 {
			return errors.New("invalid broker text")
		}
		if !wirePattern2.MatchString(string((*v.VideoOperationId))) {
			return errors.New("invalid broker pattern")
		}
		if string((*v.VideoOperationId)) == "00000000-0000-0000-0000-000000000000" {
			return errors.New("invalid broker identity")
		}
	}
	if v.ProviderStatus != nil {
		if err := (*v.ProviderStatus).Validate(); err != nil {
			return err
		}
	}
	if v.ProviderObservedAt != nil {
		if (*v.ProviderObservedAt) < 0 {
			return errors.New("invalid broker number")
		}
	}
	if v.ArtifactId != nil {
		if utf8.RuneCountInString(string((*v.ArtifactId))) < 36 {
			return errors.New("invalid broker text")
		}
		if utf8.RuneCountInString(string((*v.ArtifactId))) > 36 {
			return errors.New("invalid broker text")
		}
		if !wirePattern2.MatchString(string((*v.ArtifactId))) {
			return errors.New("invalid broker pattern")
		}
		if string((*v.ArtifactId)) == "00000000-0000-0000-0000-000000000000" {
			return errors.New("invalid broker identity")
		}
	}
	if v.DeliveryTurnId != nil {
		if utf8.RuneCountInString(string((*v.DeliveryTurnId))) < 36 {
			return errors.New("invalid broker text")
		}
		if utf8.RuneCountInString(string((*v.DeliveryTurnId))) > 36 {
			return errors.New("invalid broker text")
		}
		if !wirePattern2.MatchString(string((*v.DeliveryTurnId))) {
			return errors.New("invalid broker pattern")
		}
		if string((*v.DeliveryTurnId)) == "00000000-0000-0000-0000-000000000000" {
			return errors.New("invalid broker identity")
		}
	}
	if v.Candidates != nil {
		if (*v.Candidates) == nil {
			return errors.New("null broker collection")
		}
		if len((*v.Candidates)) < 1 {
			return errors.New("invalid broker collection")
		}
		if len((*v.Candidates)) > 5 {
			return errors.New("invalid broker collection")
		}
		for _, item := range *v.Candidates {
			if err := item.Validate(); err != nil {
				return err
			}
		}
	}
	if v.CandidatesComplete != nil {
	}
	ready := v.Status == VideoStatusStaged || v.Status == VideoStatusCommitted
	if ready != (v.Code == VideoCodeOk) || (v.ArtifactId != nil) != (v.DeliveryTurnId != nil) || (v.ProviderStatus != nil) != (v.ProviderObservedAt != nil) || (v.Candidates != nil) != (v.CandidatesComplete != nil) {
		return errors.New("inconsistent video receipt")
	}
	if v.ArtifactId != nil && (v.VideoOperationId == nil || (v.Code != VideoCodeOk && v.Code != VideoCodeDeliveryUnconfirmed)) {
		return errors.New("invalid artifact reference")
	}
	if ready && (v.ArtifactId == nil || (v.NextAction != NextActionCheckArtifact && v.NextAction != NextActionReportStatus)) {
		return errors.New("invalid delivered receipt")
	}
	if v.Status == VideoStatusStaged && v.NextAction != NextActionCheckArtifact {
		return errors.New("staged receipt needs ACK check")
	}
	if v.Code == VideoCodeDeliveryUnconfirmed && (v.ArtifactId == nil || v.Status != VideoStatusUnknown || v.NextAction != NextActionReportStatus) {
		return errors.New("invalid unconfirmed delivery")
	}
	if v.Code == VideoCodeSelectionRequired {
		if v.Candidates == nil || v.Status != VideoStatusBlocked || v.NextAction != NextActionSelectOperation || v.VideoOperationId != nil {
			return errors.New("invalid selection receipt")
		}
	} else if v.Candidates != nil {
		return errors.New("unexpected candidates")
	}
	if (v.NextAction == NextActionGetVideoResult || v.NextAction == NextActionCheckArtifact) && v.VideoOperationId == nil {
		return errors.New("missing original operation")
	}
	if v.Code == VideoCodeExpertRequired && (v.Status != VideoStatusBlocked || v.NextAction != NextActionUseExpert) {
		return errors.New("invalid expert receipt")
	}
	return nil
}
func (v *VideoResult) UnmarshalJSON(b []byte) error {
	type wire VideoResult
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
	if x, ok := raw["video_operation_id"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if x, ok := raw["provider_status"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if x, ok := raw["provider_observed_at"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if x, ok := raw["artifact_id"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if x, ok := raw["delivery_turn_id"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if x, ok := raw["candidates"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if x, ok := raw["candidates_complete"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
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
	x := VideoResult(w)
	if err := x.Validate(); err != nil {
		return err
	}
	*v = x
	return nil
}

const GenerateToolName = "generate_video"
const GetToolName = "get_video_result"
const GenerateDescription = "易界内置视频生成 / Seedance video generation. Use for an explicit new video request with no selected expert. One creation per root; an unresolved video in this conversation blocks another creation. Fixed model and provider defaults; never add --parameter suffixes. Only text-to-video and current-turn images are supported. Video/audio reference, editing or extending an existing clip are unavailable. Do not promise exact duration, resolution, aspect ratio or audio settings. A revision requires an explicitly requested new generation, not an in-place update. image_indices are 1-based current-turn images; first_last_frames allows the same image twice. Do not use for image/video analysis or script writing. A selected expert never falls back here. After acceptance, await get_video_result for the original operation using native Code Mode exec/wait; do not repeat creation, use a CLI, or ask for provider keys."
const GetDescription = "查询并取回易界内置视频 / get the original Seedance video result. Never creates a provider task. Omit video_operation_id to resolve this root or the unique unresolved operation in the same conversation; ambiguity returns local candidates only. pending means continue the SAME operation, not failure. Stop cancels local waiting; the remote task may continue. After Artifact handoff, only report the existing Artifact state; never re-register with publish_artifact or download through a CLI. A selected expert blocks builtin recovery. staged is not Native commit; check the existing get_artifact_status."
const GeneratePath = "/internal/media-generation/v1/generate-video"
const GetPath = "/internal/media-generation/v1/get-video-result"
const MaxRequestBytes = 16384
const MaxResponseBytes = 16384
const BindingWaitMilliseconds = 5000
const GenerateTimeoutSeconds = 30
const GetTimeoutSeconds = 120
const ToolTimeoutSeconds = 150
const ToolApprovalMode = "approve"
const Model = "doubao-seedance-2-5-260628"
const Endpoint = "https://ark.cn-beijing.volces.com/api/v3/contents/generations/tasks"
const PollIntervalSeconds = 10
const QueryTimeoutSeconds = 15
const DownloadTimeoutSeconds = 60
const MaxVideoBytes = 67108864
const MaxDownloads = 2

var AllowedVideoHosts = [...]string{"ark-content-generation-cn-beijing.tos-cn-beijing.volces.com", "ark-acg-cn-beijing.tos-cn-beijing.volces.com"}

func forbiddenPromptParameter(value string) bool {
	for _, word := range strings.Fields(value) {
		if strings.HasPrefix(word, "--") {
			name := strings.SplitN(word[2:], "=", 2)[0]
			switch name {
			case "resolution", "rs", "ratio", "rt", "ar", "duration", "dur", "frames", "fps", "framespersecond", "seed", "camera_fixed", "cf", "watermark", "wm", "output_format", "generate_audio", "draft", "return_last_frame", "service_tier", "execution_expires_after", "priority", "tools", "callback_url":
				return true
			}
		}
	}
	return false
}
func (v VideoResult) IsError() bool {
	switch v.Code {
	case VideoCodeOk, VideoCodeProviderPending, VideoCodeExistingOperation, VideoCodeSelectionRequired:
		return false
	default:
		return true
	}
}
