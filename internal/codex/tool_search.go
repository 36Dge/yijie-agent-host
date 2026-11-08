package codex

import (
	"encoding/json"
	"errors"
	"path/filepath"

	native "github.com/36Dge/yijie-agent-host/internal/contracts/runtimetoolsearch"
)

const toolSearchCatalogName = "yijie-tool-search-models-v1.json"

// The catalogue selects native behavior; Host never indexes or searches tools.
func toolSearchCatalog() ([]byte, error) {
	raw, err := nativeToolsCatalog()
	if err != nil {
		return nil, err
	}
	var catalog struct {
		Models []map[string]any `json:"models"`
	}
	var profile struct {
		Models map[string]map[string]any `json:"model_overrides"`
	}
	if json.Unmarshal(raw, &catalog) != nil || json.Unmarshal(native.Profile, &profile) != nil || len(profile.Models) != 1 {
		return nil, errors.New("tool search profile unavailable")
	}
	applied := 0
	for _, model := range catalog.Models {
		slug, _ := model["slug"].(string)
		if overrides, ok := profile.Models[slug]; ok {
			if slug != "kimi-k3.1-pro" {
				return nil, errors.New("tool search model unavailable")
			}
			for key, value := range overrides {
				model[key] = value
			}
			applied++
		}
	}
	if applied != 1 {
		return nil, errors.New("tool search catalogue unavailable")
	}
	return json.MarshalIndent(catalog, "", "  ")
}

func prepareToolSearchLayer(a *managedCodexHomeAuthority) error {
	catalog, err := toolSearchCatalog()
	if err != nil {
		return err
	}
	plan, err := a.preflightManagedFile(toolSearchCatalogName, catalog, catalog)
	if err != nil {
		return err
	}
	return a.applyManagedFile(plan)
}

func validateToolSearchLayer(a *managedCodexHomeAuthority) error {
	catalog, err := toolSearchCatalog()
	if err != nil {
		return err
	}
	return a.validateManagedFileExact(toolSearchCatalogName, catalog)
}

// Only ordinary start/resume use this override. Title and input-only sessions
// retain the process catalogue. Explicitly restoring the baseline when disabled
// also prevents a resumed thread from inheriting its previous search catalogue.
func (m *Manager) toolSearchThreadConfig(config map[string]any) (map[string]any, error) {
	if !m.config.NativeToolsEnabled {
		return config, nil
	}
	m.mu.Lock()
	authority := m.codexHomeAuthority
	ready := m.status.Ready && !m.shutdownRequested
	m.mu.Unlock()
	if authority == nil || !ready {
		return nil, errors.New("tool catalogue authority unavailable")
	}
	name := nativeToolsCatalogName
	if m.config.ToolSearchEnabled {
		if err := validateToolSearchLayer(authority); err != nil {
			return nil, errors.New("tool search catalogue changed")
		}
		name = toolSearchCatalogName
	} else if err := validateNativeToolsLayer(authority); err != nil {
		return nil, errors.New("native catalogue changed")
	}
	if config == nil {
		config = make(map[string]any)
	}
	config["model_catalog_json"] = filepath.Join(authority.path, name)
	return config, nil
}
