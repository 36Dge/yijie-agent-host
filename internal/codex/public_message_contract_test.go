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

// A malformed scripted transcript is still ONE actual Item. Keep every result
// word, suppress only unquoted control lines, and do not resample just because
// its first declaration says commentary. This is the real MiniMax regression.
func TestPublicMessageMixedDeclarationDoesNotResample(t *testing.T) {
	raw := "[[yijie.public-message.v1:commentary]]\n我会比较两种协作方式。\n\n[[yijie.public-message.v1:final_answer]]\n结果：同步协作适合即时协调，异步协作适合可留痕的信息交换。"
	h := feat164AuxRuntime(t, func(_ map[string]any, number int64) map[string]any {
		if number != 1 {
			t.Error("mixed declarations caused an extra sampling request")
			return feat164AuxText("完整结果已给出。")
		}
		item := feat164AuxText(raw)
		delete(item, "phase")
		return item
	})
	ctx := feat164AuxCtx()
	thread, err := h.manager.StartThread(ctx, h.cwd)
	if err != nil {
		t.Fatal(err)
	}
	input, _ := TextUserInput("比较两种协作方式，无需工具。")
	turn, err := h.manager.StartTurnV2(ctx, thread.ID, []UserInput{input}, "max")
	if err != nil {
		t.Fatal(err)
	}
	status, _, err := h.notifications.waitForTurn(turn.ID, 30*time.Second)
	if err != nil || status != "completed" || h.calls.Load() != 1 {
		t.Fatal("mixed message did not stop normally with its readable result", status, err, h.calls.Load())
	}
	history, err := h.manager.ReadThread(ctx, thread.ID)
	if err != nil {
		t.Fatal(err)
	}
	var view struct {
		Turns []struct {
			Items []struct {
				Type  string  `json:"type"`
				Text  string  `json:"text"`
				Phase *string `json:"phase"`
			} `json:"items"`
		} `json:"turns"`
	}
	if json.Unmarshal(history, &view) != nil {
		t.Fatal("history decode failed")
	}
	count := 0
	for _, savedTurn := range view.Turns {
		for _, item := range savedTurn.Items {
			if item.Type != "agentMessage" {
				continue
			}
			count++
			if item.Phase != nil || item.Text != "我会比较两种协作方式。\n\n结果：同步协作适合即时协调，异步协作适合可留痕的信息交换。" {
				t.Error("mixed Item text or unknown phase was not preserved")
			}
		}
	}
	if count != 1 {
		t.Fatal("one actual Item was split or duplicated")
	}
}

// Real observed spelling: the model reused the activation namespace's hyphens
// in an otherwise explicit v1 commentary declaration. This must remain one
// message and reach the ordinary continuation, without a word-list guess.
func TestPublicMessageObservedHyphenDeclarationContinues(t *testing.T) {
	texts := []string{
		"[[yijie.public-message.v1:commentary]]\n我会解释缓存的原理与代价。",
		"[[yijie-public-message-v1:commentary]]\n框架已定，接下来把原理、代价和类比压缩成约350字的成稿。",
		"[[yijie-public-message-v1:final_answer]]\n缓存保存可重复利用的副本，减少再次读取或计算的等待；代价包括额外存储和内容更新后的同步。",
	}
	h := feat164AuxRuntime(t, func(_ map[string]any, number int64) map[string]any {
		if number > int64(len(texts)) {
			t.Error("unexpected answer-only retry")
			return feat164AuxText("已有完整结果。")
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
	input, _ := TextUserInput("解释缓存的原理与代价，不调用工具。")
	turn, err := h.manager.StartTurnV2(ctx, thread.ID, []UserInput{input}, "max")
	if err != nil {
		t.Fatal(err)
	}
	status, _, err := h.notifications.waitForTurn(turn.ID, 30*time.Second)
	if err != nil || status != "completed" || h.calls.Load() != 3 {
		t.Fatal("explicit commentary stopped before a deliverable", status, err, h.calls.Load())
	}
	history, err := h.manager.ReadThread(ctx, thread.ID)
	if err != nil {
		t.Fatal(err)
	}
	var view struct {
		Turns []struct {
			Items []struct {
				Type  string  `json:"type"`
				Text  string  `json:"text"`
				Phase *string `json:"phase"`
			} `json:"items"`
		} `json:"turns"`
	}
	if json.Unmarshal(history, &view) != nil {
		t.Fatal("history decode failed")
	}
	var phases []string
	for _, savedTurn := range view.Turns {
		for _, item := range savedTurn.Items {
			if item.Type != "agentMessage" {
				continue
			}
			if item.Phase == nil || strings.Contains(item.Text, "[[yijie") {
				t.Fatal("declaration leaked or explicit phase lost")
			}
			phases = append(phases, *item.Phase)
		}
	}
	if strings.Join(phases, ",") != "commentary,commentary,final_answer" {
		t.Fatal("real message sequence changed", phases)
	}
}
