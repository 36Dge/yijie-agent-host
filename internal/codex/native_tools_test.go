package codex

import (
	"encoding/json"
	"github.com/pelletier/go-toml/v2"
	"strings"
	"testing"
)

func TestNativeToolsProfile(t *testing.T) {
	config, metadata, err := nativeToolsConfiguration()
	if err != nil {
		t.Fatal(err)
	}
	features := config["features"].(map[string]any)
	if len(features) != 4 || features["multi_agent"] != false || features["memories"] != true {
		t.Fatal("unrelated tool features changed")
	}
	v2 := features["multi_agent_v2"].(map[string]any)
	if v2["enabled"] != true || v2["plaintext_messages"] != true || v2["upgrade_existing_threads"] != true || v2["inherit_mcp_servers"] != false {
		t.Fatal("V2 scope differs")
	}
	memory := config["memories"].(map[string]any)
	encoded, err := toml.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encoded), "max_concurrent_threads_per_session = 4\n") || strings.Contains(string(encoded), "max_concurrent_threads_per_session = 4.0") {
		t.Fatal("native integer became a floating-point value")
	}
	if memory["generate_memories"] != false || memory["use_memories"] != true || memory["dedicated_tools"] != true {
		t.Fatal("manual memory scope differs")
	}
	bytes, err := nativeToolsCatalog()
	if err != nil {
		t.Fatal(err)
	}
	var actual struct {
		Models []map[string]any `json:"models"`
	}
	if err := json.Unmarshal(bytes, &actual); err != nil {
		t.Fatal(err)
	}
	if len(actual.Models) != 2 {
		t.Fatal("model count changed")
	}
	for _, model := range actual.Models {
		for key, expected := range metadata {
			if model[key] != expected {
				t.Fatalf("model metadata %s differs", key)
			}
		}
		if model["include_skills_usage_instructions"] != true || model["supports_search_tool"] != false {
			t.Fatal("unrelated model capabilities changed")
		}
	}
}
