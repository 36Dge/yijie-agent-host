package codex

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/36Dge/yijie-agent-host/internal/artifact"
	document "github.com/36Dge/yijie-agent-host/internal/contracts/documentruntime"
	video "github.com/36Dge/yijie-agent-host/internal/contracts/videogeneration"
)

// The unmodified pinned Runtime and canonical adapter execute real MCP requests.
// Local HTTP replies model normal provider progress; no vendor request or key.
func TestVideoMCPFixedRuntimeCreateQueryWaitAndExpertSelection(t *testing.T) {
	binary := os.Getenv("YIJIE_MEDIA_TEST_ADAPTER")
	if binary == "" {
		t.Skip("requires canonical built media adapter")
	}
	integrationArtifactPaths(t)
	for _, profile := range []string{KimiK31ProfileID, "minimax-m3-high-v1"} {
		t.Run(profile, func(t *testing.T) {
			const artifactID = "17300000-0000-4000-8000-000000000001"
			var creates, queries, statusCalls atomic.Int64
			var mu sync.Mutex
			var metadata video.GenerateRequest
			var queryMetadata []video.GetRequest
			var adapter *artifact.Adapter
			hostToken := strings.Repeat("b", 64)
			host := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if r.URL.Path == video.GeneratePath || r.URL.Path == video.GetPath {
					if adapter == nil || r.Header.Get("Authorization") != "Bearer "+adapter.MediaHostToken {
						t.Error("video authority mismatch")
						w.WriteHeader(401)
						return
					}
				}
				switch r.URL.Path {
				case video.GeneratePath:
					var req video.GenerateRequest
					if json.NewDecoder(r.Body).Decode(&req) != nil || req.Validate() != nil || req.Input.Prompt != "A blue cup" {
						t.Error("video input mismatch")
						w.WriteHeader(400)
						return
					}
					mu.Lock()
					metadata = req
					mu.Unlock()
					creates.Add(1)
					_ = json.NewEncoder(w).Encode(video.VideoResult{Source: "builtin", Status: video.VideoStatusPending, Code: video.VideoCodeProviderPending, NextAction: video.NextActionGetVideoResult, VideoOperationId: &req.TurnId})
				case video.GetPath:
					var req video.GetRequest
					if json.NewDecoder(r.Body).Decode(&req) != nil || req.Validate() != nil || req.Input.VideoOperationId != nil {
						t.Error("optional handle lookup mismatch")
						w.WriteHeader(400)
						return
					}
					mu.Lock()
					queryMetadata = append(queryMetadata, req)
					mu.Unlock()
					if queries.Add(1) == 1 {
						timer := time.NewTimer(300 * time.Millisecond)
						defer timer.Stop()
						select {
						case <-r.Context().Done():
							return
						case <-timer.C:
						}
						_ = json.NewEncoder(w).Encode(video.VideoResult{Source: "builtin", Status: video.VideoStatusPending, Code: video.VideoCodeProviderPending, NextAction: video.NextActionGetVideoResult, VideoOperationId: &req.TurnId})
					} else {
						id := artifactID
						_ = json.NewEncoder(w).Encode(video.VideoResult{Source: "builtin", Status: video.VideoStatusStaged, Code: video.VideoCodeOk, NextAction: video.NextActionCheckArtifact, VideoOperationId: &req.TurnId, ArtifactId: &id, DeliveryTurnId: &req.TurnId})
					}
				case "/internal/document-runtime/v1/status":
					if r.Header.Get("Authorization") != "Bearer "+hostToken {
						t.Error("artifact authority mismatch")
					}
					var req document.StatusRequest
					if json.NewDecoder(r.Body).Decode(&req) != nil || req.Input.ArtifactId != artifactID {
						t.Error("artifact status input mismatch")
					}
					statusCalls.Add(1)
					_ = json.NewEncoder(w).Encode(document.StatusResponse{SchemaVersion: 1, ArtifactId: artifactID, Status: "committed"})
				default:
					t.Error("unexpected route", r.URL.Path)
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
			var expert atomic.Bool
			stage := 0
			lastCall := ""
			waited := false
			h := newNativeToolsHarnessWithSetup(t, func(h *nativeToolsHarness, body map[string]any, n int64) map[string]any {
				declaration, _ := json.Marshal(body["tools"])
				if strings.Contains(string(declaration), `"type":"image_generation"`) {
					t.Error("retired hosted image producer exposed")
				}
				if expert.Load() {
					if profile == KimiK31ProfileID && stage == 0 {
						stage++
						return feat164AuxTool("", "tool_search", "expert-video-search", map[string]any{"query": "yijie_media generate_video get_video_result", "limit": 5})
					}
					if strings.Contains(string(declaration), "mcp__yijie_media__generate_video") || strings.Contains(nativeToolsOutput(body, "expert-video-search"), "mcp__yijie_media__get_video_result") {
						t.Error("builtin exposed with selected expert")
					}
					return feat164AuxText("保留已选专家；不可用时不调用内置视频。")
				}
				if stage == 0 && profile == KimiK31ProfileID {
					stage = 1
					return feat164AuxTool("", "tool_search", "video-search", map[string]any{"query": "yijie_media generate_video get_video_result", "limit": 5})
				}
				if stage <= 1 {
					if profile == KimiK31ProfileID && !strings.Contains(nativeToolsOutput(body, "video-search"), "mcp__yijie_media__generate_video") {
						t.Error("video discovery missing")
					}
					stage = 2
					lastCall = "video-create"
					return feat164AuxTool("", "exec", lastCall, map[string]any{"input": `text(await tools.mcp__yijie_media__generate_video({prompt:"A blue cup",image_mode:"none",image_indices:[]}));`})
				}
				output := nativeToolsOutput(body, lastCall)
				if stage == 2 {
					if !strings.Contains(output, "provider_pending") || !strings.Contains(output, "video_operation_id") {
						t.Error("create receipt missing", output)
					}
					stage = 3
					lastCall = "video-query"
					return feat164AuxTool("", "exec", lastCall, map[string]any{"input": "// @exec: {\"yield_time_ms\": 1}\ntext(await tools.mcp__yijie_media__get_video_result({}));"})
				}
				if stage == 3 {
					match := regexp.MustCompile(`Script running with cell ID ([A-Za-z0-9_-]+)`).FindStringSubmatch(output)
					if len(match) == 2 {
						waited = true
						lastCall = fmt.Sprintf("video-wait-%d", n)
						return feat164AuxTool("", "wait", lastCall, map[string]any{"cell_id": match[1], "yield_time_ms": 1000})
					}
					if !strings.Contains(output, "provider_pending") {
						t.Error("pending receipt became error or was lost", output)
					}
					stage = 4
					lastCall = "video-collect"
					return feat164AuxTool("", "exec", lastCall, map[string]any{"input": `text(await tools.mcp__yijie_media__get_video_result({}));`})
				}
				if stage == 4 {
					if !strings.Contains(output, artifactID) || !strings.Contains(output, "staged") {
						t.Error("artifact handoff missing", output)
					}
					stage = 5
					lastCall = "video-artifact-status"
					return feat164AuxTool("", "exec", lastCall, map[string]any{"input": fmt.Sprintf(`text(await tools.mcp__yijie_artifacts__get_artifact_status({artifact_id:%q}));`, artifactID)})
				}
				if !strings.Contains(output, "committed") {
					t.Error("shared commit status missing", output)
				}
				return feat164AuxText("视频 MCP 本地协议链路完成。")
			}, func(m *Manager) {
				if err := m.SetArtifactMCP(adapter.URL, adapter.Token); err != nil {
					t.Fatal(err)
				}
				if err := m.SetMediaMCP(adapter.MediaURL, adapter.MediaToken); err != nil {
					t.Fatal(err)
				}
			}, func(c *Config) {
				c.BuiltinMediaEnabled = true
				c.BuiltinVideoEnabled = true
				c.InteractionToolsEnabled = true
				c.ToolSearchEnabled = true
			})
			ctx := WithPermissionMode(WithModelProfile(context.Background(), profile), PermissionAsk)
			thread, err := h.manager.StartThread(ctx, h.cwd)
			if err != nil {
				t.Fatal(err)
			}
			turn := h.turn(t, profile, thread.ID, "Execute the approved local video MCP protocol check.")
			mu.Lock()
			received := metadata
			receivedQueries := append([]video.GetRequest(nil), queryMetadata...)
			mu.Unlock()
			if creates.Load() != 1 || queries.Load() != 2 || statusCalls.Load() != 1 || !waited || received.ThreadId != thread.ID || received.TurnId != turn {
				t.Fatalf("native dispatch mismatch create=%d get=%d status=%d wait=%t", creates.Load(), queries.Load(), statusCalls.Load(), waited)
			}
			for _, q := range receivedQueries {
				if q.ThreadId != thread.ID || q.TurnId != turn {
					t.Error("query current context lost")
				}
			}
			history, err := h.manager.ReadThread(context.Background(), thread.ID)
			if err != nil || !strings.Contains(string(history), "mcpToolCall") || !strings.Contains(string(history), "get_video_result") {
				t.Fatal("native lifecycle absent", err)
			}
			expert.Store(true)
			stage = 0
			selected, err := WithExpertImageRoute(ctx, "video-reference-expert", "0.1.0", strings.Repeat("c", 64))
			if err != nil {
				t.Fatal(err)
			}
			if _, err = h.manager.SwitchThread(selected, thread.ID); err != nil {
				t.Fatal(err)
			}
			h.turn(t, profile, thread.ID, "Keep the selected expert and inspect only availability.")
			if creates.Load() != 1 || queries.Load() != 2 {
				t.Fatal("expert selection used builtin")
			}
		})
	}
}
