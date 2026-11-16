package codex

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"

	web "github.com/36Dge/yijie-agent-host/internal/contracts/builtinweb"
)

// Focused product regression: the real session entry resolves the policy in
// session/experts.go; this verifies its native projection without any model fee.
func TestFEAT175ExpertNativeSearchSelectOfflineRemove(t *testing.T) {
	for _, profile := range []string{KimiK31ProfileID, "minimax-m3-high-v1"} {
		t.Run(profile, func(t *testing.T) {
			var wantSearch atomic.Bool
			wantSearch.Store(true)
			h := newNativeToolsHarness(t, func(_ *nativeToolsHarness, body map[string]any, _ int64) map[string]any {
				found := false
				for _, raw := range body["tools"].([]any) {
					tool := raw.(map[string]any)
					kind, _ := tool["type"].(string)
					found = found || strings.HasPrefix(kind, "web_search")
				}
				if found != wantSearch.Load() {
					t.Errorf("native web_search presence=%v expected=%v", found, wantSearch.Load())
				}
				return feat164AuxText("Ordinary local expert policy verification.")
			}, func(c *Config) { c.ToolSearchEnabled, c.InteractionToolsEnabled = true, true })
			base := WithPermissionMode(WithModelProfile(context.Background(), profile), PermissionAsk)
			ctx, err := WithExpertWebPolicy(base, "reference-expert", "0.1.0", "verified-by-resource-resolver")
			if err != nil {
				t.Fatal(err)
			}
			ctx, err = WithExpertInstructions(ctx, "Verified ordinary expert.", "Verified resource index.")
			if err != nil {
				t.Fatal(err)
			}
			thread, err := h.manager.StartThread(ctx, h.cwd)
			if err != nil {
				t.Fatal(err)
			}
			h.turn(t, profile, thread.ID, "Use the ordinary local example.")
			policy := web.ExpertPolicies[2]
			offline, err := WithExpertWebPolicy(base, policy.ExpertID, policy.Version, policy.RoleSHA256)
			if err != nil {
				t.Fatal(err)
			}
			offline, err = WithExpertInstructions(offline, "Verified offline expert.", "Verified local resource index.")
			if err != nil {
				t.Fatal(err)
			}
			if _, err = h.manager.SwitchThread(offline, thread.ID); err != nil {
				t.Fatal(err)
			}
			wantSearch.Store(false)
			h.turn(t, profile, thread.ID, "Use the offline local example.")
			removed, err := WithExpertInstructions(offline, "", "")
			if err != nil {
				t.Fatal(err)
			}
			if _, err = h.manager.SwitchThread(removed, thread.ID); err != nil {
				t.Fatal(err)
			}
			wantSearch.Store(true)
			h.turn(t, profile, thread.ID, "Use the ordinary local example again.")
		})
	}
}
