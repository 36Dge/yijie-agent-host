package codex

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"

	native "github.com/36Dge/yijie-agent-host/internal/contracts/runtimeskillmarket"
	"github.com/santhosh-tekuri/jsonschema/v6"
)

// These are native RPC adapters. The Host never opens PluginStore or chooses
// cache versions. Wire shapes and validation come from the pinned Runtime.
var skillMarketplaceSchemas = sync.OnceValues(func() (map[string]*jsonschema.Schema, error) {
	resources := map[string][]byte{
		"mcpServerStatus/list.in": native.ListMcpServerStatusParamsSchema, "mcpServerStatus/list.out": native.ListMcpServerStatusResponseSchema,
		"plugin/state/read.in": native.PluginStateReadParamsSchema, "plugin/state/read.out": native.PluginStateReadResponseSchema,
		"plugin/refresh.in": native.PluginRefreshParamsSchema, "plugin/refresh.out": native.PluginRefreshResponseSchema,
		"plugin/install.in": native.PluginInstallParamsSchema, "plugin/install.out": native.PluginInstallResponseSchema,
		"plugin/uninstall.in": native.PluginUninstallParamsSchema, "plugin/uninstall.out": native.PluginUninstallResponseSchema,
		"plugin/read.in": native.PluginReadParamsSchema, "plugin/read.out": native.PluginReadResponseSchema,
		"marketplace/add.in": native.MarketplaceAddParamsSchema, "marketplace/add.out": native.MarketplaceAddResponseSchema,
		"config/read.in": native.ConfigReadParamsSchema, "config/read.out": native.ConfigReadResponseSchema,
		"config/batchWrite.in": native.ConfigBatchWriteParamsSchema, "config/batchWrite.out": native.ConfigWriteResponseSchema,
	}
	result := make(map[string]*jsonschema.Schema)
	for name, raw := range resources {
		compiler := jsonschema.NewCompiler()
		var value any
		if err := json.Unmarshal(raw, &value); err != nil {
			return nil, err
		}
		uri := "https://schemas.yijie.ai/native-skill-market/" + name
		if err := compiler.AddResource(uri, value); err != nil {
			return nil, err
		}
		compiled, err := compiler.Compile(uri)
		if err != nil {
			return nil, err
		}
		result[name] = compiled
	}
	return result, nil
})

func (m *Manager) skillMarketplaceRequest(ctx context.Context, method string, params, result any) error {
	if !m.config.NativeSkillMarketplaceEnabled {
		return errors.New("native Skill marketplace is disabled")
	}
	schemas, err := skillMarketplaceSchemas()
	if err != nil {
		return err
	}
	raw, err := json.Marshal(params)
	if err != nil {
		return err
	}
	var value any
	if json.Unmarshal(raw, &value) != nil || schemas[method+".in"] == nil || schemas[method+".in"].Validate(value) != nil {
		return errors.New("native Skill request does not conform to pinned schema")
	}
	m.mu.Lock()
	generation, client := m.runtimeGeneration, m.client
	m.mu.Unlock()
	var response json.RawMessage
	if err := m.request(ctx, method, params, &response); err != nil {
		return err
	}
	m.mu.Lock()
	sameGeneration := generation == m.runtimeGeneration && client == m.client && m.status.Ready
	m.mu.Unlock()
	if !sameGeneration {
		return errors.New("native Skill Runtime generation changed; outcome requires reconciliation")
	}
	if json.Unmarshal(response, &value) != nil || schemas[method+".out"].Validate(value) != nil {
		return errors.New("native Skill response does not conform to pinned schema")
	}
	if result != nil {
		return json.Unmarshal(response, result)
	}
	return nil
}

func (c Config) validateSkillMarketplace() error {
	if !c.NativeSkillMarketplaceEnabled {
		if len(c.SkillMarketplaceRoots) != 0 {
			return errors.New("Skill marketplace roots require the native profile")
		}
		return nil
	}
	if !c.NativeToolsEnabled || !c.KimiK31Candidate || len(c.SkillMarketplaceRoots) != 2 {
		return errors.New("Skill marketplace requires the pinned native profile and two roots")
	}
	for i, root := range c.SkillMarketplaceRoots {
		if !validRuntimeAbsolutePath(root) {
			return errors.New("Skill marketplace root is invalid")
		}
		info, err := os.Lstat(root)
		real, e := filepath.EvalSymlinks(root)
		if err != nil || e != nil || !info.IsDir() || info.Mode().Perm()&0077 != 0 || real != root {
			return errors.New("Skill marketplace root must be a private canonical directory")
		}
		if i > 0 && (root == c.SkillMarketplaceRoots[0] || strings.HasPrefix(root, c.SkillMarketplaceRoots[0]+string(os.PathSeparator)) || strings.HasPrefix(c.SkillMarketplaceRoots[0], root+string(os.PathSeparator))) {
			return errors.New("Skill marketplace roots overlap")
		}
	}
	return nil
}

func (m *Manager) skillMarketplaceArguments() ([]string, error) {
	if err := m.config.validateSkillMarketplace(); err != nil {
		return nil, err
	}
	roots, _ := json.Marshal(m.config.SkillMarketplaceRoots)
	return nativeConfigArguments([]byte("plugin_marketplace_mode = \"local_only\"\nplugin_refresh_mode = \"explicit\"\nplugin_allowed_local_roots = " + string(roots) + "\n[features]\nplugins = true\nremote_plugin = false\nhooks = false\napps = false\ntool_suggest = false\nskill_mcp_dependency_install = false\n"))
}

func (m *Manager) validateSkillMarketplaceEffective(config map[string]json.RawMessage) error {
	var mode, refresh string
	var roots []string
	var features map[string]any
	if json.Unmarshal(config["plugin_marketplace_mode"], &mode) != nil || mode != "local_only" || json.Unmarshal(config["plugin_refresh_mode"], &refresh) != nil || refresh != "explicit" || json.Unmarshal(config["plugin_allowed_local_roots"], &roots) != nil || !reflect.DeepEqual(roots, m.config.SkillMarketplaceRoots) || json.Unmarshal(config["features"], &features) != nil {
		return errors.New("native Skill policy mismatch")
	}
	for key, expected := range map[string]bool{"plugins": true, "remote_plugin": false, "hooks": false, "apps": false, "tool_suggest": false, "skill_mcp_dependency_install": false} {
		if features[key] != expected {
			return errors.New("native Skill feature policy mismatch")
		}
	}
	return nil
}

func (m *Manager) RegisterSkillMarketplace(ctx context.Context, root string) error {
	if !m.skillRootAllowed(root) {
		return errors.New("Skill marketplace root is not admitted")
	}
	return m.skillMarketplaceRequest(ctx, "marketplace/add", map[string]any{"source": root}, nil)
}
func (m *Manager) skillRootAllowed(root string) bool {
	for _, r := range m.config.SkillMarketplaceRoots {
		if r == root {
			return true
		}
	}
	return false
}
func (m *Manager) ReadSkillPlugin(ctx context.Context, root, name string) (native.PluginDetail, error) {
	var out native.PluginReadResponse
	if !m.skillRootAllowed(root) {
		return out.Plugin, errors.New("Skill marketplace root is not admitted")
	}
	err := m.skillMarketplaceRequest(ctx, "plugin/read", map[string]any{"marketplacePath": filepath.Join(root, ".agents/plugins/marketplace.json"), "pluginName": name}, &out)
	return out.Plugin, err
}
func (m *Manager) InstallSkillPlugin(ctx context.Context, root, name string) error {
	if !m.skillRootAllowed(root) {
		return errors.New("Skill marketplace root is not admitted")
	}
	return m.skillMarketplaceRequest(ctx, "plugin/install", map[string]any{"marketplacePath": filepath.Join(root, ".agents/plugins/marketplace.json"), "pluginName": name}, nil)
}
func (m *Manager) UninstallSkillPlugin(ctx context.Context, id string) error {
	return m.skillMarketplaceRequest(ctx, "plugin/uninstall", map[string]any{"pluginId": id}, nil)
}
func (m *Manager) ReadSkillPluginStates(ctx context.Context, ids, markets []string, cwd string) ([]native.PluginState, error) {
	p := native.PluginStateReadParams{PluginIds: ids, MarketplaceNames: markets}
	if cwd != "" {
		v := native.AbsolutePathBuf(cwd)
		p.Cwd = &v
	}
	var out native.PluginStateReadResponse
	err := m.skillMarketplaceRequest(ctx, "plugin/state/read", p, &out)
	return out.States, err
}
func (m *Manager) RefreshSkillPlugin(ctx context.Context, id, root string, force bool) error {
	if !m.skillRootAllowed(root) {
		return errors.New("Skill marketplace root is not admitted")
	}
	var out native.PluginRefreshResponse
	if err := m.skillMarketplaceRequest(ctx, "plugin/refresh", native.PluginRefreshParams{PluginId: id, MarketplacePath: native.AbsolutePathBuf(filepath.Join(root, ".agents/plugins/marketplace.json")), Force: &force}, &out); err != nil {
		return err
	}
	return m.ReloadSkillPluginConfig(ctx)
}
func (m *Manager) ReadSkillPluginConfig(ctx context.Context, cwd string) (map[string]any, error) {
	p := map[string]any{"includeLayers": true}
	if cwd != "" {
		p["cwd"] = cwd
	}
	var out map[string]any
	err := m.skillMarketplaceRequest(ctx, "config/read", p, &out)
	return out, err
}
func (m *Manager) WriteSkillPluginConfig(ctx context.Context, edits []any, version string) error {
	p := map[string]any{"edits": edits, "reloadUserConfig": true}
	if version != "" {
		p["expectedVersion"] = version
	}
	var out map[string]any
	if err := m.skillMarketplaceRequest(ctx, "config/batchWrite", p, &out); err != nil {
		return err
	}
	if out["status"] != "ok" {
		return errors.New("native Skill configuration was overridden")
	}
	return nil
}
func (m *Manager) ReloadSkillPluginConfig(ctx context.Context) error {
	return m.WriteSkillPluginConfig(ctx, []any{}, "")
}
func (m *Manager) SetSkillPluginEnabled(ctx context.Context, id string, enabled bool) error {
	config, err := m.ReadSkillPluginConfig(ctx, "")
	if err != nil {
		return err
	}
	version, err := SkillUserConfigVersion(config)
	if err != nil {
		return err
	}
	return m.WriteSkillPluginConfig(ctx, []any{map[string]any{"keyPath": "plugins." + strconv.Quote(id) + ".enabled", "mergeStrategy": "replace", "value": enabled}}, version)
}
func SkillUserConfigVersion(config map[string]any) (string, error) {
	layers, _ := config["layers"].([]any)
	for _, raw := range layers {
		layer, _ := raw.(map[string]any)
		name, _ := layer["name"].(map[string]any)
		if name["type"] == "user" {
			if v, ok := layer["version"].(string); ok && v != "" {
				return v, nil
			}
		}
	}
	return "", errors.New("native user config version unavailable")
}

type skillAdmissionContextKey struct{}

// HoldSkillResources spans reference resolution through native turn admission.
// request() also guards every turn/start (including titles and scheduled work).
func (m *Manager) SetSkillSubmissionReady(ready bool) { m.skillSubmissionReady.Store(ready) }
func (m *Manager) HoldSkillResources(ctx context.Context) (context.Context, func(), error) {
	if m.config.NativeSkillMarketplaceEnabled && !m.skillSubmissionReady.Load() && ctx.Value(skillAdmissionContextKey{}) != m {
		return ctx, nil, errors.New("Skill maintenance is pending")
	}
	if !m.config.NativeSkillMarketplaceEnabled || ctx.Value(skillAdmissionContextKey{}) == m {
		return ctx, func() {}, nil
	}
	if !m.skillResourceAdmission.TryRLock() {
		return ctx, nil, errors.New("Skill maintenance is in progress")
	}
	if !m.skillSubmissionReady.Load() {
		m.skillResourceAdmission.RUnlock()
		return ctx, nil, errors.New("Skill maintenance is pending")
	}
	return context.WithValue(ctx, skillAdmissionContextKey{}, m), m.skillResourceAdmission.RUnlock, nil
}
func (m *Manager) WithSkillMaintenance(ctx context.Context, action func(context.Context) error) error {
	if !m.skillResourceAdmission.TryLock() {
		return errors.New("Skill resources are busy")
	}
	defer m.skillResourceAdmission.Unlock()
	var cursor *string
	for pages := 0; pages < 128; pages++ {
		var out struct {
			Data       []string `json:"data"`
			NextCursor *string  `json:"nextCursor"`
		}
		if err := m.request(ctx, "thread/loaded/list", map[string]any{"limit": 128, "cursor": cursor}, &out); err != nil {
			return err
		}
		for _, id := range out.Data {
			state, err := m.ReadThreadStatus(ctx, id)
			if err != nil {
				return err
			}
			if state != "idle" && state != "notLoaded" && state != "systemError" {
				return fmt.Errorf("Skill resources are busy")
			}
		}
		if out.NextCursor == nil {
			return action(context.WithValue(ctx, skillAdmissionContextKey{}, m))
		}
		cursor = out.NextCursor
	}
	return errors.New("native loaded thread inventory is incomplete")
}

func (m *Manager) ReadSkillMCPInventory(ctx context.Context) ([]native.McpServerStatus, error) {
	detail := native.McpServerStatusDetail("toolsAndAuthOnly")
	limit := int64(128)
	params := native.ListMcpServerStatusParams{Detail: &detail, Limit: &limit}
	result := []native.McpServerStatus{}
	for pages := 0; pages < 8; pages++ {
		var out native.ListMcpServerStatusResponse
		if err := m.skillMarketplaceRequest(ctx, "mcpServerStatus/list", params, &out); err != nil {
			return nil, err
		}
		result = append(result, out.Data...)
		if out.NextCursor == nil {
			return result, nil
		}
		params.Cursor = out.NextCursor
	}
	return nil, errors.New("native MCP inventory is incomplete")
}
