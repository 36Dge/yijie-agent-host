package session

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"

	"github.com/36Dge/yijie-agent-host/internal/codex"
)

const (
	EventThreadStarted            = "thread.started"
	EventTurnStarted              = "turn.started"
	EventItemStarted              = "item.started"
	EventItemAgentMessageDelta    = "item.agent_message.delta"
	EventItemCompleted            = "item.completed"
	EventTurnCompleted            = "turn.completed"
	EventError                    = "error"
	EventWarning                  = "warning"
	maxPendingNotifications       = 256
	maxPendingNotificationsThread = 32

	RuntimeNotificationError                 = "error"
	RuntimeNotificationItemAgentMessageDelta = "item/agentMessage/delta"
	RuntimeNotificationItemCompleted         = "item/completed"
	RuntimeNotificationItemStarted           = "item/started"
	RuntimeNotificationThreadStarted         = "thread/started"
	RuntimeNotificationTurnCompleted         = "turn/completed"
	RuntimeNotificationTurnStarted           = "turn/started"
	RuntimeNotificationWarning               = "warning"
)

var runtimeNotifications = []string{
	RuntimeNotificationError,
	RuntimeNotificationItemAgentMessageDelta,
	RuntimeNotificationItemCompleted,
	RuntimeNotificationItemStarted,
	RuntimeNotificationThreadStarted,
	RuntimeNotificationTurnCompleted,
	RuntimeNotificationTurnStarted,
	RuntimeNotificationWarning,
}

func SupportedRuntimeNotifications() []string {
	return append([]string(nil), runtimeNotifications...)
}

var bearerPattern = regexp.MustCompile(`(?i)bearer\s+[A-Za-z0-9._~+/=-]+`)

type Runtime interface {
	StartThread(context.Context, string) (codex.ThreadInfo, error)
	ResumeThread(context.Context, string) (codex.ThreadInfo, error)
	StartTurn(context.Context, string, string, string) (codex.TurnInfo, error)
	InterruptTurn(context.Context, string, string) error
}

type StartSessionInput struct {
	TaskID string
	Cwd    string
	Trace  TraceContext
}

type StartTurnInput struct {
	AgentSessionID  string
	Input           string
	ReasoningEffort string
	Trace           TraceContext
}

type pendingNotification struct {
	method string
	params json.RawMessage
}

type Service struct {
	runtime Runtime
	store   *Store
	events  *EventHub
	logger  *slog.Logger

	pendingMu    sync.Mutex
	pending      map[string][]pendingNotification
	pendingCount int
}

func NewService(runtime Runtime, store *Store, events *EventHub, logger *slog.Logger) *Service {
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	return &Service{
		runtime: runtime,
		store:   store,
		events:  events,
		logger:  logger,
		pending: make(map[string][]pendingNotification),
	}
}

func (s *Service) StartSession(ctx context.Context, input StartSessionInput) (Record, error) {
	if err := requireUUID("task_id", input.TaskID); err != nil {
		return Record{}, err
	}
	cwd, err := canonicalDirectory(input.Cwd)
	if err != nil {
		return Record{}, err
	}
	sessionID, err := newUUID()
	if err != nil {
		return Record{}, fmt.Errorf("generate agent session id: %w", err)
	}
	record := Record{
		TaskID:         input.TaskID,
		AgentSessionID: sessionID,
		Cwd:            cwd,
		Trace:          input.Trace,
	}
	if err := s.store.Reserve(record); err != nil {
		return Record{}, err
	}
	thread, err := s.runtime.StartThread(ctx, cwd)
	if err != nil {
		_, _ = s.store.MarkFailed(sessionID, "thread_start_failed")
		return Record{}, fmt.Errorf("%w: %v", ErrRuntimeRequest, err)
	}
	if err := requireUUID("codex_thread_id", thread.ID); err != nil {
		_, _ = s.store.MarkFailed(sessionID, "thread_start_response_invalid")
		return Record{}, fmt.Errorf("%w: thread/start returned an invalid thread id", ErrRuntimeRequest)
	}
	if thread.Model != codex.MiniMaxModel || thread.ModelProvider != codex.MiniMaxProviderID {
		_, _ = s.store.MarkFailed(sessionID, "provider_identity_mismatch")
		return Record{}, fmt.Errorf("%w: thread/start returned an unexpected model provider identity", ErrRuntimeRequest)
	}
	record, err = s.store.BindThread(
		sessionID,
		thread.ID,
		thread.RuntimeSession,
		thread.Model,
		thread.ModelProvider,
	)
	if err != nil {
		return Record{}, err
	}
	s.flushPending(thread.ID)
	return record, nil
}

func (s *Service) ResumeSession(ctx context.Context, sessionID string, trace TraceContext) (Record, error) {
	if err := requireUUID("agent_session_id", sessionID); err != nil {
		return Record{}, err
	}
	record, err := s.store.Get(sessionID)
	if err != nil {
		return Record{}, err
	}
	if record.CodexThreadID == "" {
		return Record{}, ErrSessionNotUsable
	}
	thread, err := s.runtime.ResumeThread(ctx, record.CodexThreadID)
	if err != nil {
		_, _ = s.store.MarkFailed(sessionID, "thread_resume_failed")
		return Record{}, fmt.Errorf("%w: %v", ErrRuntimeRequest, err)
	}
	if !validUUID(thread.ID) || thread.ID != record.CodexThreadID {
		_, _ = s.store.MarkFailed(sessionID, "thread_resume_response_invalid")
		return Record{}, fmt.Errorf("%w: thread/resume returned an unexpected thread id", ErrRuntimeRequest)
	}
	if thread.Model != codex.MiniMaxModel || thread.ModelProvider != codex.MiniMaxProviderID {
		_, _ = s.store.MarkFailed(sessionID, "provider_identity_mismatch")
		return Record{}, fmt.Errorf("%w: thread/resume returned an unexpected model provider identity", ErrRuntimeRequest)
	}
	activeTurnID, lastTurnID, lastStatus := resumedTurnState(thread.Turns)
	record, err = s.store.Resume(
		sessionID,
		trace,
		thread.RuntimeSession,
		activeTurnID,
		lastTurnID,
		lastStatus,
	)
	if err != nil {
		return Record{}, err
	}
	s.flushPending(thread.ID)
	return record, nil
}

func (s *Service) GetSession(sessionID string) (Record, error) {
	if err := requireUUID("agent_session_id", sessionID); err != nil {
		return Record{}, err
	}
	return s.store.Get(sessionID)
}

func (s *Service) StartTurn(ctx context.Context, input StartTurnInput) (codex.TurnInfo, error) {
	if err := requireUUID("agent_session_id", input.AgentSessionID); err != nil {
		return codex.TurnInfo{}, err
	}
	if strings.TrimSpace(input.Input) == "" || len(input.Input) > 1<<20 {
		return codex.TurnInfo{}, fmt.Errorf("%w: turn input is empty or too large", ErrInvalidArgument)
	}
	if input.ReasoningEffort != "" && input.ReasoningEffort != "none" && input.ReasoningEffort != "high" {
		return codex.TurnInfo{}, fmt.Errorf("%w: reasoning effort must be none or high", ErrInvalidArgument)
	}
	record, err := s.store.PrepareTurn(input.AgentSessionID, input.Trace)
	if err != nil {
		return codex.TurnInfo{}, err
	}
	turn, err := s.runtime.StartTurn(ctx, record.CodexThreadID, input.Input, input.ReasoningEffort)
	if err != nil {
		_, _ = s.store.TurnStartFailed(input.AgentSessionID, "turn_start_failed")
		return codex.TurnInfo{}, fmt.Errorf("%w: %v", ErrRuntimeRequest, err)
	}
	if err := requireUUID("turn_id", turn.ID); err != nil {
		_, _ = s.store.TurnStartFailed(input.AgentSessionID, "turn_start_response_invalid")
		return codex.TurnInfo{}, fmt.Errorf("%w: turn/start returned an invalid turn id", ErrRuntimeRequest)
	}
	if _, err := s.store.BindTurn(input.AgentSessionID, turn.ID); err != nil {
		return codex.TurnInfo{}, err
	}
	return turn, nil
}

func (s *Service) InterruptTurn(ctx context.Context, sessionID, turnID string, trace TraceContext) error {
	if err := requireUUID("agent_session_id", sessionID); err != nil {
		return err
	}
	if err := requireUUID("turn_id", turnID); err != nil {
		return err
	}
	record, err := s.store.Get(sessionID)
	if err != nil {
		return err
	}
	if record.ActiveTurnID != turnID {
		return ErrTurnNotActive
	}
	if _, err := s.store.UpdateTrace(sessionID, trace); err != nil {
		return err
	}
	if err := s.runtime.InterruptTurn(ctx, record.CodexThreadID, turnID); err != nil {
		return fmt.Errorf("%w: %v", ErrRuntimeRequest, err)
	}
	return nil
}

func (s *Service) SubscribeEvents(
	sessionID, streamID string,
	after uint64,
) (string, []Event, <-chan Event, func(), error) {
	if err := requireUUID("agent_session_id", sessionID); err != nil {
		return "", nil, nil, nil, err
	}
	if _, err := s.store.Get(sessionID); err != nil {
		return "", nil, nil, nil, err
	}
	return s.events.Subscribe(sessionID, streamID, after)
}

func (s *Service) HandleNotification(method string, params json.RawMessage) {
	if !supportedNotification(method) {
		return
	}
	threadID, err := notificationThreadID(method, params)
	if err != nil || threadID == "" || requireUUID("codex_thread_id", threadID) != nil {
		s.logger.Warn("discarding malformed Codex notification", "method", method)
		return
	}
	if _, err := s.store.GetByThread(threadID); err != nil {
		if errors.Is(err, ErrNotFound) {
			s.queuePending(threadID, method, params)
			return
		}
		s.logger.Warn("failed to correlate Codex notification", "method", method)
		return
	}
	if err := s.processNotification(method, params); err != nil {
		s.logger.Warn("failed to map Codex notification", "method", method)
	}
}

func (s *Service) processNotification(method string, params json.RawMessage) error {
	switch method {
	case RuntimeNotificationThreadStarted:
		var notification struct {
			Thread struct {
				ID string `json:"id"`
			} `json:"thread"`
		}
		if err := json.Unmarshal(params, &notification); err != nil {
			return err
		}
		record, err := s.store.GetByThread(notification.Thread.ID)
		if err != nil {
			return err
		}
		return s.publish(record, Event{
			EventType: EventThreadStarted,
			Payload: EventPayload{
				Model:         record.Model,
				ModelProvider: record.ModelProvider,
			},
		})
	case RuntimeNotificationTurnStarted:
		var notification turnNotification
		if err := json.Unmarshal(params, &notification); err != nil {
			return err
		}
		if err := requireUUID("turn_id", notification.Turn.ID); err != nil {
			return err
		}
		status := normalizeTurnStatus(notification.Turn.Status)
		if status != "in_progress" {
			return errors.New("turn/started notification status is not in progress")
		}
		record, err := s.store.GetByThread(notification.ThreadID)
		if err != nil {
			return err
		}
		record, err = s.store.BindTurn(record.AgentSessionID, notification.Turn.ID)
		if err != nil {
			return err
		}
		return s.publish(record, Event{
			TurnID:    notification.Turn.ID,
			EventType: EventTurnStarted,
			Payload:   EventPayload{Status: status},
		})
	case RuntimeNotificationItemStarted, RuntimeNotificationItemCompleted:
		var notification itemNotification
		if err := json.Unmarshal(params, &notification); err != nil {
			return err
		}
		if err := requireUUID("turn_id", notification.TurnID); err != nil {
			return err
		}
		if notification.Item.ID == "" {
			return errors.New("item notification omitted item id")
		}
		if notification.Item.Type == "" {
			return errors.New("item notification omitted item type")
		}
		record, err := s.store.GetByThread(notification.ThreadID)
		if err != nil {
			return err
		}
		eventType := EventItemStarted
		text := ""
		if method == RuntimeNotificationItemCompleted {
			eventType = EventItemCompleted
			if notification.Item.Type == "agentMessage" {
				text = notification.Item.Text
			}
		}
		return s.publish(record, Event{
			TurnID:    notification.TurnID,
			ItemID:    notification.Item.ID,
			EventType: eventType,
			Payload: EventPayload{
				ItemType: notification.Item.Type,
				Text:     text,
			},
		})
	case RuntimeNotificationItemAgentMessageDelta:
		var notification struct {
			ThreadID string `json:"threadId"`
			TurnID   string `json:"turnId"`
			ItemID   string `json:"itemId"`
			Delta    string `json:"delta"`
		}
		if err := json.Unmarshal(params, &notification); err != nil {
			return err
		}
		if err := requireUUID("turn_id", notification.TurnID); err != nil {
			return err
		}
		if notification.ItemID == "" {
			return errors.New("agent message delta notification omitted item id")
		}
		record, err := s.store.GetByThread(notification.ThreadID)
		if err != nil {
			return err
		}
		return s.publish(record, Event{
			TurnID:    notification.TurnID,
			ItemID:    notification.ItemID,
			EventType: EventItemAgentMessageDelta,
			Payload:   EventPayload{Delta: stringPointer(notification.Delta)},
		})
	case RuntimeNotificationTurnCompleted:
		var notification turnNotification
		if err := json.Unmarshal(params, &notification); err != nil {
			return err
		}
		if err := requireUUID("turn_id", notification.Turn.ID); err != nil {
			return err
		}
		switch notification.Turn.Status {
		case "completed", "interrupted", "failed":
		default:
			return errors.New("turn/completed notification status is not terminal")
		}
		record, err := s.store.GetByThread(notification.ThreadID)
		if err != nil {
			return err
		}
		status := normalizeTurnStatus(notification.Turn.Status)
		record, err = s.store.CompleteTurn(record.AgentSessionID, notification.Turn.ID, status)
		if err != nil {
			return err
		}
		payload := EventPayload{Status: status}
		if notification.Turn.Error != nil {
			payload.Code = normalizeCodexErrorCode(notification.Turn.Error.CodexErrorInfo)
			payload.Message = stringPointer(sanitizeMessage(notification.Turn.Error.Message))
		}
		return s.publish(record, Event{
			TurnID:    notification.Turn.ID,
			EventType: EventTurnCompleted,
			Terminal:  true,
			Payload:   payload,
		})
	case RuntimeNotificationError:
		var notification struct {
			ThreadID  string    `json:"threadId"`
			TurnID    string    `json:"turnId"`
			Error     turnError `json:"error"`
			WillRetry bool      `json:"willRetry"`
		}
		if err := json.Unmarshal(params, &notification); err != nil {
			return err
		}
		if err := requireUUID("turn_id", notification.TurnID); err != nil {
			return err
		}
		record, err := s.store.GetByThread(notification.ThreadID)
		if err != nil {
			return err
		}
		willRetry := notification.WillRetry
		return s.publish(record, Event{
			TurnID:    notification.TurnID,
			EventType: EventError,
			Payload: EventPayload{
				Code:      normalizeCodexErrorCode(notification.Error.CodexErrorInfo),
				Message:   stringPointer(sanitizeMessage(notification.Error.Message)),
				WillRetry: &willRetry,
			},
		})
	case RuntimeNotificationWarning:
		var notification struct {
			ThreadID string `json:"threadId"`
			Message  string `json:"message"`
		}
		if err := json.Unmarshal(params, &notification); err != nil {
			return err
		}
		record, err := s.store.GetByThread(notification.ThreadID)
		if err != nil {
			return err
		}
		willRetry := false
		return s.publish(record, Event{
			EventType: EventWarning,
			Payload: EventPayload{
				Message:   stringPointer(sanitizeMessage(notification.Message)),
				WillRetry: &willRetry,
			},
		})
	default:
		return nil
	}
}

type turnNotification struct {
	ThreadID string `json:"threadId"`
	Turn     struct {
		ID     string     `json:"id"`
		Status string     `json:"status"`
		Error  *turnError `json:"error"`
	} `json:"turn"`
}

type turnError struct {
	Message        string          `json:"message"`
	CodexErrorInfo json.RawMessage `json:"codexErrorInfo"`
}

type itemNotification struct {
	ThreadID string `json:"threadId"`
	TurnID   string `json:"turnId"`
	Item     struct {
		ID   string `json:"id"`
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"item"`
}

func (s *Service) publish(record Record, event Event) error {
	event.TaskID = record.TaskID
	event.AgentSessionID = record.AgentSessionID
	event.CodexThreadID = record.CodexThreadID
	event.TraceID = record.Trace.TraceID
	event.RequestID = record.Trace.RequestID
	event.TenantID = record.Trace.TenantID
	event.UserID = record.Trace.UserID
	_, err := s.events.Publish(event)
	return err
}

func (s *Service) queuePending(threadID, method string, params json.RawMessage) {
	s.pendingMu.Lock()
	defer s.pendingMu.Unlock()
	if s.pendingCount >= maxPendingNotifications || len(s.pending[threadID]) >= maxPendingNotificationsThread {
		s.logger.Warn("dropping uncorrelated Codex notification", "method", method)
		return
	}
	s.pending[threadID] = append(s.pending[threadID], pendingNotification{
		method: method,
		params: append(json.RawMessage(nil), params...),
	})
	s.pendingCount++
}

func (s *Service) flushPending(threadID string) {
	s.pendingMu.Lock()
	pending := s.pending[threadID]
	delete(s.pending, threadID)
	s.pendingCount -= len(pending)
	s.pendingMu.Unlock()
	for _, notification := range pending {
		if err := s.processNotification(notification.method, notification.params); err != nil {
			s.logger.Warn("failed to map buffered Codex notification", "method", notification.method)
		}
	}
}

func supportedNotification(method string) bool {
	for _, supported := range runtimeNotifications {
		if method == supported {
			return true
		}
	}
	return false
}

func notificationThreadID(method string, params json.RawMessage) (string, error) {
	if method == RuntimeNotificationThreadStarted {
		var notification struct {
			Thread struct {
				ID string `json:"id"`
			} `json:"thread"`
		}
		if err := json.Unmarshal(params, &notification); err != nil {
			return "", err
		}
		return notification.Thread.ID, nil
	}
	var notification struct {
		ThreadID *string `json:"threadId"`
	}
	if err := json.Unmarshal(params, &notification); err != nil {
		return "", err
	}
	if notification.ThreadID == nil {
		return "", nil
	}
	return *notification.ThreadID, nil
}

func canonicalDirectory(path string) (string, error) {
	if path == "" || !filepath.IsAbs(path) {
		return "", fmt.Errorf("%w: cwd must be an absolute path", ErrInvalidArgument)
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", fmt.Errorf("%w: resolve cwd: %v", ErrInvalidArgument, err)
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return "", fmt.Errorf("%w: stat cwd: %v", ErrInvalidArgument, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("%w: cwd is not a directory", ErrInvalidArgument)
	}
	return filepath.Clean(resolved), nil
}

func resumedTurnState(turns []codex.TurnInfo) (activeTurnID, lastTurnID, lastStatus string) {
	for _, turn := range turns {
		lastTurnID = turn.ID
		lastStatus = normalizeTurnStatus(turn.Status)
	}
	if lastStatus == "in_progress" {
		activeTurnID = lastTurnID
	}
	return activeTurnID, lastTurnID, lastStatus
}

func normalizeTurnStatus(status string) string {
	switch status {
	case "completed", "interrupted", "failed", "inProgress", "in_progress":
		if status == "inProgress" {
			return "in_progress"
		}
		return status
	default:
		return "failed"
	}
}

func normalizeCodexErrorCode(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return text
	}
	var object map[string]json.RawMessage
	if json.Unmarshal(raw, &object) == nil {
		keys := make([]string, 0, len(object))
		for key := range object {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		if len(keys) > 0 {
			return keys[0]
		}
	}
	return "other"
}

func sanitizeMessage(message string) string {
	message = bearerPattern.ReplaceAllString(message, "Bearer [REDACTED]")
	message = strings.ReplaceAll(message, "\x00", "")
	if len(message) > 4096 {
		message = message[:4096]
	}
	return message
}

func stringPointer(value string) *string {
	return &value
}
