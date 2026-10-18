package codex

import "errors"

const ArtifactMCPTokenEnv = "YIJIE_ARTIFACT_MCP_TOKEN"

// Only the Host-owned loopback adapter may configure this capability.
func (m *Manager) SetArtifactMCP(url, token string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.client != nil || url == "" || len(token) != 64 {
		return errors.New("artifact transport configuration unavailable")
	}
	m.artifactMCPURL = url
	m.artifactMCPToken = token
	return nil
}
func (m *Manager) artifactThreadConfig(config map[string]any) map[string]any {
	if m.artifactMCPURL == "" {
		return config
	}
	if config == nil {
		config = make(map[string]any)
	}
	servers, ok := config["mcp_servers"].(map[string]any)
	if !ok {
		servers = make(map[string]any)
	}
	servers["yijie_artifacts"] = map[string]any{"url": m.artifactMCPURL, "bearer_token_env_var": ArtifactMCPTokenEnv, "enabled": true, "default_tools_approval_mode": "auto", "startup_timeout_sec": 10, "tool_timeout_sec": 35, "supports_parallel_tool_calls": false}
	config["mcp_servers"] = servers
	return config
}
