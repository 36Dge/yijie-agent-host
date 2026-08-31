package codex

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"testing"
	"time"
)

func TestClientHandlesDynamicReverseRequestWithoutBlockingReader(t *testing.T) {
	clientInput, runtimeReads := io.Pipe()
	clientOutput, runtimeWrites := io.Pipe()
	client := NewClient(runtimeReads, clientOutput, 1<<20, 8, nil, nil)
	client.setServerRequestHandler(func(_ context.Context, request serverRequest) serverRequestResult {
		if request.method != RuntimeMethodDynamicToolCall || !request.exactEnvelope || !json.Valid(request.params) {
			t.Fatalf("unexpected reverse request: method=%q", request.method)
		}
		return serverRequestResult{respond: true, value: map[string]any{
			"contentItems": []any{map[string]any{"type": "inputText", "text": "published"}}, "success": true,
		}}
	})
	client.Start()
	t.Cleanup(func() {
		_ = runtimeWrites.Close()
		_ = client.CloseInput()
	})
	request := map[string]any{
		"id": "call-1", "method": RuntimeMethodDynamicToolCall,
		"params": map[string]any{"threadId": "thread", "turnId": "turn", "callId": "call", "tool": DynamicToolGenerateImage, "arguments": map[string]any{}},
	}
	if err := json.NewEncoder(runtimeWrites).Encode(request); err != nil {
		t.Fatal(err)
	}
	var response struct {
		ID     string `json:"id"`
		Result struct {
			Success bool `json:"success"`
		} `json:"result"`
	}
	decoded := make(chan error, 1)
	go func() { decoded <- json.NewDecoder(bufio.NewReader(clientInput)).Decode(&response) }()
	select {
	case err := <-decoded:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for reverse response")
	}
	if response.ID != "call-1" || !response.Result.Success {
		t.Fatalf("unexpected reverse response: %+v", response)
	}
}
