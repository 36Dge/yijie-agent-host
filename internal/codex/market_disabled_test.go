package codex

import (
	"encoding/json"
	"testing"
)

func TestFEAT158DisabledMarketConfigurationKeepsValidTransports(t *testing.T) {
	gateway := "http://127.0.0.1:18765"
	for _, inherited := range []map[string]json.RawMessage{
		nil,
		{
			"ordinary_http":  json.RawMessage(`{"url":"http://127.0.0.1:18766/mcp","enabled":true,"startup_timeout_sec":3}`),
			"ordinary_stdio": json.RawMessage(`{"command":"/usr/bin/true","args":[],"enabled":true}`),
		},
		{marketServerName: json.RawMessage(`{"url":"http://127.0.0.1:18765/prior-reference","enabled":true}`)},
	} {
		servers, err := disabledMarketServers(inherited, gateway)
		if err != nil {
			t.Fatal(err)
		}
		if servers[marketServerName] == nil || len(servers) < len(inherited) {
			t.Fatal("disabled inventory lost a managed or inherited server")
		}
		for name, value := range servers {
			server := value.(map[string]any)
			if server["enabled"] != false || (server["command"] == nil && server["url"] == nil) {
				t.Fatal("disabled server omitted native transport or enabled execution", name)
			}
			if raw, ok := inherited[name]; ok {
				var original map[string]any
				if json.Unmarshal(raw, &original) != nil {
					t.Fatal("normal source configuration unavailable")
				}
				if original["enabled"] != true || server["url"] != original["url"] || server["command"] != original["command"] {
					t.Fatal("inherited transport was changed")
				}
			} else if server["url"] != gateway || len(server) != 2 {
				t.Fatal("fresh disabled server gained a capability or extra configuration")
			}
		}
	}
}
