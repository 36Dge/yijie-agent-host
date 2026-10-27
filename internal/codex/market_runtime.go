package codex

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"reflect"
	"sync"

	wire "github.com/36Dge/yijie-agent-host/internal/contracts/marketbrokercontrol"
	"github.com/36Dge/yijie-agent-host/internal/marketbroker"
	"github.com/google/uuid"
)

// MarketConfig is private process assembly, never a renderer or HTTP DTO.
type MarketConfig struct{ Manifest, LibraryHome, HostInstanceID string }

func (c MarketConfig) enabled() bool { return c.Manifest != "" }
func (c MarketConfig) validate() error {
	if !c.enabled() && c.LibraryHome == "" && c.HostInstanceID == "" {
		return nil
	}
	if !filepath.IsAbs(c.Manifest) || !filepath.IsAbs(c.LibraryHome) || wire.CanonicalId(c.HostInstanceID).Validate() != nil {
		return errors.New("market worker assembly is invalid")
	}
	return nil
}

type marketRuntimeState struct {
	mu         sync.Mutex
	owner      *marketbroker.Owner
	process    wire.ProcessBinding
	gatewayURL string
	turns      map[string]*marketTurnState
}

func (m *Manager) MarketEnabled() bool { return m.config.Market.enabled() }

// StartMarketProvider starts owner control without a Runtime binding. OAuth can
// therefore be configured even when no model/Runtime is ready.
func (m *Manager) StartMarketProvider(ctx context.Context) error {
	if !m.MarketEnabled() {
		return nil
	}
	if err := m.config.Market.validate(); err != nil {
		return err
	}
	m.mu.Lock()
	closing, generation := m.shutdownRequested, m.runtimeGeneration
	if closing {
		m.mu.Unlock()
		return marketbroker.ErrClosed
	}
	// Acquire the owner lock before releasing lifecycle admission, so a
	// concurrent normal shutdown cannot miss a just-starting provider child.
	m.market.mu.Lock()
	m.mu.Unlock()
	defer m.market.mu.Unlock()
	if m.market.owner != nil {
		if m.market.process.RuntimeGeneration == "" || string(m.market.process.RuntimeGeneration) == generation {
			return m.market.owner.StartControl(ctx)
		}
		if err := m.market.owner.Close(ctx); err != nil {
			return err
		}
	}
	kimiKey := ""
	if m.config.ChatModelsEnabled {
		kimiKey = m.config.KimiAPIKey
	}
	owner, err := marketbroker.NewOwnerWithModelCredential(m.config.Market.Manifest, m.config.Market.LibraryHome, kimiKey)
	if err != nil {
		return err
	}
	m.market.owner, m.market.process, m.market.gatewayURL = owner, wire.ProcessBinding{}, ""
	m.market.turns = make(map[string]*marketTurnState)
	return owner.StartControl(ctx)
}

// Initialize only after the fixed Runtime process completed its real handshake.
func (m *Manager) StartMarketBroker(ctx context.Context) error {
	if !m.MarketEnabled() {
		return nil
	}
	m.mu.Lock()
	process := wire.ProcessBinding{HostInstanceId: wire.CanonicalId(m.config.Market.HostInstanceID), RuntimeGeneration: wire.CanonicalId(m.runtimeGeneration)}
	m.mu.Unlock()
	m.market.mu.Lock()
	defer m.market.mu.Unlock()
	if m.market.owner == nil {
		return marketbroker.ErrNotStarted
	}
	if m.market.gatewayURL != "" {
		if m.market.process == process {
			return nil
		}
		return marketbroker.ErrBinding
	}
	response, err := m.market.owner.Initialize(ctx, wire.InitializeRequest{SchemaVersion: 1, RequestId: wire.CanonicalId(uuid.NewString()), Method: "initialize", Payload: wire.InitializePayload{OperationId: wire.CanonicalId(uuid.NewString()), Process: process}})
	if err != nil {
		return err
	}
	m.market.process, m.market.gatewayURL = process, string(response.Data.GatewayUrl)
	return nil
}

// MarketOwner is available only to the private Native control adapter; it does
// not imply Runtime/model readiness or a tool grant.
func (m *Manager) MarketOwner() (*marketbroker.Owner, error) {
	m.market.mu.Lock()
	defer m.market.mu.Unlock()
	if m.market.owner == nil {
		return nil, marketbroker.ErrNotStarted
	}
	return m.market.owner, nil
}

func (m *Manager) closeMarketBroker(ctx context.Context) error {
	m.market.mu.Lock()
	owner := m.market.owner
	m.market.mu.Unlock()
	if owner == nil {
		return nil
	}
	return owner.Close(ctx)
}

func (m *Manager) marketRuntimeEnvironment(current []string) ([]string, error) {
	if !m.MarketEnabled() {
		return current, nil
	}
	m.market.mu.Lock()
	owner := m.market.owner
	process := wire.ProcessBinding{HostInstanceId: wire.CanonicalId(m.config.Market.HostInstanceID), RuntimeGeneration: wire.CanonicalId(m.runtimeGeneration)}
	m.market.process = process
	m.market.mu.Unlock()
	if owner == nil {
		return nil, marketbroker.ErrNotStarted
	}
	return owner.ReserveRuntimeEnvironment(current, process)
}

func marketExcludedEnvironment() []any {
	return []any{ArtifactMCPTokenEnv, SorftimeSecretEnv, SorftimeEnabledEnv, MiniMaxRuntimeEnvKey, KimiRuntimeEnvKey, "MOONSHOT_API_KEY", "YIJIE_KIMI_API_KEY", "YIJIE_KIMI_API_KEY_FILE", marketbroker.RuntimeCapabilityEnvironment}
}

// This additional command-line layer leaves all existing provider/model source
// files byte-identical. It contains only variable names and boolean policy.
func marketNativeLayerArguments() ([]string, error) {
	return nativeConfigArguments([]byte(`[features]
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
exclude = ["YIJIE_ARTIFACT_MCP_TOKEN", "YIJIE_FEAT144_SORFTIME_ACCOUNT_SK", "YIJIE_FEAT144_SORFTIME_ENABLED", "MINIMAX_API_KEY", "KIMI_API_KEY", "MOONSHOT_API_KEY", "YIJIE_KIMI_API_KEY", "YIJIE_KIMI_API_KEY_FILE", "YIJIE_MARKET_RUNTIME_CAPABILITY"]
set = {}
`))
}

func validateMarketEffectiveIsolation(config map[string]json.RawMessage) error {
	var features map[string]any
	if json.Unmarshal(config["features"], &features) != nil {
		return errors.New("market execution isolation unavailable")
	}
	for _, name := range []string{"hooks", "plugins", "apps", "tool_suggest", "shell_snapshot", "multi_agent", "js_repl", "memories"} {
		if features[name] != false {
			return errors.New("market execution isolation mismatch")
		}
	}
	if features["tool_call_mcp_elicitation"] != true {
		return errors.New("market native elicitation unavailable")
	}
	var environment map[string]any
	if json.Unmarshal(config["shell_environment_policy"], &environment) != nil || !reflect.DeepEqual(environment, map[string]any{
		"inherit": "core", "exclude": marketExcludedEnvironment(), "set": map[string]any{}, "ignore_default_excludes": nil, "include_only": nil, "experimental_use_profile": nil,
	}) {
		return errors.New("market capability environment isolation mismatch")
	}
	return nil
}

func (m *Manager) validateMarketIsolation(ctx context.Context, cwd string) error {
	if !m.MarketEnabled() {
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
		return errors.New("market native configuration unavailable")
	}
	return validateMarketEffectiveIsolation(response.Config)
}

// CloseMarketAdmission is an irreversible Native authority boundary. It closes
// the worker input before HTTP draining and prevents startup racing past EOF.
func (m *Manager) CloseMarketAdmission(ctx context.Context) error {
	m.mu.Lock()
	m.shutdownRequested = true
	cancel := m.startupCancel
	m.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	return m.closeMarketBroker(ctx)
}
