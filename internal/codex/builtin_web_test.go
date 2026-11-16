package codex

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	wire "github.com/36Dge/yijie-agent-host/internal/contracts/builtinweb"
)

func TestFEAT175PolicyAndConfiguration(t *testing.T) {
	m := NewManager(Config{BuiltinWebEnabled: true}, nil)
	if err := m.SetBuiltinWebMCP("http://127.0.0.1:18080/mcp", strings.Repeat("a", 64), nil, nil); err != nil {
		t.Fatal(err)
	}
	for _, provider := range []string{"kimi", MiniMaxProviderID} {
		ctx, err := WithExpertWebPolicy(context.Background(), "ordinary-reviewed-expert", "1.0.0", "ordinary-role-digest")
		if err != nil {
			t.Fatal(err)
		}
		ctx, err = WithExpertInstructions(ctx, "Ordinary verified expert instructions.", "Ordinary local reference index.")
		if err != nil {
			t.Fatal(err)
		}
		config, err := m.builtinWebThreadConfig(ctx, map[string]any{"web_search": "cached", "features.code_mode.function_namespaces": []any{"collaboration", "mcp__yijie_media"}, "mcp_servers": map[string]any{"other": map[string]any{"enabled": true}}}, provider)
		if err != nil || config["web_search"] != "cached" {
			t.Fatal("inherit changed hosted", err)
		}
		servers := config["mcp_servers"].(map[string]any)
		if servers[wire.ServerName].(map[string]any)["enabled"] != true || servers["other"] == nil {
			t.Fatal("server composition lost a capability")
		}
		if provider == MiniMaxProviderID && !reflect.DeepEqual(config["features.code_mode.function_namespaces"], []any{"collaboration", "mcp__yijie_media", "mcp__yijie_web"}) {
			t.Fatal("namespace union lost prior tools")
		}
		for _, p := range wire.ExpertPolicies {
			disabled, err := WithExpertWebPolicy(ctx, p.ExpertID, p.Version, p.RoleSHA256)
			if err != nil {
				t.Fatal(err)
			}
			config, err = m.builtinWebThreadConfig(disabled, config, provider)
			if err != nil || config["web_search"] != "disabled" || config["mcp_servers"].(map[string]any)[wire.ServerName].(map[string]any)["enabled"] != false {
				t.Fatal("explicit restriction overwritten", err)
			}
		}
	}
	if _, err := WithExpertWebPolicy(context.Background(), "data-analyst-pro", "future-version", "ordinary-unreviewed-digest"); err == nil {
		t.Fatal("unreviewed version admitted")
	}
	unreviewed, _ := WithExpertInstructions(context.Background(), "Ordinary verified role.", "Ordinary index.")
	if _, err := m.builtinWebThreadConfig(unreviewed, nil, "kimi"); err == nil {
		t.Fatal("missing resolved policy admitted")
	}
}

func TestFEAT175NullableElicitationDeclinesWithoutAuthority(t *testing.T) {
	m := NewManager(Config{BuiltinWebEnabled: true}, nil)
	raw := json.RawMessage(`{"serverName":"yijie_web","threadId":"ordinary-thread","turnId":null,"mode":"form","message":"Read an ordinary source?","requestedSchema":{"type":"object","properties":{}},"_meta":{"codex_approval_kind":"mcp_tool_call"}}`)
	result, handled := m.handleBuiltinWebElicitation(context.Background(), serverRequest{id: json.RawMessage(`"opaque-175"`), params: raw, exactEnvelope: true})
	if !handled || !result.respond || result.value.(wire.McpServerElicitationRequestResponse).Action != wire.Decline {
		t.Fatal("nullable turn was guessed")
	}
}
