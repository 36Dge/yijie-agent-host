package codex

import (
	"encoding/json"
	"testing"
)

func TestFEAT164TitleCollectorNativeInputLifecycle(t *testing.T) {
	c := &titleCollector{done: make(chan struct{})}
	for _, method := range []string{"item/started", "item/completed"} {
		c.handle(method, json.RawMessage(`{"item":{"type":"userMessage"}}`))
	}
	c.handle("item/completed", json.RawMessage(`{"item":{"type":"agentMessage","text":"{\"title\":\"普通标题\"}"}}`))
	c.handle("turn/completed", json.RawMessage(`{"turn":{"status":"completed"}}`))
	if c.failed || c.text == "" {
		t.Fatal("normal native input notification rejected")
	}
	for _, kind := range []string{"commandExecution", "futureItem"} {
		other := &titleCollector{done: make(chan struct{})}
		raw, _ := json.Marshal(map[string]any{"item": map[string]any{"type": kind}})
		other.handle("item/started", raw)
		if !other.failed {
			t.Fatal("non-title output accepted")
		}
	}
}
