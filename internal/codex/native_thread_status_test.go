package codex

import (
	"context"
	"encoding/json"
	"io"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestFEAT144NativeThreadStatusReadsWithoutTurnsAndRejectsUnavailableStatus(t *testing.T) {
	for _, tc := range []struct {
		name, id, status string
		wantError        bool
	}{
		{"not loaded", "native-thread", "notLoaded", false},
		{"idle", "native-thread", "idle", false},
		{"active", "native-thread", "active", false},
		{"system error", "native-thread", "systemError", false},
		{"unknown", "native-thread", "futureStatus", true},
		{"missing", "native-thread", "", true},
		{"identity mismatch", "another-thread", "idle", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			requests, clientInput := io.Pipe()
			clientOutput, responses := io.Pipe()
			client := NewClient(clientInput, clientOutput, 1<<20, 8, nil, nil)
			client.Start()
			t.Cleanup(func() { _ = client.CloseInput(); _ = responses.Close(); _ = requests.Close(); _ = clientOutput.Close() })
			manager := NewManager(DefaultConfig(), nil)
			manager.client = client
			manager.status.Ready = true
			manager.status.State = StateReady
			before := manager.Snapshot()
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			wireResult := make(chan error, 1)
			go func() {
				var request struct {
					ID     json.RawMessage `json:"id"`
					Method string          `json:"method"`
					Params map[string]any  `json:"params"`
				}
				if err := json.NewDecoder(requests).Decode(&request); err != nil {
					wireResult <- err
					return
				}
				if request.Method != "thread/read" || !reflect.DeepEqual(request.Params, map[string]any{"threadId": "native-thread", "includeTurns": false}) {
					wireResult <- io.ErrUnexpectedEOF
					return
				}
				status := map[string]any{"type": tc.status, "activeFlags": []any{"waitingOnApproval"}}
				if tc.status == "" {
					status = nil
				}
				wireResult <- json.NewEncoder(responses).Encode(map[string]any{"id": request.ID, "result": map[string]any{"thread": map[string]any{"id": tc.id, "status": status, "cwd": "/private/ordinary-example", "preview": "ordinary response text"}}})
			}()
			status, err := manager.ReadThreadStatus(ctx, "native-thread")
			if err := <-wireResult; err != nil {
				t.Fatal(err)
			}
			if (err != nil) != tc.wantError || (!tc.wantError && status != tc.status) || (tc.wantError && status != "") {
				t.Fatalf("unexpected native status: %q, %v", status, err)
			}
			if err != nil && strings.Contains(err.Error(), "ordinary") {
				t.Fatal("native metadata escaped the error boundary")
			}
			if !reflect.DeepEqual(before, manager.Snapshot()) {
				t.Fatal("status observation changed Runtime state")
			}
		})
	}
}

func TestFEAT144NativeThreadStatusRequiresReadyRuntimeAndIdentity(t *testing.T) {
	manager := NewManager(DefaultConfig(), nil)
	for _, id := range []string{"", "native-thread"} {
		if status, err := manager.ReadThreadStatus(context.Background(), id); err == nil || status != "" {
			t.Fatal("unavailable Runtime or absent identity became a native observation")
		}
	}
}
