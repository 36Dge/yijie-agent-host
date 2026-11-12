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

func TestVideoMCPConfigurationKeepsNativePolicyAndRollback(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		m := NewManager(Config{BuiltinMediaEnabled: true, BuiltinVideoEnabled: enabled}, nil)
		if err := m.SetMediaMCP("http://127.0.0.1:18091/media/mcp", strings.Repeat("a", 64)); err != nil {
			t.Fatal(err)
		}
		for _, provider := range []string{"kimi", MiniMaxProviderID} {
			config, err := m.mediaThreadConfig(context.Background(), map[string]any{}, provider)
			if err != nil {
				t.Fatal(err)
			}
			server := config["mcp_servers"].(map[string]any)["yijie_media"].(map[string]any)
			tools := server["enabled_tools"].([]string)
			want := 1
			if enabled {
				want = 3
			}
			if len(tools) != want || tools[0] != media.ToolName || server["supports_parallel_tool_calls"] != false || server["tool_timeout_sec"] != media.ToolTimeoutSeconds {
				t.Fatal("native policy mismatch", server)
			}
			if enabled {
				for _, name := range []string{"generate_video", "get_video_result"} {
					if server["tools"].(map[string]any)[name].(map[string]any)["approval_mode"] != "approve" {
						t.Fatal("missing exact tool policy")
					}
				}
			}
			config, err = m.mediaThreadConfig(WithScheduledDraft(context.Background(), "/ordinary-workspace"), map[string]any{}, provider)
			if err != nil || config["mcp_servers"].(map[string]any)["yijie_media"].(map[string]any)["enabled"] != false {
				t.Fatal("scheduled draft exposed video")
			}
		}
	}
}

func TestVideoMCPInstructionsKeepBuiltinCapabilityBoundary(t *testing.T) {
	m := NewManager(Config{BuiltinMediaEnabled: true, BuiltinVideoEnabled: true}, nil)
	instructions := m.videoToolInstructions(context.Background())
	for _, required := range []string{"Video/audio references, editing or extending an existing clip are unavailable", "exact output duration", "explicit intent for a new generation"} {
		if !strings.Contains(instructions, required) {
			t.Fatal("builtin scope missing", required)
		}
	}
	selected, err := WithExpertImageRoute(context.Background(), "video-reference-expert", "0.1.0", strings.Repeat("c", 64))
	if err != nil {
		t.Fatal(err)
	}
	expert := m.videoToolInstructions(selected)
	if !strings.Contains(expert, "retain the explicitly selected expert") || strings.Contains(expert, "editing or extending an existing clip are unavailable") {
		t.Fatal("builtin restrictions leaked into expert capabilities")
	}
}
