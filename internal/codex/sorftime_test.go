package codex

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestFEAT157SorftimeRetirementKeepsMigrationReaderOnly(t *testing.T) {
	for _, profile := range []ManagedReasoningProfile{ManagedReasoningProfileDefault, ManagedReasoningProfileHighRaw} {
		current, err := miniMaxManagedConfigForRuntime("/ordinary-managed-home", profile, false)
		if err != nil {
			t.Fatal(err)
		}
		old, err := legacySorftimeManagedConfig("/ordinary-managed-home", profile)
		if err != nil || !bytes.Equal(old, append(append([]byte{}, current...), []byte(sorftimeManagedBlock)...)) {
			t.Fatal("legacy owned template no longer recognized")
		}
		if bytes.Contains(current, []byte("mcp_servers")) || bytes.Contains(current, []byte(SorftimeSecretEnv)) {
			t.Fatal("new config retained legacy activation")
		}
	}
	environment := runtimeEnvironment([]string{SorftimeSecretEnv + "=ordinary-unit-placeholder", SorftimeEnabledEnv + "=true", SorftimeProxyEnv + "=http://localhost:8080", "PATH=/usr/bin"}, "/ordinary-managed-home", "", false)
	for _, value := range environment {
		if strings.Contains(value, "YIJIE_FEAT144") {
			t.Fatal("legacy input reached Runtime")
		}
	}
}

func TestFEAT157SorftimeLegacyScopeCannotActivateOrApprove(t *testing.T) {
	m := NewManager(Config{RuntimePermissionsEnabled: true}, nil)
	for _, mode := range []PermissionMode{PermissionAsk, PermissionAuto, PermissionFull} {
		active, restarted, err := m.PrepareMcpPermissionScope(context.Background(), mode)
		if err != nil || active || restarted || m.started || m.shutdownRequested {
			t.Fatal("retired compatibility route changed lifecycle")
		}
	}
	if _, _, err := m.PrepareMcpPermissionScope(context.Background(), PermissionMode("")); err == nil {
		t.Fatal("invalid mode accepted")
	}
	// Ordinary old-style prompt receipt, not an external tool call.
	response := m.handleServerRequest(context.Background(), serverRequest{method: "mcpServer/elicitation/request", params: json.RawMessage(`{"threadId":"thread","turnId":"turn","serverName":"sorftime","mode":"form"}`)})
	encoded, _ := json.Marshal(response.value)
	if !response.respond || !strings.Contains(string(encoded), `"action":"decline"`) || len(m.ListRuntimeApprovals("thread")) != 0 {
		t.Fatal("legacy elicitation minted an approval")
	}
	for _, status := range []string{"pending", "approved"} {
		entry := &runtimeApprovalCallback{view: RuntimeApproval{ID: status, Kind: "mcp", Status: status, Mcp: &McpApprovalScope{Server: "sorftime", Tool: "product_detail"}}, threadID: "thread", turnID: "turn", resolve: make(chan string, 1)}
		if err := m.addRuntimeApproval(entry); err != nil {
			t.Fatal(err)
		}
		for _, decision := range []string{"approve_once", "reject", "cancel"} {
			if _, err := m.DecideRuntimeApproval(context.Background(), "thread", status, decision); err == nil {
				t.Fatal("historical receipt acquired authority")
			}
		}
		if len(entry.resolve) != 0 || entry.decision != "" || entry.view.Status != status {
			t.Fatal("historical receipt was dispatched or rewritten")
		}
	}
	if len(m.ListRuntimeApprovals("thread")) != 2 {
		t.Fatal("historical views lost")
	}
}
