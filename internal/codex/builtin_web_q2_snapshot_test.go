package codex

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	wire "github.com/36Dge/yijie-agent-host/internal/contracts/builtinweb"
)

// Freeze existing assembly functions without starting Runtime, reading a
// credential, constructing a model client or touching a user's configuration.
func TestFEAT175Q2FrozenProfile(t *testing.T) {
	cfg := DefaultConfig()
	cfg.ChatModelsEnabled, cfg.KimiK31Candidate, cfg.RuntimePermissionsEnabled, cfg.NativeToolsEnabled = true, true, true, true
	cfg.BuiltinWebEnabled, cfg.ToolSearchEnabled, cfg.InteractionToolsEnabled = true, true, true
	m := NewManager(cfg, nil)
	if err := m.SetBuiltinWebMCP("http://127.0.0.1:1/q2-static-template", strings.Repeat("a", 64), nil, nil); err != nil {
		t.Fatal(err)
	}
	policy, reviewer, sandbox, err := permissionPolicy(PermissionAsk)
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := toolSearchCatalog()
	if err != nil {
		t.Fatal(err)
	}
	var modelCatalog any
	if err = json.Unmarshal(catalog, &modelCatalog); err != nil {
		t.Fatal(err)
	}
	native, _, err := nativeToolsConfiguration()
	if err != nil {
		t.Fatal(err)
	}
	var models []any
	for _, id := range []string{KimiK31ProfileID, "minimax-m3-high-v1"} {
		model, ok := ModelProfile(id)
		if !ok {
			t.Fatal("existing model profile missing")
		}
		ctx := WithPermissionMode(WithModelProfile(context.Background(), id), PermissionAsk)
		config := applyModelConfig(nil, model)
		config = m.executionProgressConfig(ctx, config)
		config, err = m.builtinWebThreadConfig(ctx, config, model.Provider)
		if err != nil {
			t.Fatal(err)
		}
		// Endpoint/capability are assigned by the future owned local process.
		// Only their stable native configuration keys belong in this freeze.
		server := config["mcp_servers"].(map[string]any)[wire.ServerName].(map[string]any)
		delete(server, "url")
		models = append(models, map[string]any{"definition": model, "ordinary_config_projection": config})
	}
	value := map[string]any{
		"scope":           "static preparation projection, not real provider wire or runtime qualification",
		"runtime_started": false, "model_requests": 0, "supplier_requests": 0,
		"base_instructions":              executionBaseInstructions,
		"developer_instructions":         m.sessionDeveloperInstructions() + retrievalCapabilityInstructions,
		"thread_default_approval_policy": m.sessionApprovalPolicy(),
		"turn_approval_policy":           policy, "turn_approvals_reviewer": reviewer, "turn_sandbox": sandbox,
		"web_search_mode":              "inherit pinned Runtime default; actual request still must be recorded in Q2",
		"native_feature_configuration": native, "model_catalog": modelCatalog, "models": models,
	}
	encoded, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	encoded = append(encoded, '\n')
	file := filepath.Join("testdata", "feat175-q2", "effective-profile.json")
	if os.Getenv("YIJIE_FEAT175_Q2_UPDATE_PROFILE") == "1" {
		if err = os.WriteFile(file, encoded, 0600); err != nil {
			t.Fatal(err)
		}
	}
	current, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if string(current) != string(encoded) {
		t.Fatal("Q2 profile changed; review and explicitly regenerate the frozen preparation snapshot")
	}
}
