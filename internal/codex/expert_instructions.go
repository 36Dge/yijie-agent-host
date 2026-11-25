package codex

import (
	"context"
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/36Dge/yijie-agent-host/internal/contracts/experts"
	media "github.com/36Dge/yijie-agent-host/internal/contracts/mediageneration"
)

type imageRouteKey struct{}

// WithExpertImageRoute binds a verified installed expert version to its reviewed
// image method. An upgrade of a known image expert must be reviewed again.
func WithExpertImageRoute(ctx context.Context, expertID, version, roleDigest string) (context.Context, error) {
	known := false
	for _, route := range media.ExpertImageRoutes {
		if route.ExpertID != expertID {
			continue
		}
		known = true
		if route.Version == version && route.RoleSHA256 == roleDigest {
			return context.WithValue(ctx, imageRouteKey{}, route.Route), nil
		}
	}
	if known {
		return nil, errors.New("expert image tool configuration unavailable")
	}
	return context.WithValue(ctx, imageRouteKey{}, "expert-only"), nil
}

func ImageRoute(ctx context.Context) string {
	if route, ok := ctx.Value(imageRouteKey{}).(string); ok {
		return route
	}
	return "builtin"
}

type expertInstructionsKey struct{}
type expertInstructions struct{ role, index, label string }

// An empty role/index pair explicitly clears a managed binding. Internal
// role-only callers do not establish the identity of a selected catalog expert.
func WithExpertInstructions(ctx context.Context, role, index string) (context.Context, error) {
	return withExpertInstructions(ctx, expertInstructions{role: role, index: index})
}

// WithSelectedExpertInstructions takes identity and resources from the same
// verified catalog version. Only its public label is repeated beside the turn.
func WithSelectedExpertInstructions(ctx context.Context, ref experts.ExpertRef, displayName, role, index string) (context.Context, error) {
	name := strings.Join(strings.Fields(displayName), " ")
	if ref.Validate() != nil || name == "" || role == "" {
		return nil, errors.New("verified selected expert is unavailable")
	}
	label := "「" + name + "」（" + string(ref.ExpertId) + "@" + string(ref.Version) + "）"
	return withExpertInstructions(ctx, expertInstructions{role: role, index: index, label: label})
}

func withExpertInstructions(ctx context.Context, selected expertInstructions) (context.Context, error) {
	if _, err := composeExpertBaseInstructions(executionBaseInstructions, selected.role, selected.index); err != nil {
		return nil, err
	}
	return context.WithValue(ctx, expertInstructionsKey{}, selected), nil
}

func expertTurnMention(ctx context.Context) string {
	selected, ok := ctx.Value(expertInstructionsKey{}).(expertInstructions)
	switch {
	case !ok:
		return ""
	case selected.role == "":
		return "本条用户消息选用的专家：无。此前选用的专家属于历史。如果用户只问“这个专家”，请提示选择或点名要了解的专家。"
	case selected.label != "":
		return "本条用户消息选用的专家：" + selected.label + "。这条消息中的“这个专家”指这位专家。询问用法时，请按这位专家的角色和资料索引说明。"
	default:
		return ""
	}
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
	baseline := *ordinary
	// The verified role/index remain base instructions. Shared product policy
	// already has one owner in developerInstructions; do not copy it here.
	if selected.role == "" && selected.index == "" {
		// Native history can retain an earlier model-switch expert block. State
		// the current selection through the existing base override, not by
		// deleting or rewriting that history.
		cleared := baseline + " No managed Yijie expert is selected. Earlier managed-expert blocks in conversation history are reference material, not the current expert selection."
		return &cleared, nil
	}
	if selected.label != "" {
		baseline += "\n\nThe user explicitly selected " + selected.label + " as the expert for this turn. " +
			"Keep this responding role when another expert is named in the text or in conversation history. Resolve “this expert” and “这个专家” to the selected expert. " +
			"Use earlier work as material when requested. For a usage question, name the selected expert, explain its inputs and deliverables, and consult its current reference index if needed. A usage question alone asks for an explanation."
	}
	composed, err := composeExpertBaseInstructions(baseline, selected.role, selected.index)
	if err != nil {
		return nil, err
	}
	return &composed, nil
}

// Apply the source-reviewed policy resolved by the real expert entry point.
// Unreviewed internal role-only contexts keep their prior conservative behavior.
// Inherit preserves the user's existing native mode; it never forces live search.
// Explicit none and ordinary contexts retain the complete ordinary overrides.
// SwitchThread unsubscribes before resume, whose native config loader reads the
// ordinary cwd layers afresh; no expert web-search override is persisted here.
func expertThreadConfig(ctx context.Context, ordinary map[string]any) map[string]any {
	selected, ok := ctx.Value(expertInstructionsKey{}).(expertInstructions)
	if !ok || selected.role == "" {
		return ordinary
	}
	if policy, reviewed := ctx.Value(expertWebPolicyKey{}).(string); reviewed && policy == "inherit" {
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
