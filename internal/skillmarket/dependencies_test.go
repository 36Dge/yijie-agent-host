package skillmarket

import (
	"encoding/json"
	"testing"

	native "github.com/36Dge/yijie-agent-host/internal/contracts/runtimeskillmarket"
	wire "github.com/36Dge/yijie-agent-host/internal/contracts/skillmarketv2"
)

func TestFEAT174DependencyFactsRemainSeparate(t *testing.T) {
	tools := json.RawMessage(`{"ordinary_tool":{"name":"ordinary_tool","inputSchema":{"type":"object"}}}`)
	cases := []struct {
		name      string
		inventory []native.McpServerStatus
		complete  bool
		want      wire.DependencyState
	}{
		{"missing", nil, true, wire.DependencyStateMissing},
		{"authorization", []native.McpServerStatus{{Name: "ordinary", AuthStatus: "notLoggedIn", Tools: json.RawMessage(`{}`)}}, true, wire.DependencyStateNeedsAuthorization},
		{"ready", []native.McpServerStatus{{Name: "ordinary", AuthStatus: "unsupported", Tools: tools}}, true, wire.DependencyStateReady},
		{"unknown", nil, false, wire.DependencyStateUnknown},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			if got := dependencyState([]string{"ordinary"}, []string{"ordinary"}, test.inventory, test.complete); got != test.want {
				t.Fatal(got)
			}
		})
	}
	if dependencyState([]string{"product.search"}, nil, nil, true) != wire.DependencyStateUnknown {
		t.Fatal("product label was guessed to be a native tool")
	}
}
