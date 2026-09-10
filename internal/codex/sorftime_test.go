package codex

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestFEAT144NativeConfigurationUsesOnlyReferences(t *testing.T) {
	value, err := miniMaxManagedConfigForRuntime("/ordinary-managed-home", ManagedReasoningProfileDefault, false, true)
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{`url = "https://mcp.sorftime.com/"`, `bearer_token_env_var = "` + SorftimeSecretEnv + `"`, `"User-Agent" = "codex-mcp-client/0.144.6"`, `enabled_tools = ["product_detail"]`, `approval_mode = "prompt"`, `tool_call_mcp_elicitation = true`, `inherit = "core"`, `set = {}`} {
		if !strings.Contains(string(value), required) {
			t.Fatal("native safety configuration missing")
		}
	}
	if directory := os.Getenv("FEAT144_NATIVE_CONFIG_DIRECTORY"); directory != "" {
		if !filepath.IsAbs(directory) {
			t.Fatal("absolute parser directory required")
		}
		config, err := miniMaxManagedConfigForRuntime(directory, ManagedReasoningProfileDefault, false, true)
		if err != nil {
			t.Fatal(err)
		}
		catalog, err := miniMaxModelCatalog()
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(directory, "config.toml"), config, 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(directory, managedModelCatalogName), catalog, 0600); err != nil {
			t.Fatal(err)
		}
	}
	if strings.Contains(string(value), "?key=") {
		t.Fatal("query authentication is forbidden")
	}
	if _, err := miniMaxManagedConfigForRuntime("/ordinary-managed-home", ManagedReasoningProfileDefault, true, true); err == nil {
		t.Fatal("retired approval profile combined with MCP")
	}
	base, _ := miniMaxManagedConfigForRuntime("/ordinary-managed-home", ManagedReasoningProfileDefault, false, false)
	legacy, _ := miniMaxManagedConfigForAuthority("/ordinary-managed-home", ManagedReasoningProfileDefault, false)
	if string(base) != string(legacy) {
		t.Fatal("original native profile changed")
	}
	environment := runtimeEnvironment([]string{SorftimeSecretEnv + "=public-unit-placeholder", SorftimeEnabledEnv + "=true", "PATH=/usr/bin"}, "/ordinary-managed-home", "", false)
	if strings.Contains(strings.Join(environment, "\n"), "SORFTIME") {
		t.Fatal("ambient credential inherited")
	}
}

func TestFEAT144NativePromptUsesActualParametersAndWrittenDecision(t *testing.T) {
	for _, decision := range []string{"approve_once", "reject", "cancel"} {
		t.Run(decision, func(t *testing.T) {
			config := DefaultConfig()
			config.RuntimePermissionsEnabled = true
			config.Sorftime.Enabled = true
			m := NewManager(config, nil)
			m.sorftime.verified["thread"] = true
			m.sorftime.catalogVerified["thread"] = true
			m.sorftime.startup["thread"] = "ready"
			m.handlePermissionNotification("turn/started", json.RawMessage(`{"threadId":"thread","turn":{"id":"turn"}}`))
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			response := make(chan serverRequestResult, 1)
			go func() {
				response <- m.handleServerRequest(ctx, serverRequest{method: "mcpServer/elicitation/request", idKey: "n:14", params: json.RawMessage(`{"threadId":"thread","turnId":"turn","serverName":"sorftime","mode":"form","message":"Confirm tool call","requestedSchema":{"type":"object","properties":{}},"_meta":{"codex_approval_kind":"mcp_tool_call","tool_params":{"asin":"B07H9PZDQW","amz_site":"US"}}}`)})
			}()
			var views []RuntimeApproval
			for len(views) == 0 {
				select {
				case <-ctx.Done():
					t.Fatal("native Prompt not pending")
				case <-time.After(time.Millisecond):
					views = m.ListRuntimeApprovals("thread")
				}
			}
			if views[0].Mcp == nil || views[0].Mcp.ASIN != "B07H9PZDQW" || views[0].Mcp.Marketplace != "US" || views[0].Kind != "mcp" {
				t.Fatal("actual scope lost")
			}
			done := make(chan error, 1)
			go func() { _, err := m.DecideRuntimeApproval(ctx, "thread", views[0].ID, decision); done <- err }()
			result := <-response
			wire := result.value.(map[string]any)
			want := map[string]string{"approve_once": "accept", "reject": "decline", "cancel": "cancel"}[decision]
			if wire["action"] != want {
				t.Fatal("native action changed")
			}
			select {
			case <-done:
				t.Fatal("UI completed before actual native response write")
			default:
			}
			result.onResponseWritten(nil)
			if err := <-done; err != nil {
				t.Fatal(err)
			}
			if m.ListRuntimeApprovals("thread")[0].Status != map[string]string{"approve_once": "approved", "reject": "rejected", "cancel": "cancelled"}[decision] {
				t.Fatal("callback result missing")
			}
		})
	}
}

func TestFEAT144UnavailableScopeDeclinesAndActiveTurnPreventsStop(t *testing.T) {
	config := DefaultConfig()
	config.RuntimePermissionsEnabled = true
	config.Sorftime.Enabled = true
	m := NewManager(config, nil)
	response := m.handleServerRequest(context.Background(), serverRequest{method: "mcpServer/elicitation/request", params: json.RawMessage(`{"threadId":"thread","turnId":null,"serverName":"sorftime","mode":"form","requestedSchema":{"type":"object","properties":{}},"_meta":{"codex_approval_kind":"mcp_tool_call","tool_params":{"asin":"B07H9PZDQW","amz_site":"US"}}}`)})
	if response.value.(map[string]any)["action"] != "decline" || len(m.ListRuntimeApprovals("thread")) != 0 {
		t.Fatal("missing native binding must decline")
	}
	m.handlePermissionNotification("turn/started", json.RawMessage(`{"threadId":"thread","turn":{"id":"turn"}}`))
	if _, _, err := m.PrepareMcpPermissionScope(context.Background(), PermissionAuto); err == nil || m.shutdownRequested || !m.sorftimeEnabled() {
		t.Fatal("active Turn was stopped")
	}
}

func TestFEAT144CatalogRequiresTheActualNativeSchema(t *testing.T) {
	raw := map[string]any{"data": []any{map[string]any{"name": "sorftime", "authStatus": "bearerToken", "tools": map[string]any{"product_detail": map[string]any{"name": "product_detail", "inputSchema": json.RawMessage(sorftimeInputSchema)}}}}, "nextCursor": nil}
	encoded, _ := json.Marshal(raw)
	var catalog sorftimeCatalog
	if err := json.Unmarshal(encoded, &catalog); err != nil {
		t.Fatal(err)
	}
	if err := validateSorftimeCatalog(catalog); err != nil {
		t.Fatal(err)
	}
	tool := catalog.Data[0].Tools["product_detail"]
	tool.InputSchema = json.RawMessage(`{"type":"object","properties":{"asin":{"type":"string"}},"required":["asin"]}`)
	catalog.Data[0].Tools["product_detail"] = tool
	if err := validateSorftimeCatalog(catalog); err == nil {
		t.Fatal("changed native schema was accepted")
	}
}

// Captured from fixed 0.144.6 config/read without a credential, thread or MCP connection.
func TestFEAT144EffectiveConfigMatchesFixedNativeSerialization(t *testing.T) {
	raw, err := os.ReadFile("testdata/feat144-effective-native-config.json")
	if err != nil {
		t.Fatal(err)
	}
	var config map[string]json.RawMessage
	if err := json.Unmarshal(raw, &config); err != nil {
		t.Fatal(err)
	}
	if err := validateSorftimeEffectiveConfig(config, true); err != nil {
		t.Fatal(err)
	}
	// Unknown/default-less feature values must not count as verified isolation.
	for _, value := range []string{`null`, `true`} {
		var features map[string]json.RawMessage
		if err := json.Unmarshal(config["features"], &features); err != nil {
			t.Fatal(err)
		}
		features["plugins"] = json.RawMessage(value)
		changed, _ := json.Marshal(features)
		candidate := map[string]json.RawMessage{}
		for key, original := range config {
			candidate[key] = original
		}
		candidate["features"] = changed
		if err := validateSorftimeEffectiveConfig(candidate, true); err == nil {
			t.Fatal("unverified feature accepted")
		}
	}
	if err := validateSorftimeEffectiveConfig(config, false); err == nil {
		t.Fatal("configured MCP accepted as deactivated")
	}
}
