package codex

// FEAT-144 only configures the pinned native MCP client and adapts its approval
// callbacks. There is no HTTP client, MCP executor, retry loop or approval cache.
import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"net/url"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"github.com/google/uuid"
)

const SorftimeSecretEnv = "YIJIE_FEAT144_SORFTIME_ACCOUNT_SK"
const SorftimeProxyEnv = "YIJIE_FEAT144_HTTPS_PROXY"
const SorftimeEnabledEnv = "YIJIE_FEAT144_SORFTIME_ENABLED"
const sorftimeURL = "https://mcp.sorftime.com/"
const sorftimeUserAgent = "codex-mcp-client/0.144.6"

var ErrMcpTurnActive = errors.New("active native Turn prevents MCP deactivation")

type SorftimeConfig struct {
	HTTPSProxy string
	Enabled    bool
	Token      string `json:"-"`
}

func (c SorftimeConfig) String() string   { return "SorftimeConfig([REDACTED])" }
func (c SorftimeConfig) GoString() string { return c.String() }

func (c SorftimeConfig) validate(permissions, provider bool) error {
	if !c.Enabled && c.Token == "" && c.HTTPSProxy == "" {
		return nil
	}
	if !c.Enabled || !permissions || !provider || len(c.Token) < 1 || len(c.Token) > 4096 || strings.TrimSpace(c.Token) != c.Token || strings.ContainsAny(c.Token, "\x00\r\n\t ") {
		return errors.New("Sorftime requires the explicit local permission profile and an in-memory credential")
	}
	if c.HTTPSProxy != "" {
		proxy, err := url.Parse(c.HTTPSProxy)
		if err != nil || proxy.Scheme != "http" || proxy.User != nil || proxy.Hostname() == "" || proxy.RawQuery != "" || proxy.ForceQuery || proxy.Fragment != "" || proxy.Path != "" {
			return errors.New("Sorftime system proxy is invalid")
		}
		port, err := strconv.Atoi(proxy.Port())
		if err != nil || port < 1 || port > 65535 {
			return errors.New("Sorftime system proxy is invalid")
		}
	}
	return nil
}

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

func miniMaxManagedConfigForRuntime(home string, profile ManagedReasoningProfile, commandApproval, sorftime bool) ([]byte, error) {
	base, err := miniMaxManagedConfigForAuthority(home, profile, commandApproval)
	if err != nil {
		return nil, err
	}
	// Keep every old FEAT-137 check intact, before adding a separate, exact
	// FEAT-144 profile. FEAT-137 and this profile are never combined.
	if err := validateFEAT137ManagedConfig(base, commandApproval); err != nil {
		return nil, err
	}
	if !sorftime {
		return base, nil
	}
	if commandApproval {
		return nil, errors.New("Sorftime cannot use the retired approval profile")
	}
	return append(base, []byte(sorftimeManagedBlock)...), nil
}

type sorftimeRuntimeState struct {
	configured       bool
	operation        sync.Mutex // Serializes configuration changes with native starts.
	mu               sync.Mutex
	enabled          bool
	threads          map[string]string // Native thread ID -> actual Runtime generation.
	verified         map[string]bool   // Effective native configuration, not approval state.
	startup          map[string]string // Last actual native startup notification only.
	catalogAttempted map[string]bool
	catalogVerified  map[string]bool
	changed          chan struct{}
}

//go:embed sorftime-product-detail.input.schema.json
var sorftimeInputSchema []byte

func (m *Manager) observeSorftimeStartup(method string, raw json.RawMessage) {
	if method != "mcpServer/startupStatus/updated" || !m.sorftime.configured {
		return
	}
	var value struct {
		ThreadID string `json:"threadId"`
		Name     string `json:"name"`
		Status   string `json:"status"`
	}
	if json.Unmarshal(raw, &value) != nil || value.Name != "sorftime" || value.ThreadID == "" || len(value.ThreadID) > 512 {
		return
	}
	switch value.Status {
	case "starting", "ready", "failed", "cancelled":
	default:
		return
	}
	m.sorftime.mu.Lock()
	defer m.sorftime.mu.Unlock()
	if len(m.sorftime.startup) >= 512 && m.sorftime.startup[value.ThreadID] == "" {
		return
	}
	m.sorftime.startup[value.ThreadID] = value.Status
	close(m.sorftime.changed)
	m.sorftime.changed = make(chan struct{})
}

type sorftimeCatalog struct {
	Data []struct {
		Name       string `json:"name"`
		AuthStatus string `json:"authStatus"`
		Tools      map[string]struct {
			Name        string          `json:"name"`
			InputSchema json.RawMessage `json:"inputSchema"`
		} `json:"tools"`
	} `json:"data"`
	NextCursor *string `json:"nextCursor"`
}

func validateSorftimeCatalog(value sorftimeCatalog) error {
	if value.NextCursor != nil || len(value.Data) != 1 || value.Data[0].Name != "sorftime" || value.Data[0].AuthStatus != "bearerToken" || len(value.Data[0].Tools) != 1 {
		return errors.New("native MCP tool scope is unavailable")
	}
	tool, ok := value.Data[0].Tools["product_detail"]
	var actual, expected any
	if !ok || tool.Name != "product_detail" || json.Unmarshal(tool.InputSchema, &actual) != nil || json.Unmarshal(sorftimeInputSchema, &expected) != nil || !reflect.DeepEqual(actual, expected) {
		return errors.New("native MCP input schema differs from the approved source")
	}
	return nil
}

// Read the native catalog once before the first model Turn on this thread.
// This is a counted metadata operation, not a business call or a retry loop.
// A failed check stays unavailable; an outbox retry cannot repeat discovery.
func (m *Manager) verifySorftimeCatalog(ctx context.Context, threadID string) error {
	ctx, cancel := context.WithTimeout(ctx, m.config.RequestTimeout)
	defer cancel()
	if !m.sorftimeEnabled() {
		return nil
	}
	for {
		m.sorftime.mu.Lock()
		status, changed := m.sorftime.startup[threadID], m.sorftime.changed
		m.sorftime.mu.Unlock()
		if status == "ready" {
			break
		}
		if status == "failed" || status == "cancelled" {
			return errors.New("native Sorftime startup is unavailable")
		}
		select {
		case <-ctx.Done():
			return errors.New("native Sorftime startup not confirmed")
		case <-changed:
		}
	}
	m.sorftime.mu.Lock()
	if m.sorftime.catalogVerified[threadID] {
		m.sorftime.mu.Unlock()
		return nil
	}
	if m.sorftime.catalogAttempted[threadID] {
		m.sorftime.mu.Unlock()
		return errors.New("native Sorftime catalog check was not confirmed")
	}
	m.sorftime.catalogAttempted[threadID] = true
	m.sorftime.mu.Unlock()
	var catalog sorftimeCatalog
	if err := m.request(ctx, "mcpServerStatus/list", map[string]any{"threadId": threadID, "detail": "toolsAndAuthOnly", "limit": 2}, &catalog); err != nil {
		return errors.New("native Sorftime catalog unavailable")
	}
	if err := validateSorftimeCatalog(catalog); err != nil {
		return err
	}
	m.sorftime.mu.Lock()
	m.sorftime.catalogVerified[threadID] = true
	m.sorftime.mu.Unlock()
	return nil
}

func (m *Manager) sorftimeEnabled() bool {
	m.sorftime.mu.Lock()
	defer m.sorftime.mu.Unlock()
	return m.sorftime.enabled
}

func (m *Manager) validateSorftimeConfig(ctx context.Context, cwd string) error {
	if !m.sorftime.configured {
		return nil
	}
	var response struct {
		Config map[string]json.RawMessage `json:"config"`
	}
	params := map[string]any{"includeLayers": false}
	if cwd != "" {
		params["cwd"] = cwd
	}
	if err := m.request(ctx, "config/read", params, &response); err != nil {
		return errors.New("native Sorftime configuration unavailable")
	}
	return validateSorftimeEffectiveConfig(response.Config, m.sorftimeEnabled())
}

// Compare the fixed Runtime's serialized effective configuration, including
// its native defaults, rather than the shape of the input TOML.
func validateSorftimeEffectiveConfig(config map[string]json.RawMessage, enabled bool) error {
	decode := func(key string, target any) bool { return json.Unmarshal(config[key], target) == nil }
	if !enabled {
		var servers map[string]map[string]any
		if raw, present := config["mcp_servers"]; present && string(raw) != "null" {
			if !decode("mcp_servers", &servers) {
				return errors.New("native MCP deactivation unavailable")
			}
			for _, server := range servers {
				if server["enabled"] != false {
					return errors.New("native MCP remains configured; mode change cannot activate it")
				}
			}
		}
		return nil
	}
	var servers map[string]map[string]any
	if !decode("mcp_servers", &servers) || len(servers) != 1 {
		return errors.New("native MCP configuration scope mismatch")
	}
	expected := map[string]any{
		"url": sorftimeURL, "bearer_token_env_var": SorftimeSecretEnv,
		"http_headers": map[string]any{"User-Agent": sorftimeUserAgent}, "enabled": true, "environment_id": "local",
		"enabled_tools": []any{"product_detail"}, "default_tools_approval_mode": "prompt",
		"startup_timeout_sec": float64(30), "tool_timeout_sec": float64(60),
		"tools": map[string]any{"product_detail": map[string]any{"approval_mode": "prompt"}},
	}
	if !reflect.DeepEqual(servers["sorftime"], expected) {
		return errors.New("native Sorftime configuration scope mismatch")
	}
	var features map[string]any
	if !decode("features", &features) {
		return errors.New("native execution isolation unavailable")
	}
	for _, key := range []string{"hooks", "plugins", "apps", "tool_suggest", "shell_snapshot", "multi_agent", "js_repl", "memories"} {
		if value, present := features[key]; !present || value != false {
			return errors.New("native execution isolation mismatch")
		}
	}
	if features["tool_call_mcp_elicitation"] != true {
		return errors.New("stable native MCP elicitation is unavailable")
	}
	var environment map[string]any
	if !decode("shell_environment_policy", &environment) || !reflect.DeepEqual(environment, map[string]any{
		"inherit": "core", "exclude": []any{SorftimeSecretEnv, SorftimeEnabledEnv, MiniMaxRuntimeEnvKey}, "set": map[string]any{},
		"ignore_default_excludes": nil, "include_only": nil, "experimental_use_profile": nil,
	}) {
		return errors.New("native credential environment isolation mismatch")
	}
	return nil
}

func (m *Manager) rememberSorftimeThread(response threadResponse) error {
	if !m.sorftime.configured {
		return nil
	}
	if m.sorftimeEnabled() {
		var sandbox struct {
			Type          string `json:"type"`
			NetworkAccess *bool  `json:"networkAccess"`
		}
		if response.ApprovalPolicy != "on-request" || response.ApprovalsReviewer != "user" || json.Unmarshal(response.Sandbox, &sandbox) != nil || sandbox.Type != "workspaceWrite" || sandbox.NetworkAccess == nil || *sandbox.NetworkAccess {
			return errors.New("native Sorftime permission scope mismatch")
		}
	}
	m.mu.Lock()
	generation := m.runtimeGeneration
	m.mu.Unlock()
	m.sorftime.mu.Lock()
	defer m.sorftime.mu.Unlock()
	m.sorftime.threads[response.Thread.ID] = generation
	m.sorftime.verified[response.Thread.ID] = m.sorftime.enabled
	return nil
}

// A queued config reload is NOT proof of revocation. Reuse normal EOF shutdown
// and all original source/authority checks, without stopping an active Turn.
// Once deactivated, Sorftime needs a normal application restart and new input.
func (m *Manager) PrepareMcpPermissionScope(ctx context.Context, mode PermissionMode) (bool, bool, error) {
	m.sorftime.operation.Lock()
	defer m.sorftime.operation.Unlock()
	return m.prepareMcpPermissionScope(ctx, mode)
}

func (m *Manager) prepareMcpPermissionScope(ctx context.Context, mode PermissionMode) (bool, bool, error) {
	if err := m.ValidatePermissionMode(mode); err != nil {
		return false, false, err
	}
	if !m.sorftimeEnabled() {
		if m.sorftime.configured && !m.Snapshot().Ready {
			return false, false, errors.New("verified Runtime is unavailable")
		}
		if err := m.validateSorftimeConfig(ctx, ""); err != nil {
			return false, false, err
		}
		return false, false, nil
	}
	if mode == PermissionAsk {
		return true, false, nil
	}
	m.permissionCallbacks.mu.Lock()
	active := len(m.permissionCallbacks.activeTurns) > 0
	for _, entry := range m.permissionCallbacks.entries {
		if entry.view.Status == "pending" {
			active = true
		}
	}
	m.permissionCallbacks.mu.Unlock()
	if active {
		return true, false, ErrMcpTurnActive
	}
	// Read the native loaded-thread status after all local start operations
	// have completed. A missing turn/started callback is not evidence of idle.
	m.mu.Lock()
	generation := m.runtimeGeneration
	m.mu.Unlock()
	m.sorftime.mu.Lock()
	ids := []string{}
	for id, gen := range m.sorftime.threads {
		if gen == generation {
			ids = append(ids, id)
		}
	}
	m.sorftime.mu.Unlock()
	for _, id := range ids {
		var read struct {
			Thread struct {
				ID     string `json:"id"`
				Status struct {
					Type string `json:"type"`
				} `json:"status"`
			} `json:"thread"`
		}
		if err := m.request(ctx, "thread/read", map[string]any{"threadId": id, "includeTurns": false}, &read); err != nil || read.Thread.ID != id || read.Thread.Status.Type != "idle" {
			return true, false, errors.New("native thread is not confirmed idle")
		}
	}
	m.mu.Lock()
	previousClient := m.client
	m.mu.Unlock()
	if err := m.Shutdown(ctx); err != nil {
		return true, false, errors.New("normal Runtime exit could not be confirmed")
	}
	if previousClient != nil {
		select {
		case <-previousClient.fatalDone:
		case <-ctx.Done():
			return true, false, errors.New("native transport cleanup could not be confirmed")
		}
	}
	m.mu.Lock()
	if m.status.State != StateStopped || m.authorityCleanupError != nil {
		m.mu.Unlock()
		return true, false, errors.New("normal Runtime cleanup could not be confirmed")
	}
	m.config.Sorftime = SorftimeConfig{}
	m.started = false
	m.shutdownRequested = false
	m.startDone = nil
	m.startupCancel = nil
	m.cmd = nil
	m.client = nil
	m.exitDone = nil
	m.stderr = nil
	m.runtimeGeneration = uuid.NewString()
	m.mu.Unlock()
	m.sorftime.mu.Lock()
	m.sorftime.enabled = false
	m.sorftime.verified = make(map[string]bool)
	m.sorftime.startup = make(map[string]string)
	m.sorftime.catalogVerified = make(map[string]bool)
	m.sorftime.catalogAttempted = make(map[string]bool)
	m.sorftime.mu.Unlock()
	if err := m.Start(ctx); err != nil {
		return false, false, errors.New("source-verified Runtime restart failed")
	}
	if err := m.validateSorftimeConfig(ctx, ""); err != nil {
		return false, false, err
	}
	return false, true, nil
}

type McpApprovalScope struct{ Server, Tool, ASIN, Marketplace string }

var sorftimeASIN = regexp.MustCompile(`^[A-Z0-9]{10}$`)

func (m *Manager) handleSorftimeElicitation(ctx context.Context, request serverRequest) serverRequestResult {
	decline := func() serverRequestResult {
		return serverRequestResult{respond: true, value: map[string]any{"action": "decline", "content": nil, "_meta": nil}}
	}
	var p struct {
		ThreadID string                     `json:"threadId"`
		TurnID   string                     `json:"turnId"`
		Server   string                     `json:"serverName"`
		Mode     string                     `json:"mode"`
		Meta     map[string]json.RawMessage `json:"_meta"`
		Schema   struct {
			Type       string                     `json:"type"`
			Properties map[string]json.RawMessage `json:"properties"`
			Required   []string                   `json:"required"`
		} `json:"requestedSchema"`
	}
	if len(request.params) > 64<<10 || json.Unmarshal(request.params, &p) != nil || p.Server != "sorftime" || p.Mode != "form" || p.Schema.Type != "object" || p.Schema.Properties == nil || len(p.Schema.Properties) != 0 || len(p.Schema.Required) != 0 {
		return decline()
	}
	for _, field := range []string{"tool_name", "approvals_reviewer"} {
		if raw, present := p.Meta[field]; present {
			var value string
			want := "product_detail"
			if field == "approvals_reviewer" {
				want = "user"
			}
			if json.Unmarshal(raw, &value) != nil || value != want {
				return decline()
			}
		}
	}
	// Native Prompt has neither session nor persistent remember options.
	// Unsupported persistence metadata is not interpreted as a new policy.
	if _, present := p.Meta["persist"]; present {
		return decline()
	}

	var kind string
	var args map[string]json.RawMessage
	var asin, site string
	if json.Unmarshal(p.Meta["codex_approval_kind"], &kind) != nil || kind != "mcp_tool_call" || json.Unmarshal(p.Meta["tool_params"], &args) != nil || len(args) != 2 || json.Unmarshal(args["asin"], &asin) != nil || !sorftimeASIN.MatchString(asin) || json.Unmarshal(args["amz_site"], &site) != nil || site != "US" {
		return decline()
	}
	m.sorftime.mu.Lock()
	verified := m.sorftime.enabled && m.sorftime.verified[p.ThreadID] && m.sorftime.catalogVerified[p.ThreadID] && m.sorftime.startup[p.ThreadID] == "ready"
	m.sorftime.mu.Unlock()
	m.permissionCallbacks.mu.Lock()
	active := p.TurnID != "" && m.permissionCallbacks.activeTurns[p.ThreadID] == p.TurnID
	m.permissionCallbacks.mu.Unlock()
	if !verified || !active {
		return decline()
	}
	// Native Prompt omits tool_name. The name below is the verified single
	// enabled tool, not a guessed Item binding or a parse of the prompt body.
	entry := &runtimeApprovalCallback{view: RuntimeApproval{ID: uuid.NewString(), Kind: "mcp", Summary: "查询单个商品资料", Scope: "Sorftime / product_detail", Reason: "确认本次实际查询参数", Status: "pending", Mcp: &McpApprovalScope{Server: "sorftime", Tool: "product_detail", ASIN: asin, Marketplace: site}}, threadID: p.ThreadID, turnID: p.TurnID, requestKey: request.idKey, resolve: make(chan string, 1), written: make(chan struct{})}
	if err := m.addRuntimeApproval(entry); err != nil {
		return decline()
	}
	select {
	case <-ctx.Done():
		m.finishRuntimeApproval(entry, ctx.Err())
		return serverRequestResult{}
	case decision := <-entry.resolve:
		action := "decline"
		var content any
		if decision == "approve_once" {
			action = "accept"
			content = map[string]any{}
		}
		if decision == "cancel" {
			action = "cancel"
		}
		return serverRequestResult{respond: true, value: map[string]any{"action": action, "content": content, "_meta": nil}, onResponseWritten: func(err error) { m.finishRuntimeApproval(entry, err) }}
	}
}
