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
