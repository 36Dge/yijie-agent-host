package skillmarket

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/36Dge/yijie-agent-host/internal/codex"
	legacy "github.com/36Dge/yijie-agent-host/internal/skills"
	"github.com/google/uuid"
	bolt "go.etcd.io/bbolt"
)

// Version 2 is a Host recovery journal, not another installer or rule engine.
// Each cycle owns its own snapshot; completion is the only mode commit point.
type maintenance struct {
	ID, Kind, Phase, SourceCycle, ConfigVersion string
	Toggles                                     []compatibilityToggle
}

func (s *Service) activeMaintenance() (*maintenance, error) {
	var id string
	if err := s.get("meta", "active-maintenance", &id); ErrorCode(err) == "skill_not_found" {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	if id == "" {
		return nil, nil
	}
	var m maintenance
	if err := s.get("maintenance", id, &m); err != nil {
		return nil, err
	}
	if m.ID != id || m.Phase == "complete" || (m.Kind != "resume" && m.Kind != "rollback") {
		return nil, fail("maintenance_journal_invalid")
	}
	return &m, nil
}
func (s *Service) saveMaintenance(m *maintenance) error {
	return s.db.Update(func(tx *bolt.Tx) error { return encodePut(tx, "maintenance", m.ID, m) })
}

// Export uses the original receipt and byte verifier. Missing items are errors
// here, even though discovery correctly omits uninstalled legacy catalog rows.
func (s *Service) verifyRollbackSources(old *legacy.Service, records []migration) error {
	sources, err := old.ExportOrdinarySources()
	if err != nil {
		return err
	}
	byID := map[string]legacy.LegacySkillSource{}
	for _, source := range sources {
		byID[source.ID] = source
	}
	for _, m := range records {
		source, ok := byID[m.ID]
		if !ok || source.Root != m.OldRoot || source.Entrypoint != m.OldPath || source.RuntimeName != m.OldName || source.Version != m.Source.ContentVersion || source.Enabled != m.DesiredEnabled || contentDigest(sourceFiles(source.Files)) != m.Source.ContentSha256 {
			return fail("migration_old_source_unavailable")
		}
	}
	return nil
}
func (s *Service) verifyLegacyVisible(ctx context.Context, old *legacy.Service, records []migration) error {
	if err := s.verifyRollbackSources(old, records); err != nil {
		return err
	}
	for _, m := range records {
		entries, err := s.config.Runtime.ListSkills(ctx, []string{m.OldRoot}, true)
		if err != nil {
			return err
		}
		count := 0
		for _, group := range entries {
			for _, skill := range group.Skills {
				if skill.Path == m.OldPath && skill.Name == m.OldName {
					count++
					// A prepared source with no native-before snapshot has never
					// crossed migration. Do not interpret its zero-value bool as
					// a historical opt-out; receipt/rules are already unchanged.
					if m.NativeBefore != nil && skill.Enabled != m.EffectiveEnabled {
						return fail("migration_old_visibility_unresolved")
					}
				}
			}
		}
		if count != 1 {
			return fail("migration_old_visibility_unresolved")
		}
	}
	return nil
}

func (s *Service) runMaintenance(ctx context.Context, old *legacy.Service, kind string) error {
	s.serial.Lock()
	defer s.serial.Unlock()
	return s.config.Runtime.WithSkillMaintenance(ctx, func(ctx context.Context) error {
		active, err := s.activeMaintenance()
		if err != nil {
			return err
		}
		if active != nil {
			if active.Kind != kind {
				return fail("maintenance_operation_pending")
			}
			return s.continueMaintenance(ctx, old, active)
		}
		if kind == "rollback" && s.compatible.Load() {
			records, err := s.migrations()
			if err != nil {
				return err
			}
			return s.verifyLegacyVisible(ctx, old, records)
		}
		if kind == "resume" && !s.compatible.Load() {
			return s.requireNoLegacyToggleResidue()
		}
		m, err := s.prepareMaintenance(ctx, old, kind)
		if err != nil {
			return err
		}
		return s.continueMaintenance(ctx, old, m)
	})
}

// Called under the shared native maintenance admission and the Host serial lock.
// All-object validation precedes the durable intent and every side effect.
func (s *Service) prepareMaintenance(ctx context.Context, old *legacy.Service, kind string) (*maintenance, error) {
	if err := s.requireResolvedOperations(); err != nil {
		return nil, err
	}
	records, err := s.migrations()
	if err != nil {
		return nil, err
	}
	if err = s.verifyRollbackSources(old, records); err != nil {
		return nil, err
	}
	m := &maintenance{ID: uuid.NewString(), Kind: kind, Toggles: []compatibilityToggle{}}
	if kind == "rollback" {
		if err = s.requireNoLegacyToggleResidue(); err != nil {
			return nil, err
		}
		for _, item := range records {
			if err = s.checkRollbackIntent(ctx, item); err != nil {
				return nil, err
			}
		}
		edits, e := s.rollbackRules(records)
		if e != nil {
			return nil, e
		}
		if err = s.reverseRules(ctx, edits, false); err != nil {
			return nil, err
		}
		migrated := map[string]bool{}
		for _, item := range records {
			migrated[item.ID] = true
		}
		entries, e := s.entries()
		if e != nil {
			return nil, e
		}
		for _, item := range entries {
			if migrated[item.ID] || item.Authority != "native" {
				continue
			}
			state, e := s.readState(ctx, item)
			if e != nil {
				return nil, e
			}
			if absent(state) {
				continue
			}
			if state.CacheState != "valid" || !state.UserConfigPresent || state.UserEnabled == nil || state.CacheVersion == nil {
				return nil, fail("reconciliation_required")
			}
			m.Toggles = append(m.Toggles, compatibilityToggle{ID: item.ID, Epoch: item.Epoch, Version: *state.CacheVersion, WasEnabled: *state.UserEnabled})
		}
		m.Phase = "rollback_rules"
	} else {
		cycle, e := s.compatibilityCycle()
		if e != nil {
			return nil, e
		}
		config, e := s.config.Runtime.ReadSkillPluginConfig(ctx, "")
		if e != nil {
			return nil, e
		}
		actual, e := codex.SkillUserConfigVersion(config)
		if e != nil {
			return nil, e
		}
		if actual != cycle.ConfigVersion {
			return nil, fail("migration_config_conflict")
		}
		for _, change := range cycle.Toggles {
			if err = s.verifyCompatibilityToggle(ctx, change, false); err != nil {
				return nil, err
			}
		}
		m.SourceCycle, m.Toggles, m.Phase = cycle.ID, cycle.Toggles, "resume_migrate"
		for i := range m.Toggles {
			m.Toggles[i].Restored = false
		}
	}
	err = s.db.Update(func(tx *bolt.Tx) error {
		for _, item := range records {
			if kind == "rollback" {
				item.Rollback = true
			} else if item.Rollback {
				item.Rollback, item.RollbackPhase, item.Phase = false, "", "prepared"
				item.NativeBefore, item.Rules, item.NativePath, item.NativeName = nil, nil, "", ""
			}
			if err := encodePut(tx, "migrations", item.ID, item); err != nil {
				return err
			}
		}
		if err := encodePut(tx, "maintenance", m.ID, m); err != nil {
			return err
		}
		return encodePut(tx, "meta", "active-maintenance", m.ID)
	})
	if err != nil {
		return nil, err
	}
	s.ready.Store(false)
	s.config.Runtime.SetSkillSubmissionReady(false)
	return m, nil
}

func (s *Service) continueMaintenance(ctx context.Context, old *legacy.Service, m *maintenance) error {
	s.ready.Store(false)
	s.config.Runtime.SetSkillSubmissionReady(false)
	for m.Phase != "complete" {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := s.advanceMaintenance(ctx, old, m); err != nil {
			return err
		}
	}
	return nil
}

// One persisted checkpoint per call permits normal stop/reopen recovery.
func (s *Service) advanceMaintenance(ctx context.Context, old *legacy.Service, m *maintenance) error {
	records, err := s.migrations()
	if err != nil {
		return err
	}
	switch m.Phase {
	case "rollback_rules":
		if err = s.verifyRollbackSources(old, records); err != nil {
			return err
		}
		for _, change := range m.Toggles {
			if err = s.verifyCompatibilityToggle(ctx, change, change.WasEnabled); err != nil {
				return err
			}
		}
		edits, e := s.rollbackRules(records)
		if e != nil {
			return e
		}
		if err = s.reverseRules(ctx, edits, true); err != nil {
			return err
		}
		m.Phase = "rollback_uninstall"
	case "rollback_uninstall":
		if err = s.verifyRollbackSources(old, records); err != nil {
			return err
		}
		for i := len(records) - 1; i >= 0; i-- {
			item := records[i]
			if item.Phase == "rolled_back" {
				continue
			}
			if err = s.checkRollbackIntent(ctx, item); err != nil {
				return err
			}
			var e entry
			if err = s.get("entries", item.ID, &e); err != nil {
				return err
			}
			state, readErr := s.readState(ctx, e)
			if readErr != nil {
				return readErr
			}
			if !absent(state) {
				item.RollbackPhase = "native_uninstall_pending"
				if err = s.saveMigration(item); err != nil {
					return err
				}
				if err = s.config.Runtime.UninstallSkillPlugin(ctx, e.pluginID()); err != nil {
					return err
				}
				state, err = s.readState(ctx, e)
				if err != nil {
					return err
				}
			}
			if !absent(state) {
				return fail("migration_rollback_unconfirmed")
			}
			e.Authority, item.Phase, item.RollbackPhase = "legacy", "rolled_back", "complete"
			return s.db.Update(func(tx *bolt.Tx) error {
				if err := encodePut(tx, "entries", e.ID, e); err != nil {
					return err
				}
				return encodePut(tx, "migrations", item.ID, item)
			})
		}
		m.Phase = "rollback_disable"
	case "rollback_disable":
		for i := range m.Toggles {
			if m.Toggles[i].Applied {
				continue
			}
			if err = s.applyCompatibilityToggle(ctx, &m.Toggles[i], false); err != nil {
				return err
			}
			return s.saveMaintenance(m)
		}
		m.Phase = "rollback_verify"
	case "rollback_verify":
		if err = old.SyncLegacyRoots(ctx); err != nil {
			return err
		}
		if err = s.config.Runtime.ReloadSkillPluginConfig(ctx); err != nil {
			return err
		}
		if err = s.verifyLegacyVisible(ctx, old, records); err != nil {
			return err
		}
		for _, change := range m.Toggles {
			if err = s.verifyCompatibilityToggle(ctx, change, false); err != nil {
				return err
			}
		}
		config, e := s.config.Runtime.ReadSkillPluginConfig(ctx, "")
		if e != nil {
			return e
		}
		m.ConfigVersion, err = codex.SkillUserConfigVersion(config)
		if err != nil {
			return err
		}
		return s.finishMaintenance(m, true)
	case "resume_migrate":
		if err = old.SyncLegacyRoots(ctx); err != nil {
			return err
		}
		for _, item := range records {
			if item.Phase != "complete" {
				return s.migrateOne(ctx, &item, old)
			}
		}
		m.Phase = "resume_restore"
	case "resume_restore":
		for i := range m.Toggles {
			if m.Toggles[i].Restored {
				continue
			}
			if err = s.applyCompatibilityToggle(ctx, &m.Toggles[i], true); err != nil {
				return err
			}
			return s.saveMaintenance(m)
		}
		m.Phase = "resume_verify"
	case "resume_verify":
		if err = old.SyncLegacyRoots(ctx); err != nil {
			return err
		}
		if err = s.config.Runtime.ReloadSkillPluginConfig(ctx); err != nil {
			return err
		}
		for _, change := range m.Toggles {
			if err = s.verifyCompatibilityToggle(ctx, change, change.WasEnabled); err != nil {
				return err
			}
		}
		for _, item := range records {
			if item.Phase != "complete" || item.Rollback {
				return fail("migration_native_unconfirmed")
			}
		}
		return s.finishMaintenance(m, false)
	default:
		return fail("maintenance_journal_invalid")
	}
	return s.saveMaintenance(m)
}
func (s *Service) finishMaintenance(m *maintenance, compatible bool) error {
	m.Phase = "complete"
	mode := "native"
	if compatible {
		mode = "compatible_legacy"
	}
	err := s.db.Update(func(tx *bolt.Tx) error {
		if err := encodePut(tx, "maintenance", m.ID, m); err != nil {
			return err
		}
		if compatible {
			if err := encodePut(tx, "meta", "compatible-cycle", m.ID); err != nil {
				return err
			}
			if err := encodePut(tx, "meta", "compatible-config-version", m.ConfigVersion); err != nil {
				return err
			}
		}
		if err := tx.Bucket([]byte("meta")).Put([]byte("mode"), []byte(mode)); err != nil {
			return err
		}
		return encodePut(tx, "meta", "active-maintenance", "")
	})
	if err == nil {
		s.compatible.Store(compatible)
	}
	return err
}

// Import only a proven schema-1 completed compatible cycle. Native-mode partial
// residue has no complete ownership proof and must not be guessed or replayed.
func (s *Service) oldCompatibilityToggles() ([]compatibilityToggle, error) {
	result := []compatibilityToggle{}
	err := s.db.View(func(tx *bolt.Tx) error {
		return tx.Bucket([]byte("meta")).ForEach(func(key, raw []byte) error {
			if !strings.HasPrefix(string(key), "compatible-disabled:") {
				return nil
			}
			var v compatibilityToggle
			if err := json.Unmarshal(raw, &v); err != nil {
				return err
			}
			result = append(result, v)
			return nil
		})
	})
	return result, err
}
func (s *Service) requireNoLegacyToggleResidue() error {
	var adopted bool
	if s.get("meta", "schema1-toggles-adopted", &adopted) == nil && adopted {
		return nil
	}
	changes, err := s.oldCompatibilityToggles()
	if err != nil {
		return err
	}
	for _, v := range changes {
		if v.Applied {
			return fail("legacy_maintenance_review_required")
		}
	}
	return nil
}
func (s *Service) compatibilityCycle() (maintenance, error) {
	var id string
	if err := s.get("meta", "compatible-cycle", &id); err == nil {
		var m maintenance
		err = s.get("maintenance", id, &m)
		if err == nil && (m.Kind != "rollback" || m.Phase != "complete") {
			err = fail("maintenance_journal_invalid")
		}
		return m, err
	} else if ErrorCode(err) != "skill_not_found" {
		return maintenance{}, err
	}
	changes, err := s.oldCompatibilityToggles()
	if err != nil {
		return maintenance{}, err
	}
	m := maintenance{ID: uuid.NewString(), Kind: "rollback", Phase: "complete", Toggles: changes}
	if err = s.get("meta", "compatible-config-version", &m.ConfigVersion); err != nil {
		return m, err
	}
	for _, v := range changes {
		if v.WasEnabled && !v.Applied {
			return m, fail("legacy_maintenance_review_required")
		}
	}
	err = s.db.Update(func(tx *bolt.Tx) error {
		if err := encodePut(tx, "maintenance", m.ID, m); err != nil {
			return err
		}
		if err := encodePut(tx, "meta", "compatible-cycle", m.ID); err != nil {
			return err
		}
		return encodePut(tx, "meta", "schema1-toggles-adopted", true)
	})
	return m, err
}
