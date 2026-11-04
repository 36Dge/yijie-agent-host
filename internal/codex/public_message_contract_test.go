package codex

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestPublicMessagePolicyScopes(t *testing.T) {
	for _, test := range []struct {
		permissions, models, candidate, want bool
	}{
		{true, true, true, true},
		{true, true, false, false},
		{true, false, true, false},
		{false, true, true, false},
	} {
		cfg := DefaultConfig()
		cfg.RuntimePermissionsEnabled = test.permissions
		cfg.ChatModelsEnabled = test.models
		cfg.KimiK31Candidate = test.candidate
		m := NewManager(cfg, nil)
		got := m.sessionDeveloperInstructions()
		if strings.Contains(got, "[yijie-public-message-v1]") != test.want {
			t.Fatal("transport declaration escaped its pinned Runtime profile")
		}
		if test.want && strings.Count(got, "[yijie-public-progress-v3]") != 1 {
			t.Fatal("shared generation policy lost its single owner")
		}
	}
	params, err := scheduledDraftStartParams(t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(params)
	if strings.Contains(string(raw), "yijie-public-message") || strings.Contains(string(raw), "yijie-public-progress") {
		t.Fatal("machine-only draft inherited public conversation framing")
	}
}

// Verify the two distinct histories across a real, normal Runtime shutdown:
// model input retains raw declarations and missing provider phase; thread/read
// retains the normalized, model-declared public messages. Reopening is read-only
// and must not dispatch another model response.
func TestPublicMessageNativeNormalReopen(t *testing.T) {
	texts := []string{
		"[[yijie.public-message.v1:commentary]]\n我会比较两种协作方式的适用范围。",
		"[[yijie.public-message.v1:commentary]]\n已区分即时决策与可留痕的信息，组合方式将围绕这个区别。",
		"[[yijie.public-message.v1:final_answer]]\n同步协作用于即时协调，异步协作用于可留痕的信息交换，两者按任务组合。",
	}
	h := feat164AuxRuntime(t, func(body map[string]any, number int64) map[string]any {
		if number > int64(len(texts)) {
			t.Error("unexpected model request")
			return feat164AuxText("完整结果已给出。")
		}
		if number > 1 {
			found := 0
			for _, value := range body["input"].([]any) {
				item, _ := value.(map[string]any)
				if item["role"] != "assistant" {
					continue
				}
				parts, _ := item["content"].([]any)
				if len(parts) == 0 {
					continue
				}
				part, _ := parts[0].(map[string]any)
				for i := 0; i < int(number)-1; i++ {
					if part["text"] == texts[i] {
						found++
						if item["phase"] != nil {
							t.Error("projection rewrote provider phase in model input")
						}
					}
				}
			}
			if found != int(number)-1 {
				t.Error("native request lost prior raw messages")
			}
		}
		item := feat164AuxText(texts[number-1])
		delete(item, "phase")
		return item
	})
	ctx := feat164AuxCtx()
	thread, err := h.manager.StartThread(ctx, h.cwd)
	if err != nil {
		t.Fatal(err)
	}
	input, _ := TextUserInput("比较两种协作方式，说明适用范围。无需工具。")
	turn, err := h.manager.StartTurnV2(ctx, thread.ID, []UserInput{input}, "max")
	if err != nil {
		t.Fatal(err)
	}
	status, _, err := h.notifications.waitForTurn(turn.ID, 30*time.Second)
	if err != nil || status != "completed" || h.calls.Load() != 3 {
		t.Fatal("normal declared-message continuation did not complete", status, err, h.calls.Load())
	}
	checkHistory := func() {
		t.Helper()
		raw, err := h.manager.ReadThread(ctx, thread.ID)
		if err != nil {
			t.Fatal(err)
		}
		var history struct {
			Turns []struct {
				Items []struct {
					Type  string `json:"type"`
					Text  string `json:"text"`
					Phase string `json:"phase"`
				} `json:"items"`
			} `json:"turns"`
		}
		if json.Unmarshal(raw, &history) != nil {
			t.Fatal("native history decode failed")
		}
		index := 0
		for _, savedTurn := range history.Turns {
			for _, item := range savedTurn.Items {
				if item.Type != "agentMessage" {
					continue
				}
				if index >= len(texts) {
					t.Fatal("native history duplicated a public message")
				}
				phase := "commentary"
				if index == 2 {
					phase = "final_answer"
				}
				_, body, _ := strings.Cut(texts[index], "\n")
				if item.Text != body || item.Phase != phase {
					t.Errorf("public history differs at %d", index)
				}
				index++
			}
		}
		if index != len(texts) {
			t.Fatal("public message missing from native history")
		}
	}
	checkHistory()
	shutdown, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	err = h.manager.Shutdown(shutdown)
	cancel()
	if err != nil {
		t.Fatal("normal Runtime shutdown pending; no forced termination", err)
	}
	cfg := h.manager.config
	h.manager = NewManager(cfg, nil)
	if err = h.manager.SetNotificationHandler(h.notifications.handle); err != nil {
		t.Fatal(err)
	}
	if err = h.manager.Start(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err = h.manager.ResumeThread(ctx, thread.ID); err != nil {
		t.Fatal(err)
	}
	checkHistory()
	if h.calls.Load() != 3 {
		t.Fatal("reopening dispatched model generation")
	}
}
