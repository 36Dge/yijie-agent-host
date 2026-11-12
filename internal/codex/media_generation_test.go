package codex

import (
	"context"
	media "github.com/36Dge/yijie-agent-host/internal/contracts/mediageneration"
	"strings"
	"testing"
)

func TestMediaMCPConfigurationFollowsExplicitSelection(t *testing.T) {
	m := NewManager(Config{BuiltinMediaEnabled: true}, nil)
	if err := m.SetMediaMCP("http://127.0.0.1:18091/media/mcp", strings.Repeat("a", 64)); err != nil {
		t.Fatal(err)
	}
	selected, err := WithExpertImageRoute(context.Background(), media.ExpertImageRoutes[0].ExpertID, media.ExpertImageRoutes[0].Version, media.ExpertImageRoutes[0].RoleSHA256)
	if err != nil {
		t.Fatal(err)
	}
	other, err := WithExpertImageRoute(context.Background(), "reference-expert", "0.1.0", strings.Repeat("b", 64))
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name    string
		ctx     context.Context
		enabled bool
	}{{"ordinary", context.Background(), true}, {"image expert", selected, false}, {"other expert", other, false}} {
		t.Run(tc.name, func(t *testing.T) {
			config, err := m.mediaThreadConfig(tc.ctx, map[string]any{"features": map[string]any{"tool_search": true}}, MiniMaxProviderID)
			if err != nil {
				t.Fatal(err)
			}
			server := config["mcp_servers"].(map[string]any)["yijie_media"].(map[string]any)
			if server["enabled"] != tc.enabled || config["yijie_host_images"] != false || config["features"].(map[string]any)["tool_search"] != true {
				t.Fatal("image profile mismatch", config)
			}
			if !tc.enabled && !strings.Contains(m.imageToolInstructions(tc.ctx), "Never switch") {
				t.Fatal("selected expert policy missing")
			}
		})
	}
	args, err := mediaNativeLayerArguments()
	if err != nil || !strings.Contains(strings.Join(args, " "), "features.image_generation=false") {
		t.Fatal("native image gate missing", err)
	}
	if _, err := WithExpertImageRoute(context.Background(), media.ExpertImageRoutes[0].ExpertID, "0.2.0", media.ExpertImageRoutes[0].RoleSHA256); err == nil {
		t.Fatal("unreviewed expert version accepted")
	}
}
