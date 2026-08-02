package session

import (
	"errors"
	"sync"
	"time"
)

const (
	EventSchemaVersion   = 1
	EventSchemaVersionV2 = 2
)

var (
	ErrStreamChanged     = errors.New("event stream id changed")
	ErrReplayUnavailable = errors.New("requested events are no longer available")
	ErrInvalidSequence   = errors.New("event sequence is invalid")
)

type EventPayload struct {
	Model         string              `json:"model,omitempty"`
	ModelProvider string              `json:"model_provider,omitempty"`
	Status        string              `json:"status,omitempty"`
	ItemType      string              `json:"item_type,omitempty"`
	Text          string              `json:"text,omitempty"`
	Delta         *string             `json:"delta,omitempty"`
	Code          string              `json:"code,omitempty"`
	Message       *string             `json:"message,omitempty"`
	WillRetry     *bool               `json:"will_retry,omitempty"`
	ContentIndex  *int                `json:"content_index,omitempty"`
	Contents      *[]ReasoningContent `json:"contents,omitempty"`
	ReasonCode    string              `json:"reason_code,omitempty"`
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
	if schemaVersion != EventSchemaVersionV2 {
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
	stream.next++
	event.SchemaVersion = h.schemaVersion
	event.EventID = eventID
	event.StreamID = stream.id
	event.Sequence = stream.next
	event.OccurredAt = time.Now().UTC()
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
