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
	"testing"
	"time"
)

func TestNativeSelectedSkillHotReloadKeepsThreadAndBudget(t *testing.T) {
	binary, manifest := integrationArtifactPaths(t)
	root := integrationPrivateTempDir(t)
	home, workspace, skills := filepath.Join(root, "runtime"), filepath.Join(root, "workspace"), filepath.Join(root, "skills")
	for _, directory := range []string{home, workspace, filepath.Join(skills, "a"), filepath.Join(skills, "b")} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"a", "b"} {
		contents := "---\nname: ordinary-same-name\ndescription: Ordinary arithmetic explanation.\n---\nselected-body-" + name + ": explain multiplication briefly.\n"
		if err := os.WriteFile(filepath.Join(skills, name, "SKILL.md"), []byte(contents), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	var mu sync.Mutex
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if json.NewDecoder(r.Body).Decode(&body) != nil {
			t.Error("invalid normal loopback request")
			return
		}
		mu.Lock()
		requests++
		n := requests
		mu.Unlock()
		input, _ := json.Marshal(body["input"])
		if strings.Contains(string(input), "<skills_instructions>") {
			t.Error("automatic Skill catalogue was injected in explicit-only mode")
		}
		if n == 2 && (!strings.Contains(string(input), "selected-body-a:") || strings.Contains(string(input), "selected-body-b:")) {
			t.Error("native structured selection/hot reload did not select exact A")
		}
		id := fmt.Sprintf("ordinary_skill_%d", n)
		item := map[string]any{"id": "msg_" + id, "type": "message", "role": "assistant", "phase": "final_answer", "status": "completed", "content": []any{map[string]any{"type": "output_text", "text": "普通计算结果：六乘七等于四十二。"}}}
		w.Header().Set("Content-Type", "text/event-stream")
		emit := func(v any) { b, _ := json.Marshal(v); fmt.Fprintf(w, "data: %s\n\n", b); w.(http.Flusher).Flush() }
		emit(map[string]any{"type": "response.created", "response": map[string]any{"id": id, "status": "in_progress", "output": []any{}}})
		emit(map[string]any{"type": "response.output_item.done", "output_index": 0, "item": item})
		emit(map[string]any{"type": "response.completed", "response": map[string]any{"id": id, "status": "completed", "output": []any{item}, "usage": map[string]any{"input_tokens": 1, "output_tokens": 1, "total_tokens": 2}}})
	}))
	defer server.Close()
	config := integrationConfig(binary, manifest, home, "ordinary-loopback-placeholder")
	config.RuntimePermissionsEnabled, config.ChatModelsEnabled, config.KimiK31Candidate = true, true, true
	config.KimiAPIKey, config.PermissionVerificationBaseURL = "ordinary-loopback-placeholder", server.URL+"/v1"
	config.RolloutBudgetLimitTokens = 5
	manager := NewManager(config, nil)
	notifications := newIntegrationNotifications()
	if err := manager.SetNotificationHandler(notifications.handle); err != nil {
		t.Fatal(err)
	}
	if err := manager.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer shutdownIntegrationManager(t, manager, config.ShutdownTimeout)
	ctx := WithPermissionMode(WithModelProfile(context.Background(), KimiK31ProfileID), PermissionAsk)
	selectedPath := filepath.Join(skills, "a", "SKILL.md")
	if err := manager.SetSkillsExtraRoots(ctx, []string{skills}); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.WriteSkillConfig(ctx, selectedPath, false); err != nil {
		t.Fatal(err)
	}
	thread, err := manager.StartThread(ctx, workspace)
	if err != nil {
		t.Fatal(err)
	}
	for i, expected := range []string{"completed", "completed", "failed"} {
		text, _ := TextUserInput("请说明六乘七的结果。")
		inputs := []UserInput{text}
		if i == 1 {
			if _, err := manager.WriteSkillConfig(ctx, selectedPath, true); err != nil {
				t.Fatal(err)
			}
			skill, err := SkillUserInput("ordinary-same-name", selectedPath)
			if err != nil {
				t.Fatal(err)
			}
			inputs = append(inputs, skill)
		}
		turn, err := manager.StartTurnV2(ctx, thread.ID, inputs, "max")
		if err != nil {
			t.Fatal(err)
		}
		status, _, err := notifications.waitForTurn(turn.ID, 30*time.Second)
		if err != nil || status != expected {
			t.Fatalf("turn=%d status=%s expected=%s error=%v", i, status, expected, err)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if requests != 3 {
		t.Fatalf("unexpected loopback request count %d", requests)
	}
}
