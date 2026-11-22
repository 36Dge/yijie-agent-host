package codex

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"time"

	native "github.com/36Dge/yijie-agent-host/internal/contracts/runtimenativetools"
	"github.com/pelletier/go-toml/v2"
)

const nativeToolsCatalogName = "yijie-native-tools-models-v1.json"

// Configuration only. All four tool groups execute in the existing Runtime.
func nativeToolsConfiguration() (map[string]any, map[string]any, error) {
	var profile struct {
		Config map[string]any `json:"native_config"`
		Models map[string]any `json:"model_metadata"`
	}
	decoder := json.NewDecoder(bytes.NewReader(native.Profile))
	decoder.UseNumber()
	if decoder.Decode(&profile) != nil || profile.Config == nil || profile.Models == nil {
		return nil, nil, errors.New("native tool profile unavailable")
	}
	if _, err := nativeToolsIntegerValues(profile.Config); err != nil {
		return nil, nil, err
	}
	return profile.Config, profile.Models, nil
}

// Configure the Runtime's native adapter for models that require plain functions.
// Preserve namespaces already contributed by the other owned MCP integrations.
func addNativeFunctionNamespaces(config map[string]any, names ...string) error {
	const key = "features.code_mode.function_namespaces"
	configured, exists := config[key]
	if !exists {
		profile, _, err := nativeToolsConfiguration()
		if err != nil {
			return err
		}
		configured = profile["features"].(map[string]any)["code_mode"].(map[string]any)["function_namespaces"]
	}
	var values []any
	switch list := configured.(type) {
	case []any:
		values = append(values, list...)
	case []string:
		for _, name := range list {
			values = append(values, name)
		}
	default:
		return errors.New("native function namespace configuration unavailable")
	}
	for _, name := range names {
		values = append(values, name)
	}
	seen := make(map[string]bool)
	merged := make([]any, 0, len(values))
	for _, value := range values {
		name, ok := value.(string)
		if !ok {
			return errors.New("invalid native namespace")
		}
		if !seen[name] {
			seen[name] = true
			merged = append(merged, name)
		}
	}
	config[key] = merged
	return nil
}

// Preserve canonical integer types when crossing JSON -> TOML. Rust's native
// FeatureToml deliberately rejects a floating-point thread count such as 4.0.
func nativeToolsIntegerValues(value any) (any, error) {
	switch v := value.(type) {
	case json.Number:
		n, err := v.Int64()
		if err != nil {
			return nil, errors.New("native tool profile integer unavailable")
		}
		return n, nil
	case map[string]any:
		for key, child := range v {
			converted, err := nativeToolsIntegerValues(child)
			if err != nil {
				return nil, err
			}
			v[key] = converted
		}
	case []any:
		for index, child := range v {
			converted, err := nativeToolsIntegerValues(child)
			if err != nil {
				return nil, err
			}
			v[index] = converted
		}
	}
	return value, nil
}

func nativeToolsCatalog() ([]byte, error) {
	return nativeToolsCatalogVersion(true)
}

func nativeToolsCatalogVersion(highDefault bool) ([]byte, error) {
	_, metadata, err := nativeToolsConfiguration()
	if err != nil {
		return nil, err
	}
	current, err := kimiK31CatalogVersion(highDefault)
	if err != nil {
		return nil, err
	}
	var catalog struct {
		Models []map[string]any `json:"models"`
	}
	if json.Unmarshal(current, &catalog) != nil || len(catalog.Models) != 2 {
		return nil, errors.New("native tool models unavailable")
	}
	for _, model := range catalog.Models {
		for key, value := range metadata {
			model[key] = value
		}
	}
	return json.MarshalIndent(catalog, "", "  ")
}

func prepareNativeToolsLayer(a *managedCodexHomeAuthority) ([]string, error) {
	plan, err := preflightKimiCatalog(a, nativeToolsCatalogName, nativeToolsCatalogVersion)
	if err != nil {
		return nil, err
	}
	if err = a.applyManagedFile(plan); err != nil {
		return nil, err
	}
	config, _, err := nativeToolsConfiguration()
	if err != nil {
		return nil, err
	}
	config["model_catalog_json"] = filepath.Join(a.path, nativeToolsCatalogName)
	bytes, err := toml.Marshal(config)
	if err != nil {
		return nil, err
	}
	// Use the existing native -c argument writer after decoding the canonical
	// profile. No settings are written into user-owned config.toml.
	return nativeConfigArguments(bytes)
}

func validateNativeToolsLayer(a *managedCodexHomeAuthority) error {
	catalog, err := nativeToolsCatalog()
	if err != nil {
		return err
	}
	return a.validateManagedFileExact(nativeToolsCatalogName, catalog)
}

func (m *Manager) registerNativeToolsRoot(thread string) {
	if !m.config.NativeToolsEnabled || thread == "" {
		return
	}
	c := &m.permissionCallbacks
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.nativeRoots == nil {
		c.nativeRoots = make(map[string]bool)
	}
	c.nativeRoots[thread] = true
}

// Resolve only Runtime-owned ancestry to a root already created/resumed by this
// Host generation. Model arguments never provide an ownership relationship.
func (m *Manager) nativeApprovalOwner(ctx context.Context, thread, turn string) (string, error) {
	return m.nativeApprovalOwnerForTurn(ctx, thread, turn, true)
}

// Native denied actions can be approved after their original turn has ended.
// Only the native thread ancestry is projected; the Runtime still validates and
// records the exact approval in that child; it does not start a new turn.
func (m *Manager) nativeApprovalOwnerForTurn(ctx context.Context, thread, turn string, requireActive bool) (string, error) {
	c := &m.permissionCallbacks
	c.mu.Lock()
	root := c.nativeRoots[thread]
	active := c.activeTurns[thread] == turn
	c.mu.Unlock()
	if root && (active || !requireActive) {
		return thread, nil
	}
	if !m.config.NativeToolsEnabled {
		return "", errors.New("native approval is not active")
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	seen := make(map[string]bool)
	current := thread
	for depth := 0; depth < 16; depth++ {
		if current == "" || seen[current] {
			break
		}
		seen[current] = true
		var response native.ThreadReadProjection
		if err := m.request(ctx, "thread/read", map[string]any{"threadId": current, "includeTurns": current == thread}, &response); err != nil {
			return "", err
		}
		if response.Thread.ID != current {
			break
		}
		if current == thread {
			confirmed := false
			for _, candidate := range response.Thread.Turns {
				if candidate.ID == turn && (!requireActive || candidate.Status == "inProgress") {
					confirmed = true
				}
			}
			if !confirmed {
				break
			}
		}
		if response.Thread.ParentThreadID == nil {
			break
		}
		parent := *response.Thread.ParentThreadID
		c.mu.Lock()
		owned := c.nativeRoots[parent]
		c.mu.Unlock()
		if owned {
			c.mu.Lock()
			if requireActive {
				if c.activeTurns == nil {
					c.activeTurns = make(map[string]string)
				}
				c.activeTurns[thread] = turn
			}
			c.mu.Unlock()
			return parent, nil
		}
		current = parent
	}
	return "", errors.New("native approval ancestry unavailable")
}

// Notification delivery runs on the stdio reader. Resolve an unobserved child
// asynchronously so thread/read can complete on that same native connection.
func (m *Manager) addNativeGuardianApproval(entry *runtimeApprovalCallback) {
	owner, err := m.nativeApprovalOwnerForTurn(context.Background(), entry.threadID, entry.turnID, false)
	if err != nil {
		return
	}
	entry.ownerThreadID = owner
	if owner != entry.threadID {
		entry.view.Summary = "子任务：" + entry.view.Summary
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.shutdownRequested || !m.status.Ready {
		return
	}
	_ = m.addRuntimeApproval(entry)
}
