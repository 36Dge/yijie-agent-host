package skillmarket

import (
	"context"
	"encoding/json"
	bolt "go.etcd.io/bbolt"
	"os"
	"path/filepath"
	"sort"

	"github.com/36Dge/yijie-agent-host/internal/codex"
	contract "github.com/36Dge/yijie-agent-host/internal/contracts"
	native "github.com/36Dge/yijie-agent-host/internal/contracts/runtimeskillmarket"
	wire "github.com/36Dge/yijie-agent-host/internal/contracts/skillmarketv2"
)

func (s *Service) Initialize(ctx context.Context) error {
	s.serial.Lock()
	defer s.serial.Unlock()
	config, err := s.config.Runtime.ReadSkillPluginConfig(ctx, "")
	if err != nil {
		return err
	}
	// Native marketplace/add records last_updated, even for an existing local
	// registration. Read the native user layer and only register missing roots;
	// reopening an App is not a marketplace edit or a new config generation.
	var registered map[string]any
	if layers, ok := config["layers"].([]any); ok {
		for _, raw := range layers {
			layer, _ := raw.(map[string]any)
			name, _ := layer["name"].(map[string]any)
			if name["type"] == "user" {
				user, _ := layer["config"].(map[string]any)
				registered, _ = user["marketplaces"].(map[string]any)
			}
		}
	}
	for _, market := range []string{"yijie-builtin", "yijie-user"} {
		if value, exists := registered[market]; exists {
			entry, ok := value.(map[string]any)
			if !ok || entry["source_type"] != "local" || entry["source"] != s.root(market) {
				return fail("marketplace_registration_conflict")
			}
			continue
		}
		if err := s.config.Runtime.RegisterSkillMarketplace(ctx, s.root(market)); err != nil {
			return err
		}
	}
	return s.config.Runtime.ReloadSkillPluginConfig(ctx)
}
func (s *Service) MarkReady() { s.ready.Store(true); s.signal() }
func (s *Service) Catalog(ctx context.Context) (wire.Catalog, error) {
	if !s.ready.Load() {
		if code, ok := s.startupErr.Load().(string); ok && code != "" {
			return wire.Catalog{}, fail(code)
		}
		return wire.Catalog{}, fail("skill_migration_pending")
	}
	return s.catalog(ctx)
}
func (s *Service) catalog(ctx context.Context) (wire.Catalog, error) {
	entries, err := s.entries()
	if err != nil {
		return wire.Catalog{}, err
	}
	inventory, err := s.config.Runtime.ReadSkillPluginStates(ctx, nil, []string{"yijie-builtin", "yijie-user"}, "")
	if err != nil {
		return wire.Catalog{}, err
	}
	states := map[string]native.PluginState{}
	for _, state := range inventory {
		states[state.PluginId] = state
	}
	visible, err := s.config.Runtime.ListSkills(ctx, []string{s.config.DataRoot}, true)
	if err != nil {
		return wire.Catalog{}, err
	}
	byPath := map[string]codex.SkillMetadata{}
	for _, item := range visible {
		for _, skill := range item.Skills {
			byPath[skill.Path] = skill
		}
	}
	needsMCP := false
	for _, e := range entries {
		needsMCP = needsMCP || len(e.MCPDependencies) > 0
	}
	var mcpInventory []native.McpServerStatus
	mcpComplete := false
	if needsMCP {
		mcpInventory, err = s.config.Runtime.ReadSkillMCPInventory(ctx)
		mcpComplete = err == nil
	}
	result := wire.Catalog{SchemaVersion: 2, Skills: []wire.Skill{}, Operations: []wire.PendingOperation{}}
	err = s.db.View(func(tx *bolt.Tx) error {
		return tx.Bucket([]byte("pending")).ForEach(func(key, _ []byte) error {
			raw := tx.Bucket([]byte("operations")).Get(key)
			var op operation
			if err := json.Unmarshal(raw, &op); err != nil {
				return err
			}
			if op.Result.State == wire.OperationStatePending || op.Result.State == wire.OperationStateNeedsAttention {
				result.Operations = append(result.Operations, wire.PendingOperation{OperationId: op.Result.OperationId, SkillId: op.Result.SkillId, Epoch: op.Result.Epoch, State: op.Result.State, ErrorCode: op.Result.ErrorCode})
			}
			return nil
		})
	})
	if err != nil {
		return wire.Catalog{}, err
	}
	var legacyIDs []string
	if s.get("meta", "legacy-recovered-requests", &legacyIDs) == nil && len(legacyIDs) > 0 {
		notice := "上次未确认的技能操作已保留为失败记录，请按当前状态重新操作。"
		result.Notice = &notice
	}
	known := map[string]bool{}
	for _, entry := range entries {
		id := entry.pluginID()
		known[id] = true
		state := states[id]
		state.PluginId = id
		if s.compatible.Load() && entry.Authority == "legacy" && s.legacy != nil {
			old, err := s.legacy.InspectOrdinary(ctx, entry.ID)
			if err != nil {
				return wire.Catalog{}, err
			}
			value := projectSkill(entry, state, byPath)
			value.Version = old.Version
			value.RuntimeName = old.RuntimeName
			value.Enabled = old.Enabled
			value.RuntimeVisible = old.RuntimeVisible
			value.InstallationState = wire.InstallState(old.InstallationStatus)
			value.FailureCode = "compatibility_mode"
			result.Skills = append(result.Skills, value)
			continue
		}
		if !entry.Published {
			continue
		}
		if entry.Offer == nil && entry.ActiveOperation == "" && state.CacheState != "valid" && state.CacheState != "invalid" && !state.UserConfigPresent {
			continue
		}
		projected := projectSkill(entry, state, byPath)
		if state.CacheState == "valid" && !s.sourcePresent(entry) {
			projected.FailureCode = "source_unavailable"
		}
		projected.DependencyState = dependencyState(projected.RequiredTools, entry.MCPDependencies, mcpInventory, mcpComplete)
		result.Skills = append(result.Skills, projected)
	}
	for _, state := range inventory {
		if known[state.PluginId] {
			continue
		}
		result.Skills = append(result.Skills, wire.Skill{Id: orphanID(state.PluginId), Version: "unresolved", DisplayName: "来源待确认的技能", Description: "保留现有安装，可停用或移除。", Category: "personal", Origin: wire.OriginUnknown, InstallationState: installState(state), Enabled: state.EffectiveEnabled, Retired: true, DependencyState: wire.DependencyStateUnknown, RequiredTools: []string{}, FailureCode: "source_unavailable"})
	}
	sort.SliceStable(result.Skills, func(i, j int) bool {
		a, b := result.Skills[i], result.Skills[j]
		if a.Origin != b.Origin {
			return a.Origin < b.Origin
		}
		if a.Order != b.Order {
			return a.Order < b.Order
		}
		return a.Id < b.Id
	})
	if s.compatible.Load() {
		for i := range result.Skills {
			result.Skills[i].FailureCode = "compatibility_mode"
		}
	}
	result.Revision = digest(canonical(result.Skills))
	return result, result.Validate()
}
func orphanID(id string) string { return "unresolved-" + digest([]byte(id)) }
func installState(state native.PluginState) wire.InstallState {
	if state.CacheState == "invalid" || state.CacheState == "valid" && !state.ManifestValid {
		return wire.InstallStateInvalid
	}
	if state.CacheState == "valid" && state.UserConfigPresent {
		return wire.InstallStateInstalled
	}
	if state.CacheState == "valid" || state.UserConfigPresent {
		return wire.InstallStatePartial
	}
	return wire.InstallStateNotInstalled
}
func projectSkill(e entry, state native.PluginState, visible map[string]codex.SkillMetadata) wire.Skill {
	source := e.Selected
	if state.CacheState == "absent" && e.Offer != nil {
		source = *e.Offer
	}
	value := wire.Skill{Id: e.ID, Version: source.ContentVersion, DisplayName: source.DisplayName, Description: source.Description, Category: source.Category, Order: source.Order, IconKey: source.IconKey, Origin: e.Origin, InstallationState: installState(state), Enabled: state.EffectiveEnabled, Retired: e.Origin == wire.OriginBuiltin && e.Offer == nil, DependencyState: wire.DependencyStateReady, RequiredTools: append([]string{}, source.RequiredTools...), Epoch: e.Epoch}
	if e.Origin == wire.OriginUser {
		if e.DisplayName != "" {
			value.DisplayName = e.DisplayName
		}
		if e.Description != "" {
			value.Description = e.Description
		}
	}
	value.RequiredTools = append(value.RequiredTools, e.DeclaredTools...)
	if len(value.RequiredTools) > 0 {
		value.DependencyState = wire.DependencyStateUnknown
	}
	if state.CacheState == "valid" && len(state.Skills) == 1 {
		skill := state.Skills[0]
		value.RuntimeName = skill.Name
		if skill.Path != nil {
			actual, ok := visible[string(*skill.Path)]
			value.RuntimeVisible = ok && actual.Enabled && actual.Name == skill.Name && state.EffectiveEnabled
			if actual.Dependencies != nil && len(actual.Dependencies.Tools) > 0 {
				value.DependencyState = wire.DependencyStateUnknown
				for _, tool := range actual.Dependencies.Tools {
					value.RequiredTools = append(value.RequiredTools, tool.Value)
				}
			}
		}
	}
	tools := []string{}
	seen := map[string]bool{}
	for _, tool := range value.RequiredTools {
		if !seen[tool] && len(tools) < 128 {
			tools = append(tools, tool)
			seen[tool] = true
		}
	}
	value.RequiredTools = tools
	if e.ActiveOperation != "" {
		value.RuntimeVisible = false
		value.FailureCode = "operation_pending"
	}
	if len(state.Errors) > 0 || value.InstallationState == wire.InstallStateInvalid || value.InstallationState == wire.InstallStatePartial {
		value.FailureCode = "reconciliation_required"
	}
	if state.CacheVersion != nil && *state.CacheVersion != e.Selected.NativePackageVersion {
		value.RuntimeVisible = false
		value.FailureCode = "source_version_unresolved"
	}
	return value
}

type cwdContextKey struct{}

func WithWorkingDirectory(ctx context.Context, cwd string) context.Context {
	return context.WithValue(ctx, cwdContextKey{}, cwd)
}
func (s *Service) ResolveSelectedSkills(ctx context.Context, refs []contract.SelectedSkillRef) ([]codex.UserInput, error) {
	if !s.ready.Load() {
		return nil, fail("skill_migration_pending")
	}
	cwd, _ := ctx.Value(cwdContextKey{}).(string)
	if cwd == "" {
		return nil, fail("skill_workspace_missing")
	}
	entries, err := s.config.Runtime.ListSkills(ctx, []string{cwd}, true)
	if err != nil {
		return nil, err
	}
	byPath := map[string]codex.SkillMetadata{}
	for _, e := range entries {
		if e.CWD != cwd {
			continue
		}
		for _, skill := range e.Skills {
			byPath[skill.Path] = skill
		}
	}
	result := []codex.UserInput{}
	seen := map[string]bool{}
	for _, ref := range refs {
		if seen[ref.Id] {
			return nil, fail("skill_selection_invalid")
		}
		seen[ref.Id] = true
		var e entry
		if err = s.get("entries", ref.Id, &e); err != nil {
			return nil, err
		}
		if s.compatible.Load() && e.Authority == "legacy" && s.legacy != nil {
			values, err := s.legacy.ResolveSelectedSkillsInDirectory(ctx, []contract.SelectedSkillRef{ref}, cwd)
			if err != nil {
				return nil, err
			}
			result = append(result, values...)
			continue
		}
		if e.Authority != "native" || !e.Published || e.ActiveOperation != "" || e.Selected.ContentVersion != ref.Version {
			return nil, fail("skill_version_unavailable")
		}
		states, err := s.config.Runtime.ReadSkillPluginStates(ctx, []string{e.pluginID()}, nil, cwd)
		if err != nil {
			return nil, err
		}
		if len(states) != 1 || !states[0].EffectiveEnabled || states[0].CacheState != "valid" || !states[0].UserConfigPresent || states[0].CacheVersion == nil || *states[0].CacheVersion != e.Selected.NativePackageVersion || len(states[0].Skills) != 1 {
			return nil, fail("skill_selection_unavailable")
		}
		skill := states[0].Skills[0]
		if skill.Path == nil {
			return nil, fail("skill_selection_unavailable")
		}
		actual, ok := byPath[string(*skill.Path)]
		if !ok || !actual.Enabled || actual.Name != skill.Name {
			return nil, fail("skill_selection_unavailable")
		}
		input, err := codex.SkillUserInput(actual.Name, actual.Path)
		if err != nil {
			return nil, err
		}
		result = append(result, input)
	}
	return result, nil
}

// Exported for migration evidence; it is private state, never an HTTP response.
func (s *Service) NativeRoute(id string) bool {
	var e entry
	// A failed private read never grants the legacy writer authority. The
	// legacy adapter separately excludes its known expert resources.
	if s.get("entries", id, &e) != nil {
		return true
	}
	return e.Authority != "legacy"
}
func (s *Service) Operation(ctx context.Context, id string) (wire.Operation, error) {
	var op operation
	if err := s.get("operations", id, &op); err != nil {
		return wire.Operation{}, err
	}
	return op.Result, nil
}
func (s *Service) operationReplayed(id, fingerprint string) (wire.Operation, bool, error) {
	var op operation
	err := s.get("operations", id, &op)
	if err != nil {
		if ErrorCode(err) == "skill_not_found" {
			return wire.Operation{}, false, nil
		}
		return wire.Operation{}, false, err
	}
	if op.Fingerprint != fingerprint {
		return wire.Operation{}, true, fail("operation_conflict")
	}
	return op.Result, true, nil
}
func operationFingerprint(v any) string { raw, _ := json.Marshal(v); return digest(raw) }

func (s *Service) ResolveSelectedSkillsInDirectory(ctx context.Context, refs []contract.SelectedSkillRef, cwd string) ([]codex.UserInput, error) {
	return s.ResolveSelectedSkills(WithWorkingDirectory(ctx, cwd), refs)
}

func (s *Service) SetStartupError(err error) { s.startupErr.Store(ErrorCode(err)) }

func (s *Service) RecordLegacyRecovery(ids []string) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		var previous []string
		if raw := tx.Bucket([]byte("meta")).Get([]byte("legacy-recovered-requests")); raw != nil {
			if err := json.Unmarshal(raw, &previous); err != nil {
				return err
			}
		}
		seen := map[string]bool{}
		for _, id := range previous {
			seen[id] = true
		}
		for _, id := range ids {
			if !seen[id] {
				previous = append(previous, id)
				seen[id] = true
			}
		}
		return encodePut(tx, "meta", "legacy-recovered-requests", previous)
	})
}

func (s *Service) sourcePresent(e entry) bool {
	root := filepath.Join(s.root(e.market()), "sources", e.Selected.PluginName, e.Selected.SourceRevision)
	for _, name := range []string{".codex-plugin/plugin.json", "skills/main/SKILL.md"} {
		info, err := os.Lstat(filepath.Join(root, name))
		if err != nil || !info.Mode().IsRegular() {
			return false
		}
	}
	return true
}
