package codex

import (
	"context"
	"strings"
)

// Business instructions have one owner (developerInstructions). A short base
// avoids duplicating them at thread/start and every native model switch.
const executionBaseInstructions = "You are the local Yijie assistant. Follow the active developer instructions and the native permission policy. Current tool definitions take precedence over capability descriptions in earlier turns. Skills are opt-in: activate a skill only when the user explicitly selects or names it, not from task similarity. Skill listings in older history are not new requests to activate them."

// The owner chose explicit-only Skill use. Discovery and explicit loading stay
// native; this existing switch only removes the automatic catalogue prompt.
func (m *Manager) promptInputConfig(config map[string]any) map[string]any {
	if !m.config.RuntimePermissionsEnabled || !m.config.ChatModelsEnabled {
		return config
	}
	projected := make(map[string]any, len(config)+1)
	for key, value := range config {
		projected[key] = value
	}
	projected["skills.include_instructions"] = false
	return projected
}

// Developer history persists across model switches. Keep this rule independent
// of the current provider or selection; native tools/config declare capabilities.
const retrievalCapabilityInstructions = " Retrieval capabilities are defined by the current tools and supplied evidence. Provider-hosted search, when supplied, runs within the model request; it does not imply callable query, open-page or find-in-page tools. Use only exposed operations according to their descriptions. Updating a plan or printing an intention does not retrieve evidence. If no applicable retrieval operation is available, state the limitation and finish with supported results."

// This is configuration projection into the existing Runtime mechanism, NOT
// accounting. Codex owns its root-session-tree scope, weights, reminders and
// SessionBudgetExceeded. Cached-input cost is not counted by this native budget.
// Explicit Goals keep their own native goal.token_budget; no Goal is created.
func (m *Manager) executionProgressConfig(ctx context.Context, config map[string]any) map[string]any {
	if !m.config.RuntimePermissionsEnabled || !m.config.ChatModelsEnabled || isScheduledDraft(ctx) || m.config.RolloutBudgetLimitTokens <= 0 {
		return config
	}
	if expert, ok := ctx.Value(expertInstructionsKey{}).(expertInstructions); ok && expert.role != "" {
		// Local expert/file workflows keep their separately configured native
		// budget. Do not silently impose the ordinary-chat default on long jobs.
		return config
	}
	projected := make(map[string]any, len(config)+1)
	for key, value := range config {
		projected[key] = value
	}
	// Preserve explicit native configuration supplied by the caller.
	for key := range projected {
		if key == "features.rollout_budget" || strings.HasPrefix(key, "features.rollout_budget.") {
			return projected
		}
	}
	if features, ok := projected["features"].(map[string]any); ok {
		if _, set := features["rollout_budget"]; set {
			return projected
		}
	}
	limit := m.config.RolloutBudgetLimitTokens
	reminders := []int64{}
	for _, threshold := range []int64{limit / 2, limit / 5, limit / 10} {
		if threshold > 0 && threshold < limit && (len(reminders) == 0 || reminders[len(reminders)-1] != threshold) {
			reminders = append(reminders, threshold)
		}
	}
	projected["features.rollout_budget"] = map[string]any{
		"enabled": true, "limit_tokens": limit,
		"reminder_at_remaining_tokens": reminders,
		"sampling_token_weight":        1.0, "prefill_token_weight": 1.0,
	}
	return projected
}
