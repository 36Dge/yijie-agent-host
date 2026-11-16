package codex

import (
	"encoding/json"
	"testing"
)

func TestMarketOwnedProxyUsesNativeApproveWithoutChangingUserApprovalMode(t *testing.T) {
	proxy := ownedMarketServer("http://127.0.0.1:12345/mcp", "ordinary-lease")
	if proxy["default_tools_approval_mode"] != "approve" || proxy["enabled"] != true || proxy["url"] != "http://127.0.0.1:12345/mcp/ordinary-lease" {
		t.Fatal("owned proxy configuration is incomplete")
	}
	if _, exists := proxy["tools"]; exists {
		t.Fatal("proxy must not overwrite per-tool policy")
	}
	policy, reviewer, _, err := permissionPolicy(PermissionAsk)
	if err != nil || policy != "on-request" || reviewer != "user" {
		t.Fatal("market must still wait for a user decision")
	}
	inherited := map[string]json.RawMessage{"ordinary": json.RawMessage(`{"url":"https://example.invalid/mcp","default_tools_approval_mode":"prompt"}`)}
	servers, err := disabledMarketServers(inherited, "http://127.0.0.1:12345/mcp")
	if err != nil || servers["ordinary"].(map[string]any)["default_tools_approval_mode"] != "prompt" || servers[marketServerName].(map[string]any)["enabled"] != false {
		t.Fatal("empty selection or unrelated server policy changed")
	}
}
