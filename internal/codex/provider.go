package codex

import (
	"bytes"
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
	managedRulesDirectory    = "rules"
	managedDefaultRulesFile  = "default.rules"
)

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
	codexHome string,
	profile ManagedReasoningProfile,
	commandApprovalEnabled bool,
) error {
	if err := profile.validate(true, false); err != nil {
		return err
	}
	if err := os.Chmod(codexHome, 0o700); err != nil {
		return fmt.Errorf("protect managed CODEX_HOME: %w", err)
	}
	catalogPath := filepath.Join(codexHome, managedModelCatalogName)
	catalog, err := miniMaxModelCatalog()
	if err != nil {
		return err
	}
	if err := writeManagedFile(catalogPath, catalog, false); err != nil {
		return fmt.Errorf("write MiniMax model catalog: %w", err)
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
		"model_catalog_json = "+strconv.Quote(catalogPath),
		"",
		"[model_providers.minimax]",
		"name = \"MiniMax\"",
		"base_url = "+strconv.Quote(MiniMaxChinaBaseURL),
		"env_key = "+strconv.Quote(MiniMaxRuntimeEnvKey),
		"wire_api = \"responses\"",
		"requires_openai_auth = false",
		"supports_websockets = false",
		"",
	)
	config := managedConfigMarker + strings.Join(configLines, "\n")
	if err := writeManagedFile(filepath.Join(codexHome, "config.toml"), []byte(config), true); err != nil {
		return fmt.Errorf("write managed CODEX_HOME config: %w", err)
	}
	if err := reconcileManagedFEAT137ExecPolicy(codexHome, commandApprovalEnabled); err != nil {
		return err
	}
	return nil
}

// reconcileManagedFEAT137ExecPolicy makes the exact FEAT-137 safe prompt rule
// share the same Host-owned CODEX_HOME lifecycle as the managed provider
// config. Runtime loads every *.rules file under this directory, so an
// unmanaged or additional rule is an authority expansion and must fail closed.
// The gate-off path removes only the file carrying this feature's exact marker.
func reconcileManagedFEAT137ExecPolicy(codexHome string, enabled bool) error {
	rulesDirectory := filepath.Join(codexHome, managedRulesDirectory)
	rulesPath := filepath.Join(rulesDirectory, managedDefaultRulesFile)
	info, err := os.Lstat(rulesDirectory)
	if errors.Is(err, os.ErrNotExist) {
		if !enabled {
			return nil
		}
		if err := os.Mkdir(rulesDirectory, 0o700); err != nil {
			return fmt.Errorf("create managed Runtime rules directory: %w", err)
		}
		info, err = os.Lstat(rulesDirectory)
	}
	if err != nil {
		return fmt.Errorf("inspect managed Runtime rules directory: %w", err)
	}
	if !currentUserOwns(info) || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm() != 0o700 {
		return errors.New("managed Runtime rules directory must be an owner-only non-symlink directory")
	}
	entries, err := os.ReadDir(rulesDirectory)
	if err != nil {
		return fmt.Errorf("read managed Runtime rules directory: %w", err)
	}
	for _, entry := range entries {
		if entry.Name() != managedDefaultRulesFile {
			return errors.New("managed Runtime rules directory contains an unexpected entry")
		}
	}

	ruleInfo, err := os.Lstat(rulesPath)
	if errors.Is(err, os.ErrNotExist) {
		if !enabled {
			if len(entries) == 0 {
				_ = os.Remove(rulesDirectory)
			}
			return nil
		}
		if err := writeManagedFile(rulesPath, []byte(managedFEAT137ExecPolicy), false); err != nil {
			return fmt.Errorf("write managed FEAT-137 Runtime exec policy: %w", err)
		}
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect managed FEAT-137 Runtime exec policy: %w", err)
	}
	if !currentUserOwns(ruleInfo) || !ruleInfo.Mode().IsRegular() ||
		ruleInfo.Mode()&os.ModeSymlink != 0 || ruleInfo.Mode().Perm() != 0o600 {
		return errors.New("managed FEAT-137 Runtime exec policy must be an owner-only regular file")
	}
	if ruleInfo.Size() > 4096 {
		return errors.New("managed FEAT-137 Runtime exec policy exceeds its closed size limit")
	}
	existing, err := os.ReadFile(rulesPath)
	if err != nil {
		return fmt.Errorf("read managed FEAT-137 Runtime exec policy: %w", err)
	}
	if !bytes.HasPrefix(existing, []byte(managedFEAT137RuleMarker)) {
		return errors.New("refusing to replace unmanaged Runtime exec policy")
	}
	if enabled {
		if !bytes.Equal(existing, []byte(managedFEAT137ExecPolicy)) {
			return errors.New("managed FEAT-137 Runtime exec policy drifted")
		}
		return nil
	}
	if err := os.Remove(rulesPath); err != nil {
		return fmt.Errorf("remove disabled FEAT-137 Runtime exec policy: %w", err)
	}
	if err := os.Remove(rulesDirectory); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove empty managed Runtime rules directory: %w", err)
	}
	return nil
}

func currentUserOwns(info os.FileInfo) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && stat.Uid == uint32(os.Geteuid())
}

func prepareFakeResponsesCodexHome(codexHome string, fake FakeResponsesConfig) error {
	if err := fake.validate(); err != nil {
		return err
	}
	if err := validateFEAT126CodexHome(codexHome); err != nil {
		return err
	}
	catalogPath := filepath.Join(codexHome, managedModelCatalogName)
	catalog, err := miniMaxModelCatalog()
	if err != nil {
		return err
	}
	if err := writeManagedFile(catalogPath, catalog, false); err != nil {
		return fmt.Errorf("write FEAT-126 model catalog: %w", err)
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
	if err := writeManagedFile(filepath.Join(codexHome, "config.toml"), []byte(config), true); err != nil {
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

func writeManagedFile(path string, content []byte, allowManagedReplacement bool) error {
	existing, err := os.ReadFile(path)
	if err == nil {
		if bytes.Equal(existing, content) {
			return nil
		}
		if !allowManagedReplacement || !bytes.HasPrefix(existing, []byte(managedConfigMarker)) {
			return fmt.Errorf("refusing to overwrite unmanaged file %s", filepath.Base(path))
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}

	temp, err := os.CreateTemp(filepath.Dir(path), ".yijie-managed-*")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)
	if err := temp.Chmod(0o600); err != nil {
		_ = temp.Close()
		return err
	}
	if _, err := temp.Write(content); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Sync(); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	return os.Rename(tempPath, path)
}
