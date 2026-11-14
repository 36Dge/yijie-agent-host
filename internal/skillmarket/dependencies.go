package skillmarket

import (
	"encoding/json"

	native "github.com/36Dge/yijie-agent-host/internal/contracts/runtimeskillmarket"
	wire "github.com/36Dge/yijie-agent-host/internal/contracts/skillmarketv2"
)

// Only native-declared MCP server identities are comparable with native MCP
// status. Product tool labels without an exact native identity stay unknown.
func dependencyState(required, mcps []string, inventory []native.McpServerStatus, complete bool) wire.DependencyState {
	if len(required) == 0 && len(mcps) == 0 {
		return wire.DependencyStateReady
	}
	if !complete || len(mcps) == 0 {
		return wire.DependencyStateUnknown
	}
	byName := map[string]native.McpServerStatus{}
	for _, server := range inventory {
		byName[server.Name] = server
	}
	result := wire.DependencyStateReady
	if len(required) > len(mcps) {
		result = wire.DependencyStateUnknown
	}
	for _, name := range mcps {
		server, ok := byName[name]
		if !ok {
			if result != wire.DependencyStateNeedsAuthorization {
				result = wire.DependencyStateMissing
			}
			continue
		}
		if server.AuthStatus == "notLoggedIn" {
			result = wire.DependencyStateNeedsAuthorization
			continue
		}
		var tools map[string]json.RawMessage
		if json.Unmarshal(server.Tools, &tools) != nil || len(tools) == 0 {
			if result == wire.DependencyStateReady {
				result = wire.DependencyStateUnknown
			}
		}
	}
	return result
}
