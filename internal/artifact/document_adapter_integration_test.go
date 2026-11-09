package artifact

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	wire "github.com/36Dge/yijie-agent-host/internal/contracts/documentruntime"
	"github.com/36Dge/yijie-agent-host/internal/documents"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func TestDocumentRealAdapterReadOnlyToolsAndBinding(t *testing.T) {
	binary := os.Getenv("YIJIE_DOCUMENT_TEST_ADAPTER")
	if binary == "" {
		t.Skip("requires canonical built MCP adapter")
	}
	token := strings.Repeat("a", 64)
	calls := 0
	host := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+token {
			t.Error("missing existing Host bearer")
		}
		if r.URL.Path != "/internal/document-runtime/v1/environment" {
			t.Error("unexpected route", r.URL.Path)
			w.WriteHeader(404)
			return
		}
		var request wire.EnvironmentRequest
		if json.NewDecoder(r.Body).Decode(&request) != nil || request.ThreadId != testSession || request.TurnId != testArtifact || request.Input.Format == nil || *request.Input.Format != "docx" {
			t.Error("private request binding was not preserved")
		}
		calls++
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(documents.Unavailable("missing"))
	}))
	defer host.Close()
	adapter, err := StartAdapter(binary, host.URL, token, true)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := adapter.Stop(ctx); err != nil {
			t.Error("normal adapter shutdown", err)
		}
	}()
	session := ""
	client := &http.Client{Timeout: 10 * time.Second}
	id := 0
	request := func(method string, params any) map[string]any {
		id++
		data, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params})
		req, err := http.NewRequest("POST", adapter.URL, bytes.NewReader(data))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Authorization", "Bearer "+adapter.Token)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json, text/event-stream")
		if session != "" {
			req.Header.Set("Mcp-Session-Id", session)
			req.Header.Set("MCP-Protocol-Version", "2025-03-26")
		}
		response, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		if value := response.Header.Get("Mcp-Session-Id"); value != "" {
			session = value
		}
		raw, err := io.ReadAll(io.LimitReader(response.Body, 64<<10))
		if err != nil {
			t.Fatal(err)
		}
		var value map[string]any
		if strings.Contains(response.Header.Get("Content-Type"), "text/event-stream") {
			scan := bufio.NewScanner(bytes.NewReader(raw))
			for scan.Scan() {
				if strings.HasPrefix(scan.Text(), "data: ") {
					_ = json.Unmarshal([]byte(strings.TrimPrefix(scan.Text(), "data: ")), &value)
				}
			}
		} else {
			_ = json.Unmarshal(raw, &value)
		}
		if response.StatusCode != 200 || value == nil || value["error"] != nil {
			t.Fatalf("MCP %s failed: %d %s", method, response.StatusCode, raw)
		}
		return value["result"].(map[string]any)
	}
	request("initialize", map[string]any{"protocolVersion": "2025-03-26", "capabilities": map[string]any{}, "clientInfo": map[string]any{"name": "yijie-document-normal-check", "version": "1"}})
	listed := request("tools/list", map[string]any{})
	names := map[string]bool{}
	for _, raw := range listed["tools"].([]any) {
		tool := raw.(map[string]any)
		name := tool["name"].(string)
		names[name] = true
		if name != "publish_artifact" && tool["annotations"].(map[string]any)["readOnlyHint"] != true {
			t.Fatal("document query not readonly")
		}
	}
	if len(names) != 3 || !names["load_document_environment"] || !names["get_artifact_status"] || !names["publish_artifact"] {
		t.Fatal("native publisher/tools not preserved")
	}
	result := request("tools/call", map[string]any{"name": "load_document_environment", "arguments": map[string]any{"format": "docx"}, "_meta": map[string]any{"threadId": testSession, "x-codex-turn-metadata": map[string]any{"turn_id": testArtifact}}})
	if result["isError"] == true || calls != 1 {
		t.Fatal("document environment bridge failed", result, calls)
	}
}
