package codex

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"path/filepath"
)

const (
	RuntimeMethodSkillsList          = "skills/list"
	RuntimeMethodSkillsExtraRootsSet = "skills/extraRoots/set"
	RuntimeMethodSkillsConfigWrite   = "skills/config/write"
	RuntimeNotificationSkillsChanged = "skills/changed"
)

var skillsRuntimeMethods = []string{
	RuntimeMethodSkillsList,
	RuntimeMethodSkillsExtraRootsSet,
	RuntimeMethodSkillsConfigWrite,
}

// SupportedSkillsRuntimeMethods returns the stable Skills methods consumed
// from the pinned Codex Runtime. The returned slice may be modified by the
// caller.
func SupportedSkillsRuntimeMethods() []string {
	return append([]string(nil), skillsRuntimeMethods...)
}

type SkillScope string

const (
	SkillScopeUser   SkillScope = "user"
	SkillScopeRepo   SkillScope = "repo"
	SkillScopeSystem SkillScope = "system"
	SkillScopeAdmin  SkillScope = "admin"
)

type SkillMetadata struct {
	Name             string             `json:"name"`
	Description      string             `json:"description"`
	ShortDescription *string            `json:"shortDescription,omitempty"`
	Interface        *SkillInterface    `json:"interface,omitempty"`
	Dependencies     *SkillDependencies `json:"dependencies,omitempty"`
	Path             string             `json:"path"`
	Scope            SkillScope         `json:"scope"`
	Enabled          bool               `json:"enabled"`
}

type SkillInterface struct {
	DisplayName      *string `json:"displayName"`
	ShortDescription *string `json:"shortDescription"`
	IconSmall        *string `json:"iconSmall"`
	IconLarge        *string `json:"iconLarge"`
	BrandColor       *string `json:"brandColor"`
	DefaultPrompt    *string `json:"defaultPrompt"`
}

type SkillDependencies struct {
	Tools []SkillToolDependency `json:"tools"`
}

type SkillToolDependency struct {
	Type        string  `json:"type"`
	Value       string  `json:"value"`
	Description *string `json:"description,omitempty"`
	Transport   *string `json:"transport,omitempty"`
	Command     *string `json:"command,omitempty"`
	URL         *string `json:"url,omitempty"`
}

type SkillErrorInfo struct {
	Path    string `json:"path"`
	Message string `json:"message"`
}

type SkillsListEntry struct {
	CWD    string           `json:"cwd"`
	Skills []SkillMetadata  `json:"skills"`
	Errors []SkillErrorInfo `json:"errors"`
}

type skillsListParams struct {
	CWDs        []string `json:"cwds,omitempty"`
	ForceReload bool     `json:"forceReload,omitempty"`
}

type skillsListResponse struct {
	Data []SkillsListEntry
}

type skillsListResponseWire struct {
	Data *[]skillsListEntryWire `json:"data"`
}

type skillsListEntryWire struct {
	CWD    *string               `json:"cwd"`
	Skills *[]skillMetadataWire  `json:"skills"`
	Errors *[]skillErrorInfoWire `json:"errors"`
}

type skillMetadataWire struct {
	Name             *string                `json:"name"`
	Description      *string                `json:"description"`
	ShortDescription *string                `json:"shortDescription"`
	Interface        *skillInterfaceWire    `json:"interface"`
	Dependencies     *skillDependenciesWire `json:"dependencies"`
	Path             *string                `json:"path"`
	Scope            *SkillScope            `json:"scope"`
	Enabled          *bool                  `json:"enabled"`
}

type skillInterfaceWire struct {
	DisplayName      *string `json:"displayName"`
	ShortDescription *string `json:"shortDescription"`
	IconSmall        *string `json:"iconSmall"`
	IconLarge        *string `json:"iconLarge"`
	BrandColor       *string `json:"brandColor"`
	DefaultPrompt    *string `json:"defaultPrompt"`
}

type skillDependenciesWire struct {
	Tools *[]skillToolDependencyWire `json:"tools"`
}

type skillToolDependencyWire struct {
	Type        *string `json:"type"`
	Value       *string `json:"value"`
	Description *string `json:"description"`
	Transport   *string `json:"transport"`
	Command     *string `json:"command"`
	URL         *string `json:"url"`
}

type skillErrorInfoWire struct {
	Path    *string `json:"path"`
	Message *string `json:"message"`
}

func (response *skillsListResponse) UnmarshalJSON(raw []byte) error {
	var wire skillsListResponseWire
	if !decodeStrictSkillsObject(raw, &wire) || wire.Data == nil {
		return errors.New("skills/list response is invalid")
	}

	data := make([]SkillsListEntry, len(*wire.Data))
	for entryIndex, entryWire := range *wire.Data {
		entry, ok := convertSkillsListEntry(entryWire)
		if !ok {
			return errors.New("skills/list response is invalid")
		}
		data[entryIndex] = entry
	}
	response.Data = data
	return nil
}

func convertSkillsListEntry(wire skillsListEntryWire) (SkillsListEntry, bool) {
	if wire.CWD == nil || wire.Skills == nil || wire.Errors == nil {
		return SkillsListEntry{}, false
	}

	skills := make([]SkillMetadata, len(*wire.Skills))
	for skillIndex, skillWire := range *wire.Skills {
		skill, ok := convertSkillMetadata(skillWire)
		if !ok {
			return SkillsListEntry{}, false
		}
		skills[skillIndex] = skill
	}

	errorsInfo := make([]SkillErrorInfo, len(*wire.Errors))
	for errorIndex, errorWire := range *wire.Errors {
		if errorWire.Path == nil || errorWire.Message == nil {
			return SkillsListEntry{}, false
		}
		errorsInfo[errorIndex] = SkillErrorInfo{
			Path:    *errorWire.Path,
			Message: *errorWire.Message,
		}
	}

	return SkillsListEntry{
		CWD:    *wire.CWD,
		Skills: skills,
		Errors: errorsInfo,
	}, true
}

func convertSkillMetadata(wire skillMetadataWire) (SkillMetadata, bool) {
	if wire.Name == nil || wire.Description == nil || wire.Path == nil ||
		wire.Scope == nil || wire.Enabled == nil || !validRuntimeAbsolutePath(*wire.Path) ||
		!validSkillScope(*wire.Scope) {
		return SkillMetadata{}, false
	}

	var skillInterface *SkillInterface
	if wire.Interface != nil {
		if (wire.Interface.IconSmall != nil && !validRuntimeAbsolutePath(*wire.Interface.IconSmall)) ||
			(wire.Interface.IconLarge != nil && !validRuntimeAbsolutePath(*wire.Interface.IconLarge)) {
			return SkillMetadata{}, false
		}
		skillInterface = &SkillInterface{
			DisplayName:      copyOptionalString(wire.Interface.DisplayName),
			ShortDescription: copyOptionalString(wire.Interface.ShortDescription),
			IconSmall:        copyOptionalString(wire.Interface.IconSmall),
			IconLarge:        copyOptionalString(wire.Interface.IconLarge),
			BrandColor:       copyOptionalString(wire.Interface.BrandColor),
			DefaultPrompt:    copyOptionalString(wire.Interface.DefaultPrompt),
		}
	}

	var dependencies *SkillDependencies
	if wire.Dependencies != nil {
		if wire.Dependencies.Tools == nil {
			return SkillMetadata{}, false
		}
		tools := make([]SkillToolDependency, len(*wire.Dependencies.Tools))
		for toolIndex, toolWire := range *wire.Dependencies.Tools {
			if toolWire.Type == nil || toolWire.Value == nil {
				return SkillMetadata{}, false
			}
			tools[toolIndex] = SkillToolDependency{
				Type:        *toolWire.Type,
				Value:       *toolWire.Value,
				Description: copyOptionalString(toolWire.Description),
				Transport:   copyOptionalString(toolWire.Transport),
				Command:     copyOptionalString(toolWire.Command),
				URL:         copyOptionalString(toolWire.URL),
			}
		}
		dependencies = &SkillDependencies{Tools: tools}
	}

	return SkillMetadata{
		Name:             *wire.Name,
		Description:      *wire.Description,
		ShortDescription: copyOptionalString(wire.ShortDescription),
		Interface:        skillInterface,
		Dependencies:     dependencies,
		Path:             *wire.Path,
		Scope:            *wire.Scope,
		Enabled:          *wire.Enabled,
	}, true
}

func validSkillScope(scope SkillScope) bool {
	switch scope {
	case SkillScopeUser, SkillScopeRepo, SkillScopeSystem, SkillScopeAdmin:
		return true
	default:
		return false
	}
}

func copyOptionalString(value *string) *string {
	if value == nil {
		return nil
	}
	copyValue := *value
	return &copyValue
}

// ListSkills returns the pinned Runtime's Skills projection. Paths remain in
// this Runtime adapter result and must not be logged or copied to public Host
// responses without an explicit projection.
func (m *Manager) ListSkills(
	ctx context.Context,
	cwds []string,
	forceReload bool,
) ([]SkillsListEntry, error) {
	for _, cwd := range cwds {
		if !validRuntimeAbsolutePath(cwd) {
			return nil, errors.New("Runtime skills cwd is invalid")
		}
	}
	params := skillsListParams{
		CWDs:        append([]string(nil), cwds...),
		ForceReload: forceReload,
	}
	var response skillsListResponse
	if err := m.request(ctx, RuntimeMethodSkillsList, params, &response); err != nil {
		return nil, err
	}
	return response.Data, nil
}

type skillsExtraRootsSetParams struct {
	ExtraRoots []string `json:"extraRoots"`
}

type skillsExtraRootsSetResponse struct{}

func (response *skillsExtraRootsSetResponse) UnmarshalJSON(raw []byte) error {
	var wire struct{}
	if !decodeStrictSkillsObject(raw, &wire) {
		return errors.New("skills/extraRoots/set response is invalid")
	}
	*response = skillsExtraRootsSetResponse{}
	return nil
}

// SetSkillsExtraRoots replaces the pinned Runtime's process-local extra Skill
// roots. An empty slice explicitly clears all roots.
func (m *Manager) SetSkillsExtraRoots(ctx context.Context, extraRoots []string) error {
	roots := make([]string, len(extraRoots))
	for index, root := range extraRoots {
		if !validRuntimeAbsolutePath(root) {
			return errors.New("Runtime skill extra root is invalid")
		}
		roots[index] = root
	}
	return m.request(
		ctx,
		RuntimeMethodSkillsExtraRootsSet,
		skillsExtraRootsSetParams{ExtraRoots: roots},
		&skillsExtraRootsSetResponse{},
	)
}

type skillsConfigWriteParams struct {
	Path    string  `json:"path"`
	Name    *string `json:"name"`
	Enabled bool    `json:"enabled"`
}

type skillsConfigWriteResponse struct {
	EffectiveEnabled bool
}

func (response *skillsConfigWriteResponse) UnmarshalJSON(raw []byte) error {
	var wire struct {
		EffectiveEnabled *bool `json:"effectiveEnabled"`
	}
	if !decodeStrictSkillsObject(raw, &wire) || wire.EffectiveEnabled == nil {
		return errors.New("skills/config/write response is invalid")
	}
	response.EffectiveEnabled = *wire.EffectiveEnabled
	return nil
}

// WriteSkillConfig sets enabled state by the exact absolute SKILL.md path.
// Name-based selection is intentionally not exposed by Agent Host because it
// is ambiguous across managed and non-managed roots.
func (m *Manager) WriteSkillConfig(ctx context.Context, skillPath string, enabled bool) (bool, error) {
	if !validRuntimeAbsolutePath(skillPath) || filepath.Base(skillPath) != "SKILL.md" {
		return false, errors.New("Runtime skill config path is invalid")
	}
	var response skillsConfigWriteResponse
	if err := m.request(
		ctx,
		RuntimeMethodSkillsConfigWrite,
		skillsConfigWriteParams{Path: skillPath, Enabled: enabled},
		&response,
	); err != nil {
		return false, err
	}
	if response.EffectiveEnabled != enabled {
		return false, errors.New("skills/config/write response is inconsistent")
	}
	return response.EffectiveEnabled, nil
}

// ValidateSkillsChangedNotification validates the pinned Runtime's empty
// skills/changed payload. Callers must not synchronously issue another Runtime
// request from the notification callback; the Runtime reader invokes that
// callback inline.
func ValidateSkillsChangedNotification(params json.RawMessage) error {
	var wire struct{}
	if !decodeStrictSkillsObject(params, &wire) {
		return errors.New("skills/changed notification is invalid")
	}
	return nil
}

func validRuntimeAbsolutePath(path string) bool {
	return path != "" && filepath.IsAbs(path) && filepath.Clean(path) == path
}

func decodeStrictSkillsObject(raw []byte, target any) bool {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return false
	}
	decoder := json.NewDecoder(bytes.NewReader(trimmed))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return false
	}
	var trailing any
	return errors.Is(decoder.Decode(&trailing), io.EOF)
}
