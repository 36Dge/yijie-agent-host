package codex

import (
	"encoding/json"
	"errors"
	"strings"

	legacy "github.com/36Dge/yijie-agent-host/internal/contracts/chatmodels"
	models "github.com/36Dge/yijie-agent-host/internal/contracts/chatmodelsv2"
)

const KimiK31ProfileID = "kimi-k3-1-pro-max-v1"
const kimiK31CatalogName = "yijie-chat-models-v2.json"
const kimiK31ConfigName = "yijie-chat-models-v2.toml"

func kimiK31Profile(id string) (legacy.ModelDefinition, bool) {
	for _, p := range models.Definitions() {
		if string(p.ProfileId) == id && id == KimiK31ProfileID {
			// Private Runtime configuration adapter; never emitted on the v1 wire.
			return legacy.ModelDefinition{ProfileId: legacy.ProfileId(p.ProfileId), Label: p.Label, Provider: p.Provider, Model: p.Model, Effort: p.Effort, ContextWindow: p.ContextWindow}, true
		}
	}
	return legacy.ModelDefinition{}, false
}
func (m *Manager) ChatModelsV2Enabled() bool {
	return m.config.ChatModelsEnabled && m.config.KimiK31Candidate
}
func (m *Manager) ChatModelsV2Ready() bool { return m.ChatModelsV2Enabled() && m.Snapshot().Ready }

func kimiK31Catalog() ([]byte, error) {
	raw, err := chatModelCatalog()
	if err != nil {
		return nil, err
	}
	var catalog map[string]any
	if err = json.Unmarshal(raw, &catalog); err != nil {
		return nil, err
	}
	entries := catalog["models"].([]any)
	for i, p := range models.Definitions() {
		entry := entries[i].(map[string]any)
		entry["slug"], entry["display_name"], entry["description"] = p.Model, p.Label, p.Label+" managed Responses profile"
		entry["default_reasoning_level"], entry["context_window"], entry["max_context_window"] = p.Effort, p.ContextWindow, p.ContextWindow
	}
	return json.MarshalIndent(catalog, "", "  ")
}

// The comment is a managed-file writer boundary, not a Runtime option.
// Retained r2 Host exact-byte validation rejects this newer writer revision.
func kimiK31Config(home string) []byte {
	return append([]byte("# yijie-native-failure-history-writer-v1\n"), kimiK31PriorConfig(home)...)
}

// Exact retained r2 configuration recipe; no arbitrary user configuration is accepted.
func kimiK31PriorConfig(home string) []byte {
	config := strings.Replace(string(chatModelConfig(home)), "model = \"kimi-k3\"", "model = \"kimi-k3.1-pro\"", 1)
	config = strings.Replace(config, chatModelsCatalogName, kimiK31CatalogName, 1)
	return []byte(config)
}
func prepareKimiK31Layer(a *managedCodexHomeAuthority) ([]string, error) {
	return prepareKimiK31LayerWithPredecessor(a, false)
}
func prepareKimiK31LayerWithPredecessor(a *managedCodexHomeAuthority, allow bool) ([]string, error) {
	if err := validateKimiK31Predecessor(a, allow); err != nil {
		return nil, err
	}
	catalog, err := kimiK31Catalog()
	if err != nil {
		return nil, err
	}
	config := kimiK31Config(a.path)
	cp, err := a.preflightManagedFile(kimiK31CatalogName, catalog, catalog)
	if err != nil {
		return nil, err
	}
	fp, err := a.preflightManagedFile(kimiK31ConfigName, config, config, kimiK31PriorConfig(a.path))
	if err != nil {
		return nil, err
	}
	if !cp.existed && fp.existed {
		return nil, errors.New("invalid managed model pair order")
	}
	for _, plan := range []managedFilePlan{cp, fp} {
		if err = a.applyManagedFile(plan); err != nil {
			return nil, err
		}
	}
	return nativeConfigArguments(config)
}
func (m *Manager) validateSelectedChatModelLayer(a *managedCodexHomeAuthority) error {
	if !m.config.KimiK31Candidate {
		return validateChatModelLayer(a)
	}
	if err := validateKimiK31Predecessor(a, m.config.KimiK31AllowManagedPredecessor); err != nil {
		return err
	}
	catalog, err := kimiK31Catalog()
	if err != nil {
		return err
	}
	if err = a.validateManagedFileExact(kimiK31CatalogName, catalog); err != nil {
		return err
	}
	return a.validateManagedFileExact(kimiK31ConfigName, kimiK31Config(a.path))
}
