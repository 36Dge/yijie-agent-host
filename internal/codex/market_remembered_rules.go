package codex

import (
	"context"
	"errors"
	"sort"
	"strconv"
	"strings"

	broker "github.com/36Dge/yijie-agent-host/internal/contracts/marketbrokercontrol"
	wire "github.com/36Dge/yijie-agent-host/internal/contracts/markethost"
)

func marketApprovalAccount(credential, operation broker.CanonicalId) string {
	return marketScopeDigest([]any{credential, operation})
}

func marketInstallPrefix(scope broker.ScopeBinding, service broker.ServiceId, ref broker.SelectionRef, account string) string {
	return marketScopeDigest([]any{"yijie.market.install/v1", scope.OwnerUserId, scope.TenantId, service, ref.InstallationId, ref.Generation, account})
}

// Use only the versioned native user layer. Never return unrelated configuration.
func (m *Manager) marketRuleSnapshot(ctx context.Context, prefix string) (map[string]string, string, error) {
	cfg, err := m.ReadSkillPluginConfig(ctx, "")
	if err != nil {
		return nil, "", err
	}
	version, err := SkillUserConfigVersion(cfg)
	if err != nil {
		return nil, "", err
	}
	found := map[string]string{}
	layers, _ := cfg["layers"].([]any)
	for _, raw := range layers {
		layer, _ := raw.(map[string]any)
		name, _ := layer["name"].(map[string]any)
		if name["type"] != "user" {
			continue
		}
		user, _ := layer["config"].(map[string]any)
		apps, _ := user["apps"].(map[string]any)
		for app, settings := range apps {
			key, ok := strings.CutPrefix(app, "yijie-market-")
			if !ok || !strings.HasPrefix(key, prefix) || broker.ArgsDigest(key).Validate() != nil {
				continue
			}
			entry, _ := settings.(map[string]any)
			tools, _ := entry["tools"].(map[string]any)
			if len(tools) != 1 {
				continue
			}
			for tool, v := range tools {
				mode, _ := v.(map[string]any)
				if broker.ToolName(tool).Validate() == nil && mode["approval_mode"] == "approve" {
					found[key] = tool
				}
			}
		}
	}
	return found, version, nil
}

func (m *Manager) RememberedMarketRules(ctx context.Context, p wire.RememberedRulesPayload) (wire.RememberedToolRules, error) {
	var out wire.RememberedToolRules
	if !m.NativeMarketApprovals() {
		return out, errors.New("native market rules unavailable")
	}
	prefix := marketInstallPrefix(p.Scope, p.ServiceId, p.Reference, marketApprovalAccount(p.CredentialRef, p.AuthorityOperationId))
	m.market.ruleMu.Lock()
	defer m.market.ruleMu.Unlock()
	rules, version, err := m.marketRuleSnapshot(ctx, prefix)
	if err != nil {
		return out, err
	}
	if p.Revoke != nil {
		key := string(*p.Revoke)
		if !strings.HasPrefix(key, prefix) {
			return out, errors.New("market rule outside installation")
		}
		if _, ok := rules[key]; ok {
			// Retire unconsumed permits before changing the native configuration.
			if err = m.RevokeMarketAuthority(ctx, p.Scope, &p.Reference.InstallationId); err != nil {
				return out, err
			}
			edit := map[string]any{"keyPath": "apps." + strconv.Quote("yijie-market-"+key), "mergeStrategy": "replace", "value": nil}
			if err = m.WriteSkillPluginConfig(ctx, []any{edit}, version); err != nil {
				return out, err
			}
			rules, _, err = m.marketRuleSnapshot(ctx, prefix)
			if err != nil {
				return out, err
			}
			if _, exists := rules[key]; exists {
				return out, errors.New("native rule removal unconfirmed")
			}
		}
	}
	keys := make([]string, 0, len(rules))
	for key := range rules {
		if p.After == nil || key > string(*p.After) {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	out.Rules = []wire.RememberedToolRule{}
	if len(keys) > 32 {
		cursor := wire.ArgsDigest(keys[31])
		out.Next = &cursor
		keys = keys[:32]
	}
	for _, key := range keys {
		out.Rules = append(out.Rules, wire.RememberedToolRule{Key: wire.ArgsDigest(key), Tool: wire.ToolName(rules[key])})
	}
	return out, nil
}
