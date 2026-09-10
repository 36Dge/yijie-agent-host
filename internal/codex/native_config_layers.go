package codex

import (
	"bytes"
	"errors"
	"path/filepath"
	"sort"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

// The fixed native app-server persists project trust in config.toml. Its -c
// layer carries our exact, secret-free provider/security configuration instead.
// Keep these ownership domains separate; never discard native project records
// just to make a managed-file comparison succeed.
const managedNativeConfigName = "yijie-runtime-v1.toml"

type nativeProjectTrust struct {
	TrustLevel *string `toml:"trust_level"`
}

type nativeProjectConfig struct {
	Projects map[string]nativeProjectTrust `toml:"projects"`
}

func validateNativeProjectConfig(content []byte) error {
	var config nativeProjectConfig
	if err := toml.NewDecoder(bytes.NewReader(content)).DisallowUnknownFields().Decode(&config); err != nil {
		// TOML errors can quote source contents. Do not surface them.
		return errors.New("native project configuration has unsupported fields or syntax")
	}
	for path, project := range config.Projects {
		if !filepath.IsAbs(path) || filepath.Clean(path) != path || strings.ContainsAny(path, "\x00\r\n") {
			return errors.New("native project configuration has an unsupported path")
		}
		if project.TrustLevel != nil && *project.TrustLevel != "trusted" && *project.TrustLevel != "untrusted" {
			return errors.New("native project configuration has an unsupported trust value")
		}
	}
	return nil
}

func nativeManagedTemplates(home string) ([][]byte, error) {
	var values [][]byte
	for _, profile := range []ManagedReasoningProfile{ManagedReasoningProfileDefault, ManagedReasoningProfileHighRaw} {
		for _, mcp := range []bool{true, false} {
			value, err := miniMaxManagedConfigForRuntime(home, profile, false, mcp)
			if err != nil {
				return nil, err
			}
			values = append(values, value)
		}
	}
	return values, nil
}

func (a *managedCodexHomeAuthority) preflightNativeProjectConfig(legacy [][]byte) (managedFilePlan, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closed {
		return managedFilePlan{}, errors.New("managed CODEX_HOME authority is closed")
	}
	if err := a.validateRootPathLocked(); err != nil {
		return managedFilePlan{}, err
	}
	content, info, exists, err := readOwnedRegularAt(a.root, "config.toml", maxManagedAuthorityFileBytes)
	if err != nil {
		return managedFilePlan{}, err
	}
	if !exists && legacy == nil {
		return managedFilePlan{}, errors.New("native project configuration is missing")
	}
	native := content
	if err := validateNativeProjectConfig(native); err != nil {
		matched := false
		for _, template := range legacy {
			if bytes.HasPrefix(content, template) {
				suffix := content[len(template):]
				if validateNativeProjectConfig(suffix) == nil {
					native = suffix
					matched = true
					break
				}
			}
		}
		if !matched {
			return managedFilePlan{}, errors.New("native config migration requires an exact managed template and valid project records")
		}
	}
	return managedFilePlan{
		name: "config.toml", desired: append([]byte(nil), native...),
		allowedExisting: [][]byte{append([]byte(nil), content...)},
		existed:         exists, existingInfo: info,
	}, nil
}

func prepareNativeLayeredConfig(a *managedCodexHomeAuthority, profile ManagedReasoningProfile, mcp bool) ([]string, error) {
	config, err := miniMaxManagedConfigForRuntime(a.path, profile, false, mcp)
	if err != nil {
		return nil, err
	}
	args, err := nativeConfigArguments(config)
	if err != nil {
		return nil, err
	}
	legacy, err := nativeManagedTemplates(a.path)
	if err != nil {
		return nil, err
	}
	rootPlan, err := a.preflightNativeProjectConfig(legacy)
	if err != nil {
		return nil, err
	}
	configPlan, err := a.preflightManagedFile(managedNativeConfigName, config, legacy...)
	if err != nil {
		return nil, err
	}
	if configPlan.existed && !rootPlan.existed {
		return nil, errors.New("existing native configuration layout has no project config")
	}
	catalog, err := miniMaxModelCatalog()
	if err != nil {
		return nil, err
	}
	catalogPlan, err := a.preflightManagedFile(managedModelCatalogName, catalog, catalog)
	if err != nil {
		return nil, err
	}
	// Prepare the exact managed source before migrating the old root. All
	// preflights precede mutations; each apply rechecks the original identity.
	for _, plan := range []managedFilePlan{catalogPlan, configPlan, rootPlan} {
		if err := a.applyManagedFile(plan); err != nil {
			return nil, err
		}
	}
	return args, nil
}

func (a *managedCodexHomeAuthority) validateNativeLayeredConfig(expected []byte) error {
	if err := a.validateManagedFileExact(managedNativeConfigName, expected); err != nil {
		return err
	}
	// No legacy prefix is accepted after migration. The native root may only
	// contain the closed ProjectConfig shape of the pinned Runtime.
	_, err := a.preflightNativeProjectConfig(nil)
	return err
}

func nativeConfigArguments(content []byte) ([]string, error) {
	var config map[string]any
	if err := toml.Unmarshal(content, &config); err != nil {
		return nil, errors.New("managed native configuration is invalid")
	}
	var args []string
	var appendValue func(string, any) error
	appendValue = func(key string, value any) error {
		if table, ok := value.(map[string]any); ok && len(table) > 0 {
			keys := make([]string, 0, len(table))
			for name := range table {
				keys = append(keys, name)
			}
			sort.Strings(keys)
			for _, name := range keys {
				if !nativeConfigKey(name) {
					return errors.New("managed native configuration has an unsupported key")
				}
				if err := appendValue(key+"."+name, table[name]); err != nil {
					return err
				}
			}
			return nil
		}
		encoded := "{}"
		if _, table := value.(map[string]any); !table {
			bytes, err := toml.Marshal(map[string]any{"value": value})
			if err != nil || !strings.HasPrefix(string(bytes), "value = ") {
				return errors.New("managed native configuration cannot be encoded")
			}
			encoded = strings.TrimSpace(strings.TrimPrefix(string(bytes), "value = "))
		}
		args = append(args, "-c", key+"="+encoded)
		return nil
	}
	keys := make([]string, 0, len(config))
	for name := range config {
		keys = append(keys, name)
	}
	sort.Strings(keys)
	for _, name := range keys {
		if !nativeConfigKey(name) {
			return nil, errors.New("managed native configuration has an unsupported key")
		}
		if err := appendValue(name, config[name]); err != nil {
			return nil, err
		}
	}
	return args, nil
}

// Native CLI splits the key on dots without TOML key unquoting. Our fixed
// source uses only bare segments; never guess how to encode other segments.
func nativeConfigKey(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-') {
			return false
		}
	}
	return true
}

func (m *Manager) usesNativeConfigLayers() bool {
	return m.config.MiniMax.Enabled && m.config.RuntimePermissionsEnabled && !m.config.CommandApprovalEnabled && !m.config.FakeResponses.Enabled
}
