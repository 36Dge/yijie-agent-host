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

	"github.com/google/uuid"
)

const (
	MiniMaxProviderID       = "minimax"
	MiniMaxModel            = "MiniMax-M3"
	MiniMaxChinaBaseURL     = "https://api.minimaxi.com/v1"
	MiniMaxRuntimeEnvKey    = "MINIMAX_API_KEY"
	FEAT126FakeBaseURL      = "http://127.0.0.1:18082/v1"
	FEAT126FakeFixtureID    = "normal-000"
	feat126RunIDHeader      = "X-Yijie-Feat126-Run-Id"
	feat126FixtureIDHeader  = "X-Yijie-Feat126-Fixture-Id"
	managedConfigMarker     = "# Managed by yijie-agent-host Runtime Baseline 2.\n"
	managedModelCatalogName = "minimax-m3-model-catalog.json"
)

type MiniMaxConfig struct {
	Enabled bool
	APIKey  string
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
	parsed, err := uuid.Parse(c.RunID)
	if err != nil || parsed == uuid.Nil || parsed.String() != c.RunID {
		return errors.New("fake Responses run id must be a canonical non-zero UUID")
	}
	if c.FixtureID != FEAT126FakeFixtureID {
		return errors.New("fake Responses fixture id must use the frozen FEAT-126 fixture")
	}
	return nil
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

func prepareMiniMaxCodexHome(codexHome string) error {
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

	config := managedConfigMarker + strings.Join([]string{
		"model = " + strconv.Quote(MiniMaxModel),
		"model_provider = " + strconv.Quote(MiniMaxProviderID),
		"model_context_window = 1000000",
		"model_reasoning_effort = \"none\"",
		"model_reasoning_summary = \"none\"",
		"model_catalog_json = " + strconv.Quote(catalogPath),
		"",
		"[model_providers.minimax]",
		"name = \"MiniMax\"",
		"base_url = " + strconv.Quote(MiniMaxChinaBaseURL),
		"env_key = " + strconv.Quote(MiniMaxRuntimeEnvKey),
		"wire_api = \"responses\"",
		"requires_openai_auth = false",
		"supports_websockets = false",
		"",
	}, "\n")
	if err := writeManagedFile(filepath.Join(codexHome, "config.toml"), []byte(config), true); err != nil {
		return fmt.Errorf("write managed CODEX_HOME config: %w", err)
	}
	return nil
}

func prepareFakeResponsesCodexHome(codexHome string, fake FakeResponsesConfig) error {
	if err := fake.validate(); err != nil {
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
		"[model_providers.minimax]",
		"name = \"MiniMax FEAT-126 deterministic fake\"",
		"base_url = " + strconv.Quote(fake.BaseURL),
		"wire_api = \"responses\"",
		"requires_openai_auth = false",
		"supports_websockets = false",
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
