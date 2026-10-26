package codex

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

// Natural old history, stable native lifecycle, actual Host SwitchThread and
// normal user interruption. No history edits, replacement binary or forced exit.
func TestFEAT164LoadedLegacyMigrationResumeCancel(t *testing.T) {
	h := newFEAT164Cutover(t, func(body map[string]any, _ int) map[string]any {
		if body["model"] == "kimi-k3" {
			return feat164AuxText("S4Q_OLD_K3_HISTORY")
		}
		return feat164AuxText("S4Q_CURRENT_HISTORY")
	})
	h.keep = true
	streaming := make(chan struct{})
	streamDone := make(chan struct{})
	h.stream = func(w http.ResponseWriter, r *http.Request, _ map[string]any, n int) bool {
		if n != 3 {
			return false
		}
		defer close(streamDone)
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"type\":\"response.created\",\"response\":{\"id\":\"s4q_stream\",\"status\":\"in_progress\",\"output\":[]}}\n\n")
		w.(http.Flusher).Flush()
		close(streaming)
		// A finite ordinary response remains finite even if the client keeps its
		// HTTP stream after interrupt. Native interrupted is not a disconnect proof.
		select {
		case <-r.Context().Done():
		case <-time.After(time.Second):
			fmt.Fprint(w, "data: {\"type\":\"response.completed\",\"response\":{\"id\":\"s4q_stream\",\"status\":\"completed\",\"output\":[],\"usage\":{\"input_tokens\":1,\"output_tokens\":0,\"total_tokens\":1}}}\n\n")
			w.(http.Flusher).Flush()
		}
		return true
	}
	h.start(false)
	old := WithPermissionMode(WithModelProfile(context.Background(), "kimi-k3-max-v1"), PermissionAsk)
	thread, err := h.manager.StartThread(old, h.cwd)
	if err != nil {
		t.Fatal(err)
	}
	h.hostTurn(old, thread.ID, "Return S4Q_OLD_K3_HISTORY, no tools or external resources.")
	h.loaded(thread.ID)
	if h.count() != 1 || !h.stop() {
		t.Fatal("legacy generation did not finish normally")
	}
	h.start(true)
	// Loading a retained identity for history is not permission to execute it.
	// The stable native API accepts this normal resume; no turn is sent.
	var loaded threadResponse
	if err = h.manager.request(context.Background(), "thread/resume", map[string]any{
		"threadId": thread.ID, "model": "kimi-k3", "modelProvider": "kimi",
		"config": map[string]any{"model_reasoning_effort": "max"},
	}, &loaded); err != nil {
		t.Fatal(err)
	}
	if loaded.Model != "kimi-k3" || loaded.Thread.ID != thread.ID || h.count() != 1 {
		t.Fatal("old loaded identity or read-only request count differs")
	}
	h.loaded(thread.ID)
	current := feat164AuxCtx()
	switched, err := h.manager.SwitchThread(current, thread.ID)
	if err != nil || switched.ID != thread.ID || switched.Model != "kimi-k3.1-pro" || switched.ReasoningEffort != "max" {
		t.Fatalf("loaded old K3 explicit Host switch: %+v, %v", switched, err)
	}
	h.hostTurn(current, thread.ID, "Return S4Q_CURRENT_HISTORY, no tools.")
	input, _ := TextUserInput("S4Q_CANCEL: stream an ordinary short answer; user may cancel.")
	turn, err := h.manager.StartTurnV2(current, thread.ID, []UserInput{input}, "max")
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-streaming:
	case <-time.After(30 * time.Second):
		t.Fatal("ordinary stream did not start")
	}
	if err = h.manager.InterruptTurn(current, thread.ID, turn.ID); err != nil {
		t.Fatal(err)
	}
	state, _, err := h.notifications.waitForTurn(turn.ID, 30*time.Second)
	if err != nil || state != "interrupted" {
		t.Fatal("normal cancel terminal", state, err)
	}
	select {
	case <-streamDone:
	case <-time.After(5 * time.Second):
		t.Fatal("finite ordinary fixture response did not finish")
	}
	if h.count() != 3 || !h.stop() {
		t.Fatal("cancel generation not normally closed")
	}
	h.start(true)
	read, err := h.manager.ReadThread(context.Background(), thread.ID)
	if err != nil || !strings.Contains(string(read), "S4Q_OLD_K3_HISTORY") || !strings.Contains(string(read), "S4Q_CURRENT_HISTORY") || !strings.Contains(string(read), "interrupted") || h.count() != 3 {
		t.Fatal("cold history or no-resend invariant", err)
	}
	resumed, err := h.manager.ResumeThread(current, thread.ID)
	if err != nil || resumed.ID != thread.ID || resumed.Model != "kimi-k3.1-pro" {
		t.Fatal("explicit same-thread resume", err)
	}
	h.hostTurn(current, thread.ID, "Return one final local confirmation, no tools.")
	if h.count() != 4 || !h.stop() {
		t.Fatal("final generation not normally closed")
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, call := range h.calls[1:] {
		if call.Model != "kimi-k3.1-pro" || call.Effort != "max" {
			t.Fatal("old identity after migration cutoff")
		}
	}
	proof, _ := json.Marshal(map[string]any{"thread_id": thread.ID, "interrupted_turn_id": turn.ID, "retained_root": h.root, "loopback_requests": 4, "real_model_http": 0})
	t.Logf("S4Q_LOADED_MIGRATION_PASS %s", proof)
}
