package skillmarket

import (
	"context"
	"encoding/json"
	"path/filepath"
	"reflect"

	native "github.com/36Dge/yijie-agent-host/internal/contracts/runtimeskillmarket"
	wire "github.com/36Dge/yijie-agent-host/internal/contracts/skillmarketv2"
	legacy "github.com/36Dge/yijie-agent-host/internal/skills"
	bolt "go.etcd.io/bbolt"
)

type ruleLayer struct {
	Name    map[string]any `json:"name"`
	Version string         `json:"version"`
	Rules   []any          `json:"rules"`
}
type ruleChange struct {
	Before, After []any
	BeforeVersion string
	Applied       bool
}
type migration struct {
	ID, Phase, OldRoot, OldPath, OldName, NativePath, NativeName string
	Source                                                       wire.CatalogSource
	DesiredEnabled, EffectiveEnabled, ToggleEvidence             bool
	NativeBefore                                                 *native.PluginState
	Layers                                                       []ruleLayer
	Rules                                                        *ruleChange
	Rollback                                                     bool
	RollbackPhase                                                string
}

// PrepareMigration owns no native side effect. The old service has already
// completed normal filesystem recovery; snapshots stay under its original ID.
func (s *Service) PrepareMigration(old *legacy.Service) error {
	s.legacy = old
	sources, err := old.ExportOrdinarySources()
	if err != nil {
		return err
	}
	for _, source := range sources {
		var existing migration
		if e := s.get("migrations", source.ID, &existing); e == nil {
			continue
		} else if ErrorCode(e) != "skill_not_found" {
			return e
		}
		var item entry
		if err = s.get("entries", source.ID, &item); err != nil {
			if ErrorCode(err) != "skill_not_found" {
				return err
			}
			// No current offer is required for a receipt-validated historical
			// installation. Its source is retained solely for use and rollback.
			item = entry{ID: source.ID, Origin: wire.OriginBuiltin, Selected: wire.CatalogSource{
				Id: source.ID, PluginName: "yj-builtin-" + digest([]byte(source.ID))[:24],
				DisplayName: source.DisplayName, Description: source.Description, Category: source.Category,
				IconKey: "skill", RequiredTools: []string{},
			}}
		}
		target := item.Selected
		target.ContentVersion = source.Version
		files := sourceFiles(source.Files)
		target = populateSource(target, files)
		if err = materializeSource(s.root(item.market()), target, files); err != nil {
			return err
		}
		record := migration{ID: source.ID, Phase: "prepared", OldRoot: source.Root, OldPath: source.Entrypoint, OldName: source.RuntimeName, DesiredEnabled: source.Enabled, ToggleEvidence: source.ToggleEvidence, Source: target}
		item.Selected = target
		item.Authority = "legacy"
		item.Published = true
		item.Listed = true
		err = s.db.Update(func(tx *bolt.Tx) error {
			if e := encodePut(tx, "migrations", source.ID, record); e != nil {
				return e
			}
			return encodePut(tx, "entries", item.ID, item)
		})
		if err != nil {
			return err
		}
	}
	if err = s.db.Sync(); err != nil {
		return err
	}
	if err = old.FreezeNativeJournal(filepath.Join(s.config.HostHome, "skill-market-v2/state.db")); err != nil {
		return err
	}
	old.SetNativeAuthority(s.NativeRoute)
	old.SetNativeProjection(s.LegacyProjection)
	return s.publishCatalog("yijie-builtin")
}
func (s *Service) migrations() ([]migration, error) {
	result := []migration{}
	err := s.db.View(func(tx *bolt.Tx) error {
		return tx.Bucket([]byte("migrations")).ForEach(func(_, raw []byte) error {
			var m migration
			if err := json.Unmarshal(raw, &m); err != nil {
				return err
			}
			result = append(result, m)
			return nil
		})
	})
	return result, err
}
func (s *Service) saveMigration(m migration) error {
	return s.db.Update(func(tx *bolt.Tx) error { return encodePut(tx, "migrations", m.ID, m) })
}

func (s *Service) Migrate(ctx context.Context, old *legacy.Service) error {
	s.serial.Lock()
	defer s.serial.Unlock()
	return s.config.Runtime.WithSkillMaintenance(ctx, func(ctx context.Context) error {
		if active, err := s.activeMaintenance(); err != nil {
			return err
		} else if active != nil {
			return s.continueMaintenance(ctx, old, active)
		}
		if s.compatible.Load() {
			return old.SyncLegacyRoots(ctx)
		}
		if err := s.requireNoLegacyToggleResidue(); err != nil {
			return err
		}
		records, err := s.migrations()
		if err != nil {
			return err
		}
		if err = old.SyncLegacyRoots(ctx); err != nil {
			return err
		}
		for _, m := range records {
			if m.Rollback {
				return fail("migration_rollback_pending")
			}
			if m.Phase != "complete" {
				if err = s.migrateOne(ctx, &m, old); err != nil {
					return err
				}
			}
		}
		return s.config.Runtime.ReloadSkillPluginConfig(ctx)
	})
}
func (s *Service) migrateOne(ctx context.Context, m *migration, old *legacy.Service) error {
	var e entry
	if err := s.get("entries", m.ID, &e); err != nil {
		return err
	}
	if m.NativeBefore == nil {
		state, err := s.readState(ctx, e)
		if err != nil {
			return err
		}
		if !absent(state) {
			return fail("migration_native_collision")
		}
		config, err := s.config.Runtime.ReadSkillPluginConfig(ctx, m.OldRoot)
		if err != nil {
			return err
		}
		layers, err := readRuleLayers(config)
		if err != nil {
			return err
		}
		// Scope cannot be silently widened: matching non-user policy needs a
		// separately owned layer adapter, so stop this object before installation.
		for _, layer := range layers {
			if layer.Name["type"] != "user" && rulesReference(layer.Rules, m.OldPath, m.OldName) {
				return fail("migration_rule_scope_unresolved")
			}
		}
		entries, err := s.config.Runtime.ListSkills(ctx, []string{m.OldRoot}, true)
		if err != nil {
			return err
		}
		found := false
		for _, entry := range entries {
			for _, skill := range entry.Skills {
				if skill.Path == m.OldPath && skill.Name == m.OldName {
					if found {
						return fail("migration_identity_ambiguous")
					}
					m.EffectiveEnabled = skill.Enabled
					found = true
				}
			}
		}
		if !found {
			return fail("migration_old_visibility_unresolved")
		}
		m.NativeBefore = &state
		m.Layers = layers
		if err = s.saveMigration(*m); err != nil {
			return err
		}
	}
	if m.Phase == "prepared" {
		detail, err := s.config.Runtime.ReadSkillPlugin(ctx, s.root(e.market()), e.Selected.PluginName)
		if err != nil {
			return err
		}
		if len(detail.Skills) != 1 || len(detail.McpServers) != 0 || len(detail.Hooks) != 0 {
			return fail("native_preflight_failed")
		}
		state, err := s.readState(ctx, e)
		if err != nil {
			return err
		}
		if absent(state) {
			if err = s.requireInstallationSlot(ctx, e.pluginID()); err != nil {
				return err
			}
			if err = s.config.Runtime.InstallSkillPlugin(ctx, s.root(e.market()), e.Selected.PluginName); err != nil {
				return err
			}
			state, err = s.readState(ctx, e)
			if err != nil {
				return err
			}
		}
		if state.CacheState != "valid" || state.CacheVersion == nil || *state.CacheVersion != e.Selected.NativePackageVersion || len(state.Skills) != 1 || state.Skills[0].Path == nil {
			return fail("migration_native_unconfirmed")
		}
		if err = s.config.Runtime.SetSkillPluginEnabled(ctx, e.pluginID(), m.DesiredEnabled); err != nil {
			return err
		}
		m.NativePath = string(*state.Skills[0].Path)
		m.NativeName = state.Skills[0].Name
		m.Phase = "native_verified"
		if err = s.saveMigration(*m); err != nil {
			return err
		}
	}
	if m.Phase == "native_verified" {
		if err := s.rebindMigrationRules(ctx, m, false); err != nil {
			return err
		}
		state, err := s.readState(ctx, e)
		if err != nil {
			return err
		}
		if len(state.Skills) != 1 || state.Skills[0].Enabled != m.EffectiveEnabled || state.EffectiveEnabled != m.DesiredEnabled {
			return fail("migration_effective_state_changed")
		}
		// Recheck in the original CWD. N2's user-layer state alone cannot prove
		// project or session policy preserved the effective skill selection.
		visible, err := s.config.Runtime.ListSkills(ctx, []string{m.OldRoot}, true)
		if err != nil {
			return err
		}
		enabled := false
		found := false
		for _, item := range visible {
			for _, skill := range item.Skills {
				if skill.Path == m.NativePath && skill.Name == m.NativeName {
					enabled = skill.Enabled
					found = true
				}
			}
		}
		if m.EffectiveEnabled && (!found || !enabled) || !m.EffectiveEnabled && enabled {
			return fail("migration_effective_state_changed")
		}
		m.Phase = "rules_verified"
		if err = s.saveMigration(*m); err != nil {
			return err
		}
	}
	if m.Phase == "rules_verified" {
		e.Authority = "native"
		m.Phase = "routing_committed"
		if err := s.db.Update(func(tx *bolt.Tx) error {
			if err := encodePut(tx, "entries", e.ID, e); err != nil {
				return err
			}
			return encodePut(tx, "migrations", m.ID, m)
		}); err != nil {
			return err
		}
	}
	if err := old.SyncLegacyRoots(ctx); err != nil {
		return err
	}
	if err := s.config.Runtime.ReloadSkillPluginConfig(ctx); err != nil {
		return err
	}
	m.Phase = "complete"
	return s.saveMigration(*m)
}

func readRuleLayers(config map[string]any) ([]ruleLayer, error) {
	raw, ok := config["layers"].([]any)
	if !ok {
		return nil, fail("native_config_layers_unavailable")
	}
	layers := []ruleLayer{}
	for _, value := range raw {
		layer, ok := value.(map[string]any)
		if !ok {
			return nil, fail("native_config_layers_unavailable")
		}
		name, _ := layer["name"].(map[string]any)
		version, _ := layer["version"].(string)
		cfg, _ := layer["config"].(map[string]any)
		skills, _ := cfg["skills"].(map[string]any)
		rules, exists := skills["config"]
		values := []any{}
		if exists {
			var ok bool
			values, ok = rules.([]any)
			if !ok {
				return nil, fail("native_config_rules_unavailable")
			}
		}
		layers = append(layers, ruleLayer{Name: name, Version: version, Rules: values})
	}
	return layers, nil
}
func rulesReference(rules []any, oldPath, oldName string) bool {
	for _, r := range rules {
		rule, ok := r.(map[string]any)
		if !ok {
			return true
		}
		if rule["path"] == oldPath || oldName != "" && rule["name"] == oldName {
			return true
		}
	}
	return false
}

// This adapter changes selectors, not native precedence or rule evaluation.
// Name rules retain their original target and gain an adjacent native target;
// this also preserves a same-name unmanaged Skill without guessing ownership.
func mapRules(rules []any, oldPath, oldName, newPath, newName string, removeHostToggle bool, desired bool) ([]any, error) {
	result := []any{}
	for _, raw := range rules {
		rule, ok := raw.(map[string]any)
		if !ok {
			return nil, fail("migration_rule_unresolved")
		}
		matchingPath := rule["path"] == oldPath
		matchingName := oldName != "" && newName != oldName && rule["name"] == oldName
		if !matchingPath && !matchingName {
			result = append(result, raw)
			continue
		}
		if matchingPath && removeHostToggle && rule["name"] == nil && rule["enabled"] == desired && len(rule) == 2 {
			continue
		}
		mapped := map[string]any{}
		for k, v := range rule {
			mapped[k] = v
		}
		if matchingPath {
			mapped["path"] = newPath
		}
		if matchingName {
			mapped["name"] = newName
		}
		if matchingName && !matchingPath {
			result = append(result, raw)
		}
		result = append(result, mapped)
	}
	return result, nil
}
func (s *Service) rebindMigrationRules(ctx context.Context, m *migration, rollback bool) error {
	config, err := s.config.Runtime.ReadSkillPluginConfig(ctx, m.OldRoot)
	if err != nil {
		return err
	}
	layers, err := readRuleLayers(config)
	if err != nil {
		return err
	}
	var user *ruleLayer
	for i := range layers {
		if layers[i].Name["type"] == "user" {
			user = &layers[i]
		}
	}
	if user == nil {
		return fail("native_config_layers_unavailable")
	}
	if m.Rules == nil {
		changed, err := mapRules(user.Rules, m.OldPath, m.OldName, m.NativePath, m.NativeName, m.ToggleEvidence, m.DesiredEnabled)
		if err != nil {
			return err
		}
		m.Rules = &ruleChange{Before: user.Rules, After: changed, BeforeVersion: user.Version}
		if err = s.saveMigration(*m); err != nil {
			return err
		}
	}
	from, to := m.Rules.Before, m.Rules.After
	if rollback {
		from, to = to, from
	}
	if reflect.DeepEqual(user.Rules, to) {
		m.Rules.Applied = !rollback
		return s.saveMigration(*m)
	}
	if !reflect.DeepEqual(user.Rules, from) {
		return fail("migration_config_conflict")
	}
	if err = s.config.Runtime.WriteSkillPluginConfig(ctx, []any{map[string]any{"keyPath": "skills.config", "mergeStrategy": "replace", "value": to}}, user.Version); err != nil {
		return err
	}
	m.Rules.Applied = !rollback
	return s.saveMigration(*m)
}

// RollbackMigration is only available on the compatible reader. It preserves
// imported sources and ordinary history, and reverses only evidenced edits.
func (s *Service) RollbackMigration(ctx context.Context, old *legacy.Service) error {
	return s.rollbackMigration(ctx, old)
}

func (s *Service) LegacyProjection(ctx context.Context, id string) (legacy.State, error) {
	var e entry
	if err := s.get("entries", id, &e); err != nil {
		return legacy.State{}, err
	}
	state, err := s.readState(ctx, e)
	if err != nil {
		return legacy.State{}, err
	}
	installation := "not_installed"
	switch installState(state) {
	case wire.InstallStateInstalled:
		installation = "installed"
	case wire.InstallStatePartial, wire.InstallStateInvalid:
		installation = "error"
	}
	visible := state.EffectiveEnabled && len(state.Skills) == 1 && state.Skills[0].Enabled && e.ActiveOperation == ""
	result := legacy.State{ID: id, Version: e.Selected.ContentVersion, InstallationStatus: installation, Enabled: state.EffectiveEnabled, RuntimeVisible: visible}
	if installation == "error" {
		result.FailureCode = string(legacy.CodeRuntimeSyncFailed)
	}
	return result, nil
}

func (s *Service) CompatibleMode() bool { return s.compatible.Load() }
func (s *Service) ResumeMigration(ctx context.Context, old *legacy.Service) error {
	return s.runMaintenance(ctx, old, "resume")
}
func (s *Service) rollbackMigration(ctx context.Context, old *legacy.Service) error {
	return s.runMaintenance(ctx, old, "rollback")
}
