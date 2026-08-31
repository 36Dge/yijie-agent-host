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
	"time"

	"github.com/36Dge/yijie-agent-host/internal/artifact"
	"github.com/36Dge/yijie-agent-host/internal/codex"
	"github.com/36Dge/yijie-agent-host/internal/imagegen"
)

const (
	EventThreadStarted            = "thread.started"
	EventTurnStarted              = "turn.started"
	EventItemStarted              = "item.started"
	EventItemAgentMessageDelta    = "item.agent_message.delta"
	EventItemCommandOutputDelta   = "item.command_output.delta"
	EventItemToolProgress         = "item.tool.progress"
	EventItemCompleted            = "item.completed"
	EventItemReasoningTextDelta   = "item.reasoning_text.delta"
	EventItemReasoningFinalized   = "item.reasoning_text.finalized"
	EventItemArtifactStarted      = "item.artifact.started"
	EventItemArtifactProgress     = "item.artifact.progress"
	EventItemArtifactCompleted    = "item.artifact.completed"
	EventItemArtifactFailed       = "item.artifact.failed"
	EventTurnPlanUpdated          = "turn.plan.updated"
	EventTurnCompleted            = "turn.completed"
	EventApprovalRequested        = "approval.requested"
	EventApprovalResolved         = "approval.resolved"
	EventError                    = "error"
	EventWarning                  = "warning"
	maxPendingNotifications       = 256
	maxPendingNotificationsThread = 32

	RuntimeNotificationError                 = "error"
	RuntimeNotificationItemAgentMessageDelta = "item/agentMessage/delta"
	RuntimeNotificationCommandOutputDelta    = "item/commandExecution/outputDelta"
	RuntimeNotificationItemCompleted         = "item/completed"
	RuntimeNotificationMcpToolProgress       = "item/mcpToolCall/progress"
	RuntimeNotificationReasoningTextDelta    = "item/reasoning/textDelta"
	RuntimeNotificationItemStarted           = "item/started"
	RuntimeNotificationThreadStarted         = "thread/started"
	RuntimeNotificationTurnCompleted         = "turn/completed"
	RuntimeNotificationTurnPlanUpdated       = "turn/plan/updated"
	RuntimeNotificationTurnStarted           = "turn/started"
	RuntimeNotificationWarning               = "warning"
)

var runtimeNotifications = []string{
	RuntimeNotificationError,
	RuntimeNotificationItemAgentMessageDelta,
	RuntimeNotificationCommandOutputDelta,
	RuntimeNotificationItemCompleted,
	RuntimeNotificationMcpToolProgress,
	RuntimeNotificationReasoningTextDelta,
	RuntimeNotificationItemStarted,
	RuntimeNotificationThreadStarted,
	RuntimeNotificationTurnCompleted,
	RuntimeNotificationTurnPlanUpdated,
	RuntimeNotificationTurnStarted,
	RuntimeNotificationWarning,
}

func SupportedV2RuntimeNotifications() []string {
	return []string{RuntimeNotificationReasoningTextDelta}
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
	DeleteThread(context.Context, string) error
}

type CleanupResult struct {
	OperationID       string
	Outcome           string
	RuntimeThreadTree string
	HostMapping       string
	HostReplay        string
	ReasonCode        string
}

func (s *Service) CleanupSession(ctx context.Context, sessionID, operationID string) (CleanupResult, error) {
	if err := requireUUID("agent_session_id", sessionID); err != nil {
		return CleanupResult{}, err
	}
	if err := requireUUID("operation_id", operationID); err != nil {
		return CleanupResult{}, err
	}
	if receipt, err := s.store.CleanupReceipt(operationID, sessionID); err == nil {
		return CleanupResult{OperationID: operationID, Outcome: receipt.Outcome, RuntimeThreadTree: receipt.RuntimeTree, HostMapping: receipt.HostMapping, HostReplay: receipt.HostReplay}, nil
	} else if errors.Is(err, ErrCleanupConflict) {
		return CleanupResult{}, ErrCleanupConflict
	}
	operation, err := s.store.BeginCleanup(operationID, sessionID)
	if errors.Is(err, ErrTurnActive) {
		return incompleteCleanup(operationID, "active_turn", "not_attempted", "not_attempted", "not_attempted"), nil
	}
	if err != nil {
		return CleanupResult{}, err
	}
	if operation.State == CleanupStateRuntimeDeletePending {
		deleteErr := s.runtime.DeleteThread(ctx, operation.ThreadID)
		if deleteErr != nil && !codex.IsThreadNotFound(deleteErr, operation.ThreadID) {
			return incompleteCleanup(operationID, "runtime_delete_unconfirmed", "incomplete", "not_attempted", "not_attempted"), nil
		}
		operation, err = s.store.MarkCleanupRuntimeDeleted(operationID, sessionID)
		if err != nil {
			return incompleteCleanup(operationID, "operation_state_unavailable", "incomplete", "not_attempted", "not_attempted"), nil
		}
	}
	if operation.State != CleanupStateRuntimeDeleteConfirmed {
		return incompleteCleanup(operationID, "operation_state_unavailable", "incomplete", "not_attempted", "not_attempted"), nil
	}
	if err := s.store.DeleteSessionWithReceipt(operationID, sessionID); err != nil {
		return incompleteCleanup(operationID, "host_mapping_cleanup_failed", "complete", "incomplete", "not_attempted"), nil
	}
	s.clearApprovalSession(sessionID)
	if s.events != nil {
		s.events.DeleteSession(sessionID)
	}
	if s.eventsV2 != nil {
		s.eventsV2.DeleteSession(sessionID)
	}
	if s.eventsV3 != nil {
		s.eventsV3.DeleteSession(sessionID)
	}
	if s.eventsV4 != nil {
		s.eventsV4.DeleteSession(sessionID)
	}
	if s.eventsV5 != nil {
		s.eventsV5.DeleteSession(sessionID)
	}
	if s.eventsV6 != nil {
		s.eventsV6.DeleteSession(sessionID)
	}
	if s.artifacts != nil {
		s.artifacts.DeleteSession(sessionID)
	}
	s.clearImageSession(sessionID)
	s.abortSyntheticTerminalBarrier(sessionID)
	s.reasoningMu.Lock()
	for key := range s.reasoning {
		if key.sessionID == sessionID {
			delete(s.reasoning, key)
		}
	}
	s.reasoningMu.Unlock()
	s.clearV4Session(sessionID)
	s.clearV5Session(sessionID)
	return CleanupResult{OperationID: operationID, Outcome: "complete", RuntimeThreadTree: "complete", HostMapping: "complete", HostReplay: "complete"}, nil
}

func incompleteCleanup(operationID, reason, runtimeTree, mapping, replay string) CleanupResult {
	return CleanupResult{OperationID: operationID, Outcome: "incomplete", RuntimeThreadTree: runtimeTree, HostMapping: mapping, HostReplay: replay, ReasonCode: reason}
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

type syntheticTerminal struct {
	turnID  string
	status  string
	payload EventPayload
}

type syntheticTerminalBarrier struct {
	turnID             string
	terminal           *syntheticTerminal
	artifactsPublished bool
}

type Service struct {
	runtime              Runtime
	store                *Store
	events               *EventHub
	eventsV2             *EventHub
	eventsV3             *EventHub
	eventsV4             *EventHub
	eventsV5             *EventHub
	eventsV6             *EventHub
	artifacts            *artifact.Store
	syntheticArtifacts   bool
	rawReasoning         bool
	fixedReasoningEffort string
	logger               *slog.Logger

	pendingMu       sync.Mutex
	notificationMu  sync.Mutex
	pending         map[string][]pendingNotification
	pendingCount    int
	syntheticMu     sync.Mutex
	syntheticTurns  map[string]*syntheticTerminalBarrier
	terminalMu      sync.Mutex
	reasoningMu     sync.Mutex
	reasoning       map[reasoningTurnKey]*reasoningTurnState
	v4Mu            sync.Mutex
	v4Turns         map[v4TurnKey]*v4TurnState
	v5ItemsMu       sync.Mutex
	v5Items         map[v5ItemKey]*v5ItemState
	titleMu         sync.Mutex
	titleGenerator  TitleGenerator
	titleOperations map[titleOperationKey]*titleOperation
	imageGenerator  imagegen.Generator
	imageMu         sync.Mutex
	imageTurns      map[string]*imageTurn
	approvals       *approvalAuthority
}

type ServiceOption func(*Service)

func WithV2Events(events *EventHub) ServiceOption {
	return func(service *Service) {
		service.eventsV2 = events
		// The v2 route is itself the legacy explicit raw-reasoning authority.
		// V3/V4 hubs never imply this authorization.
		service.rawReasoning = events != nil
	}
}

func WithV3Artifacts(events *EventHub, artifacts *artifact.Store, synthetic bool) ServiceOption {
	return func(service *Service) {
		service.eventsV3 = events
		service.artifacts = artifacts
		service.syntheticArtifacts = synthetic
	}
}

func WithV4Events(events *EventHub) ServiceOption {
	return func(service *Service) { service.eventsV4 = events }
}

func WithV5Events(events *EventHub) ServiceOption {
	return func(service *Service) { service.eventsV5 = events }
}

func WithV6Approvals(events *EventHub, acknowledgementTimeout time.Duration) ServiceOption {
	return func(service *Service) {
		service.eventsV6 = events
		service.approvals = newApprovalAuthority(acknowledgementTimeout)
	}
}

func WithRawReasoningProjection(enabled bool) ServiceOption {
	return func(service *Service) { service.rawReasoning = enabled }
}

func WithFixedReasoningEffort(effort string) ServiceOption {
	return func(service *Service) {
		if effort == "high" {
			service.fixedReasoningEffort = effort
		}
	}
}

func WithImageGenerator(generator imagegen.Generator) ServiceOption {
	return func(service *Service) { service.imageGenerator = generator }
}

func NewService(runtime Runtime, store *Store, events *EventHub, logger *slog.Logger, options ...ServiceOption) *Service {
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	service := &Service{
		runtime:         runtime,
		store:           store,
		events:          events,
		logger:          logger,
		pending:         make(map[string][]pendingNotification),
		syntheticTurns:  make(map[string]*syntheticTerminalBarrier),
		reasoning:       make(map[reasoningTurnKey]*reasoningTurnState),
		v4Turns:         make(map[v4TurnKey]*v4TurnState),
		v5Items:         make(map[v5ItemKey]*v5ItemState),
		titleOperations: make(map[titleOperationKey]*titleOperation),
		imageTurns:      make(map[string]*imageTurn),
	}
	for _, option := range options {
		if option != nil {
			option(service)
		}
	}
	return service
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
	if s.eventsV4 != nil && (!validV4ManagedIdentity(thread.Model) || !validV4ManagedIdentity(thread.ModelProvider)) {
		_, _ = s.store.MarkFailed(sessionID, "provider_identity_limit_exceeded")
		return Record{}, fmt.Errorf("%w: managed provider identity", ErrEventLimitExceeded)
	}
	if thread.Model != codex.MiniMaxModel || thread.ModelProvider != codex.MiniMaxProviderID {
		_, _ = s.store.MarkFailed(sessionID, "provider_identity_mismatch")
		return Record{}, fmt.Errorf("%w: thread/start returned an unexpected model provider identity", ErrRuntimeRequest)
	}
	s.notificationMu.Lock()
	record, err = s.store.BindThread(
		sessionID,
		thread.ID,
		thread.RuntimeSession,
		thread.Model,
		thread.ModelProvider,
	)
	if err != nil {
		s.notificationMu.Unlock()
		return Record{}, err
	}
	s.flushPending(thread.ID)
	s.notificationMu.Unlock()
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
	if record.State == StateCleaning || record.CleanupOperationID != "" {
		return Record{}, ErrSessionNotUsable
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
	if s.eventsV4 != nil && (!validV4ManagedIdentity(thread.Model) || !validV4ManagedIdentity(thread.ModelProvider)) {
		_, _ = s.store.MarkFailed(sessionID, "provider_identity_limit_exceeded")
		return Record{}, fmt.Errorf("%w: managed provider identity", ErrEventLimitExceeded)
	}
	if thread.Model != codex.MiniMaxModel || thread.ModelProvider != codex.MiniMaxProviderID {
		_, _ = s.store.MarkFailed(sessionID, "provider_identity_mismatch")
		return Record{}, fmt.Errorf("%w: thread/resume returned an unexpected model provider identity", ErrRuntimeRequest)
	}
	activeTurnID, lastTurnID, lastStatus := resumedTurnState(thread.Turns)
	s.notificationMu.Lock()
	record, err = s.store.Resume(
		sessionID,
		trace,
		thread.RuntimeSession,
		activeTurnID,
		lastTurnID,
		lastStatus,
	)
	if err != nil {
		s.notificationMu.Unlock()
		return Record{}, err
	}
	s.flushPending(thread.ID)
	s.notificationMu.Unlock()
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
	if err := s.beginImageTurn(record, nil); err != nil {
		_, _ = s.store.TurnStartFailed(input.AgentSessionID, "image_turn_state_failed")
		return codex.TurnInfo{}, err
	}
	if err := s.beginSyntheticTerminalBarrier(input.AgentSessionID); err != nil {
		s.clearImageTurn(record.CodexThreadID)
		_, _ = s.store.TurnStartFailed(input.AgentSessionID, "synthetic_terminal_barrier_failed")
		return codex.TurnInfo{}, err
	}
	effectiveEffort := input.ReasoningEffort
	if s.fixedReasoningEffort != "" {
		effectiveEffort = s.fixedReasoningEffort
	}
	turn, err := s.runtime.StartTurn(ctx, record.CodexThreadID, input.Input, effectiveEffort)
	if err != nil {
		s.clearImageTurn(record.CodexThreadID)
		s.abortSyntheticTerminalBarrier(input.AgentSessionID)
		_, _ = s.store.TurnStartFailed(input.AgentSessionID, "turn_start_failed")
		return codex.TurnInfo{}, fmt.Errorf("%w: %v", ErrRuntimeRequest, err)
	}
	if err := requireUUID("turn_id", turn.ID); err != nil {
		s.clearImageTurn(record.CodexThreadID)
		s.abortSyntheticTerminalBarrier(input.AgentSessionID)
		_, _ = s.store.TurnStartFailed(input.AgentSessionID, "turn_start_response_invalid")
		return codex.TurnInfo{}, fmt.Errorf("%w: turn/start returned an invalid turn id", ErrRuntimeRequest)
	}
	if _, err := s.store.BindTurn(input.AgentSessionID, turn.ID); err != nil {
		s.clearImageTurn(record.CodexThreadID)
		s.abortSyntheticTerminalBarrier(input.AgentSessionID)
		return codex.TurnInfo{}, err
	}
	if err := s.bindImageTurn(record.CodexThreadID, turn.ID); err != nil {
		s.clearImageTurn(record.CodexThreadID)
		return codex.TurnInfo{}, err
	}
	if s.syntheticArtifacts {
		if err := s.finishSyntheticTurn(input.AgentSessionID, turn.ID); err != nil {
			_, _ = s.store.TurnStartFailed(input.AgentSessionID, "synthetic_artifact_failed")
			_, _ = s.store.MarkFailed(input.AgentSessionID, "synthetic_artifact_failed")
			return codex.TurnInfo{}, fmt.Errorf("%w: synthetic artifact publication failed", ErrRuntimeRequest)
		}
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
	s.cancelImageTurn(record.CodexThreadID, turnID)
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

func (s *Service) SubscribeEventsV2(
	sessionID, streamID string,
	after uint64,
) (string, []Event, <-chan Event, func(), error) {
	if s.eventsV2 == nil {
		return "", nil, nil, nil, ErrSessionNotUsable
	}
	if err := requireUUID("agent_session_id", sessionID); err != nil {
		return "", nil, nil, nil, err
	}
	if _, err := s.store.Get(sessionID); err != nil {
		return "", nil, nil, nil, err
	}
	return s.eventsV2.Subscribe(sessionID, streamID, after)
}

func (s *Service) SubscribeEventsV3(
	sessionID, streamID string,
	after uint64,
) (string, []Event, <-chan Event, func(), error) {
	if s.eventsV3 == nil {
		return "", nil, nil, nil, ErrSessionNotUsable
	}
	if err := requireUUID("agent_session_id", sessionID); err != nil {
		return "", nil, nil, nil, err
	}
	if _, err := s.store.Get(sessionID); err != nil {
		return "", nil, nil, nil, err
	}
	return s.eventsV3.Subscribe(sessionID, streamID, after)
}

func (s *Service) SubscribeEventsV4(
	sessionID, streamID string,
	after uint64,
) (string, []Event, <-chan Event, func(), error) {
	if s.eventsV4 == nil {
		return "", nil, nil, nil, ErrSessionNotUsable
	}
	if err := requireUUID("agent_session_id", sessionID); err != nil {
		return "", nil, nil, nil, err
	}
	if _, err := s.store.Get(sessionID); err != nil {
		return "", nil, nil, nil, err
	}
	return s.eventsV4.Subscribe(sessionID, streamID, after)
}

func (s *Service) SubscribeEventsV5(
	sessionID, streamID string,
	after uint64,
) (string, []Event, <-chan Event, func(), error) {
	if s.eventsV5 == nil {
		return "", nil, nil, nil, ErrSessionNotUsable
	}
	if err := requireUUID("agent_session_id", sessionID); err != nil {
		return "", nil, nil, nil, err
	}
	if _, err := s.store.Get(sessionID); err != nil {
		return "", nil, nil, nil, err
	}
	return s.eventsV5.Subscribe(sessionID, streamID, after)
}

func (s *Service) SubscribeEventsV6(
	sessionID, streamID string,
	after uint64,
) (string, []Event, <-chan Event, func(), error) {
	if s.eventsV6 == nil || s.approvals == nil {
		return "", nil, nil, nil, ErrSessionNotUsable
	}
	if err := requireUUID("agent_session_id", sessionID); err != nil {
		return "", nil, nil, nil, err
	}
	if _, err := s.store.Get(sessionID); err != nil {
		return "", nil, nil, nil, err
	}
	return s.eventsV6.Subscribe(sessionID, streamID, after)
}

func (s *Service) HandleNotification(method string, params json.RawMessage) {
	if !supportedNotification(method) {
		return
	}
	// Command deltas and MCP progress are v5-only Runtime notifications. Keep
	// them completely outside the legacy correlation/pending path when the v5
	// consumer is disabled so a producer cannot consume shared v1-v4 capacity.
	if (method == RuntimeNotificationCommandOutputDelta || method == RuntimeNotificationMcpToolProgress) &&
		s.eventsV5 == nil && s.eventsV6 == nil {
		return
	}
	if method == RuntimeNotificationTurnPlanUpdated && s.eventsV4 == nil {
		return
	}
	if method == RuntimeNotificationReasoningTextDelta && !s.rawReasoning {
		return
	}
	s.notificationMu.Lock()
	defer s.notificationMu.Unlock()
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
		if method == RuntimeNotificationCommandOutputDelta || method == RuntimeNotificationMcpToolProgress {
			s.logger.Warn("failed to map Codex v5 notification", "method", method)
			return
		}
		s.rejectMalformedV4Notification(method, params)
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
		if s.eventsV4 != nil && notification.Turn.Status != "inProgress" {
			return errors.New("turn/started notification has invalid Runtime status")
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
		if err := s.bindImageTurn(notification.ThreadID, notification.Turn.ID); err != nil {
			return err
		}
		return s.publish(record, Event{
			TurnID:    notification.Turn.ID,
			EventType: EventTurnStarted,
			Payload:   EventPayload{Status: status},
		})
	case RuntimeNotificationTurnPlanUpdated:
		var notification planNotification
		if err := json.Unmarshal(params, &notification); err != nil {
			return err
		}
		if err := requireUUID("turn_id", notification.TurnID); err != nil {
			return err
		}
		if notification.Plan == nil {
			return errors.New("turn/plan/updated notification omitted plan array")
		}
		record, err := s.store.GetByThread(notification.ThreadID)
		if err != nil {
			return err
		}
		steps := make([]PlanStep, 0, len(*notification.Plan))
		for _, step := range *notification.Plan {
			if step.Step == nil {
				return errors.New("turn/plan/updated notification omitted step text")
			}
			var status string
			switch step.Status {
			case "pending", "completed":
				status = step.Status
			case "inProgress":
				status = "in_progress"
			default:
				return errors.New("turn/plan/updated notification has invalid step status")
			}
			steps = append(steps, PlanStep{Step: *step.Step, Status: status})
		}
		return s.publishV4(record, Event{
			TurnID:    notification.TurnID,
			EventType: EventTurnPlanUpdated,
			Payload: EventPayload{
				Explanation: nullableString(notification.Explanation),
				Plan:        &steps,
			},
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
		if s.eventsV4 != nil && notification.Item.Type == "agentMessage" && notification.Item.Text == nil {
			return errors.New("agent message item notification omitted text")
		}
		record, err := s.store.GetByThread(notification.ThreadID)
		if err != nil {
			return err
		}
		if method == RuntimeNotificationItemCompleted {
			s.resolveApprovalForItem(record, notification.TurnID, notification.Item.ID)
		}
		eventType := EventItemStarted
		var text *string
		if method == RuntimeNotificationItemCompleted {
			eventType = EventItemCompleted
			if notification.Item.Type == "agentMessage" {
				if notification.Item.Text == nil {
					text = stringPointer("")
				} else {
					text = copiedStringPointer(notification.Item.Text)
				}
			}
			if notification.Item.Type == "reasoning" {
				var contents []string
				if len(notification.Item.Content) == 0 || json.Unmarshal(notification.Item.Content, &contents) != nil {
					return errors.New("reasoning item notification has invalid content")
				}
				if err := s.finalizeReasoning(record, notification.TurnID, notification.Item.ID, contents); err != nil {
					return err
				}
			}
		}
		var phase **string
		var v4Text *string
		if notification.Item.Type == "agentMessage" {
			if s.eventsV4 != nil && method == RuntimeNotificationItemStarted {
				v4Text = copiedStringPointer(notification.Item.Text)
			}
			phase = nullableString(notification.Item.Phase)
		}
		event := Event{
			TurnID:    notification.TurnID,
			ItemID:    notification.Item.ID,
			EventType: eventType,
			Payload: EventPayload{
				ItemType: notification.Item.Type,
				Text:     text,
				V4Text:   v4Text,
				Phase:    phase,
			},
		}
		if err := s.publish(record, event); err != nil {
			return err
		}
		if notification.Item.Type == "commandExecution" || notification.Item.Type == "mcpToolCall" {
			if err := s.publishV5Lifecycle(record, method, params, notification); err != nil {
				s.logger.Warn("failed to map Codex v5 lifecycle", "method", method)
			}
		}
		return nil
	case RuntimeNotificationCommandOutputDelta:
		var notification commandOutputDeltaNotification
		if err := json.Unmarshal(params, &notification); err != nil {
			return err
		}
		if err := requireUUID("turn_id", notification.TurnID); err != nil {
			return err
		}
		if notification.ItemID == "" || notification.Delta == nil {
			return errors.New("command output delta notification omitted required content")
		}
		record, err := s.store.GetByThread(notification.ThreadID)
		if err != nil {
			return err
		}
		if err := s.publishV5CommandDelta(record, notification); err != nil {
			s.logger.Warn("failed to publish Codex v5 Command delta", "method", method)
		}
		return nil
	case RuntimeNotificationMcpToolProgress:
		var notification toolProgressNotification
		if err := json.Unmarshal(params, &notification); err != nil {
			return err
		}
		if err := requireUUID("turn_id", notification.TurnID); err != nil {
			return err
		}
		if notification.ItemID == "" || notification.Message == nil {
			return errors.New("MCP Tool progress notification omitted required content")
		}
		record, err := s.store.GetByThread(notification.ThreadID)
		if err != nil {
			return err
		}
		if err := s.publishV5ToolProgress(record, notification); err != nil {
			s.logger.Warn("failed to publish Codex v5 Tool progress", "method", method)
		}
		return nil
	case RuntimeNotificationItemAgentMessageDelta:
		var notification struct {
			ThreadID string  `json:"threadId"`
			TurnID   string  `json:"turnId"`
			ItemID   string  `json:"itemId"`
			Delta    *string `json:"delta"`
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
		if s.eventsV4 != nil && notification.Delta == nil {
			return errors.New("agent message delta notification omitted delta")
		}
		record, err := s.store.GetByThread(notification.ThreadID)
		if err != nil {
			return err
		}
		delta := ""
		if notification.Delta != nil {
			delta = *notification.Delta
		}
		return s.publish(record, Event{
			TurnID:    notification.TurnID,
			ItemID:    notification.ItemID,
			EventType: EventItemAgentMessageDelta,
			Payload:   EventPayload{Delta: stringPointer(delta)},
		})
	case RuntimeNotificationReasoningTextDelta:
		var notification struct {
			ThreadID     string  `json:"threadId"`
			TurnID       string  `json:"turnId"`
			ItemID       string  `json:"itemId"`
			Delta        *string `json:"delta"`
			ContentIndex *int    `json:"contentIndex"`
		}
		if err := json.Unmarshal(params, &notification); err != nil {
			return err
		}
		if err := requireUUID("turn_id", notification.TurnID); err != nil {
			return err
		}
		if notification.ItemID == "" {
			return errors.New("reasoning delta notification omitted item id")
		}
		if s.eventsV4 != nil && (notification.Delta == nil || notification.ContentIndex == nil) {
			return errors.New("reasoning delta notification omitted required content")
		}
		record, err := s.store.GetByThread(notification.ThreadID)
		if err != nil {
			return err
		}
		delta := ""
		if notification.Delta != nil {
			delta = *notification.Delta
		}
		contentIndex := 0
		if notification.ContentIndex != nil {
			contentIndex = *notification.ContentIndex
		}
		return s.appendReasoningDelta(record, notification.TurnID, notification.ItemID, contentIndex, delta)
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
		s.resolveApprovalForTurn(record, notification.Turn.ID)
		if s.v4SanitizedTerminalPublished(record.AgentSessionID, notification.Turn.ID) {
			s.clearImageTurn(notification.ThreadID)
			s.abortSyntheticTerminalBarrier(record.AgentSessionID)
			s.discardReasoningTurn(record.AgentSessionID, notification.Turn.ID)
			return nil
		}
		s.clearImageTurn(notification.ThreadID)
		status := normalizeTurnStatus(notification.Turn.Status)
		payload := EventPayload{Status: status}
		if notification.Turn.Error != nil {
			if s.eventsV4 != nil && notification.Turn.Error.Message == nil {
				return errors.New("turn/completed notification error omitted message")
			}
			payload.Code = normalizeCodexErrorCode(notification.Turn.Error.CodexErrorInfo)
			message := ""
			if notification.Turn.Error.Message != nil {
				message = *notification.Turn.Error.Message
			}
			payload.Message, payload.V4Message = sanitizedMessagePointers(message)
		}
		terminal := syntheticTerminal{turnID: notification.Turn.ID, status: status, payload: payload}
		if deferred, err := s.deferSyntheticTerminal(record.AgentSessionID, terminal); deferred || err != nil {
			return err
		}
		return s.completeTerminal(record, terminal)
	case RuntimeNotificationError:
		var notification struct {
			ThreadID  string     `json:"threadId"`
			TurnID    string     `json:"turnId"`
			Error     *turnError `json:"error"`
			WillRetry *bool      `json:"willRetry"`
		}
		if err := json.Unmarshal(params, &notification); err != nil {
			return err
		}
		if err := requireUUID("turn_id", notification.TurnID); err != nil {
			return err
		}
		if s.eventsV4 != nil && (notification.Error == nil || notification.Error.Message == nil || notification.WillRetry == nil) {
			return errors.New("error notification omitted required content")
		}
		record, err := s.store.GetByThread(notification.ThreadID)
		if err != nil {
			return err
		}
		willRetry := false
		messageText := ""
		var codexErrorInfo json.RawMessage
		if notification.WillRetry != nil {
			willRetry = *notification.WillRetry
		}
		if notification.Error != nil {
			codexErrorInfo = notification.Error.CodexErrorInfo
			if notification.Error.Message != nil {
				messageText = *notification.Error.Message
			}
		}
		message, v4Message := sanitizedMessagePointers(messageText)
		return s.publish(record, Event{
			TurnID:    notification.TurnID,
			EventType: EventError,
			Payload: EventPayload{
				Code:      normalizeCodexErrorCode(codexErrorInfo),
				Message:   message,
				V4Message: v4Message,
				WillRetry: &willRetry,
			},
		})
	case RuntimeNotificationWarning:
		var notification struct {
			ThreadID string  `json:"threadId"`
			Message  *string `json:"message"`
		}
		if err := json.Unmarshal(params, &notification); err != nil {
			return err
		}
		if s.eventsV4 != nil && notification.Message == nil {
			return errors.New("warning notification omitted message")
		}
		record, err := s.store.GetByThread(notification.ThreadID)
		if err != nil {
			return err
		}
		willRetry := false
		messageText := ""
		if notification.Message != nil {
			messageText = *notification.Message
		}
		message, v4Message := sanitizedMessagePointers(messageText)
		return s.publish(record, Event{
			EventType: EventWarning,
			Payload: EventPayload{
				Message:   message,
				V4Message: v4Message,
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

func (s *Service) beginSyntheticTerminalBarrier(sessionID string) error {
	if !s.syntheticArtifacts {
		return nil
	}
	s.syntheticMu.Lock()
	defer s.syntheticMu.Unlock()
	if s.syntheticTurns[sessionID] != nil {
		return errors.New("synthetic terminal barrier already exists")
	}
	s.syntheticTurns[sessionID] = &syntheticTerminalBarrier{}
	return nil
}

func (s *Service) abortSyntheticTerminalBarrier(sessionID string) {
	s.syntheticMu.Lock()
	delete(s.syntheticTurns, sessionID)
	s.syntheticMu.Unlock()
}

func (s *Service) deferSyntheticTerminal(sessionID string, terminal syntheticTerminal) (bool, error) {
	if !s.syntheticArtifacts {
		return false, nil
	}
	s.syntheticMu.Lock()
	defer s.syntheticMu.Unlock()
	barrier := s.syntheticTurns[sessionID]
	if barrier == nil || barrier.artifactsPublished {
		return false, nil
	}
	if barrier.turnID != "" && barrier.turnID != terminal.turnID {
		return true, errors.New("synthetic terminal turn identity conflicts with start response")
	}
	if barrier.terminal != nil {
		if syntheticTerminalsEqual(*barrier.terminal, terminal) {
			return true, nil
		}
		return true, errors.New("synthetic terminal notification conflicts with buffered terminal")
	}
	copy := terminal
	barrier.terminal = &copy
	return true, nil
}

func (s *Service) finishSyntheticTurn(sessionID, turnID string) error {
	s.syntheticMu.Lock()
	barrier := s.syntheticTurns[sessionID]
	if barrier == nil {
		s.syntheticMu.Unlock()
		return errors.New("synthetic terminal barrier is unavailable")
	}
	barrier.turnID = turnID
	if barrier.terminal != nil && barrier.terminal.turnID != turnID {
		delete(s.syntheticTurns, sessionID)
		s.syntheticMu.Unlock()
		return errors.New("synthetic terminal turn identity conflicts with start response")
	}
	s.syntheticMu.Unlock()

	if err := s.publishSyntheticArtifacts(sessionID, turnID); err != nil {
		s.abortSyntheticTerminalBarrier(sessionID)
		return err
	}

	s.syntheticMu.Lock()
	barrier = s.syntheticTurns[sessionID]
	if barrier == nil || barrier.turnID != turnID {
		s.syntheticMu.Unlock()
		return errors.New("synthetic terminal barrier changed during artifact publication")
	}
	barrier.artifactsPublished = true
	terminal := barrier.terminal
	s.syntheticMu.Unlock()

	if terminal != nil {
		record, err := s.store.Get(sessionID)
		if err != nil {
			s.abortSyntheticTerminalBarrier(sessionID)
			return err
		}
		if err := s.completeTerminal(record, *terminal); err != nil {
			s.abortSyntheticTerminalBarrier(sessionID)
			return err
		}
	}

	s.syntheticMu.Lock()
	if s.syntheticTurns[sessionID] == barrier {
		delete(s.syntheticTurns, sessionID)
	}
	s.syntheticMu.Unlock()
	return nil
}

func (s *Service) completeTerminal(record Record, terminal syntheticTerminal) error {
	if s.syntheticArtifacts {
		s.terminalMu.Lock()
		defer s.terminalMu.Unlock()
		current, err := s.store.Get(record.AgentSessionID)
		if err != nil {
			return err
		}
		record = current
		if record.ActiveTurnID == "" && record.LastTurnID == terminal.turnID && record.LastTurnStatus != "" {
			if record.LastTurnStatus == terminal.status {
				return nil
			}
			return errors.New("terminal notification conflicts with persisted turn status")
		}
	}
	completed, err := s.store.CompleteTurn(record.AgentSessionID, terminal.turnID, terminal.status)
	if err != nil {
		return err
	}
	s.finalizeInterruptedReasoning(completed, terminal.turnID, terminal.status)
	return s.publish(completed, Event{
		TurnID:    terminal.turnID,
		EventType: EventTurnCompleted,
		Terminal:  true,
		Payload:   terminal.payload,
	})
}

func syntheticTerminalsEqual(left, right syntheticTerminal) bool {
	return left.turnID == right.turnID && left.status == right.status &&
		left.payload.Code == right.payload.Code && stringPointersEqual(left.payload.Message, right.payload.Message)
}

func stringPointersEqual(left, right *string) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

type turnError struct {
	Message        *string         `json:"message"`
	CodexErrorInfo json.RawMessage `json:"codexErrorInfo"`
}

type itemNotification struct {
	ThreadID string `json:"threadId"`
	TurnID   string `json:"turnId"`
	Item     struct {
		ID    string  `json:"id"`
		Type  string  `json:"type"`
		Text  *string `json:"text"`
		Phase *string `json:"phase"`
		// Runtime Item content is type-specific. Keep it opaque during envelope decoding so
		// command/tool fields (and structured user/tool content) cannot make an otherwise
		// valid legacy lifecycle malformed. Only completed reasoning Items decode the
		// closed []string raw-reasoning shape. v5 performs its own isolated typed decode.
		Content json.RawMessage `json:"content"`
	} `json:"item"`
}

type v5LifecycleNotification struct {
	ThreadID string `json:"threadId"`
	TurnID   string `json:"turnId"`
	Item     struct {
		ID               string          `json:"id"`
		Type             string          `json:"type"`
		Cwd              *string         `json:"cwd"`
		Status           *string         `json:"status"`
		AggregatedOutput *string         `json:"aggregatedOutput"`
		ExitCode         *int32          `json:"exitCode"`
		DurationMS       *int64          `json:"durationMs"`
		Arguments        json.RawMessage `json:"arguments"`
		Result           json.RawMessage `json:"result"`
		CommandActions   []struct {
			Type string `json:"type"`
		} `json:"commandActions"`
	} `json:"item"`
}

type commandOutputDeltaNotification struct {
	ThreadID string  `json:"threadId"`
	TurnID   string  `json:"turnId"`
	ItemID   string  `json:"itemId"`
	Delta    *string `json:"delta"`
}

type toolProgressNotification struct {
	ThreadID string  `json:"threadId"`
	TurnID   string  `json:"turnId"`
	ItemID   string  `json:"itemId"`
	Message  *string `json:"message"`
}

type planNotification struct {
	ThreadID    string  `json:"threadId"`
	TurnID      string  `json:"turnId"`
	Explanation *string `json:"explanation"`
	Plan        *[]struct {
		Step   *string `json:"step"`
		Status string  `json:"status"`
	} `json:"plan"`
}

func (s *Service) publish(record Record, event Event) error {
	decorateEvent(record, &event)
	legacy := legacyEvent(event)
	if _, err := s.events.Publish(legacy); err != nil {
		return err
	}
	if s.eventsV2 != nil {
		if _, err := s.eventsV2.Publish(legacy); err != nil {
			return err
		}
	}
	if s.eventsV3 != nil {
		if _, err := s.eventsV3.Publish(legacy); err != nil {
			return err
		}
	}
	return s.publishV4(record, event)
}

func (s *Service) publishV2(record Record, event Event) error {
	if !s.rawReasoning {
		return nil
	}
	decorateEvent(record, &event)
	if s.eventsV2 != nil {
		if _, err := s.eventsV2.Publish(event); err != nil {
			return err
		}
	}
	if s.eventsV3 != nil {
		if _, err := s.eventsV3.Publish(event); err != nil {
			return err
		}
	}
	return s.publishV4(record, event)
}

func legacyEvent(event Event) Event {
	event.Payload.Phase = nil
	event.Payload.V4Text = nil
	event.Payload.V4Message = nil
	event.Payload.Explanation = nil
	event.Payload.Plan = nil
	if event.Payload.Text != nil && *event.Payload.Text == "" {
		event.Payload.Text = nil
	}
	return event
}

func decorateEvent(record Record, event *Event) {
	event.TaskID = record.TaskID
	event.AgentSessionID = record.AgentSessionID
	event.CodexThreadID = record.CodexThreadID
	event.TraceID = record.Trace.TraceID
	event.RequestID = record.Trace.RequestID
	event.TenantID = record.Trace.TenantID
	event.UserID = record.Trace.UserID
}

type reasoningTurnKey struct{ sessionID, turnID string }
type reasoningItemState struct {
	parts     map[int]string
	finalized bool
}
type reasoningTurnState struct {
	items      map[string]*reasoningItemState
	totalBytes int
}

func (s *Service) appendReasoningDelta(record Record, turnID, itemID string, contentIndex int, delta string) error {
	if !s.rawReasoning {
		return nil
	}
	if contentIndex < 0 || contentIndex > 7 || delta == "" {
		return s.publishReasoningUnavailable(record, turnID, itemID, "protocol_error")
	}
	if len([]byte(delta)) > 16<<10 {
		return s.publishReasoningUnavailable(record, turnID, itemID, "limit_exceeded")
	}
	s.reasoningMu.Lock()
	state, item, ok := s.reasoningItemLocked(record.AgentSessionID, turnID, itemID)
	if !ok {
		s.reasoningMu.Unlock()
		return s.publishReasoningUnavailable(record, turnID, itemID, "limit_exceeded")
	}
	if item.finalized {
		s.reasoningMu.Unlock()
		return nil
	}
	part := item.parts[contentIndex] + delta
	newPartBytes := len([]byte(part))
	itemBytes := 0
	for index, value := range item.parts {
		if index == contentIndex {
			continue
		}
		itemBytes += len([]byte(value))
	}
	itemBytes += newPartBytes
	if newPartBytes > 64<<10 || itemBytes > 128<<10 || state.totalBytes+len([]byte(delta)) > 256<<10 {
		item.finalized = true
		item.parts = nil
		s.reasoningMu.Unlock()
		return s.publishReasoningUnavailable(record, turnID, itemID, "limit_exceeded")
	}
	item.parts[contentIndex] = part
	state.totalBytes += len([]byte(delta))
	s.reasoningMu.Unlock()
	index := contentIndex
	return s.publishV2(record, Event{TurnID: turnID, ItemID: itemID, EventType: EventItemReasoningTextDelta, Payload: EventPayload{ContentIndex: &index, Delta: stringPointer(delta)}})
}

func (s *Service) finalizeReasoning(record Record, turnID, itemID string, contents []string) error {
	if !s.rawReasoning {
		return nil
	}
	parts, total, valid := validateReasoningContents(contents)
	s.reasoningMu.Lock()
	state, item, registered := s.reasoningItemLocked(record.AgentSessionID, turnID, itemID)
	if registered && item.finalized {
		s.reasoningMu.Unlock()
		return nil
	}
	turnWithinLimit := false
	if registered && !item.finalized {
		oldBytes := reasoningPartBytes(item.parts)
		turnTotal := state.totalBytes - oldBytes + total
		turnWithinLimit = turnTotal <= 256<<10
		item.finalized = true
		item.parts = nil
		state.totalBytes = turnTotal
	}
	s.reasoningMu.Unlock()
	if !registered || !valid || total > 128<<10 || !turnWithinLimit {
		return s.publishReasoningUnavailable(record, turnID, itemID, "limit_exceeded")
	}
	return s.publishV2(record, Event{TurnID: turnID, ItemID: itemID, EventType: EventItemReasoningFinalized, Payload: EventPayload{Status: "complete", Contents: &parts}})
}

func validateReasoningContents(contents []string) ([]ReasoningContent, int, bool) {
	if len(contents) == 0 || len(contents) > 8 {
		return nil, 0, false
	}
	parts := make([]ReasoningContent, 0, len(contents))
	total := 0
	for index, value := range contents {
		bytes := len([]byte(value))
		if bytes == 0 || bytes > 64<<10 {
			return nil, 0, false
		}
		total += bytes
		parts = append(parts, ReasoningContent{ContentIndex: index, Text: value})
	}
	return parts, total, total <= 128<<10
}

func (s *Service) reasoningItemLocked(sessionID, turnID, itemID string) (*reasoningTurnState, *reasoningItemState, bool) {
	key := reasoningTurnKey{sessionID: sessionID, turnID: turnID}
	state := s.reasoning[key]
	if state == nil {
		state = &reasoningTurnState{items: make(map[string]*reasoningItemState)}
		s.reasoning[key] = state
	}
	item := state.items[itemID]
	if item == nil {
		if len(state.items) >= 8 {
			return state, nil, false
		}
		item = &reasoningItemState{parts: make(map[int]string)}
		state.items[itemID] = item
	}
	return state, item, true
}

func (s *Service) publishReasoningUnavailable(record Record, turnID, itemID, reason string) error {
	s.reasoningMu.Lock()
	if state, item, ok := s.reasoningItemLocked(record.AgentSessionID, turnID, itemID); ok && !item.finalized {
		state.totalBytes -= reasoningPartBytes(item.parts)
		item.finalized = true
		item.parts = nil
	}
	s.reasoningMu.Unlock()
	contents := []ReasoningContent{}
	return s.publishV2(record, Event{TurnID: turnID, ItemID: itemID, EventType: EventItemReasoningFinalized, Payload: EventPayload{Status: "unavailable", ReasonCode: reason, Contents: &contents}})
}

func reasoningPartBytes(parts map[int]string) int {
	total := 0
	for _, part := range parts {
		total += len([]byte(part))
	}
	return total
}

func (s *Service) discardReasoningTurn(sessionID, turnID string) {
	s.reasoningMu.Lock()
	delete(s.reasoning, reasoningTurnKey{sessionID: sessionID, turnID: turnID})
	s.reasoningMu.Unlock()
}

func (s *Service) takeUnfinishedReasoningItems(sessionID, turnID string) []string {
	key := reasoningTurnKey{sessionID: sessionID, turnID: turnID}
	s.reasoningMu.Lock()
	state := s.reasoning[key]
	delete(s.reasoning, key)
	s.reasoningMu.Unlock()
	if state == nil {
		return nil
	}
	itemIDs := make([]string, 0, len(state.items))
	for itemID, item := range state.items {
		if item != nil && !item.finalized {
			itemIDs = append(itemIDs, itemID)
		}
	}
	sort.Strings(itemIDs)
	return itemIDs
}

func (s *Service) finalizeInterruptedReasoning(record Record, turnID, status string) {
	key := reasoningTurnKey{sessionID: record.AgentSessionID, turnID: turnID}
	s.reasoningMu.Lock()
	state := s.reasoning[key]
	delete(s.reasoning, key)
	s.reasoningMu.Unlock()
	if state == nil {
		return
	}
	baseReason := "runtime_error"
	if status == "interrupted" {
		baseReason = "turn_interrupted"
	} else if status == "completed" {
		baseReason = "stream_gap"
	}
	for itemID, item := range state.items {
		if item.finalized || len(item.parts) == 0 {
			continue
		}
		indexes := make([]int, 0, len(item.parts))
		for index := range item.parts {
			indexes = append(indexes, index)
		}
		sort.Ints(indexes)
		contents := make([]ReasoningContent, 0, len(indexes))
		gap := false
		reason := baseReason
		for expected, index := range indexes {
			if index != expected {
				gap = true
				break
			}
			contents = append(contents, ReasoningContent{ContentIndex: index, Text: item.parts[index]})
		}
		if gap {
			reason = "stream_gap"
		}
		if len(contents) == 0 {
			_ = s.publishReasoningUnavailable(record, turnID, itemID, reason)
			continue
		}
		_ = s.publishV2(record, Event{TurnID: turnID, ItemID: itemID, EventType: EventItemReasoningFinalized, Payload: EventPayload{Status: "incomplete", ReasonCode: reason, Contents: &contents}})
	}
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
			if notification.method != RuntimeNotificationCommandOutputDelta && notification.method != RuntimeNotificationMcpToolProgress {
				s.rejectMalformedV4Notification(notification.method, notification.params)
			}
			s.logger.Warn("failed to map buffered Codex notification", "method", notification.method)
		}
	}
}

func supportedNotification(method string) bool {
	if method == RuntimeNotificationReasoningTextDelta {
		return true
	}
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
	message = sanitizeFullMessage(message)
	if len(message) > 4096 {
		message = message[:4096]
	}
	return message
}

func sanitizeFullMessage(message string) string {
	message = bearerPattern.ReplaceAllString(message, "Bearer [REDACTED]")
	message = strings.ReplaceAll(message, "\x00", "")
	return message
}

func sanitizedMessagePointers(message string) (*string, *string) {
	full := sanitizeFullMessage(message)
	legacy := full
	if len(legacy) > 4096 {
		legacy = legacy[:4096]
	}
	return stringPointer(legacy), stringPointer(full)
}

func stringPointer(value string) *string {
	return &value
}
