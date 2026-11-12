package codex

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/36Dge/yijie-agent-host/internal/artifact"
	document "github.com/36Dge/yijie-agent-host/internal/contracts/documentruntime"
	media "github.com/36Dge/yijie-agent-host/internal/contracts/mediageneration"
)

// Canonical Runtime + canonical Rust adapter + ordinary local provider replies.
// No vendor endpoint, real credentials, substituted executable or forced exit.
func TestMediaMCPFixedRuntimeDiscoveryCallAndExpertSelection(t *testing.T) {
	binary := os.Getenv("YIJIE_MEDIA_TEST_ADAPTER")
	if binary == "" {
		t.Skip("requires canonical built media adapter")
	}
	integrationArtifactPaths(t)
	for _, profile := range []string{KimiK31ProfileID, "minimax-m3-high-v1"} {
		t.Run(profile, func(t *testing.T) {
			const artifactID = "17200000-0000-4000-8000-000000000001"
			var calls, statusCalls atomic.Int64
			var mu sync.Mutex
			var metadata media.GenerateRequest
			var adapter *artifact.Adapter
			hostToken := strings.Repeat("b", 64)
			host := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch r.URL.Path {
				case media.PrivatePath:
					if adapter == nil || r.Header.Get("Authorization") != "Bearer "+adapter.MediaHostToken {
						t.Error("media bridge authority mismatch")
						w.WriteHeader(401)
						return
					}
					var req media.GenerateRequest
					if json.NewDecoder(r.Body).Decode(&req) != nil || req.Validate() != nil || req.Input.Prompt != "A blue ceramic cup" || req.Input.UseCurrentReference {
						t.Error("media bridge input mismatch")
						w.WriteHeader(400)
						return
					}
					mu.Lock()
					metadata = req
					mu.Unlock()
					calls.Add(1)
					id := artifactID
					_ = json.NewEncoder(w).Encode(media.GenerateResult{Source: "builtin", Status: media.GenerateStatusStaged, Code: media.GenerateCodeOk, ArtifactId: &id, NextAction: media.NextActionCheckArtifact})
				case "/internal/document-runtime/v1/status":
					if r.Header.Get("Authorization") != "Bearer "+hostToken {
						t.Error("status authority mismatch")
					}
					var req document.StatusRequest
					if json.NewDecoder(r.Body).Decode(&req) != nil || req.Input.ArtifactId != artifactID {
						t.Error("status input mismatch")
					}
					statusCalls.Add(1)
					_ = json.NewEncoder(w).Encode(document.StatusResponse{SchemaVersion: 1, ArtifactId: artifactID, Status: "committed"})
				default:
					t.Error("unexpected private route", r.URL.Path)
					w.WriteHeader(404)
				}
			}))
			defer host.Close()
			var err error
			adapter, err = artifact.StartAdapter(binary, host.URL, hostToken, false, true)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				if err := adapter.Stop(ctx); err != nil {
					t.Error("normal adapter shutdown", err)
				}
			})
			var expertSelected atomic.Bool
			var sequence atomic.Int64
			lastCall := ""
			h := newNativeToolsHarnessWithSetup(t, func(h *nativeToolsHarness, body map[string]any, n int64) map[string]any {
				step := sequence.Add(1)
				input, _ := json.Marshal(body["input"])
				if strings.Contains(string(input), filepath.Join(h.home, "skills", ".system", "imagegen", "SKILL.md")) {
					t.Error("retired imagegen skill remained in model instructions")
				}
				declaration, _ := json.Marshal(body["tools"])
				if strings.Contains(string(declaration), `"type":"image_generation"`) {
					t.Error("retired native image producer exposed")
				}
				if expertSelected.Load() {
					if profile == KimiK31ProfileID && step == 1 {
						return feat164AuxTool("", "tool_search", "selected-search", map[string]any{"query": "yijie_media generate_image", "limit": 2})
					}
					if profile == KimiK31ProfileID && strings.Contains(nativeToolsOutput(body, "selected-search"), "mcp__yijie_media__generate_image") {
						t.Error("builtin discoverable with selected expert")
					}
					if profile != KimiK31ProfileID && strings.Contains(string(declaration), "mcp__yijie_media__generate_image") {
						t.Error("builtin declared with selected expert")
					}
					return feat164AuxText("当前已选专家；不可用时保留选择，不改用内置生图。")
				}
				if profile == KimiK31ProfileID && step == 1 {
					return feat164AuxTool("", "tool_search", "media-search", map[string]any{"query": "yijie_media generate_image", "limit": 2})
				}
				generateStep := int64(1)
				if profile == KimiK31ProfileID {
					generateStep = 2
				}
				if step == generateStep {
					if profile == KimiK31ProfileID && !strings.Contains(nativeToolsOutput(body, "media-search"), "mcp__yijie_media__generate_image") {
						t.Error("builtin discovery missing")
					}
					lastCall = "media-exec"
					return feat164AuxTool("", "exec", lastCall, map[string]any{"input": `text(await tools.mcp__yijie_media__generate_image({prompt:"A blue ceramic cup",use_current_reference:false}));`})
				}
				output := nativeToolsOutput(body, lastCall)
				if step == generateStep+1 {
					if !strings.Contains(output, artifactID) || !strings.Contains(output, "staged") {
						t.Error("media result did not continue the native turn", output)
					}
					lastCall = "artifact-status"
					// Native Code Mode discovers the status tool through its existing catalogue.
					return feat164AuxTool("", "exec", lastCall, map[string]any{"input": fmt.Sprintf(`text(await tools.mcp__yijie_artifacts__get_artifact_status({artifact_id:%q}));`, artifactID)})
				}
				if !strings.Contains(output, "committed") {
					t.Error("shared Artifact status did not return", output)
				}
				return feat164AuxText("本地协议回环完成，未调用供应商。")
			}, func(m *Manager) {
				if err := m.SetArtifactMCP(adapter.URL, adapter.Token); err != nil {
					t.Fatal(err)
				}
				if err := m.SetMediaMCP(adapter.MediaURL, adapter.MediaToken); err != nil {
					t.Fatal(err)
				}
			}, func(c *Config) {
				c.BuiltinMediaEnabled = true
				c.InteractionToolsEnabled = true
				c.ToolSearchEnabled = true
			})
			entries, err := h.manager.ListSkills(context.Background(), []string{h.cwd}, true)
			if err != nil {
				t.Fatal(err)
			}
			imageDisabled, otherSystemEnabled := false, false
			for _, entry := range entries {
				for _, skill := range entry.Skills {
					if skill.Path == filepath.Join(h.home, "skills", ".system", "imagegen", "SKILL.md") {
						imageDisabled = skill.Scope == SkillScopeSystem && !skill.Enabled
					}
					if skill.Scope == SkillScopeSystem && skill.Name != "imagegen" && skill.Enabled {
						otherSystemEnabled = true
					}
				}
			}
			if !imageDisabled || !otherSystemEnabled {
				t.Fatal("native image skill retirement affected other system skills")
			}
			ctx := WithPermissionMode(WithModelProfile(context.Background(), profile), PermissionAsk)
			thread, err := h.manager.StartThread(ctx, h.cwd)
			if err != nil {
				t.Fatal(err)
			}
			turn := h.turn(t, profile, thread.ID, "Run the approved local image MCP protocol check.")
			mu.Lock()
			received := metadata
			mu.Unlock()
			if calls.Load() != 1 || statusCalls.Load() != 1 || received.ThreadId != thread.ID || received.TurnId != turn {
				t.Fatalf("native metadata/dispatch mismatch calls=%d status=%d metadata=%+v", calls.Load(), statusCalls.Load(), received)
			}
			history, err := h.manager.ReadThread(context.Background(), thread.ID)
			if err != nil || !strings.Contains(string(history), "mcpToolCall") || !strings.Contains(string(history), "yijie_media") {
				t.Fatal("native MCP lifecycle missing", err)
			}
			expertSelected.Store(true)
			sequence.Store(0)
			route := media.ExpertImageRoutes[0]
			selected, err := WithExpertImageRoute(ctx, route.ExpertID, route.Version, route.RoleSHA256)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = h.manager.SwitchThread(selected, thread.ID); err != nil {
				t.Fatal(err)
			}
			h.turn(t, profile, thread.ID, "Keep the selected expert; check only tool availability.")
			if calls.Load() != 1 {
				t.Fatal("selected expert used builtin")
			}
		})
	}
}
