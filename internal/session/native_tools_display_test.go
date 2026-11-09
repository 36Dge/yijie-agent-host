package session

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestNativeToolsFileAndAgentProjection(t *testing.T) {
	file, err := projectNativeItem(json.RawMessage(`{"id":"patch","type":"fileChange","status":"completed","changes":[{"path":"note.txt","kind":{"type":"add"},"diff":"+ordinary note"}]}`), "/workspace", false)
	if err != nil || file.Type != "fileChange" || file.Text == nil || !strings.Contains(*file.Text, "note.txt") || file.OutputText == nil || !strings.Contains(*file.OutputText, "ordinary note") || file.Mcp != nil {
		t.Fatalf("native file projection failed: %v", err)
	}
	agent, err := projectNativeItem(json.RawMessage(`{"id":"spawn","type":"subAgentActivity","kind":"started","agentThreadId":"native-child","agentPath":"/root/report"}`), "/workspace", false)
	if err != nil || agent.Type != "subAgentActivity" || agent.Text == nil || !strings.Contains(*agent.Text, "启动子任务 report") || agent.Status != nil || agent.Mcp != nil {
		t.Fatalf("native task activity projection failed: %v", err)
	}
}

func TestDocumentNativeImageViewKeepsCanonicalIdentity(t *testing.T) {
	item, err := projectNativeItem(json.RawMessage(`{"id":"view-page","type":"imageView","path":"/workspace/qa/page-001.png"}`), "/workspace", false)
	if err != nil || item.Type != "imageView" || item.Id != "view-page" || item.Availability != "available" || item.Status != nil || item.ToolLabel == nil || *item.ToolLabel != "查看图片" || item.ToolDisplay == nil {
		t.Fatalf("native image view projection missing: %v", err)
	}
	encoded, err := json.Marshal(item)
	if err != nil || strings.Contains(string(encoded), "page-001") || strings.Contains(string(encoded), "path") || item.Mcp != nil {
		t.Fatal("image display must not expose paths/pixels or invent an MCP execution")
	}
}
