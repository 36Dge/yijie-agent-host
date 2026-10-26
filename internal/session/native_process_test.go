package session

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"

	native "github.com/36Dge/yijie-agent-host/internal/contracts/nativeconversationv4"
	"github.com/getkin/kin-openapi/openapi3"
)

func TestNativeProcessOrdinarySourceURLComponents(t *testing.T) {
	data, err := os.ReadFile("../../api/fixtures/native-search-ordinary-url-components.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Cases []struct {
			URL     string `json:"url"`
			Visible bool   `json:"visible"`
		} `json:"cases"`
	}
	if err := json.Unmarshal(data, &fixture); err != nil || len(fixture.Cases) != 25 {
		t.Fatalf("invalid fixed Contracts fixture: %v", err)
	}
	for _, example := range fixture.Cases {
		t.Run(example.URL, func(t *testing.T) {
			raw, err := json.Marshal(map[string]any{"id": "source-components", "type": "webSearch", "action": map[string]any{
				"type": "search", "query": "ordinary documentation", "sources": []map[string]string{{"type": "url", "url": example.URL, "title": "Public documentation"}},
			}})
			if err != nil {
				t.Fatal(err)
			}
			item, err := projectNativeItem(raw, "/workspace", false)
			if err != nil || item.WebSearch == nil || item.WebSearch.Sources == nil {
				t.Fatalf("missing native projection: %v", err)
			}
			sources := *item.WebSearch.Sources
			if example.Visible {
				if len(sources) != 1 || sources[0].Url != example.URL || item.Availability == native.NativeItemAvailabilityPartial {
					t.Fatal("ordinary source lost, rewritten or marked partial")
				}
			} else if len(sources) != 0 || item.Availability != native.NativeItemAvailabilityPartial {
				t.Fatal("opaque component was exposed or filtering was hidden")
			}
		})
	}
	// Replay only the public source metadata from the observed twenty-source
	// result. Conservative filtering remains visible and never changes its facts.
	sources := make([]map[string]string, 0, 20)
	for _, example := range fixture.Cases[:20] {
		sources = append(sources, map[string]string{"type": "url", "url": example.URL})
	}
	raw, err := json.Marshal(map[string]any{"id": "public-source-batch", "type": "webSearch", "action": map[string]any{
		"type": "search", "query": "ordinary documentation", "sources": sources,
	}})
	if err != nil {
		t.Fatal(err)
	}
	item, err := projectNativeItem(raw, "/workspace", false)
	if err != nil || item.WebSearch == nil || item.WebSearch.Sources == nil || len(*item.WebSearch.Sources) != 18 || item.Availability != native.NativeItemAvailabilityPartial {
		t.Fatalf("public batch projection mismatch: %v", err)
	}
	event := Event{SchemaVersion: 11, Payload: EventPayload{Native: &native.NativeNotification{Item: &item}}}
	if NativeEventV10(event).Payload.Native.Item.WebSearch.Sources != nil || len(*event.Payload.Native.Item.WebSearch.Sources) != 18 {
		t.Fatal("legacy downgrade mutated the current source facts")
	}
}

func TestNativeProcessProjectsExistingWebSearchActions(t *testing.T) {
	for _, action := range []string{
		`{"type":"search","queries":["城市产业","生活成本"]}`,
		`{"type":"openPage","url":"https://example.org/report?edition=2026#table"}`,
		`{"type":"findInPage","url":"https://example.org/report","pattern":"人口"}`,
		`{"type":"other"}`,
	} {
		raw := json.RawMessage(`{"id":"web-native","type":"webSearch","action":` + action + `}`)
		item, err := projectNativeItem(raw, "/workspace", false)
		if err != nil || item.Type != native.WebSearch || item.Id != "web-native" || item.WebSearch == nil || item.Status != nil {
			t.Fatal("native web identity/action was lost or execution status was invented")
		}
		encoded, _ := json.Marshal(item)
		if strings.Contains(string(encoded), "https://") || strings.Contains(string(encoded), "edition=") || strings.Contains(string(encoded), "/report") {
			t.Fatal("page URL escaped the hostname-only display projection")
		}
	}
}

func TestNativeProcessBoundsSearchTextAndKeepsNativeAvailability(t *testing.T) {
	raw, _ := json.Marshal(map[string]any{"id": "search", "type": "webSearch", "action": map[string]any{
		"type": "search", "queries": []string{strings.Repeat("普通资料", 800)},
	}})
	item, err := projectNativeItem(raw, "/workspace", false)
	if err != nil || item.Availability != native.NativeItemAvailabilityPartial || item.WebSearch == nil || len(item.WebSearch.Queries[0]) > 1024 {
		t.Fatal("bounded display was not independent of execution")
	}
}

func TestNativeProcessSourceBoundsRemainPartial(t *testing.T) {
	sources := make([]map[string]string, 33)
	for i := range sources {
		sources[i] = map[string]string{"type": "url", "url": fmt.Sprintf("https://example.org/documentation/guides/page-%d", i), "title": strings.Repeat("普通资料", 200)}
	}
	raw := rawJSON(t, map[string]any{"id": "bounded-sources", "type": "webSearch", "action": map[string]any{"type": "search", "sources": sources}})
	item, err := projectNativeItem(raw, "/workspace", false)
	if err != nil || item.WebSearch == nil || item.WebSearch.Sources == nil || len(*item.WebSearch.Sources) != 32 || item.Availability != native.NativeItemAvailabilityPartial {
		t.Fatalf("source count limit or partial indication changed: %v", err)
	}
	for _, source := range *item.WebSearch.Sources {
		if source.Title == nil || len(*source.Title) > 512 {
			t.Fatal("source title bound changed")
		}
	}
}

func TestNativeProcessSourcesStaySearchFactsAndDowngradeWithoutMutation(t *testing.T) {
	raw := json.RawMessage(`{"id":"web-source","type":"webSearch","action":{"type":"search","query":"annual statistics","sources":[{"type":"url","url":"https://example.org/statistics/2025?edition=annual","title":"Annual statistics"},{"type":"url","url":"https://example.org/statistics/2025?edition=annual","title":"Annual statistics"}]}}`)
	item, err := projectNativeItem(raw, "/workspace", false)
	if err != nil || item.WebSearch == nil || item.WebSearch.Sources == nil || len(*item.WebSearch.Sources) != 1 {
		t.Fatal("actual source metadata lost or duplicated")
	}
	source := (*item.WebSearch.Sources)[0]
	if source.Url != "https://example.org/statistics/2025?edition=annual" || source.Title == nil || *source.Title != "Annual statistics" {
		t.Fatal("source URL or title rewritten")
	}
	event := Event{SchemaVersion: 11, Payload: EventPayload{Native: &native.NativeNotification{Item: &item}}}
	old := NativeEventV10(event)
	if old.SchemaVersion != 10 || old.Payload.Native.Item.WebSearch.Sources != nil || event.Payload.Native.Item.WebSearch.Sources == nil {
		t.Fatal("legacy projection mutated facts or leaked new fields")
	}
	absent, _ := projectNativeItem(json.RawMessage(`{"id":"old-search","type":"webSearch","action":{"type":"search","query":"annual statistics"}}`), "/workspace", false)
	if absent.WebSearch.Sources != nil {
		t.Fatal("sources invented for a legacy record")
	}
}

func TestNativeProcessNewAndLegacyEventsConformWithoutMutatingFacts(t *testing.T) {
	s := NewService(&fakeRuntime{}, openBoundFEAT134Store(t), NewEventHub(32, 8), nil)
	for _, method := range []string{RuntimeNotificationItemStarted, RuntimeNotificationItemCompleted} {
		s.HandleNotification(method, rawJSON(t, map[string]any{"threadId": testThreadID, "turnId": testTurnID,
			"item": map[string]any{"id": "web", "type": "webSearch", "query": "城市产业", "action": map[string]any{"type": "search", "query": "城市产业"}}}))
	}
	_, events, _, cancel, err := s.SubscribeNativeEvents(testSessionID, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	defer cancel()
	if len(events) != 2 {
		t.Fatal("unexpected lifecycle event count")
	}
	for _, version := range []int{11, 10, 8, 7} {
		file := "../../api/openapi/native-conversation-v4.yaml"
		if version == 10 {
			file = "../../api/openapi/native-conversation-v3.yaml"
		}
		if version == 8 {
			file = "../../api/openapi/native-conversation-v2.yaml"
		}
		if version == 7 {
			file = "../../api/openapi/native-conversation.yaml"
		}
		doc, err := openapi3.NewLoader().LoadFromFile(file)
		if err != nil {
			t.Fatal(err)
		}
		for _, source := range events {
			event := source
			if version == 10 {
				event = NativeEventV10(source)
			}
			if version == 8 {
				event = NativeEventV8(source)
			}
			if version == 7 {
				event = LegacyNativeEvent(source)
			}
			data, _ := json.Marshal(event)
			var value any
			_ = json.Unmarshal(data, &value)
			if err := doc.Components.Schemas["NativeEvent"].Value.VisitJSON(value); err != nil {
				t.Fatalf("v%d conformance: %v", version, err)
			}
			if event.EventID != source.EventID || event.Sequence != source.Sequence || event.Terminal != source.Terminal || event.Payload.Native.Method != source.Payload.Native.Method {
				t.Fatal("downgrade changed execution facts")
			}
			if version < 10 && (event.Payload.Native.Item.Type != native.Unknown || event.Payload.Native.Item.WebSearch != nil) {
				t.Fatal("new display fields leaked to old readers")
			}
		}
	}
	if events[1].SchemaVersion != 11 || events[1].Payload.Native.Item.Type != native.WebSearch || events[1].Payload.Native.Item.WebSearch == nil {
		t.Fatal("shared event buffer mutated by legacy projection")
	}
}

func TestNativeProcessHistoryVersionsReuseNativeRead(t *testing.T) {
	sourceURL := "https://example.org/documentation/guides/ordinary-pages"
	runtime := &nativeReadRuntime{raw: rawJSON(t, map[string]any{"id": testThreadID, "turns": []any{map[string]any{"id": testTurnID, "status": "completed", "items": []any{map[string]any{"id": "web", "type": "webSearch", "action": map[string]any{"type": "search", "query": "普通查询", "sources": []map[string]string{{"type": "url", "url": sourceURL}}}}}}}})}
	s := NewService(runtime, openBoundFEAT134Store(t), NewEventHub(32, 8), nil)
	current, err := s.ReadNativeThreadV4(context.Background(), testSessionID)
	if err != nil || current.SchemaVersion != 4 || current.Turns[0].Items[0].Type != native.WebSearch {
		t.Fatal("v4 native history unavailable")
	}
	if current.Turns[0].Items[0].WebSearch.Sources == nil || len(*current.Turns[0].Items[0].WebSearch.Sources) != 1 || (*current.Turns[0].Items[0].WebSearch.Sources)[0].Url != sourceURL {
		t.Fatal("current history lost its ordinary source URL")
	}
	third, err := s.ReadNativeThreadV3(context.Background(), testSessionID)
	if err != nil || third.SchemaVersion != 3 || string(third.Turns[0].Items[0].Type) != "webSearch" {
		t.Fatal("v3 native history compatibility changed")
	}
	old, err := s.ReadNativeThreadV2(context.Background(), testSessionID)
	if err != nil || old.SchemaVersion != 2 || string(old.Turns[0].Items[0].Type) != "unknown" {
		t.Fatal("v2 history compatibility changed")
	}
	legacy, err := s.ReadNativeThread(context.Background(), testSessionID)
	if err != nil || legacy.SchemaVersion != 1 || string(legacy.Turns[0].Items[0].Type) != "unknown" {
		t.Fatal("v1 history compatibility changed")
	}
}
