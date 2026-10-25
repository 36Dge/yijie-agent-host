package codex

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

// Actual pinned Runtime, ordinary loopback Responses, no credentials or paid
// mode. No executable substitute, fault injection, forced stop or user data.
func TestFEAT164NativeCandidateLifecycle(t *testing.T) {
	runFEAT164NativeLifecycle(t, false)
}

func TestFEAT164NativePredecessorAndPausedKimi(t *testing.T) {
	runFEAT164NativeLifecycle(t, true)
}

func runFEAT164NativeLifecycle(t *testing.T, paused bool) {
	t.Helper()
	profiles := []string{KimiK31ProfileID, "minimax-m3-high-v1", KimiK31ProfileID}
	if paused {
		profiles = []string{"minimax-m3-high-v1"}
	}

	binary, manifest := integrationArtifactPaths(t)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var requests atomic.Int64
	server := &http.Server{ReadHeaderTimeout: 5 * time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if r.Method != "POST" || r.URL.Path != "/v1/responses" || json.NewDecoder(r.Body).Decode(&body) != nil {
			t.Error("unexpected local request")
			w.WriteHeader(400)
			return
		}
		n := requests.Add(1)
		if n > int64(len(profiles)) {
			t.Error("extra model request")
			w.WriteHeader(429)
			return
		}
		model, effort := "kimi-k3.1-pro", "max"
		if paused || n == 2 {
			model, effort = "MiniMax-M3", "high"
		}
		reasoning, _ := body["reasoning"].(map[string]any)
		if body["model"] != model || reasoning["effort"] != effort || body["tool_choice"] != "auto" {
			t.Error("wire identity or tool choice differs")
		}
		if model == "kimi-k3.1-pro" {
			include, ok := body["include"].([]any)
			if !ok {
				t.Error("native include is not an array")
			}
			for _, value := range include {
				if value != "web_search_call.action.sources" && value != "web_search_call.results" {
					t.Error("native Kimi request contains an undocumented include value")
				}
			}
		}
		id := fmt.Sprintf("feat164_%d", n)
		item := map[string]any{"id": "message_" + id, "type": "message", "role": "assistant", "phase": "final_answer", "status": "completed", "content": []any{map[string]any{"type": "output_text", "text": "ordinary marker 164", "annotations": []any{}}}}
		w.Header().Set("Content-Type", "text/event-stream")
		for _, event := range []any{map[string]any{"type": "response.created", "response": map[string]any{"id": id, "status": "in_progress", "output": []any{}}}, map[string]any{"type": "response.output_item.added", "output_index": 0, "item": item}, map[string]any{"type": "response.output_item.done", "output_index": 0, "item": item}, map[string]any{"type": "response.completed", "response": map[string]any{"id": id, "status": "completed", "output": []any{item}, "usage": map[string]any{"input_tokens": 1, "output_tokens": 1, "total_tokens": 2}}}} {
			raw, _ := json.Marshal(event)
			fmt.Fprintf(w, "data: %s\n\n", raw)
			w.(http.Flusher).Flush()
		}
	})}
	go func() { _ = server.Serve(listener) }()
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(ctx); err != nil {
			t.Error("normal local server shutdown", err)
		}
	}()
	root, err := os.MkdirTemp("", "yijie-feat164-runtime-")
	if err != nil {
		t.Fatal(err)
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	home, cwd := filepath.Join(root, "runtime"), filepath.Join(root, "workspace")
	for _, dir := range []string{home, cwd} {
		if err = os.Mkdir(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	config := integrationConfig(binary, manifest, home, "ordinary-minimax-fixture")
	config.ChatModelsEnabled, config.KimiK31Candidate, config.RuntimePermissionsEnabled = true, true, true
	config.KimiAPIKey = "ordinary-kimi-fixture"
	config.KimiK31ExecutionPaused, config.KimiK31AllowManagedPredecessor = paused, paused
	if paused {
		old, _, configBytes := frozenKimiK31Predecessor(home)
		writeS1File(t, home, chatModelsCatalogName, old)
		writeS1File(t, home, chatModelsConfigName, configBytes)
	}

	config.PermissionVerificationBaseURL = "http://" + listener.Addr().String() + "/v1"
	manager := NewManager(config, nil)
	notifications := newIntegrationNotifications()
	if err = manager.SetNotificationHandler(notifications.handle); err != nil {
		t.Fatal(err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
		defer cancel()
		if err := manager.Shutdown(ctx); err != nil {
			t.Error("normal Runtime cleanup pending; files retained", err)
		} else {
			_ = os.RemoveAll(root)
		}
	}()
	if err = manager.Start(context.Background()); err != nil {
		t.Fatal("candidate start", err)
	}
	var thread string
	for i, profile := range profiles {
		ctx := WithPermissionMode(WithModelProfile(context.Background(), profile), PermissionAsk)
		var info ThreadInfo
		if i == 0 {
			info, err = manager.StartThread(ctx, cwd)
		} else {
			info, err = manager.SwitchThread(ctx, thread)
		}
		if err != nil {
			t.Fatal("ordinary thread lifecycle", err)
		}
		expected, _ := ModelProfile(profile)
		if info.Model != expected.Model || info.ModelProvider != expected.Provider || info.ReasoningEffort != expected.Effort || (i > 0 && info.ID != thread) {
			t.Fatal("effective native identity differs")
		}
		thread = info.ID
		input, _ := TextUserInput("Return one ordinary test marker; do not call tools.")
		turn, err := manager.StartTurnV2(ctx, thread, []UserInput{input}, expected.Effort)
		if err != nil {
			t.Fatal(err)
		}
		state, _, err := notifications.waitForTurn(turn.ID, 30*time.Second)
		if err != nil || state != "completed" {
			t.Fatalf("normal turn did not complete: %s %v", state, err)
		}
	}
	old := WithModelProfile(context.Background(), "kimi-k3-max-v1")
	input, _ := TextUserInput("ordinary local text")
	if _, err = manager.StartTurnV2(old, thread, []UserInput{input}, "max"); err == nil {
		t.Fatal("retired turn accepted")
	}
	for _, id := range []string{"", "kimi-k3-max-v1"} {
		denied := WithModelProfile(context.Background(), id)
		if _, err := manager.StartThread(denied, cwd); err == nil {
			t.Fatal("nonactive start admitted")
		}
		if _, err := manager.ResumeThread(denied, thread); err == nil {
			t.Fatal("nonactive resume admitted")
		}
		if _, err := manager.StartTurn(denied, thread, "ordinary", "high"); err == nil {
			t.Fatal("nonactive legacy turn admitted")
		}
		if _, err := manager.GenerateTitle(denied, "ordinary title"); err == nil {
			t.Fatal("nonactive title admitted")
		}
	}
	if paused {
		ctx := WithModelProfile(context.Background(), KimiK31ProfileID)
		if _, err := manager.StartThread(ctx, cwd); err == nil {
			t.Fatal("paused Kimi start admitted")
		}
		if _, err := manager.SwitchThread(ctx, thread); err == nil {
			t.Fatal("paused Kimi switch admitted")
		}
		if _, err := manager.GenerateTitle(ctx, "ordinary title"); err == nil {
			t.Fatal("paused Kimi title admitted")
		}
		if _, err := manager.ReadThread(context.Background(), thread); err != nil {
			t.Fatal("pause blocked history", err)
		}
	}
	if requests.Load() != int64(len(profiles)) {
		t.Fatal("unexpected outbound count")
	}
	t.Logf("%d loopback Responses; paused=%v; retired/headerless admission denied; history and normal cleanup retained", requests.Load(), paused)
}
