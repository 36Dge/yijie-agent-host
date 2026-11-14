package skillmarket

import (
	"context"
	"reflect"

	native "github.com/36Dge/yijie-agent-host/internal/contracts/runtimeskillmarket"
)

// Source updates change native cache paths. Read layered rules before refresh,
// then map exact selectors to the path returned by N2, under the same admission
// barrier and using native config CAS. No cache layout is inferred here.
func (s *Service) prepareRefreshRules(ctx context.Context, op *operation) error {
	if op.Before == nil || len(op.Before.Skills) != 1 || op.Before.Skills[0].Path == nil {
		return fail("refresh_identity_unresolved")
	}
	config, err := s.config.Runtime.ReadSkillPluginConfig(ctx, s.config.DataRoot)
	if err != nil {
		return err
	}
	layers, err := readRuleLayers(config)
	if err != nil {
		return err
	}
	old := op.Before.Skills[0]
	for _, layer := range layers {
		if layer.Name["type"] != "user" && rulesReference(layer.Rules, string(*old.Path), old.Name) {
			return fail("refresh_rule_scope_unresolved")
		}
	}
	return nil
}

func (s *Service) applyRefreshRules(ctx context.Context, op *operation, state native.PluginState, rollback bool) error {
	if op.Before == nil || len(op.Before.Skills) != 1 || op.Before.Skills[0].Path == nil || len(state.Skills) != 1 || state.Skills[0].Path == nil {
		return fail("refresh_identity_unresolved")
	}
	old, next := op.Before.Skills[0], state.Skills[0]
	config, err := s.config.Runtime.ReadSkillPluginConfig(ctx, s.config.DataRoot)
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
	if op.Rules == nil {
		changed, err := mapRules(user.Rules, string(*old.Path), old.Name, string(*next.Path), next.Name, false, op.DesiredEnabled)
		if err != nil {
			return err
		}
		op.Rules = &ruleChange{Before: user.Rules, After: changed, BeforeVersion: user.Version}
		if err = s.saveOperation(op); err != nil {
			return err
		}
	}
	from, to := op.Rules.Before, op.Rules.After
	if rollback {
		from, to = to, from
	}
	if !reflect.DeepEqual(user.Rules, to) {
		if !reflect.DeepEqual(user.Rules, from) {
			return fail("refresh_config_conflict")
		}
		if err = s.config.Runtime.WriteSkillPluginConfig(ctx, []any{map[string]any{"keyPath": "skills.config", "mergeStrategy": "replace", "value": to}}, user.Version); err != nil {
			return err
		}
	}
	op.Rules.Applied = !rollback
	return s.saveOperation(op)
}
