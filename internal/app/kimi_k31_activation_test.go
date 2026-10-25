package app

import (
	"github.com/36Dge/yijie-agent-host/internal/codex"
	"testing"
)

func TestFEAT164ActivationSettingsAreExplicit(t *testing.T) {
	for _, name := range []string{"YIJIE_KIMI_K31_ALLOW_MANAGED_PREDECESSOR", "YIJIE_KIMI_K31_EXECUTION_PAUSED"} {
		for _, value := range []string{"", "false", "true", "yes"} {
			for _, candidate := range []bool{false, true} {
				c := codex.Config{KimiK31Candidate: candidate}
				err := loadKimiK31Activation(&c, func(key string) string {
					if key == name {
						return value
					}
					return ""
				})
				wantErr := value == "yes" || (value == "true" && !candidate)
				if (err != nil) != wantErr {
					t.Fatal(name, value, candidate, err)
				}
				if err == nil && (c.KimiK31ExecutionPaused || c.KimiK31AllowManagedPredecessor) != (value == "true") {
					t.Fatal("flag changed")
				}
			}
		}
	}
}
