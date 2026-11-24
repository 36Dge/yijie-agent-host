package codex

import (
	"context"
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// The driver supplies recorded public provider/MCP responses on loopback.
// Optional real-model metering is outside Host. Count the native history before
// the desktop folds equal commentary and compare it with the provider receipts.
func TestHangzhouHostedSearchFollowup(t *testing.T) {
	dir := os.Getenv("HANGZHOU_HOSTED_RUN")
	if dir == "" {
		t.Skip("requires the Hangzhou hosted-search driver")
	}
	var config struct {
		Endpoint, Prompt, Token string
		RecordedMCP             bool
	}
	data, err := os.ReadFile(filepath.Join(dir, "run.json"))
	if err != nil || json.Unmarshal(data, &config) != nil {
		t.Fatal("missing run configuration")
	}
	u, err := url.Parse(config.Endpoint)
	if err != nil || u.Scheme != "http" || u.Hostname() != "127.0.0.1" || u.Port() == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
		t.Fatal("expected local metered provider")
	}
	h := newNativeToolsHarnessWithSetup(t, func(_ *nativeToolsHarness, _ map[string]any, _ int64) map[string]any {
		t.Error("unexpected provider route")
		return feat164AuxText("unexpected route")
	}, func(m *Manager) {
		if config.RecordedMCP {
			if err := m.SetBuiltinWebMCP(config.Endpoint+"/mcp", config.Token, nil, nil); err != nil {
				t.Fatal(err)
			}
		}
	}, func(c *Config) {
		feat175Config(c)
		c.BuiltinWebEnabled = config.RecordedMCP
		c.PermissionVerificationBaseURL = config.Endpoint + "/v1"
		c.KimiAPIKey = config.Token
	})
	profile := "kimi-k3-1-pro-high-v1"
	ctx := WithPermissionMode(WithModelProfile(context.Background(), profile), PermissionAsk)
	thread, err := h.manager.StartThread(ctx, h.cwd)
	if err != nil {
		t.Fatal(err)
	}
	turn := feat175Start(t, h, profile, thread.ID, config.Prompt)
	status, _, waitErr := h.notifications.waitForTurn(turn.ID, 300*time.Second)
	if waitErr != nil {
		if err := h.manager.InterruptTurn(ctx, thread.ID, turn.ID); err != nil {
			t.Fatal("could not stop the test turn normally", err)
		}
		t.Fatal(waitErr)
	}
	history, err := h.manager.ReadThread(ctx, thread.ID)
	if err != nil {
		t.Fatal(err)
	}
	var saved struct {
		Turns []struct {
			Items []struct{ ID, Type, Text, Phase, Tool string }
		}
	}
	if json.Unmarshal(history, &saved) != nil || len(saved.Turns) != 1 {
		t.Fatal("expected one native turn")
	}
	searchIDs := []string{}
	progress := []string{}
	finals := []string{}
	localCalls := map[string]int{}
	for _, item := range saved.Turns[0].Items {
		switch item.Type {
		case "webSearch":
			searchIDs = append(searchIDs, item.ID)
		case "mcpToolCall":
			localCalls[item.Tool]++
		case "agentMessage":
			switch item.Phase {
			case "commentary":
				progress = append(progress, item.Text)
			case "final_answer":
				finals = append(finals, item.Text)
			}
		}
	}
	if err := q2Write(filepath.Join(dir, "native.json"), map[string]any{
		"status": status, "turn": turn.ID, "searchIDs": searchIDs, "progress": progress, "finals": finals, "localCalls": localCalls,
	}); err != nil {
		t.Fatal(err)
	}
	if status != "completed" || len(finals) != 1 {
		t.Fatalf("native status %s, %d final answers", status, len(finals))
	}
}
