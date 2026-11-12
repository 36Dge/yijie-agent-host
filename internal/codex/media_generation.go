package codex

import (
	"context"
	"errors"
	"path/filepath"

	media "github.com/36Dge/yijie-agent-host/internal/contracts/mediageneration"
	"github.com/pelletier/go-toml/v2"
)

const MediaMCPTokenEnv = "YIJIE_MEDIA_MCP_TOKEN"

func (m *Manager) SetMediaMCP(url, token string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.started || !m.config.BuiltinMediaEnabled || url == "" || len(token) != 64 {
		return errors.New("media MCP assembly unavailable")
	}
	m.mediaMCPURL, m.mediaMCPToken = url, token
	return nil
}

func (m *Manager) mediaThreadConfig(ctx context.Context, config map[string]any, provider string) (map[string]any, error) {
	if !m.config.BuiltinMediaEnabled {
		return config, nil
	}
	if config == nil {
		config = make(map[string]any)
	}
	config["yijie_host_images"] = false
	// Native JSON overrides support leaf keys. A partial features table would
	// replace the existing profile and disable Code Mode and native transport.
	config["features.image_generation"] = false
	profile, _, err := nativeToolsConfiguration()
	if err != nil {
		return nil, err
	}
	namespaces := append([]any(nil), profile["features"].(map[string]any)["code_mode"].(map[string]any)["function_namespaces"].([]any)...)
	if provider == MiniMaxProviderID {
		// MiniMax accepts functions. Reuse the existing native namespace adapter;
		// Kimi retains normal MCP namespaces and deferred native tool search.
		namespaces = append(namespaces, "mcp__yijie_media", "mcp__yijie_artifacts")
	}
	config["features.code_mode.function_namespaces"] = namespaces
	servers, ok := config["mcp_servers"].(map[string]any)
	if !ok {
		servers = make(map[string]any)
	}
	if m.mediaMCPURL != "" && !isScheduledDraft(ctx) && ImageRoute(ctx) == "builtin" {
		servers["yijie_media"] = map[string]any{
			"url": m.mediaMCPURL, "bearer_token_env_var": MediaMCPTokenEnv,
			"enabled": true, "enabled_tools": []string{media.ToolName},
			"default_tools_approval_mode": "auto", "startup_timeout_sec": 10,
			"tools":            map[string]any{media.ToolName: map[string]any{"approval_mode": media.ToolApprovalMode}},
			"tool_timeout_sec": media.ToolTimeoutSeconds, "supports_parallel_tool_calls": false,
		}
	} else {
		servers["yijie_media"] = map[string]any{"url": "http://127.0.0.1:1/disabled-media", "enabled": false}
	}
	config["mcp_servers"] = servers
	return config, nil
}

func mediaNativeLayerArguments() ([]string, error) {
	excluded := append(marketExcludedEnvironment(), MediaMCPTokenEnv)
	encoded, err := toml.Marshal(map[string]any{
		"yijie_host_images":        false,
		"features":                 map[string]any{"image_generation": false},
		"shell_environment_policy": map[string]any{"exclude": excluded},
	})
	if err != nil {
		return nil, err
	}
	return nativeConfigArguments(encoded)
}

// The fixed Runtime bundles an OpenAI-specific imagegen guide. Its implicit
// trigger belongs to the retired image_gen/CLI entry, not this application's
// MCP. Use the native exact-path configuration API; never rewrite the guide or
// disable a user's similarly named Skill or other installed capabilities.
func (m *Manager) disableLegacySystemImageSkill(ctx context.Context, client *Client) error {
	path := filepath.Join(m.config.CodexHome, "skills", ".system", "imagegen", "SKILL.md")
	var listed skillsListResponse
	err := client.Request(ctx, RuntimeMethodSkillsList, skillsListParams{CWDs: []string{m.config.CodexHome}, ForceReload: true}, &listed)
	if err != nil {
		return err
	}
	for _, entry := range listed.Data {
		for _, skill := range entry.Skills {
			if skill.Path != path {
				continue
			}
			if skill.Scope != SkillScopeSystem || skill.Name != "imagegen" {
				return errors.New("system image skill identity unavailable")
			}
			if !skill.Enabled {
				return nil
			}
			var response skillsConfigWriteResponse
			if err = client.Request(ctx, RuntimeMethodSkillsConfigWrite, skillsConfigWriteParams{Path: path, Enabled: false}, &response); err != nil {
				return err
			}
			if response.EffectiveEnabled {
				return errors.New("retired system image skill remains enabled")
			}
			return nil
		}
	}
	return nil // A Runtime without this bundled guide needs no override.
}
