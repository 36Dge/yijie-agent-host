package codex

import (
	"context"
	"reflect"
	"strings"
	"testing"
)

func TestExecutionProgressUsesNativeRolloutBudget(t *testing.T) {
	cfg := DefaultConfig()
	cfg.RuntimePermissionsEnabled, cfg.ChatModelsEnabled = true, true
	m := NewManager(cfg, nil)
	original := map[string]any{"web_search": "disabled", "model_reasoning_effort": "max"}
	got := m.executionProgressConfig(context.Background(), original)
	want := map[string]any{"enabled": true, "limit_tokens": int64(250000), "reminder_at_remaining_tokens": []int64{125000, 50000, 25000}, "sampling_token_weight": 1.0, "prefill_token_weight": 1.0}
	if !reflect.DeepEqual(got["features.rollout_budget"], want) || got["web_search"] != "disabled" || got["model_reasoning_effort"] != "max" {
		t.Fatalf("native budget/config projection mismatch: %#v", got)
	}
	if len(original) != 2 {
		t.Fatal("mutated caller-owned config")
	}
	explicit := map[string]any{"features.rollout_budget": map[string]any{"enabled": false}}
	if !reflect.DeepEqual(m.executionProgressConfig(context.Background(), explicit), explicit) {
		t.Fatal("overrode explicit native budget")
	}
	for _, native := range []map[string]any{
		{"features.rollout_budget.limit_tokens": int64(500000)},
		{"features": map[string]any{"rollout_budget": map[string]any{"enabled": false}}},
	} {
		if !reflect.DeepEqual(m.executionProgressConfig(context.Background(), native), native) {
			t.Fatal("overrode native dotted/table budget configuration")
		}
	}
	expert, err := WithExpertInstructions(context.Background(), "ordinary expert role", "ordinary local index")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(m.executionProgressConfig(expert, original), original) {
		t.Fatal("ordinary budget imposed on expert workflow")
	}
	cfg.RolloutBudgetLimitTokens = 0
	if !reflect.DeepEqual(NewManager(cfg, nil).executionProgressConfig(context.Background(), original), original) {
		t.Fatal("zero should preserve native configuration")
	}
}

func TestExecutionProgressRetrievalCapabilitiesMatchRoute(t *testing.T) {
	if !strings.Contains(retrievalCapabilityInstructions("kimi", nil), "does not expose a callable query") {
		t.Fatal("hosted-only capabilities missing")
	}
	for _, provider := range []string{"kimi", "minimax"} {
		if strings.Contains(retrievalCapabilityInstructions(provider, map[string]any{"web_search": "disabled"}), "does not expose a callable query") {
			t.Fatal("disabled hosted route advertised")
		}
	}
	if executionBaseInstructions == permissionInstructions || strings.Contains(executionBaseInstructions, "[yijie-answer-completion-v1]") {
		t.Fatal("business policy duplicated in base")
	}
}

func TestExecutionProgressPreservesExpertBasePolicy(t *testing.T) {
	cfg := DefaultConfig()
	cfg.RuntimePermissionsEnabled, cfg.ChatModelsEnabled = true, true
	m := NewManager(cfg, nil)
	ctx, err := WithExpertInstructions(context.Background(), "ordinary expert role", "ordinary index")
	if err != nil {
		t.Fatal(err)
	}
	base := executionBaseInstructions
	got, err := m.expertBaseInstructions(ctx, &base)
	if err != nil || got == nil || !strings.HasPrefix(*got, permissionInstructions+"\n\n") {
		t.Fatalf("existing expert policy priority lost: %v", err)
	}
	ordinary, err := m.expertBaseInstructions(context.Background(), &base)
	if err != nil || ordinary == nil || *ordinary != executionBaseInstructions {
		t.Fatal("ordinary base unexpectedly expanded")
	}
}
