package codex

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

type hostedWireMessage struct {
	ID  string `json:"id"`
	SHA string `json:"sha256"`
}

// Compare public announcements with the original provider messages retained in
// the same native rollout. Ignore reasoning, developer input and tool payloads.
func hostedRolloutFacts(t *testing.T, home, threadID string) ([]string, []hostedWireMessage) {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(home, "sessions", "*", "*", "*", "*"+threadID+"*.jsonl"))
	if err != nil || len(matches) != 1 {
		t.Fatal("expected one persisted rollout for the exact weather thread", err)
	}
	file, err := os.Open(matches[0])
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	decoder := json.NewDecoder(file)
	progress := []string{}
	messages := []hostedWireMessage{}
	var terminal bool
	for {
		// Other rollout entry types can use a different shape for 'content'.
		var row struct {
			Type    string
			Payload json.RawMessage
		}
		if err := decoder.Decode(&row); err == io.EOF {
			break
		} else if err != nil {
			t.Fatal(err)
		}
		if row.Type != "event_msg" && row.Type != "response_item" {
			continue
		}
		var kind struct{ Type, Role string }
		if err := json.Unmarshal(row.Payload, &kind); err != nil {
			t.Fatal(err)
		}
		if row.Type == "event_msg" && kind.Type == "task_complete" {
			terminal = true
		}
		source := row.Type == "response_item" && kind.Type == "message" && kind.Role == "assistant"
		if !source && !(row.Type == "event_msg" && kind.Type == "agent_message") {
			continue
		}
		var item struct {
			ID, Role, Message, Phase string
			Content                  []struct{ Type, Text string }
		}
		if err := json.Unmarshal(row.Payload, &item); err != nil {
			t.Fatal(err)
		}
		if source {
			var parts strings.Builder
			for _, part := range item.Content {
				if part.Type == "output_text" {
					parts.WriteString(part.Text)
				}
			}
			messages = append(messages, hostedWireMessage{item.ID, fmt.Sprintf("%x", sha256.Sum256([]byte(parts.String())))})
		} else if item.Phase == "commentary" {
			progress = append(progress, item.Message)
		}
	}
	if !terminal {
		t.Fatal("rollout did not record a completed turn")
	}
	return progress, messages
}

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
	liveProgress := []string{}
	publicIDs := map[string]bool{}
	h.notifications.mu.Lock()
	h.notifications.observe = func(method string, raw json.RawMessage) {
		if method != "item/started" && method != "item/completed" && method != "item/agentMessage/delta" && method != "item/agentMessage/previewDelta" {
			return
		}
		var notice struct {
			ThreadID, ItemID string
			Item             struct{ ID, Type, Text, Phase string }
		}
		if json.Unmarshal(raw, &notice) != nil || notice.ThreadID != thread.ID {
			return
		}
		if notice.Item.Type == "agentMessage" {
			publicIDs[notice.Item.ID] = true
			if method == "item/completed" && notice.Item.Phase == "commentary" {
				liveProgress = append(liveProgress, notice.Item.Text)
			}
		} else if notice.ItemID != "" {
			publicIDs[notice.ItemID] = true
		}
	}
	h.notifications.mu.Unlock()
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
	rolloutProgress, rawMessages := hostedRolloutFacts(t, h.home, thread.ID)
	h.notifications.mu.Lock()
	h.notifications.observe = nil
	h.notifications.mu.Unlock()
	if !slices.Equal(liveProgress, progress) || !slices.Equal(rolloutProgress, progress) {
		t.Fatalf("native counts disagree: live %d, rollout %d, thread/read %d", len(liveProgress), len(rolloutProgress), len(progress))
	}
	if err := q2Write(filepath.Join(dir, "native.json"), map[string]any{
		"liveProgress": liveProgress, "rolloutProgress": rolloutProgress, "wireMessages": rawMessages, "publicIDs": publicIDs,
		"status": status, "turn": turn.ID, "searchIDs": searchIDs, "progress": progress, "finals": finals, "localCalls": localCalls,
	}); err != nil {
		t.Fatal(err)
	}
	if status != "completed" || len(finals) != 1 {
		t.Fatalf("native status %s, %d final answers", status, len(finals))
	}
}
