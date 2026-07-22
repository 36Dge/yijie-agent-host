package session

import (
	"errors"
	"testing"
)

func TestEventHubOrdersAndReplaysWithinStream(t *testing.T) {
	hub := NewEventHub(3, 2)
	for index := 0; index < 4; index++ {
		event, err := hub.Publish(Event{
			AgentSessionID: testSessionID,
			EventType:      EventWarning,
			Payload:        EventPayload{Message: "warning"},
		})
		if err != nil {
			t.Fatal(err)
		}
		if event.Sequence != uint64(index+1) || event.EventID == "" || event.StreamID == "" {
			t.Fatalf("unexpected event identity: %+v", event)
		}
	}

	streamID, _, initial, initialCancel, err := hub.Subscribe(testSessionID, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	initialCancel()
	if _, open := <-initial; open {
		t.Fatal("expected canceled initial subscription to close")
	}
	streamID, replay, updates, cancel, err := hub.Subscribe(testSessionID, streamID, 2)
	if err != nil {
		t.Fatal(err)
	}
	defer cancel()
	if len(replay) != 2 || replay[0].Sequence != 3 || replay[1].Sequence != 4 {
		t.Fatalf("unexpected replay: %+v", replay)
	}
	published, err := hub.Publish(Event{AgentSessionID: testSessionID, EventType: EventWarning})
	if err != nil {
		t.Fatal(err)
	}
	if received := <-updates; received.Sequence != published.Sequence {
		t.Fatalf("subscriber received wrong event: %+v", received)
	}
	if _, _, _, _, err := hub.Subscribe(testSessionID, testTaskID, 0); !errors.Is(err, ErrStreamChanged) {
		t.Fatalf("expected stream mismatch, got %v", err)
	}
	if _, _, _, _, err := hub.Subscribe(testSessionID, streamID, 1); !errors.Is(err, ErrReplayUnavailable) {
		t.Fatalf("expected bounded replay error, got %v", err)
	}
	if _, _, _, _, err := hub.Subscribe(testSessionID, "", 1); !errors.Is(err, ErrInvalidSequence) {
		t.Fatalf("expected sequence without stream id to fail, got %v", err)
	}
}

func TestEventHubDisconnectsSlowSubscriber(t *testing.T) {
	hub := NewEventHub(10, 1)
	_, _, updates, cancel, err := hub.Subscribe(testSessionID, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	defer cancel()
	if _, err := hub.Publish(Event{AgentSessionID: testSessionID, EventType: EventWarning}); err != nil {
		t.Fatal(err)
	}
	if _, err := hub.Publish(Event{AgentSessionID: testSessionID, EventType: EventWarning}); err != nil {
		t.Fatal(err)
	}
	<-updates
	if _, open := <-updates; open {
		t.Fatal("expected slow subscriber to be disconnected")
	}
}
