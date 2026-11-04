package codex

import (
	"context"
	"errors"

	agenthost "github.com/36Dge/yijie-agent-host/internal/contracts"
)

type toolProfileKey struct{}

// Product choice only. Native feature configuration owns tool registration.
func WithToolProfile(ctx context.Context, profile agenthost.ToolProfile) context.Context {
	return context.WithValue(ctx, toolProfileKey{}, profile)
}

func ToolProfile(ctx context.Context) agenthost.ToolProfile {
	profile, _ := ctx.Value(toolProfileKey{}).(agenthost.ToolProfile)
	return profile
}

func (m *Manager) ToolProfilesEnabled() bool {
	return m.config.KimiK31Candidate && m.config.ChatModelsEnabled && m.config.RuntimePermissionsEnabled
}

func (m *Manager) toolProfileConfig(ctx context.Context, config map[string]any) (map[string]any, error) {
	profile := ToolProfile(ctx)
	if profile == "" || profile == "full" {
		return config, nil
	}
	if profile != "light" || !m.ToolProfilesEnabled() || isScheduledDraft(ctx) {
		return nil, errors.New("tool profile is unavailable")
	}
	if expert, ok := ctx.Value(expertInstructionsKey{}).(expertInstructions); ok && expert.role != "" {
		return nil, errors.New("experts require the full tool profile")
	}
	projected := make(map[string]any, len(config)+2)
	for key, value := range config {
		projected[key] = value
	}
	projected["features.multi_agent"] = false
	projected["features.goals"] = false
	return projected, nil
}
