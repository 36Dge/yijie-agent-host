package codex

import (
	"context"
	"reflect"
	"strings"
	"testing"

	web "github.com/36Dge/yijie-agent-host/internal/contracts/builtinweb"
)

func TestFEAT175ResolvedExpertWebPolicyPreservesNativeDefaults(t *testing.T) {
	inherit, err := WithExpertWebPolicy(context.Background(), "reference-expert", "0.1.0", strings.Repeat("b", 64))
	if err != nil {
		t.Fatal(err)
	}
	inherit, err = WithExpertInstructions(inherit, "Verified reference role.", "Verified index.")
	if err != nil || !ExpertWebAllowed(inherit) {
		t.Fatal("ordinary verified expert cannot use web", err)
	}
	for _, mode := range []string{"cached", "live", "disabled"} {
		ordinary := map[string]any{"web_search": mode, "approvals_reviewer": "user"}
		if !reflect.DeepEqual(expertThreadConfig(inherit, ordinary), ordinary) {
			t.Fatal("inherit changed user native mode")
		}
	}
	for _, policy := range web.ExpertPolicies {
		ctx, err := WithExpertWebPolicy(context.Background(), policy.ExpertID, policy.Version, policy.RoleSHA256)
		if err != nil {
			t.Fatal(err)
		}
		ctx, err = WithExpertInstructions(ctx, "Verified offline role.", "Verified index.")
		if err != nil || ExpertWebAllowed(ctx) || expertThreadConfig(ctx, nil)["web_search"] != "disabled" {
			t.Fatal("reviewed offline version was opened", err)
		}
		removed, err := WithExpertInstructions(ctx, "", "")
		if err != nil || !ExpertWebAllowed(removed) || expertThreadConfig(removed, nil) != nil {
			t.Fatal("removal retained offline override", err)
		}
	}
}

func TestFEAT158ExpertWebSearchOverridePreservesOrdinaryConfig(t *testing.T) {
	selected, err := WithExpertInstructions(context.Background(), "Local reference role.", "Local reference index.")
	if err != nil {
		t.Fatal(err)
	}
	removed, err := WithExpertInstructions(context.Background(), "", "")
	if err != nil {
		t.Fatal(err)
	}
	for _, ordinary := range []map[string]any{
		nil,
		{"model_reasoning_effort": "max", "mcp_servers": map[string]any{"ordinary": map[string]any{"enabled": false, "url": "http://127.0.0.1:18083"}}},
		{"web_search": "cached", "approvals_reviewer": "user"},
		{"web_search": "live", "approvals_reviewer": "auto_review"},
		{"web_search": "disabled"},
	} {
		before := make(map[string]any, len(ordinary))
		for key, value := range ordinary {
			before[key] = value
		}
		expert := expertThreadConfig(selected, ordinary)
		if expert["web_search"] != "disabled" {
			t.Fatal("local expert retained built-in web search")
		}
		for key, value := range ordinary {
			if !reflect.DeepEqual(value, before[key]) || (key != "web_search" && !reflect.DeepEqual(expert[key], value)) {
				t.Fatal("expert changed ordinary configuration")
			}
		}
		if !reflect.DeepEqual(expertThreadConfig(removed, ordinary), ordinary) || !reflect.DeepEqual(expertThreadConfig(context.Background(), ordinary), ordinary) {
			t.Fatal("explicit removal or ordinary thread lost its original config")
		}
	}
}

func TestFEAT158ExpertInstructionsPreserveCompleteOrdinaryBaseline(t *testing.T) {
	ordinary := permissionInstructions + "\nCurrent profile-specific instruction.\n"
	role := "把公开资料整理为检查清单，说明缺失依据。"
	index := "参考资料：本地咨询记录（固定版本 1.0.0）。"
	got, err := composeExpertBaseInstructions(ordinary, role, index)
	if err != nil || !strings.HasPrefix(got, ordinary+"\n\n") ||
		strings.Count(got, ordinary) != 1 || !strings.Contains(got, role) || !strings.Contains(got, index) {
		t.Fatal("expert composition did not preserve the complete ordinary baseline and verified resources")
	}
	removed, err := composeExpertBaseInstructions(ordinary, "", "")
	if err != nil || removed != ordinary {
		t.Fatal("removal did not restore the exact complete ordinary baseline")
	}
}

func TestFEAT158ExpertInstructionsRequireBothVerifiedComponents(t *testing.T) {
	for _, input := range [][2]string{{"参考角色", ""}, {"", "参考索引"}} {
		if _, err := composeExpertBaseInstructions(permissionInstructions, input[0], input[1]); err == nil {
			t.Fatal("partial expert resource context was accepted")
		}
	}
	if _, err := composeExpertBaseInstructions("", "参考角色", "参考索引"); err == nil {
		t.Fatal("expert role replaced an unavailable ordinary baseline")
	}
}
