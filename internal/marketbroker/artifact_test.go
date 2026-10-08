package marketbroker

import "testing"

// Pure deployment data: no substitute executable, network or child process.
func TestMarketDeploymentProfilesRemainExplicit(t *testing.T) {
	for _, test := range []struct {
		name     string
		manifest artifact
		accepted bool
	}{
		{"legacy status capability", artifact{SchemaVersion: 1}, true},
		{"OAuth account management", artifact{SchemaVersion: 2, ExternalCallsEnabled: true, ProviderProfile: "tushare-oauth-v1"}, true},
		{"generic discovered tools", artifact{SchemaVersion: 2, ExternalCallsEnabled: true, ProviderProfile: "generic-mcp-v1"}, true},
		{"daily reader deployment", artifact{SchemaVersion: 2, ExternalCallsEnabled: true, ProviderProfile: "tushare-daily-v1"}, true},
		{"daily still declares external network", artifact{SchemaVersion: 2, ProviderProfile: "tushare-daily-v1"}, false},
		{"new profile needs review", artifact{SchemaVersion: 2, ExternalCallsEnabled: true, ProviderProfile: "tushare-daily-v2"}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			if acceptedDeploymentProfile(test.manifest) != test.accepted {
				t.Fatal("deployment capability differs")
			}
		})
	}
}
