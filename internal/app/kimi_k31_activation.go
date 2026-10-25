package app

import (
	"errors"
	"github.com/36Dge/yijie-agent-host/internal/codex"
)

func loadKimiK31Activation(c *codex.Config, get func(string) string) error {
	for name, target := range map[string]*bool{
		"YIJIE_KIMI_K31_ALLOW_MANAGED_PREDECESSOR": &c.KimiK31AllowManagedPredecessor,
		"YIJIE_KIMI_K31_EXECUTION_PAUSED":          &c.KimiK31ExecutionPaused,
	} {
		value := get(name)
		if value != "" && value != "false" && value != "true" {
			return errors.New("invalid Kimi activation setting")
		}
		if value == "true" && !c.KimiK31Candidate {
			return errors.New("Kimi activation setting requires the local candidate")
		}
		*target = value == "true"
	}
	return nil
}
