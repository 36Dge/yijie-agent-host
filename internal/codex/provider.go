package codex

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/google/uuid"
)

const (
	MiniMaxProviderID        = "minimax"
	MiniMaxModel             = "MiniMax-M3"
	MiniMaxChinaBaseURL      = "https://api.minimaxi.com/v1"
	MiniMaxRuntimeEnvKey     = "MINIMAX_API_KEY"
	FEAT126FakeBaseURL       = "http://127.0.0.1:18082/v1"
	FEAT126FakeFixtureID     = "normal-000"
	feat126RunIDHeader       = "X-Yijie-Feat126-Run-Id"
	feat126FixtureIDHeader   = "X-Yijie-Feat126-Fixture-Id"
	managedConfigMarker      = "# Managed by yijie-agent-host Runtime Baseline 2.\n"
	managedFEAT137RuleMarker = "# Managed by yijie-agent-host FEAT-137 command approval.\n"
	managedModelCatalogName  = "minimax-m3-model-catalog.json"
)

const managedFEAT137ClosedFeatures = `[features]
hooks = false
plugins = false
apps = false
tool_suggest = false
shell_snapshot = false
`

const managedFEAT137ClosedProviderRetries = `request_max_retries = 0
stream_max_retries = 0
`

const managedFEAT137ExecPolicy = managedFEAT137RuleMarker + `prefix_rule(
    pattern=["git", "rev-parse", "--is-inside-work-tree"],
    decision="prompt",
    justification="Confirm the one read-only repository check.",
    match=[["git", "rev-parse", "--is-inside-work-tree"]],
    not_match=[["git", "status"], ["git", "show", "HEAD"]],
)
`

type MiniMaxConfig struct {
	Enabled bool
	APIKey  string
}

// ManagedReasoningProfile selects one of the Host-owned, closed MiniMax
// configurations written into the managed CODEX_HOME. The zero value
// preserves the Runtime Baseline 2 behavior.
type ManagedReasoningProfile uint8

const (
	ManagedReasoningProfileDefault ManagedReasoningProfile = iota
	ManagedReasoningProfileHighRaw
)

func (p ManagedReasoningProfile) validate(miniMaxEnabled, dynamicToolsEnabled bool) error {
	switch p {
	case ManagedReasoningProfileDefault:
		return nil
	case ManagedReasoningProfileHighRaw:
		if !miniMaxEnabled {
			return errors.New("managed high/raw reasoning profile requires the MiniMax provider")
		}
		if dynamicToolsEnabled {
			return errors.New("managed high/raw reasoning profile requires experimental Runtime tools to remain disabled")
		}
		return nil
	default:
		return errors.New("unsupported managed reasoning profile")
	}
}

type FakeResponsesConfig struct {
	Enabled   bool
	BaseURL   string
	RunID     string
	FixtureID string
}

func (c FakeResponsesConfig) validate() error {
	if !c.Enabled {
		if c.BaseURL != "" || c.RunID != "" || c.FixtureID != "" {
			return errors.New("fake Responses configuration is set while the provider is disabled")
		}
		return nil
	}
	if c.BaseURL != FEAT126FakeBaseURL {
		return errors.New("fake Responses base URL must use the fixed FEAT-126 loopback endpoint")
	}
	if c.RunID == "" {
		return errors.New("fake Responses run id is required")
	}
	if !isCanonicalRFC4122UUIDv4(c.RunID) {
		return errors.New("fake Responses run id must be a canonical RFC4122 UUIDv4")
	}
	if c.FixtureID != FEAT126FakeFixtureID {
		return errors.New("fake Responses fixture id must use the frozen FEAT-126 fixture")
	}
	return nil
}

func isCanonicalRFC4122UUIDv4(value string) bool {
	parsed, err := uuid.Parse(value)
	return err == nil && parsed != uuid.Nil && parsed.String() == value &&
		parsed.Version() == uuid.Version(4) && parsed.Variant() == uuid.RFC4122
}

func (c MiniMaxConfig) validate() error {
	if !c.Enabled {
		if c.APIKey != "" {
			return errors.New("MiniMax API key is configured while the provider is disabled")
		}
		return nil
	}
	if c.APIKey == "" {
		return errors.New("MiniMax API key is required")
	}
	if len(c.APIKey) > 16<<10 {
		return errors.New("MiniMax API key exceeds 16 KiB")
	}
	if strings.TrimSpace(c.APIKey) != c.APIKey || strings.ContainsAny(c.APIKey, "\x00\r\n") {
		return errors.New("MiniMax API key contains invalid whitespace or control characters")
	}
	return nil
}

func prepareMiniMaxCodexHome(
	authority *managedCodexHomeAuthority,
	profile ManagedReasoningProfile,
) error {
	return prepareMiniMaxCodexHomeForAuthority(authority, profile, false)
}

func prepareMiniMaxCodexHomeForAuthority(
	authority *managedCodexHomeAuthority,
	profile ManagedReasoningProfile,
	commandApprovalEnabled bool,
	sorftimeEnabled ...bool,
) error {
	if err := profile.validate(true, false); err != nil {
		return err
	}
	if authority == nil {
		return errors.New("managed CODEX_HOME authority is required")
	}
	catalog, err := miniMaxModelCatalog()
	if err != nil {
		return err
	}
	sorftime := len(sorftimeEnabled) == 1 && sorftimeEnabled[0]
	config, err := miniMaxManagedConfigForRuntime(authority.path, profile, commandApprovalEnabled, sorftime)
	if err != nil {
		return err
	}
	defaultConfig, err := miniMaxManagedConfigForAuthority(authority.path, ManagedReasoningProfileDefault, false)
	if err != nil {
		return err
	}
	highRawConfig, err := miniMaxManagedConfigForAuthority(authority.path, ManagedReasoningProfileHighRaw, false)
	if err != nil {
		return err
	}
	defaultApprovalConfig, err := miniMaxManagedConfigForAuthority(
		authority.path, ManagedReasoningProfileDefault, true,
	)
	if err != nil {
		return err
	}
	highRawApprovalConfig, err := miniMaxManagedConfigForAuthority(
		authority.path, ManagedReasoningProfileHighRaw, true,
	)
	if err != nil {
		return err
	}
	defaultMcpConfig, err := miniMaxManagedConfigForRuntime(authority.path, ManagedReasoningProfileDefault, false, true)
	if err != nil {
		return err
	}
	highRawMcpConfig, err := miniMaxManagedConfigForRuntime(authority.path, ManagedReasoningProfileHighRaw, false, true)
	if err != nil {
		return err
	}
	catalogPlan, err := authority.preflightManagedFile(managedModelCatalogName, catalog, catalog)
	if err != nil {
		return fmt.Errorf("preflight MiniMax model catalog: %w", err)
	}
	configPlan, err := authority.preflightManagedFile(
		"config.toml",
		config,
		defaultConfig,
		highRawConfig,
		defaultApprovalConfig,
		highRawApprovalConfig,
		defaultMcpConfig,
		highRawMcpConfig,
	)
	if err != nil {
		return fmt.Errorf("preflight managed CODEX_HOME config: %w", err)
	}
	if err := authority.applyManagedFile(catalogPlan); err != nil {
		return fmt.Errorf("write MiniMax model catalog: %w", err)
	}
	if err := authority.applyManagedFile(configPlan); err != nil {
		return fmt.Errorf("write managed CODEX_HOME config: %w", err)
	}
	return nil
}

func currentUserOwns(info os.FileInfo) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && stat.Uid == uint32(os.Geteuid())
}

func miniMaxManagedConfig(codexHome string, profile ManagedReasoningProfile) ([]byte, error) {
	return miniMaxManagedConfigForAuthority(codexHome, profile, false)
}

func miniMaxManagedConfigForAuthority(
	codexHome string,
	profile ManagedReasoningProfile,
	commandApprovalEnabled bool,
) ([]byte, error) {
	if err := profile.validate(true, false); err != nil {
		return nil, err
	}
	reasoningLines := []string{
		"model_reasoning_effort = \"none\"",
		"model_reasoning_summary = \"none\"",
	}
	if profile == ManagedReasoningProfileHighRaw {
		reasoningLines = []string{
			"model_reasoning_effort = \"high\"",
			"model_reasoning_summary = \"none\"",
			"show_raw_agent_reasoning = true",
		}
	}
	configLines := []string{
		"model = " + strconv.Quote(MiniMaxModel),
		"model_provider = " + strconv.Quote(MiniMaxProviderID),
		"model_context_window = 1000000",
	}
	configLines = append(configLines, reasoningLines...)
	configLines = append(configLines,
		"model_catalog_json = "+strconv.Quote(filepath.Join(codexHome, managedModelCatalogName)),
		"",
	)
	if commandApprovalEnabled {
		configLines = append(configLines, strings.Split(strings.TrimSuffix(managedFEAT137ClosedFeatures, "\n"), "\n")...)
		configLines = append(configLines, "")
	}
	configLines = append(configLines,
		"[model_providers.minimax]",
		"name = \"MiniMax\"",
		"base_url = "+strconv.Quote(MiniMaxChinaBaseURL),
		"env_key = "+strconv.Quote(MiniMaxRuntimeEnvKey),
		"wire_api = \"responses\"",
		"requires_openai_auth = false",
		"supports_websockets = false",
	)
	if commandApprovalEnabled {
		configLines = append(
			configLines,
			strings.Split(strings.TrimSuffix(managedFEAT137ClosedProviderRetries, "\n"), "\n")...,
		)
	}
	configLines = append(configLines, "")
	return []byte(managedConfigMarker + strings.Join(configLines, "\n")), nil
}

func validateFEAT137ManagedConfig(config []byte, commandApprovalEnabled bool) error {
	text := string(config)
	closedBlock := "\n" + managedFEAT137ClosedFeatures + "\n"
	closedTokens := []string{
		"[features]",
		"hooks = false",
		"plugins = false",
		"apps = false",
		"tool_suggest = false",
		"shell_snapshot = false",
	}
	closedRetryTokens := []string{
		"request_max_retries = 0",
		"stream_max_retries = 0",
	}
	if !commandApprovalEnabled {
		for _, token := range append(closedTokens, closedRetryTokens...) {
			if strings.Contains(text, token) {
				return errors.New("gate-off MiniMax config contains FEAT-137 closed features")
			}
		}
		return nil
	}
	if strings.Count(text, closedBlock) != 1 {
		return errors.New("FEAT-137 command approval config requires the exact closed features block")
	}
	for _, token := range closedTokens {
		if strings.Count(text, token) != 1 {
			return errors.New("FEAT-137 command approval config contains duplicate or missing closed features")
		}
	}
	closedRetryBlock := "supports_websockets = false\n" + managedFEAT137ClosedProviderRetries
	if strings.Count(text, closedRetryBlock) != 1 {
		return errors.New("FEAT-137 command approval config requires the exact closed retry policy")
	}
	for _, token := range closedRetryTokens {
		if strings.Count(text, token) != 1 {
			return errors.New("FEAT-137 command approval config contains duplicate or missing closed retry policy")
		}
	}
	for _, forbidden := range []string{"[mcp_servers", "[plugins", "[apps", "dynamic_tools"} {
		if strings.Contains(text, forbidden) {
			return errors.New("FEAT-137 command approval config expands a managed tool surface")
		}
	}
	return nil
}

func prepareFakeResponsesCodexHome(authority *managedCodexHomeAuthority, fake FakeResponsesConfig) error {
	if err := fake.validate(); err != nil {
		return err
	}
	if authority == nil {
		return errors.New("managed CODEX_HOME authority is required")
	}
	catalogPath := filepath.Join(authority.path, managedModelCatalogName)
	catalog, err := miniMaxModelCatalog()
	if err != nil {
		return err
	}

	config := managedConfigMarker + strings.Join([]string{
		"model = " + strconv.Quote(MiniMaxModel),
		"model_provider = " + strconv.Quote(MiniMaxProviderID),
		"model_context_window = 1000000",
		"model_reasoning_effort = \"high\"",
		"model_reasoning_summary = \"none\"",
		"show_raw_agent_reasoning = true",
		"model_catalog_json = " + strconv.Quote(catalogPath),
		"",
		"[features]",
		"plugins = false",
		"",
		"[model_providers.minimax]",
		"name = \"MiniMax FEAT-126 deterministic fake\"",
		"base_url = " + strconv.Quote(fake.BaseURL),
		"wire_api = \"responses\"",
		"requires_openai_auth = false",
		"supports_websockets = false",
		"request_max_retries = 0",
		"stream_max_retries = 0",
		"http_headers = { " + strconv.Quote(feat126RunIDHeader) + " = " + strconv.Quote(fake.RunID) + ", " + strconv.Quote(feat126FixtureIDHeader) + " = " + strconv.Quote(fake.FixtureID) + " }",
		"",
	}, "\n")
	catalogPlan, err := authority.preflightManagedFile(managedModelCatalogName, catalog, catalog)
	if err != nil {
		return fmt.Errorf("preflight FEAT-126 model catalog: %w", err)
	}
	configPlan, err := authority.preflightManagedFile("config.toml", []byte(config), []byte(config))
	if err != nil {
		return fmt.Errorf("preflight managed FEAT-126 CODEX_HOME config: %w", err)
	}
	if err := authority.applyManagedFile(catalogPlan); err != nil {
		return fmt.Errorf("write FEAT-126 model catalog: %w", err)
	}
	if err := authority.applyManagedFile(configPlan); err != nil {
		return fmt.Errorf("write managed FEAT-126 CODEX_HOME config: %w", err)
	}
	return nil
}

func miniMaxModelCatalog() ([]byte, error) {
	catalog := map[string]any{
		"models": []any{map[string]any{
			"slug":                    MiniMaxModel,
			"display_name":            MiniMaxModel,
			"description":             "MiniMax M3 via the China OpenAI-compatible Responses API",
			"default_reasoning_level": "none",
			"supported_reasoning_levels": []any{
				map[string]any{"effort": "none", "description": "Think-Off"},
				map[string]any{"effort": "high", "description": "Adaptive Thinking"},
			},
			"shell_type":                       "shell_command",
			"visibility":                       "list",
			"supported_in_api":                 true,
			"priority":                         0,
			"availability_nux":                 nil,
			"upgrade":                          nil,
			"base_instructions":                "You are Codex, a read-only coding agent based on MiniMax-M3. Answer the user without changing files, running commands that modify state, or requesting additional permissions.",
			"model_messages":                   nil,
			"supports_reasoning_summaries":     true,
			"default_reasoning_summary":        "none",
			"support_verbosity":                false,
			"default_verbosity":                nil,
			"apply_patch_tool_type":            nil,
			"truncation_policy":                map[string]any{"mode": "bytes", "limit": 10_000},
			"supports_parallel_tool_calls":     true,
			"supports_image_detail_original":   false,
			"context_window":                   1_000_000,
			"max_context_window":               1_000_000,
			"auto_compact_token_limit":         900_000,
			"effective_context_window_percent": 95,
			"experimental_supported_tools":     []any{},
			"input_modalities":                 []string{"text", "image"},
			"supports_search_tool":             false,
			"use_responses_lite":               false,
		}},
	}
	content, err := json.MarshalIndent(catalog, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode MiniMax model catalog: %w", err)
	}
	return append(content, '\n'), nil
}
