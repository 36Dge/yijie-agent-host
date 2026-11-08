package codex

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	native "github.com/36Dge/yijie-agent-host/internal/contracts/runtimeinteraction"
	wire "github.com/36Dge/yijie-agent-host/internal/contracts/userinput"
	"github.com/google/uuid"
	"github.com/pelletier/go-toml/v2"
)

const userInputMethod = "item/tool/requestUserInput"

var ErrUserInputNotFound = errors.New("user input request unavailable")
var ErrUserInputConflict = errors.New("user input request already resolved")
var ErrUserInputInvalid = errors.New("invalid user input answer")

// Transport adaptation only: the native Session owns the tool wait and cancellation.
// No answer is stored in the Host database, events or diagnostic logs.
type userInputCallbacks struct {
	mu      sync.Mutex
	entries map[string]*userInputCallback
}
type userInputCallback struct {
	generation, thread, requestKey string
	view                           wire.UserInputRequest
	response                       chan native.ToolRequestUserInputResponse
	cancelled, done                chan struct{}
	timer                          *time.Timer
	desired                        wire.InputState
	fingerprint                    [32]byte
	finished                       time.Time
}

func interactionToolsArguments() ([]string, error) {
	var p struct {
		Config map[string]any `json:"native_config"`
	}
	if json.Unmarshal(native.Profile, &p) != nil || p.Config == nil {
		return nil, errors.New("interaction profile unavailable")
	}
	b, err := toml.Marshal(p.Config)
	if err != nil {
		return nil, err
	}
	return nativeConfigArguments(b)
}
func (m *Manager) UserInputEnabled() bool    { return m.config.InteractionToolsEnabled }
func inputActive(state wire.InputState) bool { return state == "pending" || state == "submitting" }
func cloneInputView(v wire.UserInputRequest) wire.UserInputRequest {
	// Return detached public data so a caller cannot change pending questions.
	b, _ := json.Marshal(v)
	var result wire.UserInputRequest
	_ = json.Unmarshal(b, &result)
	return result
}
func (c *userInputCallbacks) pruneLocked(now time.Time) {
	for id, e := range c.entries {
		if !e.finished.IsZero() && now.Sub(e.finished) > 10*time.Minute {
			delete(c.entries, id)
		}
	}
	// Retain at most 128 terminal receipts in addition to the 64 active requests.
	for len(c.entries) >= 192 {
		var oldest *userInputCallback
		var key string
		for id, e := range c.entries {
			if !e.finished.IsZero() && (oldest == nil || e.finished.Before(oldest.finished)) {
				oldest = e
				key = id
			}
		}
		if oldest == nil {
			break
		}
		delete(c.entries, key)
	}
}
func (c *userInputCallbacks) finishLocked(e *userInputCallback, state wire.InputState) {
	if !inputActive(e.view.State) {
		return
	}
	if e.timer != nil {
		e.timer.Stop()
	}
	e.view.State = state
	e.view.Questions = []wire.ToolRequestUserInputQuestion{}
	e.view.DeadlineAt = nil
	e.finished = time.Now()
	select {
	case <-e.response:
	default:
	}
	close(e.done)
}
func validInputQuestions(p native.ToolRequestUserInputParams) bool {
	if p.ThreadId == "" || p.TurnId == "" || len(p.TurnId) > 160 || p.ItemId == "" || len(p.ItemId) > 160 || len(p.Questions) < 1 || len(p.Questions) > 3 {
		return false
	}
	ids := map[string]bool{}
	for _, q := range p.Questions {
		if strings.TrimSpace(q.Id) == "" || len(q.Id) > 128 || ids[q.Id] || strings.TrimSpace(q.Header) == "" || utf8.RuneCountInString(q.Header) > 12 || strings.TrimSpace(q.Question) == "" || utf8.RuneCountInString(q.Question) > 8192 {
			return false
		}
		ids[q.Id] = true
		if q.Options != nil {
			if len(*q.Options) > 32 {
				return false
			}
			labels := map[string]bool{}
			for _, o := range *q.Options {
				if strings.TrimSpace(o.Label) == "" || utf8.RuneCountInString(o.Label) > 128 || utf8.RuneCountInString(o.Description) > 1024 || labels[o.Label] {
					return false
				}
				labels[o.Label] = true
			}
		}
	}
	return p.AutoResolutionMs == nil || (*p.AutoResolutionMs >= 60000 && *p.AutoResolutionMs <= 240000)
}
func (m *Manager) handleUserInput(ctx context.Context, r serverRequest) serverRequestResult {
	unavailable := serverRequestResult{respond: true, rpcError: &RPCError{Code: -32602, Message: "User input request unavailable"}}
	if !m.config.InteractionToolsEnabled || !r.exactEnvelope || len(r.params) > 1<<20 {
		return unavailable
	}
	var p native.ToolRequestUserInputParams
	if json.Unmarshal(r.params, &p) != nil || !validInputQuestions(p) {
		return unavailable
	}
	c := &m.userInputs
	m.mu.Lock()
	if !m.status.Ready || m.shutdownRequested {
		m.mu.Unlock()
		return unavailable
	}
	generation := m.runtimeGeneration
	// Reuse the exact root/active-turn receipts already maintained by the native
	// permission adapter. Child ancestry never grants root-only question access.
	m.permissionCallbacks.mu.Lock()
	if !m.permissionCallbacks.nativeRoots[p.ThreadId] || m.permissionCallbacks.activeTurns[p.ThreadId] != p.TurnId {
		m.permissionCallbacks.mu.Unlock()
		m.mu.Unlock()
		return unavailable
	}
	c.mu.Lock()
	if c.entries == nil {
		c.entries = map[string]*userInputCallback{}
	}
	c.pruneLocked(time.Now())
	active := 0
	for _, old := range c.entries {
		if old.generation == generation && old.requestKey == r.idKey {
			c.mu.Unlock()
			m.permissionCallbacks.mu.Unlock()
			m.mu.Unlock()
			return serverRequestResult{}
		}
		if inputActive(old.view.State) {
			active++
			if old.thread == p.ThreadId {
				c.mu.Unlock()
				m.permissionCallbacks.mu.Unlock()
				m.mu.Unlock()
				return unavailable
			}
		}
	}
	if active >= 64 {
		c.mu.Unlock()
		m.permissionCallbacks.mu.Unlock()
		m.mu.Unlock()
		return unavailable
	}
	id := uuid.New()
	var questions []wire.ToolRequestUserInputQuestion
	b, err := json.Marshal(p.Questions)
	if err != nil || json.Unmarshal(b, &questions) != nil {
		c.mu.Unlock()
		m.permissionCallbacks.mu.Unlock()
		m.mu.Unlock()
		return unavailable
	}
	e := &userInputCallback{generation: generation, thread: p.ThreadId, requestKey: r.idKey,
		view:     wire.UserInputRequest{RequestId: id, TurnId: p.TurnId, ItemId: p.ItemId, State: "pending", Questions: questions},
		response: make(chan native.ToolRequestUserInputResponse, 1), cancelled: make(chan struct{}), done: make(chan struct{})}
	c.entries[id.String()] = e
	if p.AutoResolutionMs != nil {
		deadline := time.Now().Add(time.Duration(*p.AutoResolutionMs) * time.Millisecond)
		e.view.DeadlineAt = &deadline
		e.timer = time.AfterFunc(time.Until(deadline), func() { m.expireUserInput(e) })
	}
	c.mu.Unlock()
	m.permissionCallbacks.mu.Unlock()
	m.mu.Unlock()
	select {
	case response := <-e.response:
		return serverRequestResult{respond: true, value: response, onResponseWritten: func(err error) {
			c.mu.Lock()
			defer c.mu.Unlock()
			state := e.desired
			if err != nil {
				state = "unknown"
			}
			c.finishLocked(e, state)
		}}
	case <-ctx.Done():
		c.mu.Lock()
		state := wire.InputState("cancelled")
		if e.view.State == "submitting" {
			state = "unknown"
		}
		c.finishLocked(e, state)
		c.mu.Unlock()
		return serverRequestResult{}
	case <-e.cancelled:
		return serverRequestResult{}
	}
}
func (c *userInputCallbacks) submitLocked(e *userInputCallback, r native.ToolRequestUserInputResponse, state wire.InputState) {
	if e.timer != nil {
		e.timer.Stop()
	}
	e.view.State = "submitting"
	e.view.DeadlineAt = nil
	e.desired = state
	e.response <- r
}
func (m *Manager) UserInputSnapshot(thread string) wire.UserInputSnapshot {
	c := &m.userInputs
	c.mu.Lock()
	defer c.mu.Unlock()
	c.pruneLocked(time.Now())
	generation := uuid.MustParse(m.runtimeGeneration)
	s := wire.UserInputSnapshot{Enabled: m.config.InteractionToolsEnabled, Generation: &generation, Requests: []wire.UserInputRequest{}}
	for _, e := range c.entries {
		if e.thread == thread && inputActive(e.view.State) {
			s.Requests = append(s.Requests, cloneInputView(e.view))
		}
	}
	return s
}
func (m *Manager) GetUserInput(thread, id string) (wire.UserInputRequest, error) {
	c := &m.userInputs
	c.mu.Lock()
	defer c.mu.Unlock()
	c.pruneLocked(time.Now())
	e := c.entries[id]
	if e == nil || e.thread != thread {
		return wire.UserInputRequest{}, ErrUserInputNotFound
	}
	return cloneInputView(e.view), nil
}
func (c *userInputCallbacks) expireLocked(e *userInputCallback) {
	if e.view.State == "pending" && e.view.DeadlineAt != nil && !e.view.TimeoutPaused && !time.Now().Before(*e.view.DeadlineAt) {
		c.submitLocked(e, native.ToolRequestUserInputResponse{Answers: map[string]native.ToolRequestUserInputAnswer{}}, "timed_out")
	}
}

// Admission shares the native active-turn receipt lock. Notifications update
// activeTurns under this lock before closing the question, so an HTTP answer
// cannot be admitted in between. Lock order matches handleUserInput:
// Manager -> permissionCallbacks -> userInputs; release all before waiting/writing RPC.
func (m *Manager) lockUserInputAdmission() func() {
	m.mu.Lock()
	m.permissionCallbacks.mu.Lock()
	m.userInputs.mu.Lock()
	return func() {
		m.userInputs.mu.Unlock()
		m.permissionCallbacks.mu.Unlock()
		m.mu.Unlock()
	}
}
func (m *Manager) userInputTurnActiveLocked(e *userInputCallback) bool {
	return m.status.Ready && !m.shutdownRequested && e.generation == m.runtimeGeneration && m.permissionCallbacks.nativeRoots[e.thread] && m.permissionCallbacks.activeTurns[e.thread] == e.view.TurnId
}
func (c *userInputCallbacks) cancelPendingLocked(e *userInputCallback) {
	if e.view.State != "pending" {
		return
	}
	c.finishLocked(e, "cancelled")
	close(e.cancelled)
}
func (m *Manager) expireUserInput(e *userInputCallback) {
	unlock := m.lockUserInputAdmission()
	defer unlock()
	if e.view.State != "pending" {
		return
	}
	if !m.userInputTurnActiveLocked(e) {
		m.userInputs.cancelPendingLocked(e)
		return
	}
	m.userInputs.expireLocked(e)
}
func (m *Manager) TouchUserInput(thread, id string) (wire.UserInputRequest, error) {
	unlock := m.lockUserInputAdmission()
	defer unlock()
	c := &m.userInputs
	c.pruneLocked(time.Now())
	e := c.entries[id]
	if e == nil || e.thread != thread {
		return wire.UserInputRequest{}, ErrUserInputNotFound
	}
	if e.view.State == "pending" {
		if !m.userInputTurnActiveLocked(e) {
			c.cancelPendingLocked(e)
		} else {
			c.expireLocked(e)
			if e.view.State == "pending" {
				e.view.TimeoutPaused = true
				e.view.DeadlineAt = nil
				if e.timer != nil {
					e.timer.Stop()
				}
			}
		}
	}
	return cloneInputView(e.view), nil
}
func (m *Manager) admitUserInputAnswer(thread, id string, answer wire.UserInputAnswerRequest, fingerprint [32]byte) (*userInputCallback, error) {
	unlock := m.lockUserInputAdmission()
	defer unlock()
	c := &m.userInputs
	c.pruneLocked(time.Now())
	e := c.entries[id]
	if e == nil || e.thread != thread {
		return nil, ErrUserInputNotFound
	}
	// A replay of an already admitted operation reads its original receipt,
	// including after native turn completion; it never sends another response.
	if e.view.OperationId != nil {
		if *e.view.OperationId != answer.OperationId || e.fingerprint != fingerprint {
			return nil, ErrUserInputConflict
		}
		return e, nil
	}
	if e.view.State != "pending" {
		return nil, ErrUserInputConflict
	}
	if !m.userInputTurnActiveLocked(e) {
		c.cancelPendingLocked(e)
		return nil, ErrUserInputConflict
	}
	c.expireLocked(e)
	if e.view.State != "pending" {
		return nil, ErrUserInputConflict
	}
	byID := map[string]wire.ToolRequestUserInputQuestion{}
	for _, q := range e.view.Questions {
		byID[q.Id] = q
	}
	empty := true
	for key, value := range answer.Answers {
		q, ok := byID[key]
		if !ok || len(value.Answers) > 1 {
			return nil, ErrUserInputInvalid
		}
		for _, text := range value.Answers {
			if utf8.RuneCountInString(text) > 8192 {
				return nil, ErrUserInputInvalid
			}
			if text != "" {
				empty = false
			}
			if q.IsOther == nil || !*q.IsOther {
				allowed := false
				if q.Options != nil {
					for _, o := range *q.Options {
						allowed = allowed || o.Label == text
					}
				}
				if !allowed {
					return nil, ErrUserInputInvalid
				}
			}
		}
	}
	var response native.ToolRequestUserInputResponse
	raw, err := json.Marshal(map[string]any{"answers": answer.Answers})
	if err != nil || json.Unmarshal(raw, &response) != nil {
		return nil, ErrUserInputInvalid
	}
	e.view.OperationId = &answer.OperationId
	e.fingerprint = fingerprint
	state := wire.InputState("answered")
	if empty {
		state = "skipped"
	}
	c.submitLocked(e, response, state)
	return e, nil
}
func (m *Manager) AnswerUserInput(ctx context.Context, thread, id string, answer wire.UserInputAnswerRequest) (wire.UserInputRequest, error) {
	if answer.OperationId == uuid.Nil || wire.Validate("UserInputAnswerRequest", answer) != nil {
		return wire.UserInputRequest{}, ErrUserInputInvalid
	}
	data, _ := json.Marshal(answer.Answers)
	e, err := m.admitUserInputAnswer(thread, id, answer, sha256.Sum256(data))
	if err != nil {
		return wire.UserInputRequest{}, err
	}
	select {
	case <-e.done:
		return m.GetUserInput(thread, id)
	case <-ctx.Done():
		return m.GetUserInput(thread, id)
	}
}
func (m *Manager) closeUserInputs(thread, turn, key string, all bool) {
	c := &m.userInputs
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, e := range c.entries {
		if !inputActive(e.view.State) {
			continue
		}
		if !all && (e.thread != thread || (turn != "" && e.view.TurnId != turn) || (key != "" && e.requestKey != key)) {
			continue
		}
		// A native resolved notification can race the response write receipt. Let
		// that receipt settle an already submitted answer; never downgrade it.
		if e.view.State == "submitting" && !all {
			continue
		}
		state := wire.InputState("cancelled")
		if e.view.State == "submitting" {
			state = "unknown"
		}
		c.finishLocked(e, state)
		close(e.cancelled)
	}
}
func (m *Manager) observeUserInputNotification(method string, raw json.RawMessage) {
	if !m.config.InteractionToolsEnabled {
		return
	}
	switch method {
	case "turn/completed", "serverRequest/resolved", "thread/closed", "thread/deleted":
	default:
		return
	}
	var p struct {
		ThreadID  string          `json:"threadId"`
		RequestID json.RawMessage `json:"requestId"`
		Turn      struct {
			ID string `json:"id"`
		} `json:"turn"`
	}
	if json.Unmarshal(raw, &p) != nil || p.ThreadID == "" {
		return
	}
	if method == "serverRequest/resolved" {
		key, err := requestIDKey(p.RequestID)
		if err == nil {
			m.closeUserInputs(p.ThreadID, "", key, false)
		}
	} else if method == "turn/completed" {
		if p.Turn.ID != "" {
			m.closeUserInputs(p.ThreadID, p.Turn.ID, "", false)
		}
	} else {
		m.closeUserInputs(p.ThreadID, "", "", false)
	}
}

func validateInteractionToolsEffectiveConfig(config map[string]json.RawMessage) error {
	var features map[string]json.RawMessage
	if json.Unmarshal(config["features"], &features) != nil {
		return errors.New("native interaction configuration unavailable")
	}
	var defaultMode bool
	var budget struct {
		Enabled bool  `json:"enabled"`
		Reset   *bool `json:"reset_on_compact"`
	}
	if json.Unmarshal(features["default_mode_request_user_input"], &defaultMode) != nil || !defaultMode || json.Unmarshal(features["token_budget"], &budget) != nil || !budget.Enabled || budget.Reset == nil || *budget.Reset {
		return errors.New("native interaction configuration differs")
	}
	// Canonical app-server ToolsV2 exposes only web_search. The experimental
	// question gate is verified against the native config schema at generation
	// and through actual native question calls, not invented in config/read.
	return nil
}
