package session

import (
	"encoding/json"
	"errors"
	native "github.com/36Dge/yijie-agent-host/internal/contracts/nativeconversation"
	"sync"
	"time"
)

const (
	EventSchemaVersion   = 1
	EventSchemaVersionV2 = 2
	EventSchemaVersionV3 = 3
	EventSchemaVersionV4 = 4
	EventSchemaVersionV5 = 5
	EventSchemaVersionV6 = 6

	maxRetainedEventDataBytes = 1 << 20
	// Kept for existing v4 conformance tests and downstream package references.
	maxV4EventDataBytes = maxRetainedEventDataBytes
)

var (
	ErrStreamChanged      = errors.New("event stream id changed")
	ErrReplayUnavailable  = errors.New("requested events are no longer available")
	ErrInvalidSequence    = errors.New("event sequence is invalid")
	ErrEventLimitExceeded = errors.New("event exceeds negotiated projection limit")
)

type EventPayload struct {
	Native            *native.NativeNotification `json:"native,omitempty"`
	Model             string                     `json:"model,omitempty"`
	ModelProvider     string                     `json:"model_provider,omitempty"`
	Status            string                     `json:"status,omitempty"`
	ItemType          string                     `json:"item_type,omitempty"`
	Text              *string                    `json:"text,omitempty"`
	V4Text            *string                    `json:"-"`
	Phase             **string                   `json:"phase,omitempty"`
	Delta             *string                    `json:"delta,omitempty"`
	Code              string                     `json:"code,omitempty"`
	Message           *string                    `json:"message,omitempty"`
	V4Message         *string                    `json:"-"`
	WillRetry         *bool                      `json:"will_retry,omitempty"`
	ContentIndex      *int                       `json:"content_index,omitempty"`
	Contents          *[]ReasoningContent        `json:"contents,omitempty"`
	ReasonCode        string                     `json:"reason_code,omitempty"`
	ArtifactID        string                     `json:"artifact_id,omitempty"`
	Kind              string                     `json:"kind,omitempty"`
	Provenance        string                     `json:"provenance,omitempty"`
	Ordinal           *int                       `json:"ordinal,omitempty"`
	DisplayName       string                     `json:"display_name,omitempty"`
	Stage             string                     `json:"stage,omitempty"`
	Progress          *int                       `json:"progress_percent,omitempty"`
	MediaType         string                     `json:"media_type,omitempty"`
	SizeBytes         *int64                     `json:"size_bytes,omitempty"`
	SHA256            string                     `json:"sha256,omitempty"`
	ContentHref       string                     `json:"content_href,omitempty"`
	PosterHref        string                     `json:"poster_href,omitempty"`
	ErrorCode         string                     `json:"error_code,omitempty"`
	Retryable         *bool                      `json:"retryable,omitempty"`
	Explanation       **string                   `json:"explanation,omitempty"`
	Plan              *[]PlanStep                `json:"plan,omitempty"`
	CommandSummary    *BoundedSummary            `json:"command_summary,omitempty"`
	CommandCwd        *CommandCwd                `json:"cwd,omitempty"`
	DurationMS        *int64                     `json:"duration_ms,omitempty"`
	ExitCode          *int32                     `json:"exit_code,omitempty"`
	Output            *CommandOutput             `json:"output,omitempty"`
	ItemError         *ProjectionError           `json:"error,omitempty"`
	ToolIdentity      *ToolIdentity              `json:"identity,omitempty"`
	ArgumentsSummary  *BoundedSummary            `json:"arguments_summary,omitempty"`
	ProgressIndex     *int                       `json:"progress_index,omitempty"`
	Summary           *BoundedSummary            `json:"summary,omitempty"`
	ResultSummary     *BoundedSummary            `json:"result_summary,omitempty"`
	Truncated         *bool                      `json:"truncated,omitempty"`
	TruncationReason  string                     `json:"truncation_reason,omitempty"`
	ApprovalRequestID string                     `json:"approval_request_id,omitempty"`
	Revision          *int64                     `json:"revision,omitempty"`
	ActionID          string                     `json:"action_id,omitempty"`
	WorkspaceScope    string                     `json:"workspace_scope,omitempty"`
	Decisions         *ApprovalDecisions         `json:"decisions,omitempty"`
	RequestedAt       *time.Time                 `json:"requested_at,omitempty"`
	ExpiresAt         *time.Time                 `json:"expires_at,omitempty"`
	TTLSeconds        *int                       `json:"ttl_seconds,omitempty"`
	Outcome           string                     `json:"outcome,omitempty"`
	DecisionID        string                     `json:"decision_id,omitempty"`
	Decision          string                     `json:"decision,omitempty"`
	ResolvedAt        *time.Time                 `json:"resolved_at,omitempty"`
}

type ApprovalDecisions struct {
	Primary   string `json:"primary"`
	Secondary string `json:"secondary"`
}

func (payload *EventPayload) UnmarshalJSON(data []byte) error {
	type eventPayloadAlias EventPayload
	var decoded eventPayloadAlias
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	if raw, ok := fields["phase"]; ok {
		var value *string
		if string(raw) != "null" {
			var phase string
			if err := json.Unmarshal(raw, &phase); err != nil {
				return err
			}
			value = &phase
		}
		decoded.Phase = nullableString(value)
	}
	if raw, ok := fields["explanation"]; ok {
		var value *string
		if string(raw) != "null" {
			var explanation string
			if err := json.Unmarshal(raw, &explanation); err != nil {
				return err
			}
			value = &explanation
		}
		decoded.Explanation = nullableString(value)
	}
	*payload = EventPayload(decoded)
	return nil
}

type PlanStep struct {
	Step   string `json:"step"`
	Status string `json:"status"`
}

type ReasoningContent struct {
	ContentIndex int    `json:"content_index"`
	Text         string `json:"text"`
}

type Event struct {
	SchemaVersion  int          `json:"schema_version"`
	EventID        string       `json:"event_id"`
	StreamID       string       `json:"stream_id"`
	Sequence       uint64       `json:"sequence"`
	OccurredAt     time.Time    `json:"occurred_at"`
	TraceID        string       `json:"trace_id,omitempty"`
	RequestID      string       `json:"request_id,omitempty"`
	TenantID       string       `json:"tenant_id,omitempty"`
	UserID         string       `json:"user_id,omitempty"`
	TaskID         string       `json:"task_id"`
	AgentSessionID string       `json:"agent_session_id"`
	CodexThreadID  string       `json:"codex_thread_id"`
	TurnID         string       `json:"turn_id,omitempty"`
	ItemID         string       `json:"item_id,omitempty"`
	EventType      string       `json:"event_type"`
	Terminal       bool         `json:"terminal"`
	Payload        EventPayload `json:"payload"`
}

type streamState struct {
	id          string
	next        uint64
	events      []Event
	subscribers map[uint64]chan Event
	nextSubID   uint64
}

type EventHub struct {
	mu                 sync.Mutex
	capacity           int
	subscriberCapacity int
	schemaVersion      int
	streams            map[string]*streamState
}

func NewEventHub(capacity, subscriberCapacity int) *EventHub {
	return NewEventHubVersion(EventSchemaVersion, capacity, subscriberCapacity)
}

func NewEventHubVersion(schemaVersion, capacity, subscriberCapacity int) *EventHub {
	if schemaVersion != EventSchemaVersionV2 && schemaVersion != EventSchemaVersionV3 &&
		schemaVersion != EventSchemaVersionV4 && schemaVersion != EventSchemaVersionV5 &&
		schemaVersion != EventSchemaVersionV6 && schemaVersion != 7 {
		schemaVersion = EventSchemaVersion
	}
	if capacity < 1 {
		capacity = 512
	}
	if subscriberCapacity < 1 {
		subscriberCapacity = 64
	}
	return &EventHub{
		capacity:           capacity,
		subscriberCapacity: subscriberCapacity,
		schemaVersion:      schemaVersion,
		streams:            make(map[string]*streamState),
	}
}

func (h *EventHub) Publish(event Event) (Event, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	stream, err := h.streamLocked(event.AgentSessionID)
	if err != nil {
		return Event{}, err
	}
	eventID, err := newUUID()
	if err != nil {
		return Event{}, err
	}
	event.SchemaVersion = h.schemaVersion
	event.EventID = eventID
	event.StreamID = stream.id
	event.Sequence = stream.next + 1
	event.OccurredAt = time.Now().UTC()
	if h.schemaVersion == 7 {
		if event.Payload.Native == nil || event.EventType != "native.notification" {
			return Event{}, ErrEventLimitExceeded
		}
		encoded, err := json.Marshal(event)
		if err != nil {
			return Event{}, err
		}
		if len(encoded) > maxRetainedEventDataBytes {
			return Event{}, ErrEventLimitExceeded
		}
	}
	if h.schemaVersion == EventSchemaVersionV4 || h.schemaVersion == EventSchemaVersionV5 ||
		h.schemaVersion == EventSchemaVersionV6 {
		var validateErr error
		if h.schemaVersion == EventSchemaVersionV6 {
			if event.EventType != EventApprovalRequested && event.EventType != EventApprovalResolved {
				if fitErr := fitV5RetainedEvent(&event); fitErr != nil {
					return Event{}, fitErr
				}
			}
			validateErr = validateV6RetainedEvent(event)
		} else if h.schemaVersion == EventSchemaVersionV5 {
			if fitErr := fitV5RetainedEvent(&event); fitErr != nil {
				return Event{}, fitErr
			}
			validateErr = validateV5RetainedEvent(event)
		} else {
			validateErr = validateV4RetainedEvent(event)
		}
		if validateErr != nil {
			return Event{}, validateErr
		}
		encoded, marshalErr := json.Marshal(event)
		if marshalErr != nil {
			return Event{}, marshalErr
		}
		if len(encoded) > maxRetainedEventDataBytes {
			return Event{}, ErrEventLimitExceeded
		}
	}
	stream.next++
	stream.events = append(stream.events, event)
	if len(stream.events) > h.capacity {
		stream.events = append([]Event(nil), stream.events[len(stream.events)-h.capacity:]...)
	}
	for id, subscriber := range stream.subscribers {
		select {
		case subscriber <- event:
		default:
			close(subscriber)
			delete(stream.subscribers, id)
		}
	}
	return event, nil
}

func (h *EventHub) CurrentStreamID(sessionID string) (string, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	stream, err := h.streamLocked(sessionID)
	if err != nil {
		return "", err
	}
	return stream.id, nil
}

func (h *EventHub) DeleteSession(sessionID string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	stream := h.streams[sessionID]
	if stream == nil {
		return
	}
	for id, subscriber := range stream.subscribers {
		close(subscriber)
		delete(stream.subscribers, id)
	}
	delete(h.streams, sessionID)
}

func (h *EventHub) Subscribe(
	sessionID, expectedStreamID string,
	after uint64,
) (string, []Event, <-chan Event, func(), error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if expectedStreamID != "" && !validUUID(expectedStreamID) {
		return "", nil, nil, nil, ErrInvalidSequence
	}
	stream, err := h.streamLocked(sessionID)
	if err != nil {
		return "", nil, nil, nil, err
	}
	if expectedStreamID != "" && expectedStreamID != stream.id {
		return stream.id, nil, nil, nil, ErrStreamChanged
	}
	if after > 0 && expectedStreamID == "" {
		return stream.id, nil, nil, nil, ErrInvalidSequence
	}
	if after > stream.next {
		return stream.id, nil, nil, nil, ErrInvalidSequence
	}
	if after > 0 && len(stream.events) > 0 && after+1 < stream.events[0].Sequence {
		return stream.id, nil, nil, nil, ErrReplayUnavailable
	}
	replay := make([]Event, 0, len(stream.events))
	for _, event := range stream.events {
		if event.Sequence > after {
			replay = append(replay, event)
		}
	}
	stream.nextSubID++
	subscriberID := stream.nextSubID
	updates := make(chan Event, h.subscriberCapacity)
	stream.subscribers[subscriberID] = updates
	cancel := func() {
		h.mu.Lock()
		defer h.mu.Unlock()
		current := h.streams[sessionID]
		if current == nil {
			return
		}
		if subscriber, ok := current.subscribers[subscriberID]; ok {
			close(subscriber)
			delete(current.subscribers, subscriberID)
		}
	}
	return stream.id, replay, updates, cancel, nil
}

func (h *EventHub) streamLocked(sessionID string) (*streamState, error) {
	if stream := h.streams[sessionID]; stream != nil {
		return stream, nil
	}
	streamID, err := newUUID()
	if err != nil {
		return nil, err
	}
	stream := &streamState{
		id:          streamID,
		subscribers: make(map[uint64]chan Event),
	}
	h.streams[sessionID] = stream
	return stream, nil
}
