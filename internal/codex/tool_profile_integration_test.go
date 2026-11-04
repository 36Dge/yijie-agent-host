package codex

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// Ordinary loopback protocol only: the usage values below are synthetic control
// inputs, not measurements of billed tokens. No real provider or tool executes.
func TestNativeLightProfileAndAutomaticCompaction(t *testing.T) {
	for _, scenario := range []string{"light_resume", "native_compaction"} {
		t.Run(scenario, func(t *testing.T) {
			binary, manifest := integrationArtifactPaths(t)
			root := integrationPrivateTempDir(t)
			var mu sync.Mutex
			var requests []map[string]any
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var body map[string]any
				if json.NewDecoder(r.Body).Decode(&body) != nil {
					t.Error("invalid loopback request")
					return
				}
				mu.Lock()
				requests = append(requests, body)
				n := len(requests)
				mu.Unlock()
				answer := "Six times seven is forty-two."
				input, cached := 20, 0
				if scenario == "native_compaction" && n == 1 {
					input, cached = 900100, 900000
				}
				if scenario == "native_compaction" && n == 2 {
					answer = "Checkpoint: the user asked ordinary arithmetic; six times seven is forty-two. Continue answering the user's next question."
				}
				id := fmt.Sprintf("ordinary_%s_%d", scenario, n)
				item := map[string]any{"id": "msg_" + id, "type": "message", "role": "assistant", "phase": "final_answer", "status": "completed", "content": []any{map[string]any{"type": "output_text", "text": answer}}}
				w.Header().Set("Content-Type", "text/event-stream")
				for _, event := range []any{
					map[string]any{"type": "response.created", "response": map[string]any{"id": id, "status": "in_progress", "output": []any{}}},
					map[string]any{"type": "response.output_item.done", "output_index": 0, "item": item},
					map[string]any{"type": "response.completed", "response": map[string]any{"id": id, "status": "completed", "output": []any{item}, "usage": map[string]any{"input_tokens": input, "input_tokens_details": map[string]any{"cached_tokens": cached}, "output_tokens": 1, "total_tokens": input + 1}}},
				} {
					b, _ := json.Marshal(event)
					fmt.Fprintf(w, "data: %s\n\n", b)
					w.(http.Flusher).Flush()
				}
			}))
			defer server.Close()
			cfg := integrationConfig(binary, manifest, filepath.Join(root, "runtime"), "ordinary-loopback-placeholder")
			if err := os.Mkdir(cfg.CodexHome, 0700); err != nil {
				t.Fatal(err)
			}
			cfg.RuntimePermissionsEnabled, cfg.ChatModelsEnabled, cfg.KimiK31Candidate = true, true, true
			cfg.KimiAPIKey, cfg.PermissionVerificationBaseURL = "ordinary-loopback-placeholder", server.URL+"/v1"
			start := func() (*Manager, *integrationNotifications) {
				m := NewManager(cfg, nil)
				n := newIntegrationNotifications()
				if err := m.SetNotificationHandler(n.handle); err != nil {
					t.Fatal(err)
				}
				if err := m.Start(context.Background()); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { shutdownIntegrationManager(t, m, 25*time.Second) })
				return m, n
			}
			m, notifications := start()
			ctx := WithPermissionMode(WithModelProfile(context.Background(), KimiK31ProfileID), PermissionAsk)
			turn := func(m *Manager, n *integrationNotifications, ctx context.Context, id string) {
				input, _ := TextUserInput("Please explain six times seven.")
				profile, _ := ModelProfile(ModelProfileID(ctx))
				v, err := m.StartTurnV2(ctx, id, []UserInput{input}, profile.Effort)
				if err != nil {
					t.Fatal(err)
				}
				status, _, err := n.waitForTurn(v.ID, 30*time.Second)
				if err != nil || status != "completed" {
					t.Fatalf("normal turn status=%s error=%v", status, err)
				}
			}
			full, err := m.StartThread(ctx, root)
			if err != nil {
				t.Fatal(err)
			}
			turn(m, notifications, ctx, full.ID)
			if scenario == "light_resume" {
				lightCtx := WithToolProfile(ctx, "light")
				light, err := m.StartThread(lightCtx, root)
				if err != nil {
					t.Fatal(err)
				}
				skill := filepath.Join(root, "SKILL.md")
				if err := os.WriteFile(skill, []byte("---\nname: ordinary-profile-check\ndescription: Ordinary arithmetic explanation.\n---\nExplain arithmetic briefly.\n"), 0600); err != nil {
					t.Fatal(err)
				}
				if _, err := m.WriteSkillConfig(lightCtx, skill, false); err != nil {
					t.Fatal(err)
				}
				turn(m, notifications, lightCtx, light.ID)
				shutdownIntegrationManager(t, m, 25*time.Second)
				m, notifications = start()
				resumed, err := m.ResumeThread(lightCtx, light.ID)
				if err != nil || resumed.ID != light.ID {
					t.Fatalf("normal light resume: %v", err)
				}
				turn(m, notifications, lightCtx, light.ID)
				other := WithModelProfile(lightCtx, "minimax-m3-high-v1")
				if _, err := m.SwitchThread(other, light.ID); err != nil {
					t.Fatal(err)
				}
				turn(m, notifications, other, light.ID)
			} else {
				turn(m, notifications, ctx, full.ID)
			}
			shutdownIntegrationManager(t, m, 25*time.Second)
			mu.Lock()
			defer mu.Unlock()
			want := 3
			if scenario == "light_resume" {
				want = 4
			}
			if len(requests) != want {
				t.Fatalf("request count=%d want=%d", len(requests), want)
			}
			if scenario == "light_resume" {
				for i, r := range requests {
					var encoded bytes.Buffer
					encoder := json.NewEncoder(&encoded)
					encoder.SetEscapeHTML(false)
					if err := encoder.Encode(r["tools"]); err != nil {
						t.Fatal(err)
					}
					tools := strings.TrimSuffix(encoded.String(), "\n")
					for _, name := range []string{"multi_agent_v1", "create_goal", "get_goal", "update_goal"} {
						if strings.Contains(tools, name) != (i == 0) {
							t.Fatalf("request %d tool %s disagrees with selected profile", i, name)
						}
					}
					if !strings.Contains(tools, "exec_command") || !strings.Contains(tools, "update_plan") {
						t.Fatal("light profile removed an unrelated native tool")
					}
					t.Logf("request %d: native tools JSON bytes=%d", i+1, len(tools))
				}
			} else {
				compactions := 0
				err := filepath.WalkDir(filepath.Join(cfg.CodexHome, "sessions"), func(p string, d os.DirEntry, err error) error {
					if err != nil {
						return err
					}
					if d.IsDir() || !strings.HasSuffix(p, ".jsonl") {
						return nil
					}
					data, err := os.ReadFile(p)
					if err != nil {
						return err
					}
					for _, line := range strings.Split(string(data), "\n") {
						var row map[string]any
						if json.Unmarshal([]byte(line), &row) == nil && row["type"] == "compacted" {
							compactions++
						}
					}
					return nil
				})
				if err != nil || compactions != 1 {
					t.Fatalf("native compaction facts=%d error=%v", compactions, err)
				}
				b, _ := json.Marshal(requests[2]["input"])
				if !strings.Contains(string(b), "Checkpoint:") {
					t.Fatal("native replacement summary missing from subsequent sampling")
				}
				t.Log("unchanged native automatic threshold triggered one compacted rollout item; subsequent sampling used the native summary; 0 supplier requests")
			}
		})
	}
}
