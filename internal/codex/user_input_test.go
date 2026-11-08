package codex

import (
	"context"
	"encoding/json"
	"errors"
	native "github.com/36Dge/yijie-agent-host/internal/contracts/runtimeinteraction"
	wire "github.com/36Dge/yijie-agent-host/internal/contracts/userinput"
	"github.com/google/uuid"
	"strings"
	"testing"
	"time"
)

func inputTestRequest(thread, turn string, timeout bool) serverRequest {
	p := map[string]any{"threadId": thread, "turnId": turn, "itemId": "ordinary-input", "questions": []any{map[string]any{"id": "colour", "header": "颜色", "question": "请输入普通样例颜色", "isOther": true, "isSecret": true}}}
	if timeout {
		p["autoResolutionMs"] = 60000
	}
	b, _ := json.Marshal(p)
	id := json.RawMessage(`"ordinary-input"`)
	key, _ := requestIDKey(id)
	return serverRequest{method: userInputMethod, idKey: key, id: id, params: b, exactEnvelope: true}
}
func inputTestPending(t *testing.T, timeout bool) (*Manager, string, string, <-chan serverRequestResult, context.CancelFunc) {
	t.Helper()
	m := NewManager(Config{InteractionToolsEnabled: true, NativeToolsEnabled: true, RuntimePermissionsEnabled: true}, nil)
	m.status.Ready = true
	thread, turn := uuid.NewString(), uuid.NewString()
	m.registerNativeToolsRoot(thread)
	m.permissionCallbacks.activeTurns = map[string]string{thread: turn}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	out := make(chan serverRequestResult, 1)
	go func() { out <- m.handleUserInput(ctx, inputTestRequest(thread, turn, timeout)) }()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		s := m.UserInputSnapshot(thread)
		if len(s.Requests) == 1 {
			return m, thread, s.Requests[0].RequestId.String(), out, cancel
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("question was not registered")
	return nil, "", "", nil, cancel
}
func inputTestAnswer() wire.UserInputAnswerRequest {
	return wire.UserInputAnswerRequest{OperationId: uuid.New(), Answers: map[string]wire.ToolRequestUserInputAnswer{"colour": {Answers: []string{"synthetic-sensitive-blue"}}}}
}
func TestUserInputOriginalBindingIdempotencyAndWriteReceipt(t *testing.T) {
	m, thread, id, out, _ := inputTestPending(t, false)
	answer := inputTestAnswer()
	if _, err := m.AnswerUserInput(context.Background(), uuid.NewString(), id, answer); !errors.Is(err, ErrUserInputNotFound) {
		t.Fatal("another session received the request")
	}
	done := make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func() {
			r, e := m.AnswerUserInput(context.Background(), thread, id, answer)
			if e == nil && r.State != "answered" {
				e = errors.New("answer not acknowledged")
			}
			done <- e
		}()
	}
	var result serverRequestResult
	select {
	case result = <-out:
	case <-time.After(2 * time.Second):
		t.Fatal("response unavailable")
	}
	if !result.respond || result.onResponseWritten == nil {
		t.Fatal("native response callback missing")
	}
	value := result.value.(native.ToolRequestUserInputResponse)
	if value.Answers["colour"].Answers[0] != "synthetic-sensitive-blue" {
		t.Fatal("answer changed")
	}
	snapshot, _ := m.GetUserInput(thread, id)
	if snapshot.State != "submitting" {
		t.Fatal("claimed delivery before native write")
	}
	m.observeUserInputNotification("serverRequest/resolved", json.RawMessage(`{"threadId":"`+thread+`","requestId":"ordinary-input"}`))
	result.onResponseWritten(nil)
	for i := 0; i < 2; i++ {
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
	receipt, _ := m.GetUserInput(thread, id)
	b, _ := json.Marshal(receipt)
	if strings.Contains(string(b), "synthetic-sensitive-blue") || len(receipt.Questions) != 0 {
		t.Fatal("receipt retained input content")
	}
	again, err := m.AnswerUserInput(context.Background(), thread, id, answer)
	if err != nil || again.State != "answered" {
		t.Fatal("idempotent receipt failed")
	}
	answer.OperationId = uuid.New()
	if _, err = m.AnswerUserInput(context.Background(), thread, id, answer); !errors.Is(err, ErrUserInputConflict) {
		t.Fatal("second operation accepted")
	}
	if len(m.UserInputSnapshot(thread).Requests) != 0 {
		t.Fatal("answered request still pending")
	}
}
func TestUserInputOptionalTimeoutPauseAndSkip(t *testing.T) {
	t.Run("pause", func(t *testing.T) {
		m, thread, id, out, cancel := inputTestPending(t, true)
		r, err := m.TouchUserInput(thread, id)
		if err != nil || !r.TimeoutPaused || r.DeadlineAt != nil {
			t.Fatal("timeout not paused")
		}
		cancel()
		result := <-out
		if result.respond {
			t.Fatal("cancelled request returned an answer")
		}
		r, _ = m.GetUserInput(thread, id)
		if r.State != "cancelled" {
			t.Fatal(r.State)
		}
	})
	t.Run("expired touch", func(t *testing.T) {
		m, thread, id, out, _ := inputTestPending(t, true)
		m.userInputs.mu.Lock()
		past := time.Now().Add(-time.Second)
		m.userInputs.entries[id].view.DeadlineAt = &past
		m.userInputs.mu.Unlock()
		r, err := m.TouchUserInput(thread, id)
		if err != nil || r.State != "submitting" || r.TimeoutPaused {
			t.Fatal("late touch resurrected timeout")
		}
		result := <-out
		if len(result.value.(native.ToolRequestUserInputResponse).Answers) != 0 {
			t.Fatal("timeout chose an answer")
		}
		result.onResponseWritten(nil)
		r, _ = m.GetUserInput(thread, id)
		if r.State != "timed_out" {
			t.Fatal(r.State)
		}
	})
	t.Run("skip", func(t *testing.T) {
		m, thread, id, out, _ := inputTestPending(t, false)
		done := make(chan wire.UserInputRequest, 1)
		go func() {
			r, _ := m.AnswerUserInput(context.Background(), thread, id, wire.UserInputAnswerRequest{OperationId: uuid.New(), Answers: map[string]wire.ToolRequestUserInputAnswer{}})
			done <- r
		}()
		result := <-out
		result.onResponseWritten(nil)
		if (<-done).State != "skipped" {
			t.Fatal("skip not distinguished")
		}
	})
}
func TestUserInputEndedTurnAndNewGenerationCannotAnswer(t *testing.T) {
	m, thread, id, out, _ := inputTestPending(t, false)
	r, _ := m.GetUserInput(thread, id)
	m.observeUserInputNotification("turn/completed", json.RawMessage(`{"threadId":"`+thread+`","turn":{"id":"`+r.TurnId+`"}}`))
	if (<-out).respond {
		t.Fatal("ended turn received late response")
	}
	if _, err := m.AnswerUserInput(context.Background(), thread, id, inputTestAnswer()); !errors.Is(err, ErrUserInputConflict) {
		t.Fatal("ended turn answer accepted")
	}
	other := NewManager(m.config, nil)
	if _, err := other.GetUserInput(thread, id); !errors.Is(err, ErrUserInputNotFound) {
		t.Fatal("request revived after restart")
	}
}
func TestUserInputValidatesOriginalQuestionsAndOptionality(t *testing.T) {
	m, thread, id, _, cancel := inputTestPending(t, false)
	defer cancel()
	answer := inputTestAnswer()
	answer.Answers = map[string]wire.ToolRequestUserInputAnswer{"another ordinary question": {Answers: []string{"blue"}}}
	if _, err := m.AnswerInputForTest(thread, id, answer); !errors.Is(err, ErrUserInputInvalid) {
		t.Fatal("unrelated answer accepted")
	}
	r, _ := m.GetUserInput(thread, id)
	if r.DeadlineAt != nil || r.State != "pending" {
		t.Fatal("required input acquired a deadline")
	}
	m.permissionCallbacks.mu.Lock()
	m.permissionCallbacks.nativeRoots[thread] = false
	m.permissionCallbacks.mu.Unlock()
	result := m.handleUserInput(context.Background(), inputTestRequest(thread, r.TurnId, false))
	if result.rpcError == nil {
		t.Fatal("non-root request admitted")
	}
}

// Use an already cancelled caller only for invalid inputs; invalidity must be
// decided before any native submission, independent of transport cancellation.
func (m *Manager) AnswerInputForTest(thread, id string, a wire.UserInputAnswerRequest) (wire.UserInputRequest, error) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	return m.AnswerUserInput(ctx, thread, id, a)
}

func inputTestCompleted(thread, turn string) json.RawMessage {
	return json.RawMessage(`{"threadId":"` + thread + `","turn":{"id":"` + turn + `","items":[],"itemsView":"notLoaded","status":"interrupted","error":null}}`)
}

func TestUserInputAdmissionAfterKnownTurnCompletion(t *testing.T) {
	for _, action := range []string{"answer", "touch", "timeout"} {
		t.Run(action, func(t *testing.T) {
			m, thread, id, out, _ := inputTestPending(t, true)
			r, _ := m.GetUserInput(thread, id)
			// Normal notification order: the native permission adapter has seen
			// completion, but the question observer has not run yet.
			m.handlePermissionNotification("turn/completed", inputTestCompleted(thread, r.TurnId))
			switch action {
			case "answer":
				if _, err := m.AnswerUserInput(context.Background(), thread, id, inputTestAnswer()); !errors.Is(err, ErrUserInputConflict) {
					t.Fatalf("late answer error = %v", err)
				}
			case "touch":
				r, err := m.TouchUserInput(thread, id)
				if err != nil || r.State != "cancelled" || r.TimeoutPaused {
					t.Fatalf("late touch = %s, %v", r.State, err)
				}
			case "timeout":
				m.userInputs.mu.Lock()
				e := m.userInputs.entries[id]
				past := time.Now().Add(-time.Second)
				e.view.DeadlineAt = &past
				m.userInputs.mu.Unlock()
				m.expireUserInput(e)
			}
			select {
			case result := <-out:
				if result.respond {
					t.Fatal("ended turn received a response")
				}
			case <-time.After(2 * time.Second):
				t.Fatal("question callback did not close normally")
			}
			m.observeUserInputNotification("turn/completed", inputTestCompleted(thread, r.TurnId))
			r, _ = m.GetUserInput(thread, id)
			if r.State != "cancelled" || len(r.Questions) != 0 {
				t.Fatal("cancelled receipt not retained")
			}
		})
	}
}

func TestUserInputAlreadyAdmittedAnswerKeepsReceiptAfterCompletion(t *testing.T) {
	m, thread, id, out, _ := inputTestPending(t, false)
	answer := inputTestAnswer()
	done := make(chan error, 1)
	go func() {
		r, err := m.AnswerUserInput(context.Background(), thread, id, answer)
		if err == nil && r.State != "answered" {
			err = errors.New("original receipt was lost")
		}
		done <- err
	}()
	var result serverRequestResult
	select {
	case result = <-out:
	case <-time.After(2 * time.Second):
		t.Fatal("answer did not reach original callback")
	}
	r, _ := m.GetUserInput(thread, id)
	m.handlePermissionNotification("turn/completed", inputTestCompleted(thread, r.TurnId))
	m.observeUserInputNotification("turn/completed", inputTestCompleted(thread, r.TurnId))
	m.observeUserInputNotification("serverRequest/resolved", json.RawMessage(`{"threadId":"`+thread+`","requestId":"ordinary-input"}`))
	result.onResponseWritten(nil)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	again, err := m.AnswerUserInput(context.Background(), thread, id, answer)
	if err != nil || again.State != "answered" {
		t.Fatalf("receipt replay = %s, %v", again.State, err)
	}
	m.userInputs.mu.Lock()
	defer m.userInputs.mu.Unlock()
	if len(m.userInputs.entries[id].response) != 0 {
		t.Fatal("receipt lookup replayed an answer")
	}
}

func TestUserInputAdmissionClosesWithNormalShutdown(t *testing.T) {
	m, thread, id, out, _ := inputTestPending(t, false)
	if err := m.CloseMarketAdmission(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := m.AnswerUserInput(context.Background(), thread, id, inputTestAnswer()); !errors.Is(err, ErrUserInputConflict) {
		t.Fatalf("closed admission = %v", err)
	}
	select {
	case result := <-out:
		if result.respond {
			t.Fatal("closed admission sent an answer")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("question callback did not close")
	}
}
