package codex

import (
	"encoding/json"
	"testing"
)

func TestFEAT174MarketIsolationUsesTheCompleteNativeProfile(t *testing.T) {
	features := map[string]any{"hooks": false, "plugins": true, "apps": false, "tool_suggest": false, "shell_snapshot": false, "multi_agent": false, "js_repl": false, "memories": true, "tool_call_mcp_elicitation": true, "multi_agent_v2": map[string]any{"enabled": true, "inherit_mcp_servers": false}, "code_mode": map[string]any{"enabled": true}}
	rawFeatures, _ := json.Marshal(features)
	rawEnvironment, _ := json.Marshal(map[string]any{"inherit": "core", "exclude": marketExcludedEnvironment(), "set": map[string]any{}, "ignore_default_excludes": nil, "include_only": nil, "experimental_use_profile": nil})
	effective := map[string]json.RawMessage{"features": rawFeatures, "shell_environment_policy": rawEnvironment}
	m := NewManager(Config{NativeToolsEnabled: true, NativeSkillMarketplaceEnabled: true}, nil)
	if err := m.validateMarketIsolationConfig(effective); err != nil {
		t.Fatal("paired native skills rejected by shared admission", err)
	}
	if err := validateMarketEffectiveIsolationForTools(effective, true, false, false); err == nil {
		t.Fatal("test did not distinguish the omitted Skill selector")
	}
	m.config.NativeSkillMarketplaceEnabled = false
	if err := m.validateMarketIsolationConfig(effective); err == nil {
		t.Fatal("legacy policy silently widened")
	}
}

func TestFEAT174NativeConfigurationRetainsOnlyOwnedLocalMarkets(t *testing.T) {
	content := []byte(`[marketplaces.yijie-builtin]
source_type = "local"
source = "/ordinary/builtin"
[marketplaces.yijie-user]
source_type = "local"
source = "/ordinary/user"
[plugins."yj-user-ordinary@yijie-user"]
enabled = false
`)
	if err := validateNativeProjectConfig(content); err == nil {
		t.Fatal("legacy profile accepted a new ownership surface")
	}
	if err := validateNativeProjectConfigWithMarkets(content, []string{"/ordinary/builtin", "/ordinary/user"}); err != nil {
		t.Fatal("native-owned disabled plugin cannot survive normal restart", err)
	}
	if err := validateNativeProjectConfigWithMarkets(content, []string{"/separate/builtin", "/separate/user"}); err == nil {
		t.Fatal("unrelated roots admitted")
	}
}
