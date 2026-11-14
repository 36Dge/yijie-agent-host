// Generated from market-submission-rejection-v1; DO NOT EDIT.
package submissionrejection

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"regexp"
)

var wirePattern0 = regexp.MustCompile("^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$")
var wirePattern1 = regexp.MustCompile("^[0-9a-f]{64}$")

type CanonicalId string

func (v CanonicalId) Validate() error {
	if !wirePattern0.MatchString(string(v)) {
		return errors.New("invalid broker pattern")
	}
	if string(v) == "00000000-0000-0000-0000-000000000000" {
		return errors.New("invalid broker identity")
	}
	return nil
}

type SelectionDigest string

func (v SelectionDigest) Validate() error {
	if !wirePattern1.MatchString(string(v)) {
		return errors.New("invalid broker pattern")
	}
	return nil
}

type Proof struct {
	SchemaVersion         int64           `json:"schemaVersion"`
	HostInstanceId        CanonicalId     `json:"hostInstanceId"`
	TaskId                CanonicalId     `json:"taskId"`
	LocalSessionId        CanonicalId     `json:"localSessionId"`
	SubmissionOperationId CanonicalId     `json:"submissionOperationId"`
	TurnOperationId       CanonicalId     `json:"turnOperationId"`
	SelectionDigest       SelectionDigest `json:"selectionDigest"`
	Reason                string          `json:"reason"`
	Outcome               string          `json:"outcome"`
}

func (v Proof) Validate() error {
	if v.SchemaVersion != 1 {
		return errors.New("invalid broker constant")
	}
	if err := v.HostInstanceId.Validate(); err != nil {
		return err
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
	switch string(v.Reason) {
	case "skill_maintenance_busy", "skill_unavailable":
	default:
		return errors.New("invalid broker enum")
	}
	switch string(v.Outcome) {
	case "not_dispatched":
	default:
		return errors.New("invalid broker enum")
	}
	return nil
}
func (v *Proof) UnmarshalJSON(b []byte) error {
	type wire Proof
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
	if _, ok := raw["hostInstanceId"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["hostInstanceId"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
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
	if _, ok := raw["reason"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["reason"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["outcome"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["outcome"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
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
	x := Proof(w)
	if err := x.Validate(); err != nil {
		return err
	}
	*v = x
	return nil
}

type Response struct {
	SchemaVersion int64       `json:"schemaVersion"`
	RequestId     CanonicalId `json:"requestId"`
	Data          Proof       `json:"data"`
}

func (v Response) Validate() error {
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
func (v *Response) UnmarshalJSON(b []byte) error {
	type wire Response
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
	d.DisallowUnknownFields()
	if err := d.Decode(&w); err != nil {
		return errors.New("invalid broker object")
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return errors.New("invalid broker object")
	}
	x := Response(w)
	if err := x.Validate(); err != nil {
		return err
	}
	*v = x
	return nil
}
