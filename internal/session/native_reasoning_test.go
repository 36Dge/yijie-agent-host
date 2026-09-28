package session

import (
	"context"
	"github.com/36Dge/yijie-agent-host/internal/codex"
	"testing"
)

func TestNativeDailyReasoningTurnDispatchAndIdempotency(t *testing.T) {
	for _, initiallyEnabled := range []bool{false, true} {
		t.Run(map[bool]string{false: "enable-after-accepted", true: "disable-after-accepted"}[initiallyEnabled], func(t *testing.T) {
			store := openBoundFEAT134Store(t)
			calls := 0
			effort := ""
			runtime := &multimodalTestRuntime{startV2: func(_ string, _ []codex.UserInput, e string) (codex.TurnInfo, error) {
				calls++
				effort = e
				return codex.TurnInfo{ID: testTurnID, Status: "inProgress"}, nil
			}}
			options := []ServiceOption{}
			if initiallyEnabled {
				options = append(options, WithNativeReasoning())
			}
			service := NewService(runtime, store, NewEventHub(16, 8), nil, options...)
			input := StartTurnV2Input{AgentSessionID: testSessionID, OperationID: testTurnOperationID, ContentBlocks: []TurnContentBlock{{Type: ContentBlockText, Text: "普通推理验证"}}}
			first, err := service.StartTurnV2(context.Background(), input)
			if err != nil {
				t.Fatal(err)
			}
			want := "none"
			if initiallyEnabled {
				want = "high"
			}
			if effort != want {
				t.Fatalf("effort=%s", effort)
			}
			options = nil
			if !initiallyEnabled {
				options = append(options, WithNativeReasoning())
			}
			service = NewService(runtime, store, NewEventHub(16, 8), nil, options...)
			replay, err := service.StartTurnV2(context.Background(), input)
			if err != nil || replay.ID != first.ID || calls != 1 {
				t.Fatalf("accepted retry changed after configuration switch: calls=%d err=%v", calls, err)
			}
		})
	}
}

func TestNativeDailyReasoningV1AndProjection(t *testing.T) {
	got := ""
	runtime := &fakeRuntime{startTurn: func(_, _, e string) (codex.TurnInfo, error) {
		got = e
		return codex.TurnInfo{ID: testTurnID, Status: "inProgress"}, nil
	}}
	s := NewService(runtime, openBoundFEAT134Store(t), NewEventHub(32, 8), nil, WithNativeReasoning())
	if _, err := s.StartTurn(context.Background(), StartTurnInput{AgentSessionID: testSessionID, Input: "普通推理验证"}); err != nil {
		t.Fatal(err)
	}
	if got != "high" || s.eventsV2 != nil || s.eventsV4 != nil || s.eventsV5 != nil || s.eventsV6 != nil {
		t.Fatal("daily dispatch or legacy isolation differs")
	}
	for _, input := range []struct {
		method string
		fields map[string]any
	}{
		{RuntimeNotificationItemStarted, map[string]any{"item": map[string]any{"id": "r", "type": "reasoning", "summary": []any{}, "content": []any{}}}},
		{RuntimeNotificationReasoningTextDelta, map[string]any{"itemId": "r", "contentIndex": 0, "delta": "先核对条件。"}},
		{"item/reasoning/summaryTextDelta", map[string]any{"itemId": "r", "summaryIndex": 0, "delta": "检查条件"}},
		{RuntimeNotificationItemCompleted, map[string]any{"item": map[string]any{"id": "r", "type": "reasoning", "summary": []string{"检查条件"}, "content": []string{"先核对条件。"}}}},
	} {
		input.fields["threadId"] = testThreadID
		input.fields["turnId"] = testTurnID
		s.HandleNotification(input.method, rawJSON(t, input.fields))
	}
	_, events, _, cancel, err := s.SubscribeNativeEvents(testSessionID, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	defer cancel()
	if len(events) != 4 {
		t.Fatalf("native process events=%d", len(events))
	}
	if events[1].Payload.Native.Delta == nil || *events[1].Payload.Native.Delta != "先核对条件。" {
		t.Fatal("raw delta lost")
	}
	final := events[3].Payload.Native.Item
	if final == nil || final.Content == nil || len(*final.Content) != 1 || final.Summary == nil || len(*final.Summary) != 1 || events[3].Terminal {
		t.Fatal("reasoning item content lost or turn was ended")
	}
}
