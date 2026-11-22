package codex

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/url"

	wire "github.com/36Dge/yijie-agent-host/internal/contracts/builtinweb"
	"github.com/pelletier/go-toml/v2"
)

const BuiltinWebMCPTokenEnv = "YIJIE_BUILTIN_WEB_MCP_TOKEN"

func builtinWebTool(name string) bool {
	for _, known := range wire.ToolNames {
		if known == name {
			return true
		}
	}
	return false
}

type expertWebPolicyKey struct{}

// The caller must first resolve the complete installed ExpertRef. This only
// projects the reviewed source policy, just like WithExpertImageRoute.
func WithExpertWebPolicy(ctx context.Context, id, version, roleDigest string) (context.Context, error) {
	known := false
	for _, policy := range wire.ExpertPolicies {
		if policy.ExpertID != id {
			continue
		}
		known = true
		if policy.Version == version && policy.RoleSHA256 == roleDigest {
			return context.WithValue(ctx, expertWebPolicyKey{}, policy.Policy), nil
		}
	}
	if known {
		return nil, errors.New("expert web policy qualification required")
	}
	return context.WithValue(ctx, expertWebPolicyKey{}, "inherit"), nil
}

// ExpertWebAllowed projects the already-resolved current expert binding for
// execution admission. It does not parse ROLE prose or grant native permissions.
func ExpertWebAllowed(ctx context.Context) bool {
	selected, selectedExpert := ctx.Value(expertInstructionsKey{}).(expertInstructions)
	if !selectedExpert || selected.role == "" {
		return true
	}
	policy, reviewed := ctx.Value(expertWebPolicyKey{}).(string)
	return reviewed && policy == "inherit"
}

// These are private in-process hooks. The Runtime owns requests/decisions and
// turn events. The future session/worker adapter must independently check its
// real accepted binding; an event alone does not authorize external I/O.
type BuiltinWebTurnEvent struct{ ThreadID, TurnID, State string }
type BuiltinWebElicitationHandler func(context.Context, json.RawMessage, wire.McpServerElicitationRequestParams) wire.McpServerElicitationRequestResponse
type builtinWebRuntime struct {
	url, token                              string
	apiOrigin, admissionURL, admissionToken string
	profile                                 string
	elicitation                             BuiltinWebElicitationHandler
	lifecycle                               func(BuiltinWebTurnEvent)
}

func (builtinWebRuntime) String() string     { return "builtinWebRuntime([redacted])" }
func (b builtinWebRuntime) GoString() string { return b.String() }

func (m *Manager) SetBuiltinWebMCP(endpoint, token string, elicitation BuiltinWebElicitationHandler, lifecycle func(BuiltinWebTurnEvent)) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	u, err := url.Parse(endpoint)
	decoded, tokenErr := hex.DecodeString(token)
	if m.started || !m.config.BuiltinWebEnabled || err != nil || u.Scheme != "http" || u.Hostname() != "127.0.0.1" || u.Port() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || tokenErr != nil || len(decoded) != 32 {
		return errors.New("builtin web MCP assembly unavailable")
	}
	m.builtinWeb = builtinWebRuntime{url: endpoint, token: token, elicitation: elicitation, lifecycle: lifecycle}
	return nil
}

func (m *Manager) builtinWebThreadConfig(ctx context.Context, config map[string]any, provider string) (map[string]any, error) {
	if !m.config.BuiltinWebEnabled {
		return config, nil
	}
	m.mu.Lock()
	builtin := m.builtinWeb
	m.mu.Unlock()
	if config == nil {
		config = make(map[string]any)
	}
	servers, ok := config["mcp_servers"].(map[string]any)
	if !ok {
		servers = make(map[string]any)
	}
	policy, reviewed := ctx.Value(expertWebPolicyKey{}).(string)
	if selected, ok := ctx.Value(expertInstructionsKey{}).(expertInstructions); ok {
		if selected.role == "" {
			policy = "inherit"
		} else if !reviewed {
			return nil, errors.New("resolved expert web policy required")
		}
	}
	disabled := policy == "disabled" || isScheduledDraft(ctx)
	if disabled {
		config["web_search"] = "disabled"
	}
	if builtin.url == "" || disabled {
		servers[wire.ServerName] = map[string]any{"url": "http://127.0.0.1:1/disabled-web", "enabled": false}
	} else {
		toolPolicy := make(map[string]any)
		for _, name := range wire.ToolNames {
			mode := wire.ApprovalMode
			if explicit := m.config.BuiltinWebApprovals[name]; explicit != "" {
				mode = explicit
			}
			toolPolicy[name] = map[string]any{"approval_mode": mode}
		}
		servers[wire.ServerName] = map[string]any{
			"url": builtin.url, "bearer_token_env_var": BuiltinWebMCPTokenEnv,
			"enabled": true, "enabled_tools": wire.ToolNames[:], "default_tools_approval_mode": wire.ApprovalMode,
			"tools": toolPolicy, "startup_timeout_sec": wire.StartupTimeoutSeconds, "tool_timeout_sec": wire.ToolTimeoutSeconds,
		}
	}
	config["mcp_servers"] = servers
	if provider == MiniMaxProviderID {
		if err := addNativeFunctionNamespaces(config, "mcp__"+wire.ServerName); err != nil {
			return nil, err
		}
	}
	return config, nil
}

func (m *Manager) builtinWebNativeArguments() ([]string, error) {
	excluded := marketExcludedEnvironment()
	if m.config.BuiltinMediaEnabled {
		excluded = append(excluded, MediaMCPTokenEnv)
	}
	excluded = append(excluded, BuiltinWebMCPTokenEnv)
	encoded, err := toml.Marshal(map[string]any{"shell_environment_policy": map[string]any{"exclude": excluded}})
	if err != nil {
		return nil, err
	}
	return nativeConfigArguments(encoded)
}

func (m *Manager) builtinWebTurn(event BuiltinWebTurnEvent) {
	m.mu.Lock()
	handler := m.builtinWeb.lifecycle
	enabled := m.config.BuiltinWebEnabled
	m.mu.Unlock()
	if enabled && handler != nil {
		handler(event)
	} // Only local cancellation/ACK; never wait for HTTP here.
}

func (m *Manager) observeBuiltinWebNotification(method string, params json.RawMessage) {
	if !m.config.BuiltinWebEnabled || (method != "turn/started" && method != "turn/completed") {
		return
	}
	var event struct {
		ThreadID string `json:"threadId"`
		Turn     struct {
			ID string `json:"id"`
		} `json:"turn"`
	}
	if json.Unmarshal(params, &event) != nil || event.ThreadID == "" || event.Turn.ID == "" {
		return
	}
	state := "started"
	if method == "turn/completed" {
		state = "stopped"
	}
	m.builtinWebTurn(BuiltinWebTurnEvent{event.ThreadID, event.Turn.ID, state})
}

func (m *Manager) handleBuiltinWebElicitation(ctx context.Context, request serverRequest) (serverRequestResult, bool) {
	var p wire.McpServerElicitationRequestParams
	if json.Unmarshal(request.params, &p) != nil || p.ServerName != wire.ServerName {
		return serverRequestResult{}, false
	}
	decline := func() (serverRequestResult, bool) {
		return serverRequestResult{respond: true, value: wire.McpServerElicitationRequestResponse{Action: wire.Decline}}, true
	}
	if !m.config.BuiltinWebEnabled || !request.exactEnvelope || len(request.params) > wire.MaxResultBytes || p.TurnId == nil || *p.TurnId == "" {
		return decline()
	}
	form, err := p.AsMcpServerElicitationRequestParams0()
	if err != nil || form.Mode != wire.Form || form.RequestedSchema.Type != "object" || form.RequestedSchema.Properties == nil || len(form.RequestedSchema.Properties) != 0 || (form.RequestedSchema.Required != nil && len(*form.RequestedSchema.Required) != 0) {
		return decline()
	}
	meta, ok := form.UnderscoreMeta.(map[string]any)
	if !ok || meta["codex_approval_kind"] != "mcp_tool_call" {
		return decline()
	}
	if _, err = m.nativeApprovalOwner(ctx, p.ThreadId, *p.TurnId); err != nil {
		return decline()
	}
	m.mu.Lock()
	handler := m.builtinWeb.elicitation
	ready := m.status.Ready && !m.shutdownRequested
	m.mu.Unlock()
	if !ready || handler == nil {
		return decline()
	}
	response := handler(ctx, append(json.RawMessage(nil), request.id...), p)
	// S1 projects one request only. Persistent approval choices remain native
	// and require the later UI capability negotiation; never invent a choice.
	if !response.Action.Valid() || response.UnderscoreMeta != nil || ctx.Err() != nil {
		return decline()
	}
	if _, err = m.nativeApprovalOwner(ctx, p.ThreadId, *p.TurnId); err != nil {
		return decline()
	}
	return serverRequestResult{respond: true, value: response}, true
}
