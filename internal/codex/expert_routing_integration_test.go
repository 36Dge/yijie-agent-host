package codex

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/36Dge/yijie-agent-host/internal/contracts/experts"
)

// Observe the model request from the pinned Runtime, including the old answer
// on the same thread. The loopback provider does not choose an expert for it.
func TestSelectedExpertRoutingAcrossTurns(t *testing.T) {
	type expert struct{ name, id, version string }
	jd := &expert{"资深电商运营操盘手", "jd-ops-expert", "0.1.3"}
	director := &expert{"漫剧编剧与分镜师", "agentgrid-comic-storyboard-director", "0.1.0"}
	const oldAnswer = "京东运营专家可以整理开店清单。"
	const usage = "如何使用这个专家"
	const minimax = "minimax-m3-high-v1"
	steps := []struct {
		chosen     *expert
		clear      bool
		connectors []string
		profile    string
		prompt     string
	}{
		{jd, false, nil, minimax, "怎样开京东店？"},
		{director, false, []string{}, minimax, usage},
		{director, false, []string{"weibo"}, KimiK31ProfileID, usage},
		{nil, true, []string{"weibo"}, minimax, usage},
		{nil, false, nil, minimax, "普通新聊天"},
	}
	h := newNativeToolsHarness(t, func(_ *nativeToolsHarness, body map[string]any, call int64) map[string]any {
		if call > int64(len(steps)) {
			t.Error("unexpected extra provider request")
			return feat164AuxText("完成")
		}
		step := steps[call-1]
		base, _ := body["instructions"].(string)
		for _, known := range []*expert{jd, director} {
			for _, text := range []string{known.name, known.id + "@" + known.version, known.id + "/references"} {
				if strings.Contains(base, text) != (step.chosen == known) {
					t.Errorf("turn %d: wrong expert identity or index in current instructions: %s", call, text)
				}
			}
		}
		if strings.Contains(base, "No managed Yijie expert is selected") != step.clear || nativeToolsUserText(body) != step.prompt {
			t.Errorf("turn %d: incorrect clear or modified user input", call)
		}
		items, _ := body["input"].([]any)
		afterReply := 0
		seenOldAnswer := false
		for i, raw := range items {
			item := raw.(map[string]any)
			if item["role"] == "assistant" {
				afterReply = i + 1
				content, _ := json.Marshal(item["content"])
				seenOldAnswer = seenOldAnswer || strings.Contains(string(content), oldAnswer)
			}
		}
		if seenOldAnswer != (call > 1 && call < int64(len(steps))) {
			t.Errorf("turn %d: unexpected conversation history", call)
		}
		var expertText, connectorText string
		users, expertNotes, connectorNotes := 0, 0, 0
		for _, raw := range items[afterReply:] {
			item := raw.(map[string]any)
			parts, _ := item["content"].([]any)
			for _, rawPart := range parts {
				text, _ := rawPart.(map[string]any)["text"].(string)
				switch {
				case item["role"] == "user" && text == step.prompt:
					users++
				case item["role"] == "developer" && strings.HasPrefix(text, "本条用户消息选用的专家："):
					expertNotes++
					expertText = text
					if users != 0 {
						t.Error("expert note followed the typed message")
					}
				case item["role"] == "developer" && strings.HasPrefix(text, "本条用户消息选用的连接器："):
					connectorNotes++
					connectorText = text
					if users != 0 {
						t.Error("connector note followed the typed message")
					}
				}
			}
		}
		if users != 1 || expertNotes > 1 || connectorNotes > 1 {
			t.Errorf("turn %d: user=%d expert=%d connector=%d", call, users, expertNotes, connectorNotes)
		}
		switch {
		case step.chosen != nil:
			if !strings.Contains(expertText, step.chosen.name) || !strings.Contains(expertText, step.chosen.id+"@"+step.chosen.version) || !strings.Contains(expertText, "这个专家") {
				t.Errorf("turn %d: wrong current expert mention %q", call, expertText)
			}
		case step.clear:
			if !strings.HasPrefix(expertText, "本条用户消息选用的专家：无。") {
				t.Error("missing explicit removal")
			}
		case expertText != "":
			t.Error("ordinary new chat acquired an expert")
		}
		wantConnector := strings.Join(step.connectors, ", ")
		if step.connectors != nil && wantConnector == "" {
			wantConnector = "无"
		}
		if (step.connectors == nil && connectorText != "") ||
			(step.connectors != nil && !strings.HasPrefix(connectorText, "本条用户消息选用的连接器："+wantConnector+"。")) {
			t.Errorf("turn %d: incorrect independent connector mention %q", call, connectorText)
		}
		if call == 1 {
			return feat164AuxText(oldAnswer)
		}
		return feat164AuxText("完成")
	}, toolSearchConfig)
	var thread ThreadInfo
	for i, step := range steps {
		if i == 2 {
			h.restart(t, h.manager.config)
		}
		ctx := WithPermissionMode(WithModelProfile(context.Background(), step.profile), PermissionAsk)
		if step.connectors != nil {
			ctx = WithMarketPrepared(ctx, &MarketPrepared{selectedServiceIDs: step.connectors})
		}
		var err error
		if who := step.chosen; who != nil {
			digest := experts.Digest(strings.Repeat("a", 64))
			ref := experts.ExpertRef{ExpertId: experts.ExpertId(who.id), Version: experts.Version(who.version), ArchiveSha256: digest, RoleSha256: digest, KnowledgeManifestSha256: digest}
			ctx, err = WithSelectedExpertInstructions(ctx, ref, who.name, who.name+"的角色", who.id+"/references")
		} else if step.clear {
			ctx, err = WithExpertInstructions(ctx, "", "")
		}
		if err != nil {
			t.Fatal(err)
		}
		if i == 0 || i == len(steps)-1 {
			thread, err = h.manager.StartThread(ctx, h.cwd)
		} else {
			thread, err = h.manager.SwitchThread(ctx, thread.ID)
		}
		if err != nil {
			t.Fatal(err)
		}
		input, _ := TextUserInput(step.prompt)
		profile, _ := ModelProfile(step.profile)
		turn, err := h.manager.StartTurnV2(ctx, thread.ID, []UserInput{input}, profile.Effort)
		if err != nil {
			t.Fatal(err)
		}
		if state, _, err := h.notifications.waitForTurn(turn.ID, 20*time.Second); err != nil || state != "completed" {
			t.Fatal(state, err)
		}
	}
	if h.calls.Load() != int64(len(steps)) {
		t.Fatal("missing model request")
	}
}
