package session

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"

	"github.com/36Dge/yijie-agent-host/internal/codex"
)

type fakeRuntime struct {
	startThread func(string) (codex.ThreadInfo, error)
	resume      func(string) (codex.ThreadInfo, error)
	startTurn   func(string, string, string) (codex.TurnInfo, error)
	interrupt   func(string, string) error
}

func (f *fakeRuntime) StartThread(_ context.Context, cwd string) (codex.ThreadInfo, error) {
	return f.startThread(cwd)
}

func (f *fakeRuntime) ResumeThread(_ context.Context, threadID string) (codex.ThreadInfo, error) {
	return f.resume(threadID)
}

func (f *fakeRuntime) StartTurn(_ context.Context, threadID, input, effort string) (codex.TurnInfo, error) {
	return f.startTurn(threadID, input, effort)
}

func (f *fakeRuntime) InterruptTurn(_ context.Context, threadID, turnID string) error {
	return f.interrupt(threadID, turnID)
}

func TestServiceMapsThreadTurnAndAgentMessageEvents(t *testing.T) {
	store, err := OpenStore(filepath.Join(t.TempDir(), "host-home"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	hub := NewEventHub(32, 8)
	runtime := &fakeRuntime{}
	service := NewService(runtime, store, hub, nil)
	runtime.startThread = func(string) (codex.ThreadInfo, error) {
		service.HandleNotification("thread/started", rawJSON(t, map[string]any{
			"thread": map[string]any{"id": testThreadID},
		}))
		return codex.ThreadInfo{
			ID: testThreadID, RuntimeSession: testTaskID,
			Model: codex.MiniMaxModel, ModelProvider: codex.MiniMaxProviderID,
		}, nil
	}
	runtime.resume = func(string) (codex.ThreadInfo, error) {
		return codex.ThreadInfo{
			ID: testThreadID, RuntimeSession: testTaskID,
			Model: codex.MiniMaxModel, ModelProvider: codex.MiniMaxProviderID,
			Turns: []codex.TurnInfo{{ID: testTurnID, Status: "completed"}},
		}, nil
	}
	runtime.startTurn = func(_, _, effort string) (codex.TurnInfo, error) {
		if effort != "none" {
			t.Fatalf("unexpected effort %q", effort)
		}
		return codex.TurnInfo{ID: testTurnID, Status: "inProgress"}, nil
	}
	runtime.interrupt = func(threadID, turnID string) error {
		if threadID != testThreadID || turnID != testTurnID {
			t.Fatalf("unexpected interrupt ids %s %s", threadID, turnID)
		}
		return nil
	}

	record, err := service.StartSession(context.Background(), StartSessionInput{
		TaskID: testTaskID,
		Cwd:    t.TempDir(),
		Trace:  TraceContext{TraceID: "trace-1", RequestID: "request-1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if record.AgentSessionID == "" || record.CodexThreadID != testThreadID {
		t.Fatalf("unexpected mapping: %+v", record)
	}
	streamID, replay, updates, cancel, err := service.SubscribeEvents(record.AgentSessionID, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	defer cancel()
	if streamID == "" || len(replay) != 1 || replay[0].EventType != EventThreadStarted {
		t.Fatalf("thread notification was not buffered and replayed: %+v", replay)
	}

	turn, err := service.StartTurn(context.Background(), StartTurnInput{
		AgentSessionID:  record.AgentSessionID,
		Input:           "reply only with OK",
		ReasoningEffort: "none",
		Trace:           TraceContext{TraceID: "trace-2", RequestID: "request-2"},
	})
	if err != nil {
		t.Fatal(err)
	}
	interruptTrace := TraceContext{TraceID: "trace-interrupt", RequestID: "request-interrupt"}
	if err := service.InterruptTurn(context.Background(), record.AgentSessionID, turn.ID, interruptTrace); err != nil {
		t.Fatal(err)
	}

	service.HandleNotification("turn/started", rawJSON(t, map[string]any{
		"threadId": testThreadID,
		"turn":     map[string]any{"id": testTurnID, "status": "inProgress"},
	}))
	service.HandleNotification("item/started", rawJSON(t, map[string]any{
		"threadId": testThreadID, "turnId": testTurnID,
		"item": map[string]any{"id": "item-1", "type": "agentMessage", "text": ""},
	}))
	service.HandleNotification("item/agentMessage/delta", rawJSON(t, map[string]any{
		"threadId": testThreadID, "turnId": testTurnID, "itemId": "item-1", "delta": "OK",
	}))
	service.HandleNotification("item/completed", rawJSON(t, map[string]any{
		"threadId": testThreadID, "turnId": testTurnID,
		"item": map[string]any{"id": "item-1", "type": "agentMessage", "text": "OK"},
	}))
	service.HandleNotification("warning", rawJSON(t, map[string]any{
		"threadId": testThreadID, "message": "safe warning",
	}))
	service.HandleNotification("turn/completed", rawJSON(t, map[string]any{
		"threadId": testThreadID,
		"turn": map[string]any{
			"id": testTurnID, "status": "interrupted", "error": nil,
		},
	}))

	want := []string{
		EventTurnStarted,
		EventItemStarted,
		EventItemAgentMessageDelta,
		EventItemCompleted,
		EventWarning,
		EventTurnCompleted,
	}
	for _, wantType := range want {
		event := <-updates
		if event.EventType != wantType {
			t.Fatalf("expected %s, got %+v", wantType, event)
		}
		if event.TraceID != "trace-interrupt" || event.TaskID != testTaskID || event.AgentSessionID != record.AgentSessionID {
			t.Fatalf("event correlation mismatch: %+v", event)
		}
	}
	current, err := service.GetSession(record.AgentSessionID)
	if err != nil {
		t.Fatal(err)
	}
	if current.State != StateIdle || current.ActiveTurnID != "" || current.LastTurnStatus != "interrupted" {
		t.Fatalf("turn terminal state mismatch: %+v", current)
	}
	if _, err := service.ResumeSession(context.Background(), record.AgentSessionID, TraceContext{TraceID: "trace-3"}); err != nil {
		t.Fatal(err)
	}
}

func TestServiceSanitizesProviderErrorsAndRejectsWrongTurn(t *testing.T) {
	if got := sanitizeMessage("Authorization: Bearer secret-token"); got != "Authorization: Bearer [REDACTED]" {
		t.Fatalf("bearer token was not redacted: %q", got)
	}
	store, err := OpenStore(filepath.Join(t.TempDir(), "host-home"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.Reserve(Record{TaskID: testTaskID, AgentSessionID: testSessionID, Cwd: t.TempDir()}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.BindThread(testSessionID, testThreadID, testTaskID, codex.MiniMaxModel, codex.MiniMaxProviderID); err != nil {
		t.Fatal(err)
	}
	service := NewService(&fakeRuntime{interrupt: func(string, string) error { return nil }}, store, NewEventHub(8, 2), nil)
	if err := service.InterruptTurn(context.Background(), testSessionID, testTurnID, TraceContext{}); !errors.Is(err, ErrTurnNotActive) {
		t.Fatalf("expected inactive turn rejection, got %v", err)
	}
}

func TestResumedTurnStateUsesOnlyLatestTurnAsActive(t *testing.T) {
	active, last, status := resumedTurnState([]codex.TurnInfo{
		{ID: testTaskID, Status: "inProgress"},
		{ID: testTurnID, Status: "completed"},
	})
	if active != "" || last != testTurnID || status != "completed" {
		t.Fatalf("historical in-progress turn was treated as active: active=%q last=%q status=%q", active, last, status)
	}
	active, last, status = resumedTurnState([]codex.TurnInfo{{ID: testTurnID, Status: "inProgress"}})
	if active != testTurnID || last != testTurnID || status != "in_progress" {
		t.Fatalf("latest in-progress turn was not restored: active=%q last=%q status=%q", active, last, status)
	}
}

func rawJSON(t *testing.T, value any) json.RawMessage {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return encoded
}
