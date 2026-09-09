package session

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strings"
	"testing"

	"github.com/36Dge/yijie-agent-host/internal/codex"
	"github.com/google/uuid"
	"github.com/santhosh-tekuri/jsonschema/v6"
)

type fakeTitleGenerator struct {
	title string
	err   error
	calls int
}

func (generator *fakeTitleGenerator) GenerateTitle(context.Context, string) (string, error) {
	generator.calls++
	return generator.title, generator.err
}

func TestServiceProjectsBoundedRawReasoningOnlyToNegotiatedV2WithoutLoggingBody(t *testing.T) {
	home := filepath.Join(t.TempDir(), "host-home")
	store, err := OpenStore(home)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.Reserve(Record{TaskID: testTaskID, AgentSessionID: testSessionID, Cwd: t.TempDir()}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.BindThread(testSessionID, testThreadID, "runtime-session", codex.MiniMaxModel, codex.MiniMaxProviderID); err != nil {
		t.Fatal(err)
	}
	var logs bytes.Buffer
	v1 := NewEventHub(16, 8)
	v2 := NewEventHubVersion(EventSchemaVersionV2, 16, 8)
	service := NewService(&fakeRuntime{}, store, v1, slog.New(slog.NewTextHandler(&logs, nil)), WithV2Events(v2), WithRawReasoningProjection(true))
	const rawCanary = "RAW-REASONING-CANARY-126"
	service.HandleNotification(RuntimeNotificationReasoningTextDelta, rawJSON(t, map[string]any{
		"threadId": testThreadID, "turnId": testTurnID, "itemId": "reasoning-1",
		"contentIndex": 0, "delta": rawCanary,
	}))
	service.HandleNotification(RuntimeNotificationItemCompleted, rawJSON(t, map[string]any{
		"threadId": testThreadID, "turnId": testTurnID,
		"item": map[string]any{"id": "reasoning-1", "type": "reasoning", "content": []string{rawCanary}},
	}))

	_, v1Replay, _, cancelV1, err := service.SubscribeEvents(testSessionID, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	cancelV1()
	if len(v1Replay) != 1 || v1Replay[0].EventType != EventItemCompleted {
		t.Fatalf("v1 received a raw reasoning variant: %+v", v1Replay)
	}
	_, v2Replay, _, cancelV2, err := service.SubscribeEventsV2(testSessionID, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	cancelV2()
	if len(v2Replay) != 3 || v2Replay[0].EventType != EventItemReasoningTextDelta ||
		v2Replay[1].EventType != EventItemReasoningFinalized || v2Replay[2].EventType != EventItemCompleted {
		t.Fatalf("unexpected v2 reasoning projection: %+v", v2Replay)
	}
	contract := compileAgentSessionEventV2Contract(t)
	for _, event := range v2Replay {
		encoded, err := json.Marshal(event)
		if err != nil {
			t.Fatal(err)
		}
		instance, err := jsonschema.UnmarshalJSON(bytes.NewReader(encoded))
		if err != nil {
			t.Fatal(err)
		}
		if err := contract.Validate(instance); err != nil {
			t.Fatalf("v2 event violates contract: %v\n%s", err, encoded)
		}
	}
	if strings.Contains(logs.String(), rawCanary) {
		t.Fatalf("raw reasoning body entered logs: %s", logs.String())
	}
	if err := store.db.Sync(); err != nil {
		t.Fatal(err)
	}
	databaseBytes, err := os.ReadFile(filepath.Join(home, "sessions.db"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(databaseBytes), rawCanary) {
		t.Fatal("raw reasoning body entered Host bbolt")
	}
}

func TestServiceWarnsOnOversizeReasoningWithoutInventingFinalized(t *testing.T) {
	store, err := OpenStore(filepath.Join(t.TempDir(), "host-home"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.Reserve(Record{TaskID: testTaskID, AgentSessionID: testSessionID, Cwd: t.TempDir()}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.BindThread(testSessionID, testThreadID, "runtime-session", codex.MiniMaxModel, codex.MiniMaxProviderID); err != nil {
		t.Fatal(err)
	}
	v2 := NewEventHubVersion(EventSchemaVersionV2, 16, 8)
	service := NewService(&fakeRuntime{}, store, NewEventHub(16, 8), nil, WithV2Events(v2), WithRawReasoningProjection(true))
	service.HandleNotification(RuntimeNotificationReasoningTextDelta, rawJSON(t, map[string]any{
		"threadId": testThreadID, "turnId": testTurnID, "itemId": "reasoning-oversize",
		"contentIndex": 0, "delta": strings.Repeat("x", (16<<10)+1),
	}))
	_, replay, _, cancel, err := service.SubscribeEventsV2(testSessionID, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	if len(replay) != 1 || replay[0].EventType != EventWarning || replay[0].Terminal || replay[0].Payload.Status != "" || replay[0].Payload.Code != "projection_unavailable" {
		t.Fatalf("unexpected oversize reasoning result: %+v", replay)
	}
	if replay[0].Payload.Contents != nil || replay[0].Payload.Delta != nil {
		t.Fatalf("oversize raw body escaped unavailable projection: %+v", replay[0])
	}
}

func TestServiceGeneratesSanitizedIdempotentTitleWithoutDurableBody(t *testing.T) {
	home := filepath.Join(t.TempDir(), "host-home")
	store, err := OpenStore(home)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.Reserve(Record{TaskID: testTaskID, AgentSessionID: testSessionID, Cwd: t.TempDir()}); err != nil {
		t.Fatal(err)
	}
	generator := &fakeTitleGenerator{title: " 设计本地聊天安全删除流程 "}
	service := NewService(&fakeRuntime{}, store, NewEventHub(8, 2), nil, WithTitleGenerator(generator))
	operationID := "019c0123-4567-7abc-8123-456789abcdee"
	result, err := service.GenerateTitle(context.Background(), testSessionID, operationID, "为新的本地聊天任务设计安全的删除流程")
	if err != nil {
		t.Fatal(err)
	}
	if result.Title != "设计本地聊天安全删除流程" || generator.calls != 1 {
		t.Fatalf("unexpected title result: %+v calls=%d", result, generator.calls)
	}
	replayed, err := service.GenerateTitle(context.Background(), testSessionID, operationID, "为新的本地聊天任务设计安全的删除流程")
	if err != nil || replayed != result || generator.calls != 1 {
		t.Fatalf("title idempotency failed: %+v err=%v calls=%d", replayed, err, generator.calls)
	}
	if _, err := service.GenerateTitle(context.Background(), testSessionID, operationID, "different input"); !errors.Is(err, ErrTitleOperationConflict) {
		t.Fatalf("expected title operation conflict, got %v", err)
	}
	databaseBytes, err := os.ReadFile(filepath.Join(home, "sessions.db"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(databaseBytes), result.Title) {
		t.Fatal("generated title entered Host bbolt")
	}
}

func TestServiceRejectsUnsafeTitleOutput(t *testing.T) {
	store, err := OpenStore(filepath.Join(t.TempDir(), "host-home"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.Reserve(Record{TaskID: testTaskID, AgentSessionID: testSessionID, Cwd: t.TempDir()}); err != nil {
		t.Fatal(err)
	}
	for _, title := range []string{"# injected", "<script>alert(1)</script>", strings.Repeat("长", 41), "line one\nline two"} {
		generator := &fakeTitleGenerator{title: title}
		service := NewService(&fakeRuntime{}, store, NewEventHub(8, 2), nil, WithTitleGenerator(generator))
		if _, err := service.GenerateTitle(context.Background(), testSessionID, uuid.NewString(), "synthetic input"); !errors.Is(err, ErrTitleOutputInvalid) {
			t.Fatalf("unsafe title %q was accepted: %v", title, err)
		}
	}
}

func TestTitleIdempotencyIsSessionScopedAndFailuresDoNotRetry(t *testing.T) {
	store, err := OpenStore(filepath.Join(t.TempDir(), "host-home"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	secondTaskID, secondSessionID := uuid.NewString(), uuid.NewString()
	for _, record := range []Record{
		{TaskID: testTaskID, AgentSessionID: testSessionID, Cwd: t.TempDir()},
		{TaskID: secondTaskID, AgentSessionID: secondSessionID, Cwd: t.TempDir()},
	} {
		if err := store.Reserve(record); err != nil {
			t.Fatal(err)
		}
	}
	operationID := uuid.NewString()
	failing := &fakeTitleGenerator{err: errors.New("synthetic unknown outcome")}
	service := NewService(&fakeRuntime{}, store, NewEventHub(8, 2), nil, WithTitleGenerator(failing))
	for attempt := 0; attempt < 2; attempt++ {
		if _, err := service.GenerateTitle(context.Background(), testSessionID, operationID, "synthetic input"); !errors.Is(err, ErrTitleUnavailable) {
			t.Fatalf("failed title attempt %d: %v", attempt, err)
		}
	}
	if failing.calls != 1 {
		t.Fatalf("failed/unknown title operation was reissued: calls=%d", failing.calls)
	}
	if _, err := service.GenerateTitle(context.Background(), secondSessionID, operationID, "synthetic input"); !errors.Is(err, ErrTitleUnavailable) {
		t.Fatalf("same operation id in another session was not independently scoped: %v", err)
	}
	if failing.calls != 2 {
		t.Fatalf("title key was not (session_id, operation_id): calls=%d", failing.calls)
	}
}

func TestServiceCleanupRequiresRuntimeConfirmationThenClearsHostSurfaces(t *testing.T) {
	store, err := OpenStore(filepath.Join(t.TempDir(), "host-home"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.Reserve(Record{TaskID: testTaskID, AgentSessionID: testSessionID, Cwd: t.TempDir()}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.BindThread(testSessionID, testThreadID, "runtime-session", codex.MiniMaxModel, codex.MiniMaxProviderID); err != nil {
		t.Fatal(err)
	}
	v1 := NewEventHub(8, 2)
	v2 := NewEventHubVersion(EventSchemaVersionV2, 8, 2)
	deletedThread := ""
	runtime := &fakeRuntime{deleteThread: func(threadID string) error { deletedThread = threadID; return nil }}
	service := NewService(runtime, store, v1, nil, WithV2Events(v2), WithRawReasoningProjection(true))
	if _, err := v1.Publish(Event{AgentSessionID: testSessionID, EventType: EventWarning}); err != nil {
		t.Fatal(err)
	}
	if _, err := v2.Publish(Event{AgentSessionID: testSessionID, EventType: EventWarning}); err != nil {
		t.Fatal(err)
	}
	operationID := "019c0123-4567-7abc-8123-456789abcdee"
	result, err := service.CleanupSession(context.Background(), testSessionID, operationID)
	if err != nil {
		t.Fatal(err)
	}
	if result.Outcome != "complete" || deletedThread != testThreadID || result.RuntimeThreadTree != "complete" || result.HostMapping != "complete" || result.HostReplay != "complete" {
		t.Fatalf("unexpected cleanup result: %+v thread=%s", result, deletedThread)
	}
	if _, err := store.Get(testSessionID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("mapping survived cleanup: %v", err)
	}
	replayed, err := service.CleanupSession(context.Background(), testSessionID, operationID)
	if err != nil || replayed.Outcome != "complete" {
		t.Fatalf("lost-response retry was not idempotent: %+v err=%v", replayed, err)
	}
	if _, _, _, _, err := v1.Subscribe(testSessionID, "", 1); !errors.Is(err, ErrInvalidSequence) {
		t.Fatalf("v1 replay surface was not cleared: %v", err)
	}
}

func TestServiceCleanupReportsIncompleteWithoutDeletingMapping(t *testing.T) {
	store, err := OpenStore(filepath.Join(t.TempDir(), "host-home"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.Reserve(Record{TaskID: testTaskID, AgentSessionID: testSessionID, Cwd: t.TempDir()}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.BindThread(testSessionID, testThreadID, "runtime-session", codex.MiniMaxModel, codex.MiniMaxProviderID); err != nil {
		t.Fatal(err)
	}
	runtime := &fakeRuntime{deleteThread: func(string) error { return errors.New("synthetic delete failure") }}
	service := NewService(runtime, store, NewEventHub(8, 2), nil)
	result, err := service.CleanupSession(context.Background(), testSessionID, "019c0123-4567-7abc-8123-456789abcdee")
	if err != nil {
		t.Fatal(err)
	}
	if result.Outcome != "incomplete" || result.ReasonCode != "runtime_delete_unconfirmed" || result.HostMapping != "not_attempted" {
		t.Fatalf("unexpected incomplete cleanup: %+v", result)
	}
	if _, err := store.Get(testSessionID); err != nil {
		t.Fatalf("mapping was deleted after unconfirmed Runtime outcome: %v", err)
	}
}

func TestServiceCleanupRecoversLostDeleteNotificationAcrossRestart(t *testing.T) {
	home := filepath.Join(t.TempDir(), "host-home")
	store, err := OpenStore(home)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Reserve(Record{TaskID: testTaskID, AgentSessionID: testSessionID, Cwd: t.TempDir()}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.BindThread(testSessionID, testThreadID, "runtime-session", codex.MiniMaxModel, codex.MiniMaxProviderID); err != nil {
		t.Fatal(err)
	}
	operationID := "019c0123-4567-7abc-8123-456789abcdee"
	deleteCalls := 0
	runtime := &fakeRuntime{deleteThread: func(threadID string) error {
		deleteCalls++
		if deleteCalls == 1 {
			return errors.New("thread/delete notification was not confirmed")
		}
		return &codex.RPCError{Code: -32600, Message: "thread not found: " + threadID}
	}}
	service := NewService(runtime, store, NewEventHub(8, 2), nil)
	first, err := service.CleanupSession(context.Background(), testSessionID, operationID)
	if err != nil || first.Outcome != "incomplete" || first.ReasonCode != "runtime_delete_unconfirmed" {
		t.Fatalf("unexpected unknown delete result: result=%+v err=%v", first, err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := OpenStore(home)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	service = NewService(runtime, reopened, NewEventHub(8, 2), nil)
	second, err := service.CleanupSession(context.Background(), testSessionID, operationID)
	if err != nil || second.Outcome != "complete" || deleteCalls != 2 {
		t.Fatalf("lost notification recovery failed: result=%+v calls=%d err=%v", second, deleteCalls, err)
	}
	replayed, err := service.CleanupSession(context.Background(), testSessionID, operationID)
	if err != nil || replayed.Outcome != "complete" || deleteCalls != 2 {
		t.Fatalf("completed cleanup retry was not idempotent: result=%+v calls=%d err=%v", replayed, deleteCalls, err)
	}
}

type fakeRuntime struct {
	startThread  func(string) (codex.ThreadInfo, error)
	resume       func(string) (codex.ThreadInfo, error)
	startTurn    func(string, string, string) (codex.TurnInfo, error)
	interrupt    func(string, string) error
	deleteThread func(string) error
}

func TestFEAT126ServiceUsesRehydratedCwdForStartAndIDOnlyResume(t *testing.T) {
	const runID = "123e4567-e89b-42d3-a456-426614174000"
	_, hostHome, projectDirectory := newFEAT126StoreAuthority(t, runID)
	option := WithFEAT126Authority(projectDirectory, runID)
	store, err := OpenStore(hostHome, option)
	if err != nil {
		t.Fatal(err)
	}
	var startedWith string
	firstRuntime := &fakeRuntime{startThread: func(cwd string) (codex.ThreadInfo, error) {
		startedWith = cwd
		return codex.ThreadInfo{
			ID: testThreadID, RuntimeSession: "runtime-session",
			Model: codex.MiniMaxModel, ModelProvider: codex.MiniMaxProviderID,
		}, nil
	}}
	firstService := NewService(firstRuntime, store, NewEventHub(8, 2), nil)
	record, err := firstService.StartSession(context.Background(), StartSessionInput{
		TaskID: testTaskID, Cwd: projectDirectory,
	})
	if err != nil {
		t.Fatal(err)
	}
	if startedWith != projectDirectory || record.Cwd != projectDirectory {
		t.Fatalf("thread/start cwd was not canonical: input=%q response=%q", startedWith, record.Cwd)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := OpenStore(hostHome, option)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	var resumedThreadID string
	secondRuntime := &fakeRuntime{resume: func(threadID string) (codex.ThreadInfo, error) {
		resumedThreadID = threadID
		return codex.ThreadInfo{
			ID: threadID, RuntimeSession: "runtime-session-restarted",
			Model: codex.MiniMaxModel, ModelProvider: codex.MiniMaxProviderID,
		}, nil
	}}
	secondService := NewService(secondRuntime, reopened, NewEventHub(8, 2), nil)
	resumed, err := secondService.ResumeSession(context.Background(), record.AgentSessionID, TraceContext{})
	if err != nil {
		t.Fatal(err)
	}
	if resumedThreadID != testThreadID || resumed.Cwd != projectDirectory {
		t.Fatalf("thread/resume authority drift: thread=%q cwd=%q", resumedThreadID, resumed.Cwd)
	}
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
	if f.interrupt == nil {
		return nil
	}
	return f.interrupt(threadID, turnID)
}

func (f *fakeRuntime) DeleteThread(_ context.Context, threadID string) error {
	if f.deleteThread == nil {
		return nil
	}
	return f.deleteThread(threadID)
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

func TestServiceEmitsStableContractEventShapes(t *testing.T) {
	store, err := OpenStore(filepath.Join(t.TempDir(), "host-home"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	trace := TraceContext{
		TraceID: "trace-1", RequestID: "request-1", TenantID: "tenant-1", UserID: "user-1",
	}
	if err := store.Reserve(Record{
		TaskID: testTaskID, AgentSessionID: testSessionID, Cwd: t.TempDir(), Trace: trace,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.BindThread(
		testSessionID,
		testThreadID,
		"runtime-session",
		codex.MiniMaxModel,
		codex.MiniMaxProviderID,
	); err != nil {
		t.Fatal(err)
	}
	hub := NewEventHub(16, 8)
	service := NewService(&fakeRuntime{}, store, hub, nil)

	tests := []struct {
		name         string
		method       string
		params       map[string]any
		wantType     string
		wantTurnID   bool
		wantItemID   bool
		wantTerminal bool
		wantPayload  map[string]any
	}{
		{
			name: "thread started", method: "thread/started", wantType: EventThreadStarted,
			params: map[string]any{"thread": map[string]any{"id": testThreadID}},
			wantPayload: map[string]any{
				"model": codex.MiniMaxModel, "model_provider": codex.MiniMaxProviderID,
			},
		},
		{
			name: "turn started", method: "turn/started", wantType: EventTurnStarted, wantTurnID: true,
			params: map[string]any{
				"threadId": testThreadID,
				"turn":     map[string]any{"id": testTurnID, "status": "inProgress"},
			},
			wantPayload: map[string]any{"status": "in_progress"},
		},
		{
			name: "item started", method: "item/started", wantType: EventItemStarted,
			wantTurnID: true, wantItemID: true,
			params: map[string]any{
				"threadId": testThreadID, "turnId": testTurnID,
				"item": map[string]any{"id": "item-1", "type": "agentMessage", "text": ""},
			},
			wantPayload: map[string]any{"item_type": "agentMessage"},
		},
		{
			name: "empty agent message delta", method: "item/agentMessage/delta",
			wantType: EventItemAgentMessageDelta, wantTurnID: true, wantItemID: true,
			params: map[string]any{
				"threadId": testThreadID, "turnId": testTurnID, "itemId": "item-1", "delta": "",
			},
			wantPayload: map[string]any{"delta": ""},
		},
		{
			name: "item completed", method: "item/completed", wantType: EventItemCompleted,
			wantTurnID: true, wantItemID: true,
			params: map[string]any{
				"threadId": testThreadID, "turnId": testTurnID,
				"item": map[string]any{"id": "item-1", "type": "agentMessage", "text": "OK"},
			},
			wantPayload: map[string]any{"item_type": "agentMessage", "text": "OK"},
		},
		{
			name: "empty error message", method: "error", wantType: EventError, wantTurnID: true,
			params: map[string]any{
				"threadId": testThreadID, "turnId": testTurnID,
				"error":     map[string]any{"message": "", "codexErrorInfo": nil},
				"willRetry": false,
			},
			wantPayload: map[string]any{"message": "", "will_retry": false},
		},
		{
			name: "empty warning message", method: "warning", wantType: EventWarning,
			params:      map[string]any{"threadId": testThreadID, "message": ""},
			wantPayload: map[string]any{"message": "", "will_retry": false},
		},
		{
			name: "turn completed", method: "turn/completed", wantType: EventTurnCompleted,
			wantTurnID: true, wantTerminal: true,
			params: map[string]any{
				"threadId": testThreadID,
				"turn":     map[string]any{"id": testTurnID, "status": "completed", "error": nil},
			},
			wantPayload: map[string]any{"status": "completed"},
		},
	}

	for _, test := range tests {
		service.HandleNotification(test.method, rawJSON(t, test.params))
	}
	_, replay, _, cancel, err := service.SubscribeEvents(testSessionID, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	if len(replay) != len(tests) {
		t.Fatalf("expected %d contract events, got %d", len(tests), len(replay))
	}
	eventContract := compileAgentSessionEventContract(t)

	commonKeys := []string{
		"agent_session_id", "codex_thread_id", "event_id", "event_type", "occurred_at", "payload",
		"request_id", "schema_version", "sequence", "stream_id", "task_id", "tenant_id", "terminal",
		"trace_id", "user_id",
	}
	for index, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			event := replay[index]
			encoded, err := json.Marshal(event)
			if err != nil {
				t.Fatal(err)
			}
			var body map[string]any
			if err := json.Unmarshal(encoded, &body); err != nil {
				t.Fatal(err)
			}
			instance, err := jsonschema.UnmarshalJSON(bytes.NewReader(encoded))
			if err != nil {
				t.Fatalf("decode event for contract validation: %v", err)
			}
			if err := eventContract.Validate(instance); err != nil {
				t.Fatalf("event violates AgentSessionEvent JSON Schema: %v\n%s", err, encoded)
			}
			wantKeys := append([]string(nil), commonKeys...)
			if test.wantTurnID {
				wantKeys = append(wantKeys, "turn_id")
			}
			if test.wantItemID {
				wantKeys = append(wantKeys, "item_id")
			}
			sort.Strings(wantKeys)
			if gotKeys := sortedJSONKeys(body); !reflect.DeepEqual(gotKeys, wantKeys) {
				t.Fatalf("top-level JSON keys mismatch\nwant: %v\n got: %v\njson: %s", wantKeys, gotKeys, encoded)
			}
			payload, ok := body["payload"].(map[string]any)
			if !ok || !reflect.DeepEqual(payload, test.wantPayload) {
				t.Fatalf("payload mismatch\nwant: %#v\n got: %#v\njson: %s", test.wantPayload, payload, encoded)
			}
			if event.EventType != test.wantType || event.Terminal != test.wantTerminal {
				t.Fatalf("event lifecycle mismatch: %+v", event)
			}
			if event.TaskID != testTaskID || event.AgentSessionID != testSessionID ||
				event.CodexThreadID != testThreadID || event.TraceID != trace.TraceID ||
				event.RequestID != trace.RequestID || event.TenantID != trace.TenantID ||
				event.UserID != trace.UserID {
				t.Fatalf("event correlation mismatch: %+v", event)
			}
			if test.wantTurnID != (event.TurnID == testTurnID) {
				t.Fatalf("turn correlation mismatch: %+v", event)
			}
			if test.wantItemID != (event.ItemID == "item-1") {
				t.Fatalf("item correlation mismatch: %+v", event)
			}
		})
	}
}

func TestServiceDropsMalformedRuntimeNotifications(t *testing.T) {
	validItem := map[string]any{"id": "item-1", "type": "agentMessage", "text": "OK"}
	tests := []struct {
		name   string
		method string
		params map[string]any
	}{
		{
			name: "turn started without turn id", method: "turn/started",
			params: map[string]any{"threadId": testThreadID, "turn": map[string]any{"status": "inProgress"}},
		},
		{
			name: "turn started with malformed turn id", method: "turn/started",
			params: map[string]any{
				"threadId": testThreadID, "turn": map[string]any{"id": "bad", "status": "inProgress"},
			},
		},
		{
			name: "turn started with terminal status", method: "turn/started",
			params: map[string]any{
				"threadId": testThreadID, "turn": map[string]any{"id": testTurnID, "status": "completed"},
			},
		},
		{
			name: "item started without turn id", method: "item/started",
			params: map[string]any{"threadId": testThreadID, "item": validItem},
		},
		{
			name: "item started without item id", method: "item/started",
			params: map[string]any{
				"threadId": testThreadID, "turnId": testTurnID,
				"item": map[string]any{"type": "agentMessage"},
			},
		},
		{
			name: "item started without item type", method: "item/started",
			params: map[string]any{
				"threadId": testThreadID, "turnId": testTurnID,
				"item": map[string]any{"id": "item-1"},
			},
		},
		{
			name: "item completed without turn id", method: "item/completed",
			params: map[string]any{"threadId": testThreadID, "item": validItem},
		},
		{
			name: "item completed without item id", method: "item/completed",
			params: map[string]any{
				"threadId": testThreadID, "turnId": testTurnID,
				"item": map[string]any{"type": "agentMessage"},
			},
		},
		{
			name: "item completed without item type", method: "item/completed",
			params: map[string]any{
				"threadId": testThreadID, "turnId": testTurnID,
				"item": map[string]any{"id": "item-1"},
			},
		},
		{
			name: "delta without turn id", method: "item/agentMessage/delta",
			params: map[string]any{"threadId": testThreadID, "itemId": "item-1", "delta": ""},
		},
		{
			name: "delta without item id", method: "item/agentMessage/delta",
			params: map[string]any{"threadId": testThreadID, "turnId": testTurnID, "delta": ""},
		},
		{
			name: "error without turn id", method: "error",
			params: map[string]any{
				"threadId": testThreadID, "error": map[string]any{"message": ""}, "willRetry": false,
			},
		},
		{
			name: "turn completed without turn id", method: "turn/completed",
			params: map[string]any{
				"threadId": testThreadID, "turn": map[string]any{"status": "completed", "error": nil},
			},
		},
		{
			name: "turn completed with nonterminal status", method: "turn/completed",
			params: map[string]any{
				"threadId": testThreadID,
				"turn":     map[string]any{"id": testTurnID, "status": "inProgress", "error": nil},
			},
		},
		{
			name: "warning with malformed thread id", method: "warning",
			params: map[string]any{"threadId": "bad", "message": "ignored"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store, err := OpenStore(filepath.Join(t.TempDir(), "host-home"))
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			if err := store.Reserve(Record{
				TaskID: testTaskID, AgentSessionID: testSessionID, Cwd: t.TempDir(),
			}); err != nil {
				t.Fatal(err)
			}
			if _, err := store.BindThread(
				testSessionID,
				testThreadID,
				"runtime-session",
				codex.MiniMaxModel,
				codex.MiniMaxProviderID,
			); err != nil {
				t.Fatal(err)
			}
			hub := NewEventHub(8, 2)
			service := NewService(&fakeRuntime{}, store, hub, nil)

			service.HandleNotification(test.method, rawJSON(t, test.params))

			_, replay, _, cancel, err := service.SubscribeEvents(testSessionID, "", 0)
			if err != nil {
				t.Fatal(err)
			}
			cancel()
			if len(replay) != 0 {
				t.Fatalf("malformed notification entered replay: %+v", replay)
			}
			record, err := store.Get(testSessionID)
			if err != nil {
				t.Fatal(err)
			}
			if record.State != StateIdle || record.ActiveTurnID != "" || record.LastTurnID != "" {
				t.Fatalf("malformed notification mutated turn state: %+v", record)
			}
			if service.pendingCount != 0 {
				t.Fatalf("malformed notification was buffered: pending=%d", service.pendingCount)
			}
		})
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

func TestServiceClassifiesInvalidRuntimeResponses(t *testing.T) {
	t.Run("invalid thread start id", func(t *testing.T) {
		store, err := OpenStore(filepath.Join(t.TempDir(), "host-home"))
		if err != nil {
			t.Fatal(err)
		}
		defer store.Close()
		runtime := &fakeRuntime{startThread: func(string) (codex.ThreadInfo, error) {
			return codex.ThreadInfo{
				ID: "not-a-uuid", Model: codex.MiniMaxModel, ModelProvider: codex.MiniMaxProviderID,
			}, nil
		}}
		service := NewService(runtime, store, NewEventHub(8, 2), nil)
		_, err = service.StartSession(context.Background(), StartSessionInput{TaskID: testTaskID, Cwd: t.TempDir()})
		if !errors.Is(err, ErrRuntimeRequest) || errors.Is(err, ErrInvalidArgument) {
			t.Fatalf("expected invalid Runtime response classification, got %v", err)
		}
	})

	t.Run("unexpected thread start provider", func(t *testing.T) {
		store, err := OpenStore(filepath.Join(t.TempDir(), "host-home"))
		if err != nil {
			t.Fatal(err)
		}
		defer store.Close()
		runtime := &fakeRuntime{startThread: func(string) (codex.ThreadInfo, error) {
			return codex.ThreadInfo{ID: testThreadID, Model: "other", ModelProvider: "other"}, nil
		}}
		service := NewService(runtime, store, NewEventHub(8, 2), nil)
		_, err = service.StartSession(context.Background(), StartSessionInput{TaskID: testTaskID, Cwd: t.TempDir()})
		if !errors.Is(err, ErrRuntimeRequest) {
			t.Fatalf("expected provider mismatch to be a Runtime error, got %v", err)
		}
	})

	t.Run("unexpected resumed thread", func(t *testing.T) {
		store, err := OpenStore(filepath.Join(t.TempDir(), "host-home"))
		if err != nil {
			t.Fatal(err)
		}
		defer store.Close()
		if err := store.Reserve(Record{
			TaskID: testTaskID, AgentSessionID: testSessionID, Cwd: t.TempDir(),
		}); err != nil {
			t.Fatal(err)
		}
		if _, err := store.BindThread(
			testSessionID, testThreadID, "runtime-session", codex.MiniMaxModel, codex.MiniMaxProviderID,
		); err != nil {
			t.Fatal(err)
		}
		runtime := &fakeRuntime{resume: func(string) (codex.ThreadInfo, error) {
			return codex.ThreadInfo{
				ID: testTaskID, Model: codex.MiniMaxModel, ModelProvider: codex.MiniMaxProviderID,
			}, nil
		}}
		service := NewService(runtime, store, NewEventHub(8, 2), nil)
		_, err = service.ResumeSession(context.Background(), testSessionID, TraceContext{})
		if !errors.Is(err, ErrRuntimeRequest) {
			t.Fatalf("expected resumed thread mismatch to be a Runtime error, got %v", err)
		}
		record, getErr := store.Get(testSessionID)
		if getErr != nil {
			t.Fatal(getErr)
		}
		if record.State != StateFailed || record.FailureCode != "thread_resume_response_invalid" {
			t.Fatalf("unexpected failed session state: %+v", record)
		}
	})

	t.Run("invalid turn start id", func(t *testing.T) {
		store, err := OpenStore(filepath.Join(t.TempDir(), "host-home"))
		if err != nil {
			t.Fatal(err)
		}
		defer store.Close()
		if err := store.Reserve(Record{
			TaskID: testTaskID, AgentSessionID: testSessionID, Cwd: t.TempDir(),
		}); err != nil {
			t.Fatal(err)
		}
		if _, err := store.BindThread(
			testSessionID, testThreadID, "runtime-session", codex.MiniMaxModel, codex.MiniMaxProviderID,
		); err != nil {
			t.Fatal(err)
		}
		runtime := &fakeRuntime{startTurn: func(string, string, string) (codex.TurnInfo, error) {
			return codex.TurnInfo{ID: "not-a-uuid", Status: "inProgress"}, nil
		}}
		service := NewService(runtime, store, NewEventHub(8, 2), nil)
		_, err = service.StartTurn(context.Background(), StartTurnInput{
			AgentSessionID: testSessionID, Input: "reply OK", ReasoningEffort: "none",
		})
		if !errors.Is(err, ErrRuntimeRequest) || errors.Is(err, ErrInvalidArgument) {
			t.Fatalf("expected invalid Runtime turn classification, got %v", err)
		}
		record, getErr := store.Get(testSessionID)
		if getErr != nil {
			t.Fatal(getErr)
		}
		if record.State != StateIdle || record.FailureCode != "turn_start_response_invalid" {
			t.Fatalf("unexpected failed turn state: %+v", record)
		}
	})
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

func sortedJSONKeys(value map[string]any) []string {
	keys := make([]string, 0, len(value))
	for key := range value {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func compileAgentSessionEventContract(t *testing.T) *jsonschema.Schema {
	t.Helper()
	_, sourceFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate Agent session contract test source")
	}
	contractPath := filepath.Clean(filepath.Join(
		filepath.Dir(sourceFile), "..", "..", "api", "jsonschema", "agent-session-event.schema.json",
	))
	contract, err := jsonschema.NewCompiler().Compile(contractPath)
	if err != nil {
		t.Fatalf("compile AgentSessionEvent JSON Schema: %v", err)
	}
	return contract
}

func compileAgentSessionEventV2Contract(t *testing.T) *jsonschema.Schema {
	t.Helper()
	_, sourceFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate Agent session v2 contract test source")
	}
	contractPath := filepath.Clean(filepath.Join(
		filepath.Dir(sourceFile), "..", "..", "api", "jsonschema", "agent-session-event-v2.schema.json",
	))
	contract, err := jsonschema.NewCompiler().Compile(contractPath)
	if err != nil {
		t.Fatalf("compile AgentSessionEventV2 JSON Schema: %v", err)
	}
	return contract
}
