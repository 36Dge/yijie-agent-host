package codex

import (
	"context"
	"strings"
)

// Business instructions have one owner (developerInstructions). A short base
// avoids duplicating them at thread/start and every native model switch.
const executionBaseInstructions = "You are the local Yijie assistant. Follow the active developer instructions and the native permission policy."

const kimiHostedRetrievalInstructions = " Current retrieval capabilities: if provider-hosted web_search is exposed, it runs within a model request. It does not expose a callable query, open-page or find-in-page tool; if absent from the tools, hosted search is unavailable. Updating a plan or running a local command does not execute the query named in that plan. Use the search evidence already supplied. If the available route cannot obtain missing evidence, state that limitation and finish with supported results; do not call a tool merely to trigger another search."

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

func retrievalCapabilityInstructions(provider string, config map[string]any) string {
	if provider == "kimi" && config["web_search"] != "disabled" {
		return kimiHostedRetrievalInstructions
	}
	return " Current retrieval capabilities are defined by the exposed tools. Use only their documented query and page-reading operations; an unavailable operation must be reported as a limitation, not simulated through planning or dummy commands."
}
