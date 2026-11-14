// Generated from skills/market-v2; DO NOT EDIT.
package skillmarketv2

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"regexp"
	"unicode/utf8"
)

var wirePattern0 = regexp.MustCompile("^[0-9a-f]{64}$")
var wirePattern1 = regexp.MustCompile("^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$")

type Origin string

const (
	OriginBuiltin Origin = "builtin"
	OriginUser    Origin = "user"
	OriginUnknown Origin = "unknown"
)

func (v Origin) Validate() error {
	switch v {
	case OriginBuiltin, OriginUser, OriginUnknown:
		return nil
	default:
		return errors.New("invalid broker enum")
	}
}
func (v *Origin) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return errors.New("invalid broker enum")
	}
	x := Origin(s)
	if err := x.Validate(); err != nil {
		return err
	}
	*v = x
	return nil
}

type InstallState string

const (
	InstallStateNotInstalled InstallState = "not_installed"
	InstallStateInstalled    InstallState = "installed"
	InstallStatePartial      InstallState = "partial"
	InstallStateInvalid      InstallState = "invalid"
	InstallStateUnknown      InstallState = "unknown"
)

func (v InstallState) Validate() error {
	switch v {
	case InstallStateNotInstalled, InstallStateInstalled, InstallStatePartial, InstallStateInvalid, InstallStateUnknown:
		return nil
	default:
		return errors.New("invalid broker enum")
	}
}
func (v *InstallState) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return errors.New("invalid broker enum")
	}
	x := InstallState(s)
	if err := x.Validate(); err != nil {
		return err
	}
	*v = x
	return nil
}

type DependencyState string

const (
	DependencyStateReady              DependencyState = "ready"
	DependencyStateMissing            DependencyState = "missing"
	DependencyStateNeedsAuthorization DependencyState = "needs_authorization"
	DependencyStateUnknown            DependencyState = "unknown"
)

func (v DependencyState) Validate() error {
	switch v {
	case DependencyStateReady, DependencyStateMissing, DependencyStateNeedsAuthorization, DependencyStateUnknown:
		return nil
	default:
		return errors.New("invalid broker enum")
	}
}
func (v *DependencyState) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return errors.New("invalid broker enum")
	}
	x := DependencyState(s)
	if err := x.Validate(); err != nil {
		return err
	}
	*v = x
	return nil
}

type OperationState string

const (
	OperationStatePending        OperationState = "pending"
	OperationStateComplete       OperationState = "complete"
	OperationStateCancelled      OperationState = "cancelled"
	OperationStateNeedsAttention OperationState = "needs_attention"
)

func (v OperationState) Validate() error {
	switch v {
	case OperationStatePending, OperationStateComplete, OperationStateCancelled, OperationStateNeedsAttention:
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

type MutationKind string

const (
	MutationKindInstall   MutationKind = "install"
	MutationKindEnable    MutationKind = "enable"
	MutationKindDisable   MutationKind = "disable"
	MutationKindUninstall MutationKind = "uninstall"
	MutationKindRefresh   MutationKind = "refresh"
	MutationKindRollback  MutationKind = "rollback"
)

func (v MutationKind) Validate() error {
	switch v {
	case MutationKindInstall, MutationKindEnable, MutationKindDisable, MutationKindUninstall, MutationKindRefresh, MutationKindRollback:
		return nil
	default:
		return errors.New("invalid broker enum")
	}
}
func (v *MutationKind) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return errors.New("invalid broker enum")
	}
	x := MutationKind(s)
	if err := x.Validate(); err != nil {
		return err
	}
	*v = x
	return nil
}

type FileEntry struct {
	Path   string `json:"path"`
	Sha256 string `json:"sha256"`
	Bytes  int64  `json:"bytes"`
}

func (v FileEntry) Validate() error {
	if utf8.RuneCountInString(string(v.Path)) > 1024 {
		return errors.New("invalid broker text")
	}
	if utf8.RuneCountInString(string(v.Sha256)) < 64 {
		return errors.New("invalid broker text")
	}
	if utf8.RuneCountInString(string(v.Sha256)) > 64 {
		return errors.New("invalid broker text")
	}
	if !wirePattern0.MatchString(string(v.Sha256)) {
		return errors.New("invalid broker pattern")
	}
	if v.Bytes < 0 {
		return errors.New("invalid broker number")
	}
	if v.Bytes > 67108864 {
		return errors.New("invalid broker number")
	}
	return nil
}
func (v *FileEntry) UnmarshalJSON(b []byte) error {
	type wire FileEntry
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
	x := FileEntry(w)
	if err := x.Validate(); err != nil {
		return err
	}
	*v = x
	return nil
}

type CatalogSource struct {
	Id                   string      `json:"id"`
	ContentVersion       string      `json:"content_version"`
	ContentSha256        string      `json:"content_sha256"`
	SourceRevision       string      `json:"source_revision"`
	NativePackageVersion string      `json:"native_package_version"`
	PluginName           string      `json:"plugin_name"`
	DisplayName          string      `json:"display_name"`
	Description          string      `json:"description"`
	Category             string      `json:"category"`
	Order                int64       `json:"order"`
	IconKey              string      `json:"icon_key"`
	RequiredTools        []string    `json:"required_tools"`
	SourcePath           string      `json:"source_path"`
	Files                []FileEntry `json:"files"`
}

func (v CatalogSource) Validate() error {
	if utf8.RuneCountInString(string(v.Id)) < 1 {
		return errors.New("invalid broker text")
	}
	if utf8.RuneCountInString(string(v.Id)) > 256 {
		return errors.New("invalid broker text")
	}
	if utf8.RuneCountInString(string(v.ContentVersion)) < 1 {
		return errors.New("invalid broker text")
	}
	if utf8.RuneCountInString(string(v.ContentVersion)) > 64 {
		return errors.New("invalid broker text")
	}
	if utf8.RuneCountInString(string(v.ContentSha256)) < 64 {
		return errors.New("invalid broker text")
	}
	if utf8.RuneCountInString(string(v.ContentSha256)) > 64 {
		return errors.New("invalid broker text")
	}
	if !wirePattern0.MatchString(string(v.ContentSha256)) {
		return errors.New("invalid broker pattern")
	}
	if utf8.RuneCountInString(string(v.SourceRevision)) < 64 {
		return errors.New("invalid broker text")
	}
	if utf8.RuneCountInString(string(v.SourceRevision)) > 64 {
		return errors.New("invalid broker text")
	}
	if !wirePattern0.MatchString(string(v.SourceRevision)) {
		return errors.New("invalid broker pattern")
	}
	if utf8.RuneCountInString(string(v.NativePackageVersion)) > 64 {
		return errors.New("invalid broker text")
	}
	if utf8.RuneCountInString(string(v.PluginName)) > 128 {
		return errors.New("invalid broker text")
	}
	if utf8.RuneCountInString(string(v.DisplayName)) < 1 {
		return errors.New("invalid broker text")
	}
	if utf8.RuneCountInString(string(v.DisplayName)) > 256 {
		return errors.New("invalid broker text")
	}
	if utf8.RuneCountInString(string(v.Description)) > 4096 {
		return errors.New("invalid broker text")
	}
	if utf8.RuneCountInString(string(v.Category)) > 64 {
		return errors.New("invalid broker text")
	}
	if v.Order < 0 {
		return errors.New("invalid broker number")
	}
	if utf8.RuneCountInString(string(v.IconKey)) > 64 {
		return errors.New("invalid broker text")
	}
	if v.RequiredTools == nil {
		return errors.New("null broker collection")
	}
	if len(v.RequiredTools) > 128 {
		return errors.New("invalid broker collection")
	}
	for _, item := range v.RequiredTools {
		if utf8.RuneCountInString(string(item)) > 256 {
			return errors.New("invalid broker text")
		}
	}
	if utf8.RuneCountInString(string(v.SourcePath)) > 1024 {
		return errors.New("invalid broker text")
	}
	if v.Files == nil {
		return errors.New("null broker collection")
	}
	if len(v.Files) > 2048 {
		return errors.New("invalid broker collection")
	}
	for _, item := range v.Files {
		if err := item.Validate(); err != nil {
			return err
		}
	}
	return nil
}
func (v *CatalogSource) UnmarshalJSON(b []byte) error {
	type wire CatalogSource
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
	if _, ok := raw["content_version"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["content_version"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["content_sha256"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["content_sha256"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["source_revision"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["source_revision"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["native_package_version"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["native_package_version"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["plugin_name"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["plugin_name"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["display_name"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["display_name"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["description"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["description"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["category"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["category"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["order"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["order"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["icon_key"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["icon_key"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["required_tools"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["required_tools"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["source_path"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["source_path"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["files"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["files"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
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
	x := CatalogSource(w)
	if err := x.Validate(); err != nil {
		return err
	}
	*v = x
	return nil
}

type Bundle struct {
	SchemaVersion   int64           `json:"schema_version"`
	MarketplaceName string          `json:"marketplace_name"`
	Skills          []CatalogSource `json:"skills"`
}

func (v Bundle) Validate() error {
	if v.SchemaVersion != 2 {
		return errors.New("invalid broker constant")
	}
	if string(v.MarketplaceName) != "yijie-builtin" {
		return errors.New("invalid broker constant")
	}
	if v.Skills == nil {
		return errors.New("null broker collection")
	}
	if len(v.Skills) > 512 {
		return errors.New("invalid broker collection")
	}
	for _, item := range v.Skills {
		if err := item.Validate(); err != nil {
			return err
		}
	}
	return nil
}
func (v *Bundle) UnmarshalJSON(b []byte) error {
	type wire Bundle
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil || raw == nil {
		return errors.New("invalid broker object")
	}
	if _, ok := raw["schema_version"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["schema_version"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["marketplace_name"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["marketplace_name"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["skills"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["skills"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
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
	x := Bundle(w)
	if err := x.Validate(); err != nil {
		return err
	}
	*v = x
	return nil
}

type Skill struct {
	Id                string          `json:"id"`
	Version           string          `json:"version"`
	RuntimeName       string          `json:"runtime_name"`
	DisplayName       string          `json:"display_name"`
	Description       string          `json:"description"`
	Category          string          `json:"category"`
	Order             int64           `json:"order"`
	IconKey           string          `json:"icon_key"`
	Origin            Origin          `json:"origin"`
	InstallationState InstallState    `json:"installation_state"`
	Enabled           bool            `json:"enabled"`
	RuntimeVisible    bool            `json:"runtime_visible"`
	Retired           bool            `json:"retired"`
	DependencyState   DependencyState `json:"dependency_state"`
	RequiredTools     []string        `json:"required_tools"`
	Epoch             int64           `json:"epoch"`
	FailureCode       string          `json:"failure_code"`
}

func (v Skill) Validate() error {
	if utf8.RuneCountInString(string(v.Id)) < 1 {
		return errors.New("invalid broker text")
	}
	if utf8.RuneCountInString(string(v.Id)) > 256 {
		return errors.New("invalid broker text")
	}
	if utf8.RuneCountInString(string(v.Version)) < 1 {
		return errors.New("invalid broker text")
	}
	if utf8.RuneCountInString(string(v.Version)) > 64 {
		return errors.New("invalid broker text")
	}
	if utf8.RuneCountInString(string(v.RuntimeName)) > 512 {
		return errors.New("invalid broker text")
	}
	if utf8.RuneCountInString(string(v.DisplayName)) < 1 {
		return errors.New("invalid broker text")
	}
	if utf8.RuneCountInString(string(v.DisplayName)) > 256 {
		return errors.New("invalid broker text")
	}
	if utf8.RuneCountInString(string(v.Description)) > 4096 {
		return errors.New("invalid broker text")
	}
	if utf8.RuneCountInString(string(v.Category)) > 64 {
		return errors.New("invalid broker text")
	}
	if v.Order < 0 {
		return errors.New("invalid broker number")
	}
	if utf8.RuneCountInString(string(v.IconKey)) > 64 {
		return errors.New("invalid broker text")
	}
	if err := v.Origin.Validate(); err != nil {
		return err
	}
	if err := v.InstallationState.Validate(); err != nil {
		return err
	}
	if err := v.DependencyState.Validate(); err != nil {
		return err
	}
	if v.RequiredTools == nil {
		return errors.New("null broker collection")
	}
	if len(v.RequiredTools) > 128 {
		return errors.New("invalid broker collection")
	}
	for _, item := range v.RequiredTools {
		if utf8.RuneCountInString(string(item)) > 256 {
			return errors.New("invalid broker text")
		}
	}
	if v.Epoch < 0 {
		return errors.New("invalid broker number")
	}
	if utf8.RuneCountInString(string(v.FailureCode)) > 128 {
		return errors.New("invalid broker text")
	}
	return nil
}
func (v *Skill) UnmarshalJSON(b []byte) error {
	type wire Skill
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
	if _, ok := raw["version"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["version"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["runtime_name"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["runtime_name"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["display_name"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["display_name"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["description"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["description"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["category"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["category"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["order"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["order"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["icon_key"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["icon_key"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["origin"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["origin"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["installation_state"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["installation_state"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["enabled"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["enabled"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["runtime_visible"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["runtime_visible"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["retired"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["retired"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["dependency_state"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["dependency_state"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["required_tools"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["required_tools"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["epoch"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["epoch"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["failure_code"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["failure_code"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
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
	x := Skill(w)
	if err := x.Validate(); err != nil {
		return err
	}
	*v = x
	return nil
}

type Catalog struct {
	SchemaVersion int64              `json:"schema_version"`
	Revision      string             `json:"revision"`
	Skills        []Skill            `json:"skills"`
	Operations    []PendingOperation `json:"operations"`
	Notice        *string            `json:"notice,omitempty"`
}

func (v Catalog) Validate() error {
	if v.SchemaVersion != 2 {
		return errors.New("invalid broker constant")
	}
	if utf8.RuneCountInString(string(v.Revision)) < 64 {
		return errors.New("invalid broker text")
	}
	if utf8.RuneCountInString(string(v.Revision)) > 64 {
		return errors.New("invalid broker text")
	}
	if !wirePattern0.MatchString(string(v.Revision)) {
		return errors.New("invalid broker pattern")
	}
	if v.Skills == nil {
		return errors.New("null broker collection")
	}
	if len(v.Skills) > 1024 {
		return errors.New("invalid broker collection")
	}
	for _, item := range v.Skills {
		if err := item.Validate(); err != nil {
			return err
		}
	}
	if v.Operations == nil {
		return errors.New("null broker collection")
	}
	if len(v.Operations) > 512 {
		return errors.New("invalid broker collection")
	}
	for _, item := range v.Operations {
		if err := item.Validate(); err != nil {
			return err
		}
	}
	if v.Notice != nil {
		if utf8.RuneCountInString(string((*v.Notice))) > 256 {
			return errors.New("invalid broker text")
		}
	}
	return nil
}
func (v *Catalog) UnmarshalJSON(b []byte) error {
	type wire Catalog
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil || raw == nil {
		return errors.New("invalid broker object")
	}
	if _, ok := raw["schema_version"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["schema_version"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["revision"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["revision"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["skills"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["skills"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["operations"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["operations"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if x, ok := raw["notice"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
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
	x := Catalog(w)
	if err := x.Validate(); err != nil {
		return err
	}
	*v = x
	return nil
}

type Mutation struct {
	OperationId   string       `json:"operation_id"`
	SkillId       string       `json:"skill_id"`
	Kind          MutationKind `json:"kind"`
	ExpectedEpoch int64        `json:"expected_epoch"`
}

func (v Mutation) Validate() error {
	if utf8.RuneCountInString(string(v.OperationId)) < 36 {
		return errors.New("invalid broker text")
	}
	if utf8.RuneCountInString(string(v.OperationId)) > 36 {
		return errors.New("invalid broker text")
	}
	if !wirePattern1.MatchString(string(v.OperationId)) {
		return errors.New("invalid broker pattern")
	}
	if string(v.OperationId) == "00000000-0000-0000-0000-000000000000" {
		return errors.New("invalid broker identity")
	}
	if utf8.RuneCountInString(string(v.SkillId)) < 1 {
		return errors.New("invalid broker text")
	}
	if utf8.RuneCountInString(string(v.SkillId)) > 256 {
		return errors.New("invalid broker text")
	}
	if err := v.Kind.Validate(); err != nil {
		return err
	}
	if v.ExpectedEpoch < 0 {
		return errors.New("invalid broker number")
	}
	return nil
}
func (v *Mutation) UnmarshalJSON(b []byte) error {
	type wire Mutation
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil || raw == nil {
		return errors.New("invalid broker object")
	}
	if _, ok := raw["operation_id"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["operation_id"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["skill_id"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["skill_id"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["kind"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["kind"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["expected_epoch"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["expected_epoch"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
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
	x := Mutation(w)
	if err := x.Validate(); err != nil {
		return err
	}
	*v = x
	return nil
}

type ImportRequest struct {
	OperationId string `json:"operation_id"`
	FileName    string `json:"file_name"`
	Format      string `json:"format"`
	Sha256      string `json:"sha256"`
	Bytes       int64  `json:"bytes"`
}

func (v ImportRequest) Validate() error {
	if utf8.RuneCountInString(string(v.OperationId)) < 36 {
		return errors.New("invalid broker text")
	}
	if utf8.RuneCountInString(string(v.OperationId)) > 36 {
		return errors.New("invalid broker text")
	}
	if !wirePattern1.MatchString(string(v.OperationId)) {
		return errors.New("invalid broker pattern")
	}
	if string(v.OperationId) == "00000000-0000-0000-0000-000000000000" {
		return errors.New("invalid broker identity")
	}
	if utf8.RuneCountInString(string(v.FileName)) > 256 {
		return errors.New("invalid broker text")
	}
	switch string(v.Format) {
	case "zip", "markdown":
	default:
		return errors.New("invalid broker enum")
	}
	if utf8.RuneCountInString(string(v.Sha256)) < 64 {
		return errors.New("invalid broker text")
	}
	if utf8.RuneCountInString(string(v.Sha256)) > 64 {
		return errors.New("invalid broker text")
	}
	if !wirePattern0.MatchString(string(v.Sha256)) {
		return errors.New("invalid broker pattern")
	}
	if v.Bytes < 1 {
		return errors.New("invalid broker number")
	}
	if v.Bytes > 67108864 {
		return errors.New("invalid broker number")
	}
	return nil
}
func (v *ImportRequest) UnmarshalJSON(b []byte) error {
	type wire ImportRequest
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil || raw == nil {
		return errors.New("invalid broker object")
	}
	if _, ok := raw["operation_id"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["operation_id"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["file_name"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["file_name"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["format"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["format"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
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
	x := ImportRequest(w)
	if err := x.Validate(); err != nil {
		return err
	}
	*v = x
	return nil
}

type Operation struct {
	OperationId string         `json:"operation_id"`
	State       OperationState `json:"state"`
	SkillId     string         `json:"skill_id"`
	Epoch       int64          `json:"epoch"`
	ErrorCode   string         `json:"error_code"`
	Catalog     *Catalog       `json:"catalog,omitempty"`
}

func (v Operation) Validate() error {
	if utf8.RuneCountInString(string(v.OperationId)) < 36 {
		return errors.New("invalid broker text")
	}
	if utf8.RuneCountInString(string(v.OperationId)) > 36 {
		return errors.New("invalid broker text")
	}
	if !wirePattern1.MatchString(string(v.OperationId)) {
		return errors.New("invalid broker pattern")
	}
	if string(v.OperationId) == "00000000-0000-0000-0000-000000000000" {
		return errors.New("invalid broker identity")
	}
	if err := v.State.Validate(); err != nil {
		return err
	}
	if utf8.RuneCountInString(string(v.SkillId)) > 256 {
		return errors.New("invalid broker text")
	}
	if v.Epoch < 0 {
		return errors.New("invalid broker number")
	}
	if utf8.RuneCountInString(string(v.ErrorCode)) > 128 {
		return errors.New("invalid broker text")
	}
	if v.Catalog != nil {
		if err := (*v.Catalog).Validate(); err != nil {
			return err
		}
	}
	return nil
}
func (v *Operation) UnmarshalJSON(b []byte) error {
	type wire Operation
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil || raw == nil {
		return errors.New("invalid broker object")
	}
	if _, ok := raw["operation_id"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["operation_id"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["state"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["state"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["skill_id"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["skill_id"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["epoch"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["epoch"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["error_code"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["error_code"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if x, ok := raw["catalog"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
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
	x := Operation(w)
	if err := x.Validate(); err != nil {
		return err
	}
	*v = x
	return nil
}

type ErrorResponse struct {
	Code      string `json:"code"`
	Retryable bool   `json:"retryable"`
}

func (v ErrorResponse) Validate() error {
	if utf8.RuneCountInString(string(v.Code)) > 128 {
		return errors.New("invalid broker text")
	}
	return nil
}
func (v *ErrorResponse) UnmarshalJSON(b []byte) error {
	type wire ErrorResponse
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil || raw == nil {
		return errors.New("invalid broker object")
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
	x := ErrorResponse(w)
	if err := x.Validate(); err != nil {
		return err
	}
	*v = x
	return nil
}

type PendingOperation struct {
	OperationId string         `json:"operation_id"`
	State       OperationState `json:"state"`
	SkillId     string         `json:"skill_id"`
	Epoch       int64          `json:"epoch"`
	ErrorCode   string         `json:"error_code"`
}

func (v PendingOperation) Validate() error {
	if utf8.RuneCountInString(string(v.OperationId)) < 36 {
		return errors.New("invalid broker text")
	}
	if utf8.RuneCountInString(string(v.OperationId)) > 36 {
		return errors.New("invalid broker text")
	}
	if !wirePattern1.MatchString(string(v.OperationId)) {
		return errors.New("invalid broker pattern")
	}
	if string(v.OperationId) == "00000000-0000-0000-0000-000000000000" {
		return errors.New("invalid broker identity")
	}
	if err := v.State.Validate(); err != nil {
		return err
	}
	if utf8.RuneCountInString(string(v.SkillId)) > 256 {
		return errors.New("invalid broker text")
	}
	if v.Epoch < 0 {
		return errors.New("invalid broker number")
	}
	if utf8.RuneCountInString(string(v.ErrorCode)) > 128 {
		return errors.New("invalid broker text")
	}
	return nil
}
func (v *PendingOperation) UnmarshalJSON(b []byte) error {
	type wire PendingOperation
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil || raw == nil {
		return errors.New("invalid broker object")
	}
	if _, ok := raw["operation_id"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["operation_id"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["state"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["state"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["skill_id"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["skill_id"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["epoch"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["epoch"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
		return errors.New("null broker field")
	}
	if _, ok := raw["error_code"]; !ok {
		return errors.New("missing broker field")
	}
	if x, ok := raw["error_code"]; ok && bytes.Equal(bytes.TrimSpace(x), []byte("null")) {
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
	x := PendingOperation(w)
	if err := x.Validate(); err != nil {
		return err
	}
	*v = x
	return nil
}

const MaxCatalogResponseBytes = 268435456
