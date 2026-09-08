package codex

import (
	"context"
	"encoding/json"
	"io"
	"testing"
	"time"
)

func TestFEAT132NativeReadUsesCanonicalRPCWithIncludeTurns(t *testing.T) {
	requests, clientInput := io.Pipe()
	clientOutput, responses := io.Pipe()
	client := NewClient(clientInput, clientOutput, 1<<20, 8, nil, nil)
	client.Start()
	t.Cleanup(func() { _ = client.CloseInput(); _ = responses.Close(); _ = requests.Close(); _ = clientOutput.Close() })
	manager := NewManager(DefaultConfig(), nil)
	manager.client = client
	manager.status.Ready = true
	manager.status.State = StateReady
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	wireResult := make(chan error, 1)
	go func() {
		var request struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
			Params struct {
				ThreadID     string `json:"threadId"`
				IncludeTurns bool   `json:"includeTurns"`
			} `json:"params"`
		}
		if err := json.NewDecoder(requests).Decode(&request); err != nil {
			wireResult <- err
			return
		}
		if request.Method != "thread/read" || request.Params.ThreadID != "native-thread" || !request.Params.IncludeTurns {
			wireResult <- io.ErrUnexpectedEOF
			return
		}
		wireResult <- json.NewEncoder(responses).Encode(map[string]any{"id": request.ID, "result": map[string]any{"thread": map[string]any{"id": "native-thread", "turns": []any{map[string]any{"id": "native-turn", "status": "completed", "items": []any{map[string]any{"id": "item-17", "type": "agentMessage", "text": "canonical history"}}}}}}})
	}()
	raw, err := manager.ReadThread(ctx, "native-thread")
	if err != nil {
		t.Fatal(err)
	}
	if err := <-wireResult; err != nil {
		t.Fatal(err)
	}
	var thread struct {
		ID    string `json:"id"`
		Turns []struct {
			ID string `json:"id"`
		} `json:"turns"`
	}
	if json.Unmarshal(raw, &thread) != nil || thread.ID != "native-thread" || len(thread.Turns) != 1 || thread.Turns[0].ID != "native-turn" {
		t.Fatal("native RPC history changed")
	}
}
