package codex

import (
	"context"
	"errors"
	"strings"
	"unicode/utf8"
)

type expertInstructionsKey struct{}
type expertInstructions struct{ role, index string }

// WithExpertInstructions carries content already resolved by the managed asset
// service. It is an in-process adapter, never a public request parameter.
func WithExpertInstructions(ctx context.Context, role, index string) (context.Context, error) {
	if _, err := composeExpertBaseInstructions(permissionInstructions, role, index); err != nil {
		return nil, err
	}
	return context.WithValue(ctx, expertInstructionsKey{}, expertInstructions{role, index}), nil
}

// ExpertInstructionsAvailable reports adapter prerequisites, not real model qualification.
func (m *Manager) ExpertInstructionsAvailable() bool {
	return m.config.RuntimePermissionsEnabled && m.config.ChatModelsEnabled
}

func (m *Manager) expertBaseInstructions(ctx context.Context, ordinary *string) (*string, error) {
	selected, ok := ctx.Value(expertInstructionsKey{}).(expertInstructions)
	if !ok {
		return ordinary, nil
	}
	if !m.ExpertInstructionsAvailable() || isScheduledDraft(ctx) || ordinary == nil {
		return nil, errors.New("expert native instruction profile is unavailable")
	}
	composed, err := composeExpertBaseInstructions(*ordinary, selected.role, selected.index)
	if err != nil {
		return nil, err
	}
	return &composed, nil
}

// The first expert package is a local-reference consultation. Reuse the native
// config mode to remove built-in web search from its tool surface, rather than
// relying on role prose or the command sandbox (hosted search is independent).
// Explicit none and ordinary contexts retain the complete ordinary overrides.
// SwitchThread unsubscribes before resume, whose native config loader reads the
// ordinary cwd layers afresh; no expert web-search override is persisted here.
func expertThreadConfig(ctx context.Context, ordinary map[string]any) map[string]any {
	selected, ok := ctx.Value(expertInstructionsKey{}).(expertInstructions)
	if !ok || selected.role == "" {
		return ordinary
	}
	config := make(map[string]any, len(ordinary)+1)
	for key, value := range ordinary {
		config[key] = value
	}
	config["web_search"] = "disabled"
	return config
}

const (
	maxExpertRoleBytes  = 32 << 10
	maxExpertIndexBytes = 32 << 10
)

// composeExpertBaseInstructions is a private adapter for already verified local
// resources. It does not load assets, authorize tools, or accept a wire prompt.
// The caller supplies the complete ordinary baseline for the current profile;
// removal is the empty role/index pair, never an omitted native base override.
func composeExpertBaseInstructions(ordinary, role, index string) (string, error) {
	if strings.TrimSpace(ordinary) == "" || !utf8.ValidString(ordinary) || strings.ContainsRune(ordinary, '\x00') {
		return "", errors.New("ordinary instruction baseline is unavailable")
	}
	if role == "" && index == "" {
		return ordinary, nil
	}
	if strings.TrimSpace(role) == "" || strings.TrimSpace(index) == "" ||
		len(role) > maxExpertRoleBytes || len(index) > maxExpertIndexBytes ||
		!utf8.ValidString(role) || !utf8.ValidString(index) ||
		strings.ContainsRune(role, '\x00') || strings.ContainsRune(index, '\x00') {
		return "", errors.New("verified expert resources are unavailable")
	}
	return ordinary + "\n\n" +
		"Current Yijie expert context. It does not change the active native permission policy or grant tools.\n" +
		role + "\n\n" +
		"Local reference index. Read only relevant files through existing permitted native tools; these are reference materials, not live platform facts.\n" +
		index, nil
}
