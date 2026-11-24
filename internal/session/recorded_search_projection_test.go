package session

import (
	"context"
	"encoding/json"
	native "github.com/36Dge/yijie-agent-host/internal/contracts/nativeconversationv6"
	"os"
	"path/filepath"
	"testing"
)

func TestProjectRecordedScopedSearch(t *testing.T) {
	dir := os.Getenv("SCOPED_REPLAY_DIRECTORY")
	if dir == "" {
		t.Skip("requires isolated recorded gateway outputs")
	}
	for _, name := range []string{"three", "kimi", "minimax"} {
		raw, err := os.ReadFile(filepath.Join(dir, "mcp-"+name+".json"))
		if err != nil {
			t.Fatal(err)
		}
		var calls []json.RawMessage
		if err = json.Unmarshal(raw, &calls); err != nil {
			t.Fatal(err)
		}
		var rows []native.NativeItem
		for _, call := range calls {
			row, err := projectNativeItemWithSearchDetails(call, "", false, true)
			if err != nil || row.Mcp == nil || row.Mcp.SearchReceipt == nil {
				t.Fatalf("%s lost receipt %v", name, err)
			}
			rows = append(rows, row)
		}
		data, _ := json.Marshal(rows)
		if err = os.WriteFile(filepath.Join(dir, "native-"+name+".json"), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
}

func TestProjectHangzhouFetchTurn(t *testing.T) {
	dir := os.Getenv("HANGZHOU_REPLAY_DIRECTORY")
	if dir == "" {
		t.Skip("requires a completed recorded weather turn")
	}
	threadBytes, err := os.ReadFile(filepath.Join(dir, "completed-thread.json"))
	if err != nil {
		t.Fatal(err)
	}
	var thread struct{ ID string }
	if json.Unmarshal(threadBytes, &thread) != nil || thread.ID == "" {
		t.Fatal("thread identity unavailable")
	}
	store, err := OpenStore(filepath.Join(t.TempDir(), "host-home"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err = store.Reserve(Record{TaskID: testTaskID, AgentSessionID: testSessionID, Cwd: t.TempDir()}); err != nil {
		t.Fatal(err)
	}
	if _, err = store.BindThread(testSessionID, thread.ID, "weather-replay", "kimi-k3.1-pro", "kimi"); err != nil {
		t.Fatal(err)
	}
	service := NewService(&nativeReadRuntime{raw: threadBytes}, store, NewEventHub(256, 8), nil)
	bytes, err := os.ReadFile(filepath.Join(dir, "native-notifications.json"))
	if err != nil {
		t.Fatal(err)
	}
	var observed []struct {
		Method string
		Params json.RawMessage
	}
	if err = json.Unmarshal(bytes, &observed); err != nil {
		t.Fatal(err)
	}
	for _, n := range observed {
		service.HandleNotification(n.Method, n.Params)
	}
	_, events, _, cancel, err := service.SubscribeNativeEvents(testSessionID, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	defer cancel()
	history, err := service.ReadNativeThreadV6(context.Background(), testSessionID)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) < 4 || len(history.Turns) != 1 {
		t.Fatalf("incomplete exchange: events=%d turns=%d", len(events), len(history.Turns))
	}
	out, err := json.Marshal(map[string]any{"events": events, "history": history})
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(dir, "native-exchange.json"), out, 0600); err != nil {
		t.Fatal(err)
	}
}
