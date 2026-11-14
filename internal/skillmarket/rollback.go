package skillmarket

import (
	"context"
	"encoding/json"
	"reflect"
	"sort"

	wire "github.com/36Dge/yijie-agent-host/internal/contracts/skillmarketv2"
	bolt "go.etcd.io/bbolt"
)

// Worker scheduling excludes needs_attention. Maintenance admission must instead
// examine the complete durable unresolved index, without changing readiness.
func (s *Service) requireResolvedOperations() error {
	return s.db.View(func(tx *bolt.Tx) error {
		if tx.Bucket([]byte("pending")).Stats().KeyN != 0 {
			return fail("skill_operations_pending")
		}
		return nil
	})
}

func (s *Service) checkRollbackIntent(ctx context.Context, m migration) error {
	var e entry
	if err := s.get("entries", m.ID, &e); err != nil {
		return err
	}
	state, err := s.readState(ctx, e)
	if err != nil {
		return err
	}
	if absent(state) {
		// Absence is ours only before the original install or after a journaled
		// rollback uninstall. A later user uninstall must never revive the old root.
		if m.Phase == "rolled_back" || m.RollbackPhase == "native_uninstall_pending" || m.Phase == "prepared" && (m.NativeBefore == nil || absent(*m.NativeBefore)) {
			return nil
		}
		return fail("migration_user_intent_changed")
	}
	if state.UserEnabled == nil || *state.UserEnabled != m.DesiredEnabled || state.CacheVersion == nil || *state.CacheVersion != e.Selected.NativePackageVersion {
		return fail("migration_user_intent_changed")
	}
	if m.NativeBefore == nil || !absent(*m.NativeBefore) {
		return fail("migration_rollback_unowned")
	}
	return nil
}

// A rule edit is a native user-layer CAS, not a second rule evaluator. All
// refresh edits happened after the migration batch. Reverse them globally before
// reversing migrations, so one Skill never restores another Skill's stale path.
type ruleReversal struct {
	change    *ruleChange
	op        *operation
	migration *migration
}

func (s *Service) rollbackRules(records []migration) ([]ruleReversal, error) {
	ids := map[string]bool{}
	for _, m := range records {
		ids[m.ID] = true
	}
	operations := []operation{}
	err := s.db.View(func(tx *bolt.Tx) error {
		return tx.Bucket([]byte("operations")).ForEach(func(_, raw []byte) error {
			var op operation
			if err := json.Unmarshal(raw, &op); err != nil {
				return err
			}
			if ids[op.Result.SkillId] && op.Result.State == wire.OperationStateComplete && op.Rules != nil && op.Rules.Applied {
				operations = append(operations, op)
			}
			return nil
		})
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(operations, func(i, j int) bool { return operations[i].CreatedAt.After(operations[j].CreatedAt) })
	result := []ruleReversal{}
	for i := range operations {
		op := &operations[i]
		result = append(result, ruleReversal{change: op.Rules, op: op})
	}
	for i := len(records) - 1; i >= 0; i-- {
		m := &records[i]
		if m.Rules != nil && m.Rules.Applied {
			result = append(result, ruleReversal{change: m.Rules, migration: m})
		}
	}
	return result, nil
}

func (s *Service) userRuleLayer(ctx context.Context) (ruleLayer, error) {
	config, err := s.config.Runtime.ReadSkillPluginConfig(ctx, s.config.DataRoot)
	if err != nil {
		return ruleLayer{}, err
	}
	layers, err := readRuleLayers(config)
	if err != nil {
		return ruleLayer{}, err
	}
	for _, layer := range layers {
		if layer.Name["type"] == "user" {
			return layer, nil
		}
	}
	return ruleLayer{}, fail("native_config_layers_unavailable")
}

func (s *Service) reverseRules(ctx context.Context, edits []ruleReversal, apply bool) error {
	layer, err := s.userRuleLayer(ctx)
	if err != nil {
		return err
	}
	// Preflight the complete inverse chain before the first write.
	rules := layer.Rules
	for _, edit := range edits {
		if !reflect.DeepEqual(rules, edit.change.Before) && !reflect.DeepEqual(rules, edit.change.After) {
			return fail("migration_config_conflict")
		}
		rules = edit.change.Before
	}
	if !apply {
		return nil
	}
	for _, edit := range edits {
		layer, err = s.userRuleLayer(ctx)
		if err != nil {
			return err
		}
		if !reflect.DeepEqual(layer.Rules, edit.change.Before) {
			if !reflect.DeepEqual(layer.Rules, edit.change.After) {
				return fail("migration_config_conflict")
			}
			if err = s.config.Runtime.WriteSkillPluginConfig(ctx, []any{map[string]any{"keyPath": "skills.config", "mergeStrategy": "replace", "value": edit.change.Before}}, layer.Version); err != nil {
				return err
			}
		}
		edit.change.Applied = false
		if edit.op != nil {
			err = s.saveOperation(edit.op)
		} else {
			err = s.saveMigration(*edit.migration)
		}
		if err != nil {
			return err
		}
	}
	return nil
}
