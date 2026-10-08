package codex

import (
	"strings"
	"testing"

	"github.com/36Dge/yijie-agent-host/internal/marketbroker"
)

func TestFEAT157RuntimeEnvironmentDropsAmbientBrokerCapability(t *testing.T) {
	name := marketbroker.RuntimeCapabilityEnvironment
	environment := runtimeEnvironment([]string{name + "=ordinary-prior-generation", strings.ToLower(name) + "=ordinary-prior-generation", "PATH=/usr/bin"}, "/ordinary-managed-home", "", false)
	for _, entry := range environment {
		key, _, _ := strings.Cut(entry, "=")
		if strings.EqualFold(key, name) {
			t.Fatal("ambient market capability reached Runtime environment")
		}
	}
}
