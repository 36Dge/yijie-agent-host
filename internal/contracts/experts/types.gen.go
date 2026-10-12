// Generated from FEAT-158 experts source and referenced authorities; DO NOT EDIT.
package experts

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	broker "github.com/36Dge/yijie-agent-host/internal/contracts/marketbrokercontrol"
	host "github.com/36Dge/yijie-agent-host/internal/contracts/markethost"
	selection "github.com/36Dge/yijie-agent-host/internal/contracts/marketselection"
	"io"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"
)

var wirePattern0 = regexp.MustCompile("^[a-z0-9]+(?:-[a-z0-9]+)*$")
var wirePattern1 = regexp.MustCompile("^[a-z0-9]+(?:[.-][a-z0-9]+)*$")
var wirePattern2 = regexp.MustCompile("^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$")
var wirePattern3 = regexp.MustCompile("^(0|[1-9][0-9]*)\\.(0|[1-9][0-9]*)\\.(0|[1-9][0-9]*)(?:-[0-9A-Za-z.-]+)?(?:\\+[0-9A-Za-z.-]+)?$")
var wirePattern4 = regexp.MustCompile("^[a-f0-9]{64}$")

type ApplyRequest struct {
	SchemaVersion int64        `json:"schemaVersion"`
	RequestId     CanonicalId  `json:"requestId"`
	Method        string       `json:"method"`
	Payload       ApplyPayload `json:"payload"`
}

func (v ApplyRequest) Validate() error {
	if v.SchemaVersion != 1 {
		return errors.New("invalid broker constant")
	}
	if err := v.RequestId.Validate(); err != nil {
		return err
	}
	if string(v.Method) != "expert_apply_binding" {
		return errors.New("invalid broker constant")
	}
	if err := v.Payload.Validate(); err != nil {
		return err
	}
	return nil
}
func (v *ApplyRequest) UnmarshalJSON(b []byte) error {
	type wire ApplyRequest
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
	x := ApplyRequest(w)
	if err := x.Validate(); err != nil {
		return err
	}
	*v = x
	return nil
}

type ApplyResponse struct {
	SchemaVersion int64        `json:"schemaVersion"`
	RequestId     CanonicalId  `json:"requestId"`
	Data          ApplyReceipt `json:"data"`
}

func (v ApplyResponse) Validate() error {
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
func (v *ApplyResponse) UnmarshalJSON(b []byte) error {
	type wire ApplyResponse
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
	x := ApplyResponse(w)
	if err := x.Validate(); err != nil {
		return err
	}
	*v = x
	return nil
}

type PrivateOperationReadRequest struct {
	SchemaVersion int64                       `json:"schemaVersion"`
	RequestId     CanonicalId                 `json:"requestId"`
	Method        string                      `json:"method"`
	Payload       PrivateOperationReadPayload `json:"payload"`
}

func (v PrivateOperationReadRequest) Validate() error {
	if v.SchemaVersion != 1 {
		return errors.New("invalid broker constant")
	}
	if err := v.RequestId.Validate(); err != nil {
		return err
	}
	if string(v.Method) != "expert_operation_read" {
		return errors.New("invalid broker constant")
	}
	if err := v.Payload.Validate(); err != nil {
		return err
	}
	return nil
}
func (v *PrivateOperationReadRequest) UnmarshalJSON(b []byte) error {
	type wire PrivateOperationReadRequest
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
	x := PrivateOperationReadRequest(w)
	if err := x.Validate(); err != nil {
		return err
	}
	*v = x
	return nil
}

type PrivateOperationReadResponse struct {
	SchemaVersion int64        `json:"schemaVersion"`
	RequestId     CanonicalId  `json:"requestId"`
	Data          ApplyReceipt `json:"data"`
}

func (v PrivateOperationReadResponse) Validate() error {
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
func (v *PrivateOperationReadResponse) UnmarshalJSON(b []byte) error {
	type wire PrivateOperationReadResponse
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
	x := PrivateOperationReadResponse(w)
	if err := x.Validate(); err != nil {
		return err
	}
	*v = x
	return nil
}

type PrivateGrantRequest struct {
	SchemaVersion int64               `json:"schemaVersion"`
	RequestId     CanonicalId         `json:"requestId"`
	Method        string              `json:"method"`
	Payload       PrivateGrantPayload `json:"payload"`
}

func (v PrivateGrantRequest) Validate() error {
	if v.SchemaVersion != 1 {
		return errors.New("invalid broker constant")
	}
	if err := v.RequestId.Validate(); err != nil {
		return err
	}
	if string(v.Method) != "expert_grant_register" {
		return errors.New("invalid broker constant")
	}
	if err := v.Payload.Validate(); err != nil {
		return err
	}
	return nil
}
func (v *PrivateGrantRequest) UnmarshalJSON(b []byte) error {
	type wire PrivateGrantRequest
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
	x := PrivateGrantRequest(w)
	if err := x.Validate(); err != nil {
		return err
	}
	*v = x
	return nil
}

type PrivateGrantResponse struct {
	SchemaVersion int64        `json:"schemaVersion"`
	RequestId     CanonicalId  `json:"requestId"`
	Data          PrivateGrant `json:"data"`
}

func (v PrivateGrantResponse) Validate() error {
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
func (v *PrivateGrantResponse) UnmarshalJSON(b []byte) error {
	type wire PrivateGrantResponse
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
	x := PrivateGrantResponse(w)
	if err := x.Validate(); err != nil {
		return err
	}
	*v = x
	return nil
}

type PrivateSubmitRequest struct {
	SchemaVersion int64               `json:"schemaVersion"`
	RequestId     CanonicalId         `json:"requestId"`
	Method        string              `json:"method"`
	Payload       MarketSubmitTrigger `json:"payload"`
}

func (v PrivateSubmitRequest) Validate() error {
	if v.SchemaVersion != 1 {
		return errors.New("invalid broker constant")
	}
	if err := v.RequestId.Validate(); err != nil {
		return err
	}
	if string(v.Method) != "expert_submit" {
		return errors.New("invalid broker constant")
	}
	if err := v.Payload.Validate(); err != nil {
		return err
	}
	return nil
}
func (v *PrivateSubmitRequest) UnmarshalJSON(b []byte) error {
	type wire PrivateSubmitRequest
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
	x := PrivateSubmitRequest(w)
	if err := x.Validate(); err != nil {
		return err
	}
	*v = x
	return nil
}

type PrivateSubmitResponse struct {
	SchemaVersion int64          `json:"schemaVersion"`
	RequestId     CanonicalId    `json:"requestId"`
	Data          PrivateReceipt `json:"data"`
}

func (v PrivateSubmitResponse) Validate() error {
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
func (v *PrivateSubmitResponse) UnmarshalJSON(b []byte) error {
	type wire PrivateSubmitResponse
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
	x := PrivateSubmitResponse(w)
	if err := x.Validate(); err != nil {
		return err
	}
	*v = x
	return nil
}

type SubmissionReadRequest struct {
	SchemaVersion int64                 `json:"schemaVersion"`
	RequestId     CanonicalId           `json:"requestId"`
	Method        string                `json:"method"`
	Payload       SubmissionReadPayload `json:"payload"`
}

func (v SubmissionReadRequest) Validate() error {
	if v.SchemaVersion != 1 {
		return errors.New("invalid broker constant")
	}
	if err := v.RequestId.Validate(); err != nil {
		return err
	}
	if string(v.Method) != "expert_submission_read" {
		return errors.New("invalid broker constant")
	}
	if err := v.Payload.Validate(); err != nil {
		return err
	}
	return nil
}
func (v *SubmissionReadRequest) UnmarshalJSON(b []byte) error {
	type wire SubmissionReadRequest
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
	x := SubmissionReadRequest(w)
	if err := x.Validate(); err != nil {
		return err
	}
	*v = x
	return nil
}

type SubmissionReadResponse struct {
	SchemaVersion int64          `json:"schemaVersion"`
	RequestId     CanonicalId    `json:"requestId"`
	Data          PrivateReceipt `json:"data"`
}

func (v SubmissionReadResponse) Validate() error {
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
func (v *SubmissionReadResponse) UnmarshalJSON(b []byte) error {
	type wire SubmissionReadResponse
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
	x := SubmissionReadResponse(w)
	if err := x.Validate(); err != nil {
		return err
	}
	*v = x
	return nil
}

type PrivateStatusRequest struct {
	SchemaVersion int64                `json:"schemaVersion"`
	RequestId     CanonicalId          `json:"requestId"`
	Method        string               `json:"method"`
	Payload       PrivateStatusPayload `json:"payload"`
}

func (v PrivateStatusRequest) Validate() error {
	if v.SchemaVersion != 1 {
		return errors.New("invalid broker constant")
	}
	if err := v.RequestId.Validate(); err != nil {
		return err
	}
	if string(v.Method) != "expert_status" {
		return errors.New("invalid broker constant")
	}
	if err := v.Payload.Validate(); err != nil {
		return err
	}
	return nil
}
func (v *PrivateStatusRequest) UnmarshalJSON(b []byte) error {
	type wire PrivateStatusRequest
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
	x := PrivateStatusRequest(w)
	if err := x.Validate(); err != nil {
		return err
	}
	*v = x
	return nil
}

type PrivateStatusResponse struct {
	SchemaVersion int64         `json:"schemaVersion"`
	RequestId     CanonicalId   `json:"requestId"`
	Data          PrivateStatus `json:"data"`
}

func (v PrivateStatusResponse) Validate() error {
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
func (v *PrivateStatusResponse) UnmarshalJSON(b []byte) error {
	type wire PrivateStatusResponse
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
	x := PrivateStatusResponse(w)
	if err := x.Validate(); err != nil {
		return err
	}
	*v = x
	return nil
}

type PrivateResourcePrepareRequest struct {
	SchemaVersion int64                  `json:"schemaVersion"`
	RequestId     CanonicalId            `json:"requestId"`
	Method        string                 `json:"method"`
	Payload       PrivateResourcePayload `json:"payload"`
}

func (v PrivateResourcePrepareRequest) Validate() error {
	if v.SchemaVersion != 1 {
		return errors.New("invalid broker constant")
	}
	if err := v.RequestId.Validate(); err != nil {
		return err
	}
	if string(v.Method) != "expert_resources_prepare" {
		return errors.New("invalid broker constant")
	}
	if err := v.Payload.Validate(); err != nil {
		return err
	}
	return nil
}
func (v *PrivateResourcePrepareRequest) UnmarshalJSON(b []byte) error {
	type wire PrivateResourcePrepareRequest
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
	x := PrivateResourcePrepareRequest(w)
	if err := x.Validate(); err != nil {
		return err
	}
	*v = x
	return nil
}

type PrivateResourcePrepareResponse struct {
	SchemaVersion int64             `json:"schemaVersion"`
	RequestId     CanonicalId       `json:"requestId"`
	Data          ResourceOperation `json:"data"`
}

func (v PrivateResourcePrepareResponse) Validate() error {
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
func (v *PrivateResourcePrepareResponse) UnmarshalJSON(b []byte) error {
	type wire PrivateResourcePrepareResponse
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
	x := PrivateResourcePrepareResponse(w)
	if err := x.Validate(); err != nil {
		return err
	}
	*v = x
	return nil
}

type PrivateResourceRemoveRequest struct {
	SchemaVersion int64                  `json:"schemaVersion"`
	RequestId     CanonicalId            `json:"requestId"`
	Method        string                 `json:"method"`
	Payload       PrivateResourcePayload `json:"payload"`
}

func (v PrivateResourceRemoveRequest) Validate() error {
	if v.SchemaVersion != 1 {
		return errors.New("invalid broker constant")
	}
	if err := v.RequestId.Validate(); err != nil {
		return err
	}
	if string(v.Method) != "expert_resources_remove" {
		return errors.New("invalid broker constant")
	}
	if err := v.Payload.Validate(); err != nil {
		return err
	}
	return nil
}
func (v *PrivateResourceRemoveRequest) UnmarshalJSON(b []byte) error {
	type wire PrivateResourceRemoveRequest
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
	x := PrivateResourceRemoveRequest(w)
	if err := x.Validate(); err != nil {
		return err
	}
	*v = x
	return nil
}

type PrivateResourceRemoveResponse struct {
	SchemaVersion int64             `json:"schemaVersion"`
	RequestId     CanonicalId       `json:"requestId"`
	Data          ResourceOperation `json:"data"`
}

func (v PrivateResourceRemoveResponse) Validate() error {
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
func (v *PrivateResourceRemoveResponse) UnmarshalJSON(b []byte) error {
	type wire PrivateResourceRemoveResponse
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
	x := PrivateResourceRemoveResponse(w)
	if err := x.Validate(); err != nil {
		return err
	}
	*v = x
	return nil
}

type PrivateResourceReadRequest struct {
	SchemaVersion int64                      `json:"schemaVersion"`
	RequestId     CanonicalId                `json:"requestId"`
	Method        string                     `json:"method"`
	Payload       PrivateResourceReadPayload `json:"payload"`
}

func (v PrivateResourceReadRequest) Validate() error {
	if v.SchemaVersion != 1 {
		return errors.New("invalid broker constant")
	}
	if err := v.RequestId.Validate(); err != nil {
		return err
	}
	if string(v.Method) != "expert_resources_operation_read" {
		return errors.New("invalid broker constant")
	}
	if err := v.Payload.Validate(); err != nil {
		return err
	}
	return nil
}
func (v *PrivateResourceReadRequest) UnmarshalJSON(b []byte) error {
	type wire PrivateResourceReadRequest
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
	x := PrivateResourceReadRequest(w)
	if err := x.Validate(); err != nil {
		return err
	}
	*v = x
	return nil
}

type PrivateResourceReadResponse struct {
	SchemaVersion int64             `json:"schemaVersion"`
	RequestId     CanonicalId       `json:"requestId"`
	Data          ResourceOperation `json:"data"`
}

func (v PrivateResourceReadResponse) Validate() error {
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
func (v *PrivateResourceReadResponse) UnmarshalJSON(b []byte) error {
	type wire PrivateResourceReadResponse
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
	x := PrivateResourceReadResponse(w)
	if err := x.Validate(); err != nil {
		return err
	}
	*v = x
	return nil
}

type PrivateBindingRecoverRequest struct {
	SchemaVersion int64                        `json:"schemaVersion"`
	RequestId     CanonicalId                  `json:"requestId"`
	Method        string                       `json:"method"`
	Payload       PrivateBindingRecoverPayload `json:"payload"`
}

func (v PrivateBindingRecoverRequest) Validate() error {
	if v.SchemaVersion != 1 {
		return errors.New("invalid broker constant")
	}
	if err := v.RequestId.Validate(); err != nil {
		return err
	}
	if string(v.Method) != "expert_binding_recover" {
		return errors.New("invalid broker constant")
	}
	if err := v.Payload.Validate(); err != nil {
		return err
	}
	return nil
}
func (v *PrivateBindingRecoverRequest) UnmarshalJSON(b []byte) error {
	type wire PrivateBindingRecoverRequest
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
	x := PrivateBindingRecoverRequest(w)
	if err := x.Validate(); err != nil {
		return err
	}
	*v = x
	return nil
}

type PrivateBindingRecoverResponse struct {
	SchemaVersion int64        `json:"schemaVersion"`
	RequestId     CanonicalId  `json:"requestId"`
	Data          ApplyReceipt `json:"data"`
}

func (v PrivateBindingRecoverResponse) Validate() error {
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
func (v *PrivateBindingRecoverResponse) UnmarshalJSON(b []byte) error {
	type wire PrivateBindingRecoverResponse
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
	x := PrivateBindingRecoverResponse(w)
	if err := x.Validate(); err != nil {
		return err
	}
	*v = x
	return nil
}

type PrivateResourceRecoverRequest struct {
	SchemaVersion int64                         `json:"schemaVersion"`
	RequestId     CanonicalId                   `json:"requestId"`
	Method        string                        `json:"method"`
	Payload       PrivateResourceRecoverPayload `json:"payload"`
}

func (v PrivateResourceRecoverRequest) Validate() error {
	if v.SchemaVersion != 1 {
		return errors.New("invalid broker constant")
	}
	if err := v.RequestId.Validate(); err != nil {
		return err
	}
	if string(v.Method) != "expert_resources_recover" {
		return errors.New("invalid broker constant")
	}
	if err := v.Payload.Validate(); err != nil {
		return err
	}
	return nil
}
func (v *PrivateResourceRecoverRequest) UnmarshalJSON(b []byte) error {
	type wire PrivateResourceRecoverRequest
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
	x := PrivateResourceRecoverRequest(w)
	if err := x.Validate(); err != nil {
		return err
	}
	*v = x
	return nil
}

type PrivateResourceRecoverResponse struct {
	SchemaVersion int64             `json:"schemaVersion"`
	RequestId     CanonicalId       `json:"requestId"`
	Data          ResourceOperation `json:"data"`
}

func (v PrivateResourceRecoverResponse) Validate() error {
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
func (v *PrivateResourceRecoverResponse) UnmarshalJSON(b []byte) error {
	type wire PrivateResourceRecoverResponse
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
	x := PrivateResourceRecoverResponse(w)
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
	if v.Retryable && v.Code != ErrorCodeTemporarilyUnavailable {
		return errors.New("expert error is not retryable")
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
	d.DisallowUnknownFields()
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

type ExpertMetadata struct {
	ExpertId              string                `json:"expert_id"`
	SourceId              string                `json:"source_id"`
	Platform              string                `json:"platform"`
	Subtitle              string                `json:"subtitle"`
	Tags                  []string              `json:"tags"`
	QuickPrompts          []string              `json:"quick_prompts"`
	KnowledgeSummary      string                `json:"knowledge_summary"`
	RoleResource          RoleResource          `json:"role_resource"`
	KnowledgeResources    []KnowledgeResource   `json:"knowledge_resources"`
	RuntimeProjection     string                `json:"runtime_projection"`
	LocalUseAuthorization LocalUseAuthorization `json:"local_use_authorization"`
}

func (v ExpertMetadata) Validate() error {
	if utf8.RuneCountInString(string(v.ExpertId)) < 1 {
		return errors.New("invalid broker text")
	}
	if utf8.RuneCountInString(string(v.ExpertId)) > 64 {
		return errors.New("invalid broker text")
	}
	if !wirePattern0.MatchString(string(v.ExpertId)) {
		return errors.New("invalid broker pattern")
	}
	if utf8.RuneCountInString(string(v.SourceId)) < 1 {
		return errors.New("invalid broker text")
	}
	if utf8.RuneCountInString(string(v.SourceId)) > 128 {
		return errors.New("invalid broker text")
	}
	if !wirePattern1.MatchString(string(v.SourceId)) {
		return errors.New("invalid broker pattern")
	}
	if utf8.RuneCountInString(string(v.Platform)) < 1 {
		return errors.New("invalid broker text")
	}
	if utf8.RuneCountInString(string(v.Platform)) > 64 {
		return errors.New("invalid broker text")
	}
	if utf8.RuneCountInString(string(v.Subtitle)) < 1 {
		return errors.New("invalid broker text")
	}
	if utf8.RuneCountInString(string(v.Subtitle)) > 80 {
		return errors.New("invalid broker text")
	}
	if v.Tags == nil {
		return errors.New("null broker collection")
	}
	if len(v.Tags) > 8 {
		return errors.New("invalid broker collection")
	}
	for _, item := range v.Tags {
		if utf8.RuneCountInString(string(item)) < 1 {
			return errors.New("invalid broker text")
		}
		if utf8.RuneCountInString(string(item)) > 40 {
			return errors.New("invalid broker text")
		}
	}
	for i, item := range v.Tags {
		for _, prior := range v.Tags[:i] {
			if item == prior {
				return errors.New("duplicate broker collection item")
			}
		}
	}
	if v.QuickPrompts == nil {
		return errors.New("null broker collection")
	}
	if len(v.QuickPrompts) > 3 {
		return errors.New("invalid broker collection")
	}
	for _, item := range v.QuickPrompts {
		if utf8.RuneCountInString(string(item)) < 1 {
			return errors.New("invalid broker text")
		}
		if utf8.RuneCountInString(string(item)) > 500 {
			return errors.New("invalid broker text")
		}
	}
	for i, item := range v.QuickPrompts {
		for _, prior := range v.QuickPrompts[:i] {
			if item == prior {
				return errors.New("duplicate broker collection item")
			}
		}
	}
	if utf8.RuneCountInString(string(v.KnowledgeSummary)) < 1 {
		return errors.New("invalid broker text")
	}
	if utf8.RuneCountInString(string(v.KnowledgeSummary)) > 300 {
		return errors.New("invalid broker text")
	}
	if err := v.RoleResource.Validate(); err != nil {
		return err
	}
	if v.KnowledgeResources == nil {
		return errors.New("null broker collection")
	}
	if len(v.KnowledgeResources) < 1 {
		return errors.New("invalid broker collection")
	}
	if len(v.KnowledgeResources) > 32 {
		return errors.New("invalid broker collection")
	}
	for _, item := range v.KnowledgeResources {
		if err := item.Validate(); err != nil {
			return err
		}
	}
	for i, item := range v.KnowledgeResources {
		for _, prior := range v.KnowledgeResources[:i] {
			if item == prior {
				return errors.New("duplicate broker collection item")
			}
		}
	}
	if string(v.RuntimeProjection) != "expert_only" {
		return errors.New("invalid broker constant")
	}
	if err := v.LocalUseAuthorization.Validate(); err != nil {
		return err
	}
	return nil
}
func (v *ExpertMetadata) UnmarshalJSON(b []byte) error {
	type wire ExpertMetadata
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil || raw == nil {
		return errors.New("invalid broker object")
	}
	if _, ok := raw["expert_id"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["expert_id"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["source_id"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["source_id"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["platform"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["platform"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["subtitle"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["subtitle"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["tags"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["tags"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["quick_prompts"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["quick_prompts"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["knowledge_summary"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["knowledge_summary"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["role_resource"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["role_resource"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["knowledge_resources"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["knowledge_resources"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["runtime_projection"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["runtime_projection"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["local_use_authorization"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["local_use_authorization"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
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
	x := ExpertMetadata(w)
	if err := x.Validate(); err != nil {
		return err
	}
	*v = x
	return nil
}

type CanonicalId = host.CanonicalId
type ApplyPayload struct {
	OperationId             CanonicalId    `json:"operationId"`
	HostInstanceId          CanonicalId    `json:"hostInstanceId"`
	Scope                   ScopeBinding   `json:"scope"`
	LocalSessionId          CanonicalId    `json:"localSessionId"`
	AgentSessionId          CanonicalId    `json:"agentSessionId"`
	NativeThreadId          NativeId       `json:"nativeThreadId"`
	ExpectedBindingRevision Revision       `json:"expectedBindingRevision"`
	BindingRevision         Revision       `json:"bindingRevision"`
	Expert                  *ExpertRef     `json:"expert"`
	Intent                  ModelIntent    `json:"intent"`
	PermissionMode          PermissionMode `json:"permissionMode"`
	MarketSnapshot          MarketSnapshot `json:"marketSnapshot"`
}

func (v ApplyPayload) Validate() error {
	if err := v.OperationId.Validate(); err != nil {
		return err
	}
	if err := v.HostInstanceId.Validate(); err != nil {
		return err
	}
	if err := v.Scope.Validate(); err != nil {
		return err
	}
	if err := v.LocalSessionId.Validate(); err != nil {
		return err
	}
	if err := v.AgentSessionId.Validate(); err != nil {
		return err
	}
	if err := v.NativeThreadId.Validate(); err != nil {
		return err
	}
	if err := v.ExpectedBindingRevision.Validate(); err != nil {
		return err
	}
	if err := v.BindingRevision.Validate(); err != nil {
		return err
	}
	if v.Expert != nil {
		if err := (*v.Expert).Validate(); err != nil {
			return err
		}
	}
	if err := v.Intent.Validate(); err != nil {
		return err
	}
	if err := v.PermissionMode.Validate(); err != nil {
		return err
	}
	if err := v.MarketSnapshot.Validate(); err != nil {
		return err
	}
	if v.BindingRevision != v.ExpectedBindingRevision+1 {
		return errors.New("invalid expert binding revision")
	}
	return nil
}
func (v *ApplyPayload) UnmarshalJSON(b []byte) error {
	type wire ApplyPayload
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
	if _, ok := raw["localSessionId"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["localSessionId"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
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
	if x, ok := raw["nativeThreadId"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["expectedBindingRevision"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["expectedBindingRevision"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["bindingRevision"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["bindingRevision"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["expert"]; !ok {
		return errors.New("missing broker field")
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
	if _, ok := raw["marketSnapshot"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["marketSnapshot"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
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
	x := ApplyPayload(w)
	if err := x.Validate(); err != nil {
		return err
	}
	*v = x
	return nil
}

type ScopeBinding = broker.ScopeBinding
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

type Revision int64

func (v Revision) Validate() error {
	if v < 0 {
		return errors.New("invalid broker number")
	}
	if v > 9007199254740991 {
		return errors.New("invalid broker number")
	}
	return nil
}

type ExpertRef struct {
	ExpertId                ExpertId `json:"expertId"`
	Version                 Version  `json:"version"`
	ArchiveSha256           Digest   `json:"archiveSha256"`
	RoleSha256              Digest   `json:"roleSha256"`
	KnowledgeManifestSha256 Digest   `json:"knowledgeManifestSha256"`
}

func (v ExpertRef) Validate() error {
	if err := v.ExpertId.Validate(); err != nil {
		return err
	}
	if err := v.Version.Validate(); err != nil {
		return err
	}
	if err := v.ArchiveSha256.Validate(); err != nil {
		return err
	}
	if err := v.RoleSha256.Validate(); err != nil {
		return err
	}
	if err := v.KnowledgeManifestSha256.Validate(); err != nil {
		return err
	}
	return nil
}
func (v *ExpertRef) UnmarshalJSON(b []byte) error {
	type wire ExpertRef
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil || raw == nil {
		return errors.New("invalid broker object")
	}
	if _, ok := raw["expertId"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["expertId"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["version"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["version"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["archiveSha256"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["archiveSha256"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["roleSha256"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["roleSha256"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["knowledgeManifestSha256"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["knowledgeManifestSha256"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
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
	x := ExpertRef(w)
	if err := x.Validate(); err != nil {
		return err
	}
	*v = x
	return nil
}

type ExpertId string

func (v ExpertId) Validate() error {
	if utf8.RuneCountInString(string(v)) < 1 {
		return errors.New("invalid broker text")
	}
	if utf8.RuneCountInString(string(v)) > 64 {
		return errors.New("invalid broker text")
	}
	if !wirePattern0.MatchString(string(v)) {
		return errors.New("invalid broker pattern")
	}
	return nil
}

type Version string

func (v Version) Validate() error {
	if !wirePattern3.MatchString(string(v)) {
		return errors.New("invalid broker pattern")
	}
	return nil
}

type Digest string

func (v Digest) Validate() error {
	if !wirePattern4.MatchString(string(v)) {
		return errors.New("invalid broker pattern")
	}
	return nil
}

type ModelIntent = host.ModelIntent
type PermissionMode = host.PermissionMode
type MarketSnapshot = selection.SelectionSnapshot
type ApplyReceipt struct {
	OperationId             CanonicalId    `json:"operationId"`
	HostInstanceId          CanonicalId    `json:"hostInstanceId"`
	LocalSessionId          CanonicalId    `json:"localSessionId"`
	AgentSessionId          CanonicalId    `json:"agentSessionId"`
	NativeThreadId          NativeId       `json:"nativeThreadId"`
	ExpectedBindingRevision Revision       `json:"expectedBindingRevision"`
	BindingRevision         Revision       `json:"bindingRevision"`
	Expert                  *ExpertRef     `json:"expert"`
	State                   OperationState `json:"state"`
	ErrorCode               *ErrorCode     `json:"errorCode,omitempty"`
}

func (v ApplyReceipt) Validate() error {
	if err := v.OperationId.Validate(); err != nil {
		return err
	}
	if err := v.HostInstanceId.Validate(); err != nil {
		return err
	}
	if err := v.LocalSessionId.Validate(); err != nil {
		return err
	}
	if err := v.AgentSessionId.Validate(); err != nil {
		return err
	}
	if err := v.NativeThreadId.Validate(); err != nil {
		return err
	}
	if err := v.ExpectedBindingRevision.Validate(); err != nil {
		return err
	}
	if err := v.BindingRevision.Validate(); err != nil {
		return err
	}
	if v.Expert != nil {
		if err := (*v.Expert).Validate(); err != nil {
			return err
		}
	}
	if err := v.State.Validate(); err != nil {
		return err
	}
	if v.ErrorCode != nil {
		if err := (*v.ErrorCode).Validate(); err != nil {
			return err
		}
	}
	expected := v.ExpectedBindingRevision
	if v.State == OperationStateSucceeded {
		expected++
	}
	if v.BindingRevision != expected {
		return errors.New("invalid effective binding revision")
	}
	return nil
}
func (v *ApplyReceipt) UnmarshalJSON(b []byte) error {
	type wire ApplyReceipt
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
	if _, ok := raw["localSessionId"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["localSessionId"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
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
	if x, ok := raw["nativeThreadId"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["expectedBindingRevision"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["expectedBindingRevision"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["bindingRevision"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["bindingRevision"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["expert"]; !ok {
		return errors.New("missing broker field")
	}
	if _, ok := raw["state"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["state"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if x, ok := raw["errorCode"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
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
	x := ApplyReceipt(w)
	if err := x.Validate(); err != nil {
		return err
	}
	*v = x
	return nil
}

type OperationState string

const (
	OperationStatePending   OperationState = "pending"
	OperationStateSucceeded OperationState = "succeeded"
	OperationStateRejected  OperationState = "rejected"
	OperationStateUnknown   OperationState = "unknown"
)

func (v OperationState) Validate() error {
	switch v {
	case OperationStatePending, OperationStateSucceeded, OperationStateRejected, OperationStateUnknown:
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

type ErrorCode string

const (
	ErrorCodeInvalidRequest            ErrorCode = "invalid_request"
	ErrorCodeContextInvalid            ErrorCode = "context_invalid"
	ErrorCodePermissionDenied          ErrorCode = "permission_denied"
	ErrorCodeUnsupportedCapability     ErrorCode = "unsupported_capability"
	ErrorCodeNotFound                  ErrorCode = "not_found"
	ErrorCodeResourceUnavailable       ErrorCode = "resource_unavailable"
	ErrorCodeResourceInUse             ErrorCode = "resource_in_use"
	ErrorCodeVersionUnavailable        ErrorCode = "version_unavailable"
	ErrorCodeRevisionConflict          ErrorCode = "revision_conflict"
	ErrorCodeRequestConflict           ErrorCode = "request_conflict"
	ErrorCodeBusy                      ErrorCode = "busy"
	ErrorCodeApprovalPending           ErrorCode = "approval_pending"
	ErrorCodeOperationPending          ErrorCode = "operation_pending"
	ErrorCodeOutcomeUnknown            ErrorCode = "outcome_unknown"
	ErrorCodeBindingUnavailable        ErrorCode = "binding_unavailable"
	ErrorCodeThreadUnavailable         ErrorCode = "thread_unavailable"
	ErrorCodeQualificationRequired     ErrorCode = "qualification_required"
	ErrorCodeModelUnavailable          ErrorCode = "model_unavailable"
	ErrorCodePermissionModeUnavailable ErrorCode = "permission_mode_unavailable"
	ErrorCodeSelectionStale            ErrorCode = "selection_stale"
	ErrorCodeCapacityExceeded          ErrorCode = "capacity_exceeded"
	ErrorCodeTemporarilyUnavailable    ErrorCode = "temporarily_unavailable"
)

func (v ErrorCode) Validate() error {
	switch v {
	case ErrorCodeInvalidRequest, ErrorCodeContextInvalid, ErrorCodePermissionDenied, ErrorCodeUnsupportedCapability, ErrorCodeNotFound, ErrorCodeResourceUnavailable, ErrorCodeResourceInUse, ErrorCodeVersionUnavailable, ErrorCodeRevisionConflict, ErrorCodeRequestConflict, ErrorCodeBusy, ErrorCodeApprovalPending, ErrorCodeOperationPending, ErrorCodeOutcomeUnknown, ErrorCodeBindingUnavailable, ErrorCodeThreadUnavailable, ErrorCodeQualificationRequired, ErrorCodeModelUnavailable, ErrorCodePermissionModeUnavailable, ErrorCodeSelectionStale, ErrorCodeCapacityExceeded, ErrorCodeTemporarilyUnavailable:
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

type PrivateOperationReadPayload struct {
	HostInstanceId CanonicalId  `json:"hostInstanceId"`
	Scope          ScopeBinding `json:"scope"`
	LocalSessionId CanonicalId  `json:"localSessionId"`
	AgentSessionId CanonicalId  `json:"agentSessionId"`
	OperationId    CanonicalId  `json:"operationId"`
}

func (v PrivateOperationReadPayload) Validate() error {
	if err := v.HostInstanceId.Validate(); err != nil {
		return err
	}
	if err := v.Scope.Validate(); err != nil {
		return err
	}
	if err := v.LocalSessionId.Validate(); err != nil {
		return err
	}
	if err := v.AgentSessionId.Validate(); err != nil {
		return err
	}
	if err := v.OperationId.Validate(); err != nil {
		return err
	}
	return nil
}
func (v *PrivateOperationReadPayload) UnmarshalJSON(b []byte) error {
	type wire PrivateOperationReadPayload
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
	if _, ok := raw["scope"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["scope"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
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
	if x, ok := raw["agentSessionId"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["operationId"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["operationId"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
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
	x := PrivateOperationReadPayload(w)
	if err := x.Validate(); err != nil {
		return err
	}
	*v = x
	return nil
}

type PrivateGrantPayload struct {
	OperationId    CanonicalId       `json:"operationId"`
	HostInstanceId CanonicalId       `json:"hostInstanceId"`
	Scope          ScopeBinding      `json:"scope"`
	Submission     PrivateSubmission `json:"submission"`
}

func (v PrivateGrantPayload) Validate() error {
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
func (v *PrivateGrantPayload) UnmarshalJSON(b []byte) error {
	type wire PrivateGrantPayload
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
	x := PrivateGrantPayload(w)
	if err := x.Validate(); err != nil {
		return err
	}
	*v = x
	return nil
}

type PrivateSubmission struct {
	Submission     MarketSubmission   `json:"submission"`
	ExpertSnapshot TurnExpertSnapshot `json:"expertSnapshot"`
}

func (v PrivateSubmission) Validate() error {
	if err := v.Submission.Validate(); err != nil {
		return err
	}
	if err := v.ExpertSnapshot.Validate(); err != nil {
		return err
	}
	if v.Submission.Snapshot.TurnOperationId != v.ExpertSnapshot.TurnOperationId {
		return errors.New("expert turn identity mismatch")
	}
	return nil
}
func (v *PrivateSubmission) UnmarshalJSON(b []byte) error {
	type wire PrivateSubmission
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil || raw == nil {
		return errors.New("invalid broker object")
	}
	if _, ok := raw["submission"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["submission"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["expertSnapshot"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["expertSnapshot"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
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
	x := PrivateSubmission(w)
	if err := x.Validate(); err != nil {
		return err
	}
	*v = x
	return nil
}

type MarketSubmission = host.Submission
type TurnExpertSnapshot struct {
	FormatVersion   int64       `json:"formatVersion"`
	TurnOperationId CanonicalId `json:"turnOperationId"`
	BindingRevision Revision    `json:"bindingRevision"`
	Expert          *ExpertRef  `json:"expert"`
	SnapshotDigest  Digest      `json:"snapshotDigest"`
}

func (v TurnExpertSnapshot) Validate() error {
	if v.FormatVersion != 1 {
		return errors.New("invalid broker constant")
	}
	if err := v.TurnOperationId.Validate(); err != nil {
		return err
	}
	if err := v.BindingRevision.Validate(); err != nil {
		return err
	}
	if v.Expert != nil {
		if err := (*v.Expert).Validate(); err != nil {
			return err
		}
	}
	if err := v.SnapshotDigest.Validate(); err != nil {
		return err
	}
	d, e := SnapshotDigest(v.TurnOperationId, v.BindingRevision, v.Expert)
	if e != nil {
		return e
	}
	if d != string(v.SnapshotDigest) {
		return errors.New("expert snapshot digest mismatch")
	}
	return nil
}
func (v *TurnExpertSnapshot) UnmarshalJSON(b []byte) error {
	type wire TurnExpertSnapshot
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil || raw == nil {
		return errors.New("invalid broker object")
	}
	if _, ok := raw["formatVersion"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["formatVersion"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["turnOperationId"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["turnOperationId"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["bindingRevision"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["bindingRevision"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["expert"]; !ok {
		return errors.New("missing broker field")
	}
	if _, ok := raw["snapshotDigest"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["snapshotDigest"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
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
	x := TurnExpertSnapshot(w)
	if err := x.Validate(); err != nil {
		return err
	}
	*v = x
	return nil
}

type PrivateGrant struct {
	Grant                MarketGrant `json:"grant"`
	ExpertSnapshotDigest Digest      `json:"expertSnapshotDigest"`
}

func (v PrivateGrant) Validate() error {
	if err := v.Grant.Validate(); err != nil {
		return err
	}
	if err := v.ExpertSnapshotDigest.Validate(); err != nil {
		return err
	}
	return nil
}
func (v *PrivateGrant) UnmarshalJSON(b []byte) error {
	type wire PrivateGrant
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil || raw == nil {
		return errors.New("invalid broker object")
	}
	if _, ok := raw["grant"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["grant"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["expertSnapshotDigest"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["expertSnapshotDigest"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
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
	x := PrivateGrant(w)
	if err := x.Validate(); err != nil {
		return err
	}
	*v = x
	return nil
}

type MarketGrant = host.GrantRegistration
type MarketSubmitTrigger = host.SubmitPayload
type PrivateReceipt struct {
	Receipt        MarketReceipt      `json:"receipt"`
	ExpertSnapshot TurnExpertSnapshot `json:"expertSnapshot"`
}

func (v PrivateReceipt) Validate() error {
	if err := v.Receipt.Validate(); err != nil {
		return err
	}
	if err := v.ExpertSnapshot.Validate(); err != nil {
		return err
	}
	if v.Receipt.TurnOperationId != v.ExpertSnapshot.TurnOperationId {
		return errors.New("expert receipt identity mismatch")
	}
	return nil
}
func (v *PrivateReceipt) UnmarshalJSON(b []byte) error {
	type wire PrivateReceipt
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil || raw == nil {
		return errors.New("invalid broker object")
	}
	if _, ok := raw["receipt"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["receipt"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["expertSnapshot"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["expertSnapshot"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
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
	x := PrivateReceipt(w)
	if err := x.Validate(); err != nil {
		return err
	}
	*v = x
	return nil
}

type MarketReceipt = host.SubmissionReceipt
type SubmissionReadPayload struct {
	HostInstanceId  CanonicalId  `json:"hostInstanceId"`
	Scope           ScopeBinding `json:"scope"`
	TaskId          CanonicalId  `json:"taskId"`
	LocalSessionId  CanonicalId  `json:"localSessionId"`
	TurnOperationId CanonicalId  `json:"turnOperationId"`
}

func (v SubmissionReadPayload) Validate() error {
	if err := v.HostInstanceId.Validate(); err != nil {
		return err
	}
	if err := v.Scope.Validate(); err != nil {
		return err
	}
	if err := v.TaskId.Validate(); err != nil {
		return err
	}
	if err := v.LocalSessionId.Validate(); err != nil {
		return err
	}
	if err := v.TurnOperationId.Validate(); err != nil {
		return err
	}
	return nil
}
func (v *SubmissionReadPayload) UnmarshalJSON(b []byte) error {
	type wire SubmissionReadPayload
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
	if _, ok := raw["scope"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["scope"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
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
	x := SubmissionReadPayload(w)
	if err := x.Validate(); err != nil {
		return err
	}
	*v = x
	return nil
}

type PrivateStatusPayload struct {
	HostInstanceId CanonicalId  `json:"hostInstanceId"`
	Scope          ScopeBinding `json:"scope"`
}

func (v PrivateStatusPayload) Validate() error {
	if err := v.HostInstanceId.Validate(); err != nil {
		return err
	}
	if err := v.Scope.Validate(); err != nil {
		return err
	}
	return nil
}
func (v *PrivateStatusPayload) UnmarshalJSON(b []byte) error {
	type wire PrivateStatusPayload
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
	if _, ok := raw["scope"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["scope"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
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
	x := PrivateStatusPayload(w)
	if err := x.Validate(); err != nil {
		return err
	}
	*v = x
	return nil
}

type PrivateStatus struct {
	CatalogRevision    Digest              `json:"catalogRevision"`
	ExecutionAvailable bool                `json:"executionAvailable"`
	ResourceOperations []ResourceOperation `json:"resourceOperations"`
}

func (v PrivateStatus) Validate() error {
	if err := v.CatalogRevision.Validate(); err != nil {
		return err
	}
	if v.ResourceOperations == nil {
		return errors.New("null broker collection")
	}
	if len(v.ResourceOperations) > 64 {
		return errors.New("invalid broker collection")
	}
	for _, item := range v.ResourceOperations {
		if err := item.Validate(); err != nil {
			return err
		}
	}
	return nil
}
func (v *PrivateStatus) UnmarshalJSON(b []byte) error {
	type wire PrivateStatus
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil || raw == nil {
		return errors.New("invalid broker object")
	}
	if _, ok := raw["catalogRevision"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["catalogRevision"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["executionAvailable"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["executionAvailable"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["resourceOperations"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["resourceOperations"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
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
	x := PrivateStatus(w)
	if err := x.Validate(); err != nil {
		return err
	}
	*v = x
	return nil
}

type ResourceOperation struct {
	OperationId CanonicalId     `json:"operationId"`
	Expert      ExpertSelection `json:"expert"`
	Action      string          `json:"action"`
	State       OperationState  `json:"state"`
	ErrorCode   *ErrorCode      `json:"errorCode,omitempty"`
}

func (v ResourceOperation) Validate() error {
	if err := v.OperationId.Validate(); err != nil {
		return err
	}
	if err := v.Expert.Validate(); err != nil {
		return err
	}
	switch string(v.Action) {
	case "prepare", "remove":
	default:
		return errors.New("invalid broker enum")
	}
	if err := v.State.Validate(); err != nil {
		return err
	}
	if v.ErrorCode != nil {
		if err := (*v.ErrorCode).Validate(); err != nil {
			return err
		}
	}
	return nil
}
func (v *ResourceOperation) UnmarshalJSON(b []byte) error {
	type wire ResourceOperation
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
	if _, ok := raw["expert"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["expert"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["action"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["action"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["state"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["state"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if x, ok := raw["errorCode"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
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
	x := ResourceOperation(w)
	if err := x.Validate(); err != nil {
		return err
	}
	*v = x
	return nil
}

type ExpertSelection struct {
	ExpertId ExpertId `json:"expertId"`
	Version  Version  `json:"version"`
}

func (v ExpertSelection) Validate() error {
	if err := v.ExpertId.Validate(); err != nil {
		return err
	}
	if err := v.Version.Validate(); err != nil {
		return err
	}
	return nil
}
func (v *ExpertSelection) UnmarshalJSON(b []byte) error {
	type wire ExpertSelection
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil || raw == nil {
		return errors.New("invalid broker object")
	}
	if _, ok := raw["expertId"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["expertId"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["version"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["version"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
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
	x := ExpertSelection(w)
	if err := x.Validate(); err != nil {
		return err
	}
	*v = x
	return nil
}

type PrivateResourcePayload struct {
	HostInstanceId CanonicalId     `json:"hostInstanceId"`
	Scope          ScopeBinding    `json:"scope"`
	OperationId    CanonicalId     `json:"operationId"`
	Expert         ExpertSelection `json:"expert"`
}

func (v PrivateResourcePayload) Validate() error {
	if err := v.HostInstanceId.Validate(); err != nil {
		return err
	}
	if err := v.Scope.Validate(); err != nil {
		return err
	}
	if err := v.OperationId.Validate(); err != nil {
		return err
	}
	if err := v.Expert.Validate(); err != nil {
		return err
	}
	return nil
}
func (v *PrivateResourcePayload) UnmarshalJSON(b []byte) error {
	type wire PrivateResourcePayload
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
	if _, ok := raw["scope"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["scope"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["operationId"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["operationId"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["expert"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["expert"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
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
	x := PrivateResourcePayload(w)
	if err := x.Validate(); err != nil {
		return err
	}
	*v = x
	return nil
}

type PrivateResourceReadPayload struct {
	HostInstanceId CanonicalId  `json:"hostInstanceId"`
	Scope          ScopeBinding `json:"scope"`
	OperationId    CanonicalId  `json:"operationId"`
}

func (v PrivateResourceReadPayload) Validate() error {
	if err := v.HostInstanceId.Validate(); err != nil {
		return err
	}
	if err := v.Scope.Validate(); err != nil {
		return err
	}
	if err := v.OperationId.Validate(); err != nil {
		return err
	}
	return nil
}
func (v *PrivateResourceReadPayload) UnmarshalJSON(b []byte) error {
	type wire PrivateResourceReadPayload
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
	if _, ok := raw["scope"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["scope"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["operationId"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["operationId"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
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
	x := PrivateResourceReadPayload(w)
	if err := x.Validate(); err != nil {
		return err
	}
	*v = x
	return nil
}

type PrivateBindingRecoverPayload struct {
	HostInstanceId CanonicalId  `json:"hostInstanceId"`
	Scope          ScopeBinding `json:"scope"`
	Original       ApplyPayload `json:"original"`
}

func (v PrivateBindingRecoverPayload) Validate() error {
	if err := v.HostInstanceId.Validate(); err != nil {
		return err
	}
	if err := v.Scope.Validate(); err != nil {
		return err
	}
	if err := v.Original.Validate(); err != nil {
		return err
	}
	return nil
}
func (v *PrivateBindingRecoverPayload) UnmarshalJSON(b []byte) error {
	type wire PrivateBindingRecoverPayload
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
	if _, ok := raw["scope"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["scope"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["original"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["original"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
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
	x := PrivateBindingRecoverPayload(w)
	if err := x.Validate(); err != nil {
		return err
	}
	*v = x
	return nil
}

type PrivateResourceRecoverPayload struct {
	HostInstanceId CanonicalId            `json:"hostInstanceId"`
	Scope          ScopeBinding           `json:"scope"`
	Original       PrivateResourcePayload `json:"original"`
	Action         string                 `json:"action"`
}

func (v PrivateResourceRecoverPayload) Validate() error {
	if err := v.HostInstanceId.Validate(); err != nil {
		return err
	}
	if err := v.Scope.Validate(); err != nil {
		return err
	}
	if err := v.Original.Validate(); err != nil {
		return err
	}
	switch string(v.Action) {
	case "prepare", "remove":
	default:
		return errors.New("invalid broker enum")
	}
	return nil
}
func (v *PrivateResourceRecoverPayload) UnmarshalJSON(b []byte) error {
	type wire PrivateResourceRecoverPayload
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
	if _, ok := raw["scope"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["scope"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["original"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["original"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["action"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["action"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
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
	x := PrivateResourceRecoverPayload(w)
	if err := x.Validate(); err != nil {
		return err
	}
	*v = x
	return nil
}

type RoleResource struct {
	Path   ResourcePath `json:"path"`
	Sha256 Digest       `json:"sha256"`
	Bytes  int64        `json:"bytes"`
}

func (v RoleResource) Validate() error {
	if err := v.Path.Validate(); err != nil {
		return err
	}
	if err := v.Sha256.Validate(); err != nil {
		return err
	}
	if v.Bytes < 1 {
		return errors.New("invalid broker number")
	}
	if v.Bytes > 32768 {
		return errors.New("invalid broker number")
	}
	return nil
}
func (v *RoleResource) UnmarshalJSON(b []byte) error {
	type wire RoleResource
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil || raw == nil {
		return errors.New("invalid broker object")
	}
	if _, ok := raw["path"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["path"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["sha256"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["sha256"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["bytes"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["bytes"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
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
	x := RoleResource(w)
	if err := x.Validate(); err != nil {
		return err
	}
	*v = x
	return nil
}

type ResourcePath string

func (v ResourcePath) Validate() error {
	if !resourcePath(string(v)) {
		return errors.New("invalid expert resource path")
	}
	return nil
}

type KnowledgeResource struct {
	Id             string       `json:"id"`
	Title          string       `json:"title"`
	Path           ResourcePath `json:"path"`
	Sha256         Digest       `json:"sha256"`
	Bytes          int64        `json:"bytes"`
	ContentVersion Version      `json:"content_version"`
	ContentStatus  string       `json:"content_status"`
}

func (v KnowledgeResource) Validate() error {
	if utf8.RuneCountInString(string(v.Id)) > 80 {
		return errors.New("invalid broker text")
	}
	if !wirePattern1.MatchString(string(v.Id)) {
		return errors.New("invalid broker pattern")
	}
	if utf8.RuneCountInString(string(v.Title)) < 1 {
		return errors.New("invalid broker text")
	}
	if utf8.RuneCountInString(string(v.Title)) > 160 {
		return errors.New("invalid broker text")
	}
	if err := v.Path.Validate(); err != nil {
		return err
	}
	if err := v.Sha256.Validate(); err != nil {
		return err
	}
	if v.Bytes < 1 {
		return errors.New("invalid broker number")
	}
	if v.Bytes > 8388608 {
		return errors.New("invalid broker number")
	}
	if err := v.ContentVersion.Validate(); err != nil {
		return err
	}
	switch string(v.ContentStatus) {
	case "pending-review", "reviewed-reference", "conflicted", "incomplete":
	default:
		return errors.New("invalid broker enum")
	}
	return nil
}
func (v *KnowledgeResource) UnmarshalJSON(b []byte) error {
	type wire KnowledgeResource
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil || raw == nil {
		return errors.New("invalid broker object")
	}
	if _, ok := raw["id"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["id"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["title"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["title"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["path"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["path"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["sha256"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["sha256"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["bytes"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["bytes"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["content_version"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["content_version"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["content_status"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["content_status"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
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
	x := KnowledgeResource(w)
	if err := x.Validate(); err != nil {
		return err
	}
	*v = x
	return nil
}

type LocalUseAuthorization struct {
	Status            string       `json:"status"`
	EvidenceReference ResourcePath `json:"evidence_reference"`
}

func (v LocalUseAuthorization) Validate() error {
	if string(v.Status) != "local-use-authorized" {
		return errors.New("invalid broker constant")
	}
	if err := v.EvidenceReference.Validate(); err != nil {
		return err
	}
	return nil
}
func (v *LocalUseAuthorization) UnmarshalJSON(b []byte) error {
	type wire LocalUseAuthorization
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
	if _, ok := raw["evidence_reference"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["evidence_reference"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
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
	x := LocalUseAuthorization(w)
	if err := x.Validate(); err != nil {
		return err
	}
	*v = x
	return nil
}

func resourcePath(s string) bool {
	return s != "" && utf8.RuneCountInString(s) <= 512 && !strings.HasPrefix(s, "/") && !strings.Contains(s, "\\") && !strings.Contains("/"+s+"/", "/../") && strings.IndexFunc(s, func(c rune) bool { return c <= 31 || c == 127 }) < 0
}
func CanonicalSnapshotBytes(turn CanonicalId, revision Revision, expert *ExpertRef) ([]byte, error) {
	if e := turn.Validate(); e != nil {
		return nil, e
	}
	if e := revision.Validate(); e != nil {
		return nil, e
	}
	var b strings.Builder
	fmt.Fprintf(&b, "yijie.expert-snapshot/v1\n%s\n%d\n", turn, revision)
	if expert == nil {
		b.WriteString("none\n")
	} else {
		if e := expert.Validate(); e != nil {
			return nil, e
		}
		fmt.Fprintf(&b, "expert\n%s\n%s\n%s\n%s\n%s\n", expert.ExpertId, expert.Version, expert.ArchiveSha256, expert.RoleSha256, expert.KnowledgeManifestSha256)
	}
	return []byte(b.String()), nil
}
func SnapshotDigest(turn CanonicalId, revision Revision, expert *ExpertRef) (string, error) {
	b, e := CanonicalSnapshotBytes(turn, revision, expert)
	if e != nil {
		return "", e
	}
	return fmt.Sprintf("%x", sha256.Sum256(b)), nil
}
func FreezeSnapshot(turn CanonicalId, revision Revision, expert *ExpertRef) (TurnExpertSnapshot, error) {
	d, e := SnapshotDigest(turn, revision, expert)
	if e != nil {
		return TurnExpertSnapshot{}, e
	}
	return TurnExpertSnapshot{FormatVersion: 1, TurnOperationId: turn, BindingRevision: revision, Expert: expert, SnapshotDigest: Digest(d)}, nil
}
func CanonicalKnowledgeManifestBytes(resources []KnowledgeResource) ([]byte, error) {
	if len(resources) < 1 || len(resources) > 32 {
		return nil, errors.New("invalid knowledge count")
	}
	rs := append([]KnowledgeResource(nil), resources...)
	sort.Slice(rs, func(i, j int) bool { return rs[i].Id < rs[j].Id })
	var out bytes.Buffer
	fmt.Fprintf(&out, "yijie.expert-knowledge/v1\n%d\n", len(rs))
	for i, r := range rs {
		if e := r.Validate(); e != nil {
			return nil, e
		}
		if i > 0 && rs[i-1].Id == r.Id {
			return nil, errors.New("duplicate knowledge id")
		}
		for _, v := range []string{r.Id, r.Title, string(r.Path), string(r.Sha256), fmt.Sprint(r.Bytes), string(r.ContentVersion), r.ContentStatus} {
			fmt.Fprintf(&out, "%d:", len([]byte(v)))
			out.WriteString(v)
		}
		out.WriteByte('\n')
	}
	return out.Bytes(), nil
}
func KnowledgeManifestDigest(resources []KnowledgeResource) (string, error) {
	b, e := CanonicalKnowledgeManifestBytes(resources)
	if e != nil {
		return "", e
	}
	return fmt.Sprintf("%x", sha256.Sum256(b)), nil
}

var MethodTimeoutMs = map[string]int64{"expert_apply_binding": 20000, "expert_operation_read": 5000, "expert_grant_register": 5000, "expert_submit": 25000, "expert_submission_read": 5000, "expert_status": 5000, "expert_resources_prepare": 25000, "expert_resources_remove": 25000, "expert_resources_operation_read": 5000, "expert_binding_recover": 20000, "expert_resources_recover": 25000}

const NativeResponseTimeoutMs = 30000
