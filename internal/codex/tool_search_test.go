package codex

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	native "github.com/36Dge/yijie-agent-host/internal/contracts/runtimetoolsearch"
	"github.com/santhosh-tekuri/jsonschema/v6"
)

func TestToolSearchCatalogPreservesOtherCapabilities(t *testing.T) {
	raw, err := toolSearchCatalog()
	if err != nil {
		t.Fatal(err)
	}
	original, err := nativeToolsCatalog()
	if err != nil {
		t.Fatal(err)
	}
	var updated, base struct {
		Models []map[string]any `json:"models"`
	}
	if json.Unmarshal(raw, &updated) != nil || json.Unmarshal(original, &base) != nil {
		t.Fatal("invalid catalogue")
	}
	if len(updated.Models) != len(base.Models) {
		t.Fatal("model count changed")
	}
	for i, model := range updated.Models {
		if model["slug"] == "kimi-k3.1-pro" {
			if model["supports_search_tool"] != true || model["tool_search_transport"] != "function_code_mode" {
				t.Fatal("search profile missing")
			}
			model["supports_search_tool"] = false
			delete(model, "tool_search_transport")
		}
		if !reflect.DeepEqual(model, base.Models[i]) {
			t.Fatal("unrelated model capability changed")
		}
	}
	var schema, catalog any
	if json.Unmarshal(native.ModelCatalogSchema, &schema) != nil || json.Unmarshal(raw, &catalog) != nil {
		t.Fatal("canonical schema unavailable")
	}
	compiler := jsonschema.NewCompiler()
	if err := compiler.AddResource("urn:yijie:tool-search:models", schema); err != nil {
		t.Fatal(err)
	}
	compiled, err := compiler.Compile("urn:yijie:tool-search:models")
	if err != nil {
		t.Fatal(err)
	}
	if err := compiled.Validate(catalog); err != nil {
		t.Fatal(err)
	}
}

func TestToolSearchConfigRequiresExistingNativeProfile(t *testing.T) {
	config := DefaultConfig()
	config.ToolSearchEnabled = true
	if config.validate() == nil {
		t.Fatal("unpaired search accepted")
	}
}

func TestToolSearchThreadCatalogAndDisableRestore(t *testing.T) {
	parent, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	home := filepath.Join(parent, "runtime")
	if err := os.Mkdir(home, 0700); err != nil {
		t.Fatal(err)
	}
	authority, err := acquireManagedCodexHomeAuthority(home)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := authority.releaseWithoutCleanup(); err != nil {
			t.Error(err)
		}
	}()
	if _, err := prepareNativeToolsLayer(authority); err != nil {
		t.Fatal(err)
	}
	if err := prepareToolSearchLayer(authority); err != nil {
		t.Fatal(err)
	}
	manager := NewManager(Config{NativeToolsEnabled: true, ToolSearchEnabled: true}, nil)
	manager.codexHomeAuthority = authority
	manager.status.Ready = true
	original := map[string]any{"model_reasoning_effort": "max"}
	selected, err := manager.toolSearchThreadConfig(original)
	if err != nil || selected["model_catalog_json"] != filepath.Join(home, toolSearchCatalogName) || selected["model_reasoning_effort"] != "max" {
		t.Fatal("scoped catalogue missing", err)
	}
	manager.config.ToolSearchEnabled = false
	restored, err := manager.toolSearchThreadConfig(selected)
	if err != nil || restored["model_catalog_json"] != filepath.Join(home, nativeToolsCatalogName) {
		t.Fatal("disabled search retained old catalogue", err)
	}
	if err := validateNativeToolsLayer(authority); err != nil {
		t.Fatal("process catalogue was changed", err)
	}
}
