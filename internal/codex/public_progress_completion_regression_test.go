package codex

import (
	"context"
	"strings"
	"testing"
	"time"
)

// Normal replay of the public, unphased preamble observed on 2026-11-04.
// This is a completion regression, not a supplier generation qualification.
func TestPublicProgressWritingPreambleRequiresDeliverable(t *testing.T) {
	h := feat164AuxRuntime(t, func(_ map[string]any, number int64) map[string]any {
		if number == 1 {
			item := feat164AuxText("我会按 copywriting 技能直接写一版友好、简短的落地页文案，只使用你给出的合成事实。")
			delete(item, "phase")
			return item
		}
		return feat164AuxText("轻松整理随身物品。这款收纳袋适合日常分类携带，帮助物品有序摆放。")
	})
	ctx := feat164AuxCtx()
	thread, err := h.manager.StartThread(ctx, h.cwd)
	if err != nil {
		t.Fatal(err)
	}
	input, _ := TextUserInput("请根据合成事实写一段收纳袋介绍：用于日常物品分类携带。无需工具。")
	turn, err := h.manager.StartTurnV2(ctx, thread.ID, []UserInput{input}, "max")
	if err != nil {
		t.Fatal(err)
	}
	status, _, err := h.notifications.waitForTurn(turn.ID, 30*time.Second)
	if err != nil || status != "completed" {
		t.Fatal(status, err)
	}
	history, err := h.manager.ReadThread(context.Background(), thread.ID)
	if err != nil {
		t.Fatal(err)
	}
	if h.calls.Load() != 2 || !strings.Contains(string(history), "轻松整理随身物品") {
		t.Fatalf("progress-only writing preamble accepted as completion: requests=%d; supported deliverable absent", h.calls.Load())
	}
}
