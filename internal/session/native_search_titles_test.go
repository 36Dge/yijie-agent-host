package session

import (
	"encoding/json"
	"fmt"
	"testing"

	native "github.com/36Dge/yijie-agent-host/internal/contracts/nativeconversationv6"
)

func TestNativeSearchTitlesNegotiationPreservesCanonicalEvent(t *testing.T) {
	rows := []native.NativeSearchSourceTitle{
		{SearchItemId: "sh", Url: "https://example.org/shanghai", Title: "上海"},
		{SearchItemId: "bj", Url: "https://example.org/beijing", Title: "北京"},
	}
	projected := projectNativeSearchTitles(rows)
	if projected == nil || len(*projected) != 2 || (*projected)[1] != rows[1] {
		t.Fatalf("lost real source associations: %v", projected)
	}
	event := Event{SchemaVersion: 13, Payload: EventPayload{Native: &native.NativeNotification{WebSearchSourceTitles: projected}}}
	old := StripNativeSearchTitles(event)
	if old.Payload.Native.WebSearchSourceTitles != nil || event.Payload.Native.WebSearchSourceTitles == nil {
		t.Fatal("negotiated and legacy notifications share mutable title state")
	}
}

func TestNativeSearchTitlesLargeResearchAndFrameFallback(t *testing.T) {
	var rows []native.NativeSearchSourceTitle
	for call := range 14 {
		for source := range 10 {
			rows = append(rows, native.NativeSearchSourceTitle{SearchItemId: fmt.Sprintf("city-%d", call), Url: fmt.Sprintf("https://example.org/city/%d/%d", call, source), Title: "城市资料"})
		}
	}
	projected := projectNativeSearchTitles(rows)
	if projected == nil || len(*projected) != 140 {
		t.Fatal("ordinary research titles were lost")
	}
	event := Event{SchemaVersion: 13, Payload: EventPayload{Native: &native.NativeNotification{WebSearchSourceTitles: projected}}}
	raw, err := json.Marshal(event)
	if err != nil {
		t.Fatal(err)
	}
	if retained := BoundNativeSearchTitles(event, len(raw)); retained.Payload.Native.WebSearchSourceTitles == nil {
		t.Fatal("in-budget rows were deferred")
	}
	deferred := BoundNativeSearchTitles(event, len(raw)-1)
	if deferred.Payload.Native.WebSearchSourceTitles != nil || deferred.Payload.Native.WebSearchSourceTitlesDeferred == nil || !*deferred.Payload.Native.WebSearchSourceTitlesDeferred {
		t.Fatal("out-of-budget frame did not request its canonical snapshot")
	}
	old := StripNativeSearchTitles(deferred)
	if old.Payload.Native.WebSearchSourceTitlesDeferred != nil || event.Payload.Native.WebSearchSourceTitles == nil {
		t.Fatal("legacy projection changed the original event")
	}
}
