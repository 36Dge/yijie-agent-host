package session

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/36Dge/yijie-agent-host/internal/codex"
	"github.com/santhosh-tekuri/jsonschema/v6"
)

func TestFEAT134V4ProjectsAgentPhaseAndStablePlanSnapshots(t *testing.T) {
	store := openBoundFEAT134Store(t)
	v4 := NewEventHubVersion(EventSchemaVersionV4, 32, 8)
	service := NewService(&fakeRuntime{}, store, NewEventHub(32, 8), nil, WithV4Events(v4))

	service.HandleNotification(RuntimeNotificationItemStarted, rawJSON(t, map[string]any{
		"threadId": testThreadID, "turnId": testTurnID,
		"item": map[string]any{"id": "agent-1", "type": "agentMessage", "text": "starting", "phase": "commentary"},
	}))
	service.HandleNotification(RuntimeNotificationItemAgentMessageDelta, rawJSON(t, map[string]any{
		"threadId": testThreadID, "turnId": testTurnID, "itemId": "agent-1", "delta": "working",
	}))
	service.HandleNotification(RuntimeNotificationTurnPlanUpdated, rawJSON(t, map[string]any{
		"threadId": testThreadID, "turnId": testTurnID, "explanation": "safe plan",
		"plan": []any{
			map[string]any{"step": "done", "status": "completed"},
			map[string]any{"step": "active", "status": "inProgress"},
			map[string]any{"step": "next", "status": "pending"},
		},
	}))
	service.HandleNotification(RuntimeNotificationItemCompleted, rawJSON(t, map[string]any{
		"threadId": testThreadID, "turnId": testTurnID,
		"item": map[string]any{"id": "agent-1", "type": "agentMessage", "text": "final", "phase": "final_answer"},
	}))
	service.HandleNotification(RuntimeNotificationItemStarted, rawJSON(t, map[string]any{
		"threadId": testThreadID, "turnId": testTurnID,
		"item": map[string]any{"id": "agent-2", "type": "agentMessage", "text": "", "phase": nil},
	}))
	service.HandleNotification(RuntimeNotificationTurnPlanUpdated, rawJSON(t, map[string]any{
		"threadId": testThreadID, "turnId": testTurnID, "plan": []any{},
	}))

	_, replay, _, cancel, err := service.SubscribeEventsV4(testSessionID, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	wantTypes := []string{
		EventItemStarted, EventItemAgentMessageDelta, EventTurnPlanUpdated,
		EventItemCompleted, EventItemStarted, EventTurnPlanUpdated,
	}
	if len(replay) != len(wantTypes) {
		t.Fatalf("unexpected v4 replay: %+v", replay)
	}
	contract := compileAgentSessionEventV4Contract(t)
	for index, event := range replay {
		if event.EventType != wantTypes[index] || event.SchemaVersion != EventSchemaVersionV4 {
			t.Fatalf("unexpected event %d: %+v", index, event)
		}
		validateFEAT134Event(t, contract, event)
	}
	if phase := *replay[0].Payload.Phase; phase == nil || *phase != "commentary" || replay[0].Payload.Text == nil || *replay[0].Payload.Text != "starting" {
		t.Fatalf("started phase was not authoritative: %+v", replay[0].Payload)
	}
	if replay[1].Payload.Phase != nil || replay[1].Payload.Delta == nil || *replay[1].Payload.Delta != "working" {
		t.Fatalf("delta leaked lifecycle metadata: %+v", replay[1].Payload)
	}
	if phase := *replay[3].Payload.Phase; phase == nil || *phase != "final_answer" || replay[3].Payload.Text == nil || *replay[3].Payload.Text != "final" {
		t.Fatalf("completed phase did not refine the item: %+v", replay[3].Payload)
	}
	if replay[4].Payload.Phase == nil || *replay[4].Payload.Phase != nil {
		t.Fatalf("unclassified Runtime phase was not explicit null: %+v", replay[4].Payload)
	}
	plan := *replay[2].Payload.Plan
	if len(plan) != 3 || plan[1].Status != "in_progress" || replay[2].Payload.Explanation == nil || **replay[2].Payload.Explanation != "safe plan" {
		t.Fatalf("stable plan snapshot was not normalized: %+v", replay[2].Payload)
	}
	if len(*replay[5].Payload.Plan) != 0 || replay[5].Payload.Explanation == nil || *replay[5].Payload.Explanation != nil {
		t.Fatalf("empty plan did not explicitly clear prior state: %+v", replay[5].Payload)
	}
	_, legacy, _, cancelLegacy, err := service.SubscribeEvents(testSessionID, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	cancelLegacy()
	if len(legacy) < 1 || legacy[0].EventType != EventItemStarted || legacy[0].Payload.Text != nil || legacy[0].Payload.Phase != nil {
		t.Fatalf("v4 lifecycle authority changed the v1 projection: %+v", legacy)
	}
}

func TestFEAT134V4KeepsStructuredNonReasoningContentOpaque(t *testing.T) {
	store := openBoundFEAT134Store(t)
	v4 := NewEventHubVersion(EventSchemaVersionV4, 8, 2)
	var logs bytes.Buffer
	service := NewService(
		&fakeRuntime{}, store, NewEventHub(8, 2), slog.New(slog.NewTextHandler(&logs, nil)),
		WithV4Events(v4), WithRawReasoningProjection(true),
	)
	const contentCanary = "FEAT134_STRUCTURED_USER_CONTENT_CANARY"
	notification := map[string]any{
		"threadId": testThreadID,
		"turnId":   testTurnID,
		"item": map[string]any{
			"id":   "user-1",
			"type": "userMessage",
			"content": []any{
				map[string]any{"type": "text", "text": contentCanary},
			},
		},
	}
	service.HandleNotification(RuntimeNotificationItemStarted, rawJSON(t, notification))
	service.HandleNotification(RuntimeNotificationItemCompleted, rawJSON(t, notification))

	_, replay, _, cancel, err := service.SubscribeEventsV4(testSessionID, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	if len(replay) != 2 || replay[0].EventType != EventItemStarted || replay[1].EventType != EventItemCompleted {
		t.Fatalf("structured non-reasoning content broke the lifecycle: %+v", replay)
	}
	for _, event := range replay {
		if event.Payload.ItemType != "userMessage" || event.Payload.Text != nil || event.Payload.Phase != nil {
			t.Fatalf("structured user content crossed the v4 projection: %+v", event.Payload)
		}
	}
	if strings.Contains(logs.String(), contentCanary) {
		t.Fatal("structured non-reasoning content entered Host logs")
	}
}

func TestFEAT134RejectsMalformedPlanSourcesWithoutInventingClearState(t *testing.T) {
	tests := []struct {
		name string
		plan any
		omit bool
	}{
		{name: "missing plan", omit: true},
		{name: "null plan", plan: nil},
		{name: "missing step text", plan: []any{map[string]any{"status": "pending"}}},
		{name: "runtime output spelling", plan: []any{map[string]any{"step": "source drift", "status": "in_progress"}}},
		{name: "unknown status", plan: []any{map[string]any{"step": "source drift", "status": "unknown"}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store := openBoundFEAT134Store(t)
			v4 := NewEventHubVersion(EventSchemaVersionV4, 8, 2)
			var logs bytes.Buffer
			service := NewService(
				&fakeRuntime{}, store, NewEventHub(8, 2), slog.New(slog.NewTextHandler(&logs, nil)), WithV4Events(v4),
			)
			notification := map[string]any{
				"threadId":    testThreadID,
				"turnId":      testTurnID,
				"explanation": "FEAT134_PLAN_SOURCE_CANARY",
			}
			if !test.omit {
				notification["plan"] = test.plan
			}
			service.HandleNotification(RuntimeNotificationTurnPlanUpdated, rawJSON(t, notification))

			_, replay, _, cancel, err := service.SubscribeEventsV4(testSessionID, "", 0)
			if err != nil {
				t.Fatal(err)
			}
			cancel()
			if len(replay) != 1 || replay[0].EventType != EventError || replay[0].Payload.Code != v4ProjectionLimitCode {
				t.Fatalf("malformed plan source was not rejected content-free: %+v", replay)
			}
			if strings.Contains(logs.String(), "FEAT134_PLAN_SOURCE_CANARY") {
				t.Fatal("malformed plan body entered Host logs")
			}
		})
	}
}

func TestFEAT134RejectsMissingAuthoritativeRuntimeFields(t *testing.T) {
	tests := []struct {
		name      string
		method    string
		params    map[string]any
		wantEvent string
	}{
		{
			name: "agent item text missing", method: RuntimeNotificationItemStarted,
			params: map[string]any{"threadId": testThreadID, "turnId": testTurnID,
				"item": map[string]any{"id": "agent-1", "type": "agentMessage", "phase": "commentary"}},
			wantEvent: EventError,
		},
		{
			name: "agent item text null", method: RuntimeNotificationItemCompleted,
			params: map[string]any{"threadId": testThreadID, "turnId": testTurnID,
				"item": map[string]any{"id": "agent-1", "type": "agentMessage", "text": nil, "phase": "final_answer"}},
			wantEvent: EventError,
		},
		{
			name: "agent delta missing", method: RuntimeNotificationItemAgentMessageDelta,
			params:    map[string]any{"threadId": testThreadID, "turnId": testTurnID, "itemId": "agent-1"},
			wantEvent: EventError,
		},
		{
			name: "reasoning delta missing", method: RuntimeNotificationReasoningTextDelta,
			params:    map[string]any{"threadId": testThreadID, "turnId": testTurnID, "itemId": "reasoning-1", "contentIndex": 0},
			wantEvent: EventError,
		},
		{
			name: "reasoning content index missing", method: RuntimeNotificationReasoningTextDelta,
			params:    map[string]any{"threadId": testThreadID, "turnId": testTurnID, "itemId": "reasoning-1", "delta": "synthetic"},
			wantEvent: EventError,
		},
		{
			name: "reasoning completed content has non-string entry", method: RuntimeNotificationItemCompleted,
			params: map[string]any{"threadId": testThreadID, "turnId": testTurnID,
				"item": map[string]any{"id": "reasoning-1", "type": "reasoning", "content": []any{map[string]any{"text": "synthetic"}}}},
			wantEvent: EventError,
		},
		{
			name: "turn started source spelling drift", method: RuntimeNotificationTurnStarted,
			params:    map[string]any{"threadId": testThreadID, "turn": map[string]any{"id": testTurnID, "status": "in_progress"}},
			wantEvent: EventError,
		},
		{
			name: "error message missing", method: RuntimeNotificationError,
			params:    map[string]any{"threadId": testThreadID, "turnId": testTurnID, "error": map[string]any{}, "willRetry": false},
			wantEvent: EventError,
		},
		{
			name: "error retry flag missing", method: RuntimeNotificationError,
			params: map[string]any{"threadId": testThreadID, "turnId": testTurnID,
				"error": map[string]any{"message": "FEAT134_ERROR_SOURCE_CANARY"}},
			wantEvent: EventError,
		},
		{
			name: "terminal error message missing", method: RuntimeNotificationTurnCompleted,
			params: map[string]any{"threadId": testThreadID,
				"turn": map[string]any{"id": testTurnID, "status": "failed", "error": map[string]any{}}},
			wantEvent: EventTurnCompleted,
		},
		{
			name:      "warning message missing",
			method:    RuntimeNotificationWarning,
			params:    map[string]any{"threadId": testThreadID},
			wantEvent: EventWarning,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store := openBoundFEAT134Store(t)
			v4 := NewEventHubVersion(EventSchemaVersionV4, 8, 2)
			service := NewService(
				&fakeRuntime{}, store, NewEventHub(8, 2), nil, WithV4Events(v4), WithRawReasoningProjection(true),
			)
			service.HandleNotification(test.method, rawJSON(t, test.params))
			_, replay, _, cancel, err := service.SubscribeEventsV4(testSessionID, "", 0)
			if err != nil {
				t.Fatal(err)
			}
			cancel()
			if len(replay) != 1 || replay[0].EventType != test.wantEvent || replay[0].Payload.Code != v4ProjectionLimitCode {
				t.Fatalf("missing Runtime field was not rejected content-free: %+v", replay)
			}
			_, legacy, _, cancelLegacy, err := service.SubscribeEvents(testSessionID, "", 0)
			if err != nil {
				t.Fatal(err)
			}
			cancelLegacy()
			if len(legacy) != 0 {
				t.Fatalf("v4 structural failure leaked into v1: %+v", legacy)
			}
		})
	}
}

func TestFEAT134CanonicalV4FixturesPassClosedProducerValidation(t *testing.T) {
	_, sourceFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate FEAT-134 fixture test source")
	}
	fixtureRoot := filepath.Clean(filepath.Join(filepath.Dir(sourceFile), "..", "..", "api", "fixtures", "agent", "session-event-v4"))
	files := []string{
		"agent-message-commentary-started.json",
		"agent-message-final-completed.json",
		"agent-message-null-phase-started.json",
		"turn-plan-cleared.json",
		"turn-plan-updated.json",
	}
	contract := compileAgentSessionEventV4Contract(t)
	for _, name := range files {
		t.Run(name, func(t *testing.T) {
			encoded, err := os.ReadFile(filepath.Join(fixtureRoot, name))
			if err != nil {
				t.Fatal(err)
			}
			var event Event
			if err := json.Unmarshal(encoded, &event); err != nil {
				t.Fatal(err)
			}
			if err := validateV4RetainedEvent(event); err != nil {
				t.Fatalf("canonical fixture failed closed producer validation: %v", err)
			}
			validateFEAT134Event(t, contract, event)
		})
	}
}

func TestFEAT134V4ClosedProducerRejectsNullCollections(t *testing.T) {
	var nilPlan []PlanStep
	var nilContents []ReasoningContent
	tests := []struct {
		name  string
		event Event
	}{
		{
			name: "plan null",
			event: Event{TaskID: testTaskID, AgentSessionID: testSessionID, CodexThreadID: testThreadID,
				TurnID: testTurnID, EventType: EventTurnPlanUpdated, Payload: EventPayload{Plan: &nilPlan}},
		},
		{
			name: "reasoning contents null",
			event: Event{TaskID: testTaskID, AgentSessionID: testSessionID, CodexThreadID: testThreadID,
				TurnID: testTurnID, ItemID: "reasoning-1", EventType: EventItemReasoningFinalized,
				Payload: EventPayload{Status: "unavailable", ReasonCode: "reasoning_not_emitted", Contents: &nilContents}},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			hub := NewEventHubVersion(EventSchemaVersionV4, 8, 2)
			if _, err := hub.Publish(test.event); !errors.Is(err, ErrEventLimitExceeded) {
				t.Fatalf("expected closed producer rejection, got %v", err)
			}
		})
	}
}

func TestFEAT134RawReasoningDoesNotFollowV3ArtifactNegotiation(t *testing.T) {
	store := openBoundFEAT134Store(t)
	v3 := NewEventHubVersion(EventSchemaVersionV3, 16, 8)
	service := NewService(&fakeRuntime{}, store, NewEventHub(16, 8), nil, WithV3Artifacts(v3, nil, false))
	service.HandleNotification(RuntimeNotificationReasoningTextDelta, rawJSON(t, map[string]any{
		"threadId": testThreadID, "turnId": testTurnID, "itemId": "reasoning-private",
		"contentIndex": 0, "delta": "must not project",
	}))
	service.HandleNotification(RuntimeNotificationItemCompleted, rawJSON(t, map[string]any{
		"threadId": testThreadID, "turnId": testTurnID,
		"item": map[string]any{"id": "reasoning-private", "type": "reasoning", "content": []string{"must not project"}},
	}))
	_, replay, _, cancel, err := service.SubscribeEventsV3(testSessionID, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	if len(replay) != 1 || replay[0].EventType != EventItemCompleted {
		t.Fatalf("v3 artifact negotiation authorized raw reasoning: %+v", replay)
	}
}

func TestFEAT134V4RawReasoningRequiresExplicitAuthorityAndStaysMemoryOnly(t *testing.T) {
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
	v4 := NewEventHubVersion(EventSchemaVersionV4, 16, 8)
	service := NewService(
		&fakeRuntime{}, store, NewEventHub(16, 8), slog.New(slog.NewTextHandler(&logs, nil)),
		WithV4Events(v4), WithRawReasoningProjection(true),
	)
	const rawCanary = "FEAT134_RAW_REASONING_CANARY"
	service.HandleNotification(RuntimeNotificationReasoningTextDelta, rawJSON(t, map[string]any{
		"threadId": testThreadID, "turnId": testTurnID, "itemId": "reasoning-1",
		"contentIndex": 0, "delta": rawCanary,
	}))
	service.HandleNotification(RuntimeNotificationItemCompleted, rawJSON(t, map[string]any{
		"threadId": testThreadID, "turnId": testTurnID,
		"item": map[string]any{"id": "reasoning-1", "type": "reasoning", "content": []string{rawCanary}},
	}))

	_, replay, _, cancel, err := service.SubscribeEventsV4(testSessionID, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	if len(replay) != 3 || replay[0].EventType != EventItemReasoningTextDelta ||
		replay[1].EventType != EventItemReasoningFinalized || replay[2].EventType != EventItemCompleted {
		t.Fatalf("unexpected v4 raw-reasoning lifecycle: %+v", replay)
	}
	contract := compileAgentSessionEventV4Contract(t)
	for _, event := range replay {
		validateFEAT134Event(t, contract, event)
	}
	if err := store.db.Sync(); err != nil {
		t.Fatal(err)
	}
	database, err := os.ReadFile(filepath.Join(home, "sessions.db"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(logs.String(), rawCanary) || bytes.Contains(database, []byte(rawCanary)) {
		t.Fatal("raw reasoning entered Host logs or bbolt")
	}
}

func TestFEAT134V4ProjectionFailureIsContentFreeAndForcesSanitizedTerminal(t *testing.T) {
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
	if _, err := store.BindTurn(testSessionID, testTurnID); err != nil {
		t.Fatal(err)
	}
	var logs bytes.Buffer
	v4 := NewEventHubVersion(EventSchemaVersionV4, 16, 8)
	service := NewService(&fakeRuntime{}, store, NewEventHub(16, 8), slog.New(slog.NewTextHandler(&logs, nil)), WithV4Events(v4))
	const canary = "FEAT134_BODY_CANARY"
	oversize := canary + strings.Repeat("x", maxV4AgentTextBytes)
	service.HandleNotification(RuntimeNotificationItemCompleted, rawJSON(t, map[string]any{
		"threadId": testThreadID, "turnId": testTurnID,
		"item": map[string]any{"id": "agent-oversize", "type": "agentMessage", "text": oversize, "phase": "final_answer"},
	}))
	service.HandleNotification(RuntimeNotificationTurnCompleted, rawJSON(t, map[string]any{
		"threadId": testThreadID,
		"turn":     map[string]any{"id": testTurnID, "status": "completed", "error": nil},
	}))

	_, replay, _, cancel, err := service.SubscribeEventsV4(testSessionID, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	if len(replay) != 2 || replay[0].EventType != EventError || replay[0].Terminal ||
		replay[1].EventType != EventTurnCompleted || !replay[1].Terminal {
		t.Fatalf("unexpected sanitized failure sequence: %+v", replay)
	}
	for _, event := range replay {
		encoded, err := json.Marshal(event)
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(encoded, []byte(canary)) || event.Payload.Code != v4ProjectionLimitCode ||
			event.Payload.Message == nil || *event.Payload.Message != v4ProjectionLimitMessage {
			t.Fatalf("projection failure leaked or drifted: %s", encoded)
		}
	}
	if replay[1].Payload.Status != "failed" {
		t.Fatalf("projection-failed turn retained the Runtime status: %+v", replay[1])
	}
	if err := store.db.Sync(); err != nil {
		t.Fatal(err)
	}
	database, err := os.ReadFile(filepath.Join(home, "sessions.db"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(logs.String(), canary) || bytes.Contains(database, []byte(canary)) {
		t.Fatal("rejected body entered Host logs or bbolt")
	}
}

func TestFEAT134V4HubRejectsOversizeBeforeRetentionWithoutSequenceGap(t *testing.T) {
	hub := NewEventHubVersion(EventSchemaVersionV4, 8, 2)
	message := strings.Repeat("x", maxV4EventDataBytes)
	willRetry := false
	if _, err := hub.Publish(Event{
		TaskID: testTaskID, AgentSessionID: testSessionID, CodexThreadID: testThreadID,
		EventType: EventWarning,
		Payload:   EventPayload{Message: &message, WillRetry: &willRetry},
	}); !errors.Is(err, ErrEventLimitExceeded) {
		t.Fatalf("expected aggregate limit rejection, got %v", err)
	}
	safe := "safe"
	published, err := hub.Publish(Event{
		TaskID: testTaskID, AgentSessionID: testSessionID, CodexThreadID: testThreadID,
		EventType: EventWarning,
		Payload:   EventPayload{Message: &safe, WillRetry: &willRetry},
	})
	if err != nil {
		t.Fatal(err)
	}
	if published.Sequence != 1 {
		t.Fatalf("rejected event consumed sequence: %+v", published)
	}
}

func TestFEAT134FixedReasoningEffortOverridesTurnRequest(t *testing.T) {
	store := openBoundFEAT134Store(t)
	gotEffort := ""
	runtime := &fakeRuntime{startTurn: func(_, _, effort string) (codex.TurnInfo, error) {
		gotEffort = effort
		return codex.TurnInfo{ID: testTurnID, Status: "inProgress"}, nil
	}}
	service := NewService(runtime, store, NewEventHub(8, 2), nil, WithFixedReasoningEffort("high"))
	if _, err := service.StartTurn(context.Background(), StartTurnInput{
		AgentSessionID: testSessionID, Input: "safe text", ReasoningEffort: "none",
	}); err != nil {
		t.Fatal(err)
	}
	if gotEffort != "high" {
		t.Fatalf("managed effective effort drifted: %q", gotEffort)
	}
}

func TestFEAT134FixedReasoningEffortOverridesMultimodalTurnRequest(t *testing.T) {
	store := openBoundFEAT134Store(t)
	gotEffort := ""
	runtime := &multimodalTestRuntime{startV2: func(_ string, _ []codex.UserInput, effort string) (codex.TurnInfo, error) {
		gotEffort = effort
		return codex.TurnInfo{ID: testTurnID, Status: "inProgress"}, nil
	}}
	service := NewService(runtime, store, NewEventHub(8, 2), nil, WithFixedReasoningEffort("high"))
	if _, err := service.StartTurnV2(context.Background(), StartTurnV2Input{
		AgentSessionID:  testSessionID,
		OperationID:     testTurnOperationID,
		ContentBlocks:   []TurnContentBlock{{Type: ContentBlockText, Text: "safe text"}},
		ReasoningEffort: "none",
	}); err != nil {
		t.Fatal(err)
	}
	if gotEffort != "high" {
		t.Fatalf("managed multimodal effective effort drifted: %q", gotEffort)
	}
}

func TestFEAT134OversizeTraceDoesNotRejectSharedOperationsAndFailsOnlyV4Projection(t *testing.T) {
	store, err := OpenStore(filepath.Join(t.TempDir(), "host-home"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	oversizeTrace := TraceContext{TraceID: strings.Repeat("t", maxV4IdentityRunes+1)}
	runtime := &fakeRuntime{
		startThread: func(string) (codex.ThreadInfo, error) {
			return codex.ThreadInfo{
				ID: testThreadID, RuntimeSession: "runtime-session",
				Model: codex.MiniMaxModel, ModelProvider: codex.MiniMaxProviderID,
			}, nil
		},
		resume: func(string) (codex.ThreadInfo, error) {
			return codex.ThreadInfo{
				ID: testThreadID, RuntimeSession: "runtime-session",
				Model: codex.MiniMaxModel, ModelProvider: codex.MiniMaxProviderID,
			}, nil
		},
		startTurn: func(_, _, _ string) (codex.TurnInfo, error) {
			return codex.TurnInfo{ID: testTurnID, Status: "inProgress"}, nil
		},
	}
	v4 := NewEventHubVersion(EventSchemaVersionV4, 16, 8)
	service := NewService(runtime, store, NewEventHub(16, 8), nil, WithV4Events(v4))
	record, err := service.StartSession(context.Background(), StartSessionInput{
		TaskID: testTaskID, Cwd: t.TempDir(), Trace: oversizeTrace,
	})
	if err != nil {
		t.Fatalf("shared StartSession rejected v4-only trace bound: %v", err)
	}
	if _, err := service.ResumeSession(context.Background(), record.AgentSessionID, oversizeTrace); err != nil {
		t.Fatalf("shared ResumeSession rejected v4-only trace bound: %v", err)
	}
	if _, err := service.StartTurn(context.Background(), StartTurnInput{
		AgentSessionID: record.AgentSessionID, Input: "synthetic", ReasoningEffort: "none", Trace: oversizeTrace,
	}); err != nil {
		t.Fatalf("shared StartTurn rejected v4-only trace bound: %v", err)
	}
	service.HandleNotification(RuntimeNotificationTurnStarted, rawJSON(t, map[string]any{
		"threadId": testThreadID,
		"turn":     map[string]any{"id": testTurnID, "status": "inProgress"},
	}))
	if err := service.InterruptTurn(context.Background(), record.AgentSessionID, testTurnID, oversizeTrace); err != nil {
		t.Fatalf("shared InterruptTurn rejected v4-only trace bound: %v", err)
	}

	_, legacy, _, cancelLegacy, err := service.SubscribeEvents(record.AgentSessionID, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	cancelLegacy()
	if len(legacy) != 1 || legacy[0].EventType != EventTurnStarted || legacy[0].TraceID != oversizeTrace.TraceID {
		t.Fatalf("shared legacy projection changed: %+v", legacy)
	}
	_, replay, _, cancel, err := service.SubscribeEventsV4(record.AgentSessionID, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	if len(replay) != 1 || replay[0].EventType != EventError || replay[0].Payload.Code != v4ProjectionLimitCode || replay[0].TraceID != "" {
		t.Fatalf("oversize trace did not fail content-free at the v4 publish boundary: %+v", replay)
	}

	v2Store := openBoundFEAT134Store(t)
	v2Runtime := &multimodalTestRuntime{startV2: func(_ string, _ []codex.UserInput, _ string) (codex.TurnInfo, error) {
		return codex.TurnInfo{ID: testTurnID, Status: "inProgress"}, nil
	}}
	v2Service := NewService(v2Runtime, v2Store, NewEventHub(8, 2), nil, WithV4Events(NewEventHubVersion(EventSchemaVersionV4, 8, 2)))
	if _, err := v2Service.StartTurnV2(context.Background(), StartTurnV2Input{
		AgentSessionID: testSessionID, OperationID: testTurnOperationID,
		ContentBlocks: []TurnContentBlock{{Type: ContentBlockText, Text: "synthetic"}},
		Trace:         oversizeTrace,
	}); err != nil {
		t.Fatalf("shared StartTurnV2 rejected v4-only trace bound: %v", err)
	}
}

func TestFEAT134MalformedTerminalCompletesStoreOnceWithoutLegacyProjection(t *testing.T) {
	store := openBoundFEAT134Store(t)
	if _, err := store.BindTurn(testSessionID, testTurnID); err != nil {
		t.Fatal(err)
	}
	v2 := NewEventHubVersion(EventSchemaVersionV2, 16, 8)
	v4 := NewEventHubVersion(EventSchemaVersionV4, 16, 8)
	service := NewService(
		&fakeRuntime{}, store, NewEventHub(16, 8), nil,
		WithV2Events(v2), WithV4Events(v4), WithRawReasoningProjection(true),
	)
	reasoningKey := reasoningTurnKey{sessionID: testSessionID, turnID: testTurnID}
	service.HandleNotification(RuntimeNotificationReasoningTextDelta, rawJSON(t, map[string]any{
		"threadId": testThreadID, "turnId": testTurnID, "itemId": "reasoning-canary",
		"contentIndex": 0, "delta": "FEAT134_REASONING_CANARY",
	}))
	service.HandleNotification(RuntimeNotificationTurnCompleted, rawJSON(t, map[string]any{
		"threadId": testThreadID,
		"turn": map[string]any{
			"id": testTurnID, "status": "failed", "error": map[string]any{"codexErrorInfo": "other"},
		},
	}))
	record, err := store.Get(testSessionID)
	if err != nil {
		t.Fatal(err)
	}
	if record.State != StateIdle || record.ActiveTurnID != "" || record.LastTurnID != testTurnID || record.LastTurnStatus != "failed" {
		t.Fatalf("malformed terminal did not finish the Store turn as failed: %+v", record)
	}
	service.reasoningMu.Lock()
	_, reasoningRetained := service.reasoning[reasoningKey]
	service.reasoningMu.Unlock()
	if reasoningRetained {
		t.Fatal("malformed terminal retained reasoning turn state")
	}
	_, replay, _, cancel, err := service.SubscribeEventsV4(testSessionID, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	if len(replay) != 3 || replay[0].EventType != EventItemReasoningTextDelta ||
		replay[1].EventType != EventItemReasoningFinalized || replay[2].EventType != EventTurnCompleted || !replay[2].Terminal ||
		replay[2].Payload.Status != "failed" || replay[2].Payload.Code != v4ProjectionLimitCode {
		t.Fatalf("malformed terminal did not order reasoning finalization before sanitized terminal: %+v", replay)
	}
	if replay[1].Payload.Status != "unavailable" || replay[1].Payload.ReasonCode != "protocol_error" ||
		replay[1].Payload.Contents == nil || len(*replay[1].Payload.Contents) != 0 || replay[1].Payload.Delta != nil {
		t.Fatalf("malformed terminal reasoning finalization was not content-free: %+v", replay[1])
	}
	contract := compileAgentSessionEventV4Contract(t)
	for _, event := range replay {
		validateFEAT134Event(t, contract, event)
	}
	assertFEAT134MalformedTerminalLegacyEvents(t, service, v2)

	service.HandleNotification(RuntimeNotificationTurnCompleted, rawJSON(t, map[string]any{
		"threadId": testThreadID,
		"turn":     map[string]any{"id": testTurnID, "status": "completed", "error": nil},
	}))
	_, replay, _, cancel, err = service.SubscribeEventsV4(testSessionID, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	if len(replay) != 3 {
		t.Fatalf("later Runtime terminal created a conflicting v4 terminal: %+v", replay)
	}
	record, err = store.Get(testSessionID)
	if err != nil {
		t.Fatal(err)
	}
	if record.LastTurnStatus != "failed" {
		t.Fatalf("later Runtime terminal overwrote sanitized terminal authority: %+v", record)
	}
	assertFEAT134MalformedTerminalLegacyEvents(t, service, v2)
}

func TestFEAT134AgentDeltaRequiresSameTurnAgentLifecycleOnlyInV4(t *testing.T) {
	otherTurnID := "019c0123-4567-7abc-8123-456789abcdf0"
	tests := []struct {
		name          string
		lifecycleType string
		lifecycleTurn string
		lifecycleItem string
		deltaTurn     string
		deltaItem     string
	}{
		{name: "wrong item", lifecycleType: "agentMessage", lifecycleTurn: testTurnID, lifecycleItem: "agent-1", deltaTurn: testTurnID, deltaItem: "agent-2"},
		{name: "cross turn", lifecycleType: "agentMessage", lifecycleTurn: testTurnID, lifecycleItem: "agent-1", deltaTurn: otherTurnID, deltaItem: "agent-1"},
		{name: "non agent item", lifecycleType: "reasoning", lifecycleTurn: testTurnID, lifecycleItem: "agent-1", deltaTurn: testTurnID, deltaItem: "agent-1"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store := openBoundFEAT134Store(t)
			v4 := NewEventHubVersion(EventSchemaVersionV4, 16, 8)
			service := NewService(&fakeRuntime{}, store, NewEventHub(16, 8), nil, WithV4Events(v4))
			item := map[string]any{"id": test.lifecycleItem, "type": test.lifecycleType}
			if test.lifecycleType == "agentMessage" {
				item["text"] = ""
				item["phase"] = "commentary"
			}
			service.HandleNotification(RuntimeNotificationItemStarted, rawJSON(t, map[string]any{
				"threadId": testThreadID, "turnId": test.lifecycleTurn, "item": item,
			}))
			service.HandleNotification(RuntimeNotificationItemAgentMessageDelta, rawJSON(t, map[string]any{
				"threadId": testThreadID, "turnId": test.deltaTurn, "itemId": test.deltaItem, "delta": "synthetic",
			}))

			_, replay, _, cancel, err := service.SubscribeEventsV4(testSessionID, "", 0)
			if err != nil {
				t.Fatal(err)
			}
			cancel()
			if len(replay) != 2 || replay[0].EventType != EventItemStarted || replay[1].EventType != EventError ||
				replay[1].TurnID != test.deltaTurn || replay[1].Payload.Code != v4ProjectionLimitCode {
				t.Fatalf("v4 retained an uncorrelated AgentMessage delta: %+v", replay)
			}
			_, legacy, _, cancelLegacy, err := service.SubscribeEvents(testSessionID, "", 0)
			if err != nil {
				t.Fatal(err)
			}
			cancelLegacy()
			if len(legacy) != 2 || legacy[1].EventType != EventItemAgentMessageDelta || legacy[1].ItemID != test.deltaItem {
				t.Fatalf("v4 correlation changed legacy delta semantics: %+v", legacy)
			}
		})
	}
}

func TestFEAT134V4FinalizesNinthReasoningItemWithoutFailingTurnOrChangingV2V3(t *testing.T) {
	store := openBoundFEAT134Store(t)
	v2 := NewEventHubVersion(EventSchemaVersionV2, 32, 8)
	v3 := NewEventHubVersion(EventSchemaVersionV3, 32, 8)
	v4 := NewEventHubVersion(EventSchemaVersionV4, 32, 8)
	service := NewService(
		&fakeRuntime{}, store, NewEventHub(32, 8), nil,
		WithV2Events(v2), WithV3Artifacts(v3, nil, false), WithV4Events(v4), WithRawReasoningProjection(true),
	)
	itemIDs := []string{"reasoning-1", "reasoning-2", "reasoning-3", "reasoning-4", "reasoning-5", "reasoning-6", "reasoning-7", "reasoning-8", "reasoning-9"}
	for _, itemID := range itemIDs {
		service.HandleNotification(RuntimeNotificationReasoningTextDelta, rawJSON(t, map[string]any{
			"threadId": testThreadID, "turnId": testTurnID, "itemId": itemID, "contentIndex": 0, "delta": "synthetic",
		}))
	}
	_, replay, _, cancel, err := service.SubscribeEventsV4(testSessionID, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	if len(replay) != maxV4ReasoningItemsPerTurn+1 {
		t.Fatalf("unexpected v4 reasoning replay length: %+v", replay)
	}
	limited := replay[len(replay)-1]
	if limited.EventType != EventItemReasoningFinalized ||
		limited.ItemID != itemIDs[8] || limited.Payload.Status != "unavailable" || limited.Payload.ReasonCode != "limit_exceeded" ||
		limited.Payload.Contents == nil || len(*limited.Payload.Contents) != 0 || limited.Payload.Delta != nil {
		t.Fatalf("ninth reasoning item did not finalize unavailable content-free: %+v", replay)
	}
	if service.v4TurnProjectionFailed(testSessionID, testTurnID) {
		t.Fatal("reasoning-specific item limit failed the whole v4 turn projection")
	}
	validateFEAT134Event(t, compileAgentSessionEventV4Contract(t), limited)
	assertFEAT134ReasoningLegacyCount(t, v2, len(itemIDs), itemIDs[8])
	assertFEAT134ReasoningLegacyCount(t, v3, len(itemIDs), itemIDs[8])

	service.HandleNotification(RuntimeNotificationReasoningTextDelta, rawJSON(t, map[string]any{
		"threadId": testThreadID, "turnId": testTurnID, "itemId": itemIDs[8], "contentIndex": 0, "delta": "again",
	}))
	_, replay, _, cancel, err = service.SubscribeEventsV4(testSessionID, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	if len(replay) != maxV4ReasoningItemsPerTurn+1 {
		t.Fatalf("repeated ninth item emitted duplicate v4 finalization: %+v", replay)
	}
	assertFEAT134ReasoningLegacyCount(t, v2, len(itemIDs)+1, itemIDs[8])
	assertFEAT134ReasoningLegacyCount(t, v3, len(itemIDs)+1, itemIDs[8])

	service.HandleNotification(RuntimeNotificationTurnPlanUpdated, rawJSON(t, map[string]any{
		"threadId": testThreadID, "turnId": testTurnID, "plan": []any{},
	}))
	_, replay, _, cancel, err = service.SubscribeEventsV4(testSessionID, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	if len(replay) != maxV4ReasoningItemsPerTurn+2 || replay[len(replay)-1].EventType != EventTurnPlanUpdated {
		t.Fatalf("reasoning-specific limit prevented later v4 projection: %+v", replay)
	}
}

func assertFEAT134MalformedTerminalLegacyEvents(t *testing.T, service *Service, v2 *EventHub) {
	t.Helper()
	_, legacy, _, cancelLegacy, err := service.SubscribeEvents(testSessionID, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	cancelLegacy()
	if len(legacy) != 0 {
		t.Fatalf("v4 failure leaked into v1: %+v", legacy)
	}
	_, replayV2, _, cancelV2, err := v2.Subscribe(testSessionID, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	cancelV2()
	if len(replayV2) != 1 || replayV2[0].EventType != EventItemReasoningTextDelta || replayV2[0].ItemID != "reasoning-canary" {
		t.Fatalf("malformed v4 terminal changed v2 reasoning semantics: %+v", replayV2)
	}
}

func assertFEAT134ReasoningLegacyCount(t *testing.T, hub *EventHub, want int, ninthItemID string) {
	t.Helper()
	_, replay, _, cancel, err := hub.Subscribe(testSessionID, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	if len(replay) != want || replay[want-1].ItemID != ninthItemID || replay[want-1].EventType != EventItemReasoningFinalized {
		t.Fatalf("legacy reasoning semantics changed: %+v", replay)
	}
}

func openBoundFEAT134Store(t *testing.T) *Store {
	t.Helper()
	store, err := OpenStore(filepath.Join(t.TempDir(), "host-home"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.Reserve(Record{TaskID: testTaskID, AgentSessionID: testSessionID, Cwd: t.TempDir()}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.BindThread(testSessionID, testThreadID, "runtime-session", codex.MiniMaxModel, codex.MiniMaxProviderID); err != nil {
		t.Fatal(err)
	}
	return store
}

func compileAgentSessionEventV4Contract(t *testing.T) *jsonschema.Schema {
	t.Helper()
	_, sourceFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate Agent session v4 contract test source")
	}
	contract, err := jsonschema.NewCompiler().Compile(filepath.Clean(filepath.Join(
		filepath.Dir(sourceFile), "..", "..", "api", "jsonschema", "agent-session-event-v4.schema.json",
	)))
	if err != nil {
		t.Fatalf("compile AgentSessionEventV4 JSON Schema: %v", err)
	}
	return contract
}

func validateFEAT134Event(t *testing.T, contract *jsonschema.Schema, event Event) {
	t.Helper()
	encoded, err := json.Marshal(event)
	if err != nil {
		t.Fatal(err)
	}
	if len(encoded) > maxV4EventDataBytes {
		t.Fatalf("retained v4 event exceeds the SSE data limit: %d", len(encoded))
	}
	instance, err := jsonschema.UnmarshalJSON(bytes.NewReader(encoded))
	if err != nil {
		t.Fatal(err)
	}
	if err := contract.Validate(instance); err != nil {
		t.Fatalf("v4 event violates contract: %v\n%s", err, encoded)
	}
}
