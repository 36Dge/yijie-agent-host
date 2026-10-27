package session

// Display the existing Codex WebSearch Item. Execution, IDs and lifecycle remain
// native; this adapter neither performs searches nor reconstructs tool results.
import (
	"context"
	"encoding/json"
	"net/url"
	"regexp"
	"strings"
	"unicode"

	v2 "github.com/36Dge/yijie-agent-host/internal/contracts/nativeconversationv2"
	v3 "github.com/36Dge/yijie-agent-host/internal/contracts/nativeconversationv3"
	native "github.com/36Dge/yijie-agent-host/internal/contracts/nativeconversationv4"
)

var nativePageHost = regexp.MustCompile(`^[A-Za-z0-9.-]{1,253}$`)

// Source metadata has a dedicated inert URL field. Reuse the existing secret
// detectors, not the command-output rule that intentionally hides every URL.
// Reject an unsafe source entirely; never rewrite its address into another URL.
func safeNativeSourceURL(value string) bool {
	decoded, err := url.PathUnescape(value)
	if err != nil || bearerPattern.MatchString(decoded) ||
		v5SensitiveNamePattern.MatchString(decoded) || v5JWTLikePattern.MatchString(decoded) ||
		v5AWSAccessKeyPattern.MatchString(decoded) || v5PEMBlockPattern.MatchString(decoded) ||
		strings.IndexFunc(decoded, unicode.IsControl) >= 0 {
		return false
	}
	page, err := url.Parse(value)
	if err != nil || v5OpaqueTokenPattern.MatchString(page.Hostname()) || v5OpaqueTokenPattern.MatchString(page.Fragment) {
		return false
	}
	// Literal URL separators delimit components, not opaque identifiers. Split
	// before decoding so an encoded separator stays inside its original component.
	for _, segment := range strings.Split(page.EscapedPath(), "/") {
		component, err := url.PathUnescape(segment)
		if err != nil || v5OpaqueTokenPattern.MatchString(component) {
			return false
		}
	}
	for _, pair := range strings.Split(page.RawQuery, "&") {
		key, value, _ := strings.Cut(pair, "=")
		for _, encoded := range []string{key, value} {
			component, err := url.QueryUnescape(encoded)
			if err != nil || v5OpaqueTokenPattern.MatchString(component) {
				return false
			}
		}
	}
	return true
}

func projectNativeWebSearch(wire nativeWireItem, out *native.NativeItem) {
	var action struct {
		Type    string   `json:"type"`
		Query   string   `json:"query"`
		Queries []string `json:"queries"`
		URL     string   `json:"url"`
		Pattern string   `json:"pattern"`
		Sources *[]struct {
			Type  string `json:"type"`
			URL   string `json:"url"`
			Title string `json:"title"`
		} `json:"sources"`
	}
	display := &native.NativeWebSearchDisplay{Action: native.NativeWebSearchDisplayActionOther, Queries: []string{}}
	out.WebSearch = display
	if len(wire.Action) > 0 && string(wire.Action) != "null" {
		if json.Unmarshal(wire.Action, &action) != nil {
			out.Availability = native.NativeItemAvailabilityPartial
			return
		}
		kind := native.NativeWebSearchDisplayAction(action.Type)
		if kind.Valid() {
			display.Action = kind
		} else {
			out.Availability = native.NativeItemAvailabilityPartial
		}
	}
	text := func(value string) string {
		sanitized := redactV5CommandText(value)
		bounded, complete := safeNativeText(sanitized, 1024)
		if !complete || sanitized != value {
			out.Availability = native.NativeItemAvailabilityPartial
		}
		return bounded
	}
	queries := action.Queries
	if len(queries) == 0 && action.Query != "" {
		queries = []string{action.Query}
	}
	// The top-level query is a native display fallback, not a guessed action.
	if len(queries) == 0 && wire.Query != "" && (display.Action == native.NativeWebSearchDisplayActionSearch || display.Action == native.NativeWebSearchDisplayActionOther) {
		queries = []string{wire.Query}
	}
	for i, query := range queries {
		if i == 8 {
			out.Availability = native.NativeItemAvailabilityPartial
			break
		}
		display.Queries = append(display.Queries, text(query))
	}
	if action.URL != "" {
		page, err := url.Parse(action.URL)
		if err == nil && (page.Scheme == "https" || page.Scheme == "http") && nativePageHost.MatchString(page.Hostname()) {
			// Never send URL credentials, path, query, fragment or a clickable URL.
			label := text(page.Hostname())
			display.PageLabel = &label
		} else {
			out.Availability = native.NativeItemAvailabilityPartial
		}
	}
	if action.Pattern != "" {
		pattern := text(action.Pattern)
		display.Pattern = &pattern
	}
	if action.Sources != nil {
		sources := []native.NativeWebSearchSourceDisplay{}
		seen := map[string]bool{}
		for i, source := range *action.Sources {
			if i == 32 {
				out.Availability = native.NativeItemAvailabilityPartial
				break
			}
			page, err := url.Parse(source.URL)
			if source.Type != "url" || err != nil || (page.Scheme != "https" && page.Scheme != "http") || page.User != nil ||
				!nativePageHost.MatchString(page.Hostname()) || len(source.URL) > 2048 ||
				strings.IndexFunc(source.URL, unicode.IsControl) >= 0 || !safeNativeSourceURL(source.URL) {
				out.Availability = native.NativeItemAvailabilityPartial
				continue
			}
			if seen[source.URL] {
				continue
			}
			seen[source.URL] = true
			entry := native.NativeWebSearchSourceDisplay{Url: source.URL}
			if source.Title != "" {
				redacted := redactV5CommandText(source.Title)
				title, complete := safeNativeText(redacted, 512)
				if !complete || redacted != source.Title {
					out.Availability = native.NativeItemAvailabilityPartial
				}
				entry.Title = &title
			}
			sources = append(sources, entry)
		}
		display.Sources = &sources
	}
}

// Downgrade only the new display metadata; original IDs and execution facts stay unchanged.
func NativeEventV10(event Event) Event {
	event.SchemaVersion = 10
	if event.Payload.Native == nil {
		return event
	}
	encoded, _ := json.Marshal(event.Payload.Native)
	var copy native.NativeNotification
	_ = json.Unmarshal(encoded, &copy)
	strip := func(item *native.NativeItem) {
		if item.WebSearch != nil {
			item.WebSearch.Sources = nil
		}
	}
	if copy.Item != nil {
		strip(copy.Item)
	}
	if copy.Turn != nil {
		for i := range copy.Turn.Items {
			strip(&copy.Turn.Items[i])
		}
	}
	event.Payload.Native = &copy
	return event
}

func (s *Service) ReadNativeThreadV3(ctx context.Context, sessionID string) (v3.NativeThreadSnapshot, error) {
	value, err := s.ReadNativeThreadV4(ctx, sessionID)
	if err != nil {
		return v3.NativeThreadSnapshot{}, err
	}
	for i := range value.Turns {
		event := NativeEventV10(Event{Payload: EventPayload{Native: &native.NativeNotification{Turn: &value.Turns[i]}}})
		value.Turns[i] = *event.Payload.Native.Turn
	}
	value.SchemaVersion = 3
	data, err := json.Marshal(value)
	var result v3.NativeThreadSnapshot
	if err == nil {
		err = json.Unmarshal(data, &result)
	}
	return result, err
}

// Preserve closed v2/v8 responses without mutating the shared native buffer.
func NativeEventV8(event Event) Event {
	event.SchemaVersion = 8
	if event.Payload.Native == nil {
		return event
	}
	encoded, _ := json.Marshal(event.Payload.Native)
	var copy native.NativeNotification
	_ = json.Unmarshal(encoded, &copy)
	strip := func(item *native.NativeItem) {
		if item.Type == native.WebSearch {
			*item = native.NativeItem{Id: item.Id, Type: native.Unknown, Availability: native.NativeItemAvailabilityUnavailable}
		}
	}
	if copy.Item != nil {
		strip(copy.Item)
	}
	if copy.Turn != nil {
		for i := range copy.Turn.Items {
			strip(&copy.Turn.Items[i])
		}
	}
	event.Payload.Native = &copy
	return event
}

func (s *Service) ReadNativeThreadV2(ctx context.Context, sessionID string) (v2.NativeThreadSnapshot, error) {
	value, err := s.ReadNativeThreadV4(ctx, sessionID)
	if err != nil {
		return v2.NativeThreadSnapshot{}, err
	}
	for i := range value.Turns {
		event := NativeEventV8(Event{Payload: EventPayload{Native: &native.NativeNotification{Turn: &value.Turns[i]}}})
		value.Turns[i] = *event.Payload.Native.Turn
	}
	value.SchemaVersion = 2
	data, err := json.Marshal(value)
	var result v2.NativeThreadSnapshot
	if err == nil {
		err = json.Unmarshal(data, &result)
	}
	return result, err
}
