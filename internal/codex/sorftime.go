package codex

// FEAT-144 compatibility only. New Sorftime use belongs to the FEAT-157
// Connectors worker; no legacy credential or approval can activate it.
import (
	"context"
	"errors"
)

const SorftimeSecretEnv = "YIJIE_FEAT144_SORFTIME_ACCOUNT_SK"
const SorftimeProxyEnv = "YIJIE_FEAT144_HTTPS_PROXY"
const SorftimeEnabledEnv = "YIJIE_FEAT144_SORFTIME_ENABLED"

// Kept for the old HTTP error projection, never produced by the retired scope route.
var ErrMcpTurnActive = errors.New("active native Turn prevents MCP deactivation")

// Exact historical bytes, used only to recognize an owned configuration during
// migration. Never append this block to a new Runtime configuration.
const sorftimeManagedBlock = `
# FEAT-144: native client configuration; no credential values.
[features]
hooks = false
plugins = false
apps = false
tool_suggest = false
shell_snapshot = false
multi_agent = false
js_repl = false
memories = false
tool_call_mcp_elicitation = true

[shell_environment_policy]
inherit = "core"
exclude = ["YIJIE_FEAT144_SORFTIME_ACCOUNT_SK", "YIJIE_FEAT144_SORFTIME_ENABLED", "MINIMAX_API_KEY"]
set = {}

[mcp_servers.sorftime]
url = "https://mcp.sorftime.com/"
bearer_token_env_var = "YIJIE_FEAT144_SORFTIME_ACCOUNT_SK"
http_headers = { "User-Agent" = "codex-mcp-client/0.144.6" }
enabled = true
enabled_tools = ["product_detail"]
default_tools_approval_mode = "prompt"
startup_timeout_sec = 30
tool_timeout_sec = 60
supports_parallel_tool_calls = false

[mcp_servers.sorftime.tools.product_detail]
approval_mode = "prompt"
`

func miniMaxManagedConfigForRuntime(home string, profile ManagedReasoningProfile, commandApproval bool) ([]byte, error) {
	base, err := miniMaxManagedConfigForAuthority(home, profile, commandApproval)
	if err != nil {
		return nil, err
	}
	if err := validateFEAT137ManagedConfig(base, commandApproval); err != nil {
		return nil, err
	}
	return base, nil
}

func legacySorftimeManagedConfig(home string, profile ManagedReasoningProfile) ([]byte, error) {
	base, err := miniMaxManagedConfigForRuntime(home, profile, false)
	if err != nil {
		return nil, err
	}
	return append(base, []byte(sorftimeManagedBlock)...), nil
}

// Historical DTO compatibility only; no new legacy approval producer exists.
type McpApprovalScope struct{ Server, Tool, ASIN, Marketplace string }

// Old callers may observe the retired scope. It never activates tools or
// restarts the Runtime; FEAT-157 owns independent selection and per-call grants.
func (m *Manager) PrepareMcpPermissionScope(_ context.Context, mode PermissionMode) (bool, bool, error) {
	return false, false, m.ValidatePermissionMode(mode)
}
