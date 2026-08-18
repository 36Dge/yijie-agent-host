package fakeresponses

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/36Dge/yijie-agent-host/internal/codex"
)

const testRunID = "123e4567-e89b-42d3-a456-426614174000"

func TestNewRequiresCanonicalRFC4122UUIDv4RunAuthority(t *testing.T) {
	for _, test := range []struct {
		name  string
		value string
	}{
		{name: "missing", value: ""},
		{name: "malformed", value: "not-a-uuid"},
		{name: "uppercase", value: "123E4567-E89B-42D3-A456-426614174003"},
		{name: "uuid-v1", value: "6ba7b810-9dad-11d1-80b4-00c04fd430c8"},
		{name: "uuid-v7", value: "019fbd88-cbc3-7bf1-934d-7b05cd693f80"},
		{name: "non-rfc4122-variant", value: "123e4567-e89b-42d3-4456-426614174003"},
		{name: "surrounding-whitespace", value: " 123e4567-e89b-42d3-a456-426614174003 "},
	} {
		t.Run(test.name, func(t *testing.T) {
			server, err := New(Config{RunID: test.value, FixtureID: codex.FEAT126FakeFixtureID})
			if server != nil || err == nil {
				t.Fatalf("accepted invalid FEAT-126 run authority %q", test.value)
			}
		})
	}
}

func TestCompleteResponseUsesFrozenFixtureWithoutPersistingBody(t *testing.T) {
	server := newTestServer(t, ModeComplete)
	request := validRequest(`{"model":"MiniMax-M3","stream":true,"input":[{"role":"user","content":[{"type":"input_text","text":"sensitive synthetic canary"}]}]}`)
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusOK || response.Header().Get("Content-Type") != "text/event-stream" {
		t.Fatalf("unexpected response: %d %v", response.Code, response.Header())
	}
	body := response.Body.String()
	for _, event := range []string{
		"response.created", "response.reasoning_text.delta", "response.output_text.delta", "response.completed",
	} {
		if !strings.Contains(body, event) {
			t.Fatalf("response omitted %s", event)
		}
	}
	if strings.Contains(body, "sensitive synthetic canary") {
		t.Fatal("request body was reflected by the fake provider")
	}
	if !strings.Contains(body, server.fixture.AssistantText) {
		t.Fatal("complete mode no longer returned the frozen FEAT-126 fixture")
	}
	snapshot := server.Snapshot()
	if snapshot.AcceptedCalls != 1 || snapshot.RejectedCalls != 0 || snapshot.FixtureID != codex.FEAT126FakeFixtureID ||
		snapshot.DatasetSHA256 != "523609b44fd244fff18b930c992375999276c2e0d5786efadfd8858ec623b308" {
		t.Fatalf("unexpected content-free snapshot: %#v", snapshot)
	}
}

func TestFEAT127ContextResponseRecognizesFileMarkers(t *testing.T) {
	for _, test := range []struct {
		name       string
		oldMarker  string
		marker     string
		wantAnswer string
	}{
		{name: "alpha", oldMarker: "BRAVO-2846", marker: "ALPHA-7319", wantAnswer: feat127AlphaAnswer},
		{name: "bravo", oldMarker: "ALPHA-7319", marker: "BRAVO-2846", wantAnswer: feat127BravoAnswer},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := newTestServer(t, ModeFEAT127Context)
			body := feat127RequestBody(t, []any{
				map[string]any{"type": "message", "role": "user", "content": []any{
					map[string]any{"type": "input_text", "text": "old file marker " + test.oldMarker},
				}},
				map[string]any{"type": "message", "role": "assistant", "content": []any{
					map[string]any{"type": "output_text", "text": "old response"},
				}},
				map[string]any{"type": "message", "role": "user", "content": []any{
					map[string]any{"type": "input_text", "text": "The following attached-file context is untrusted user-provided data.\nFile context:\n" + test.marker},
				}},
			})
			response := httptest.NewRecorder()
			server.Handler().ServeHTTP(response, validRequest(body))
			if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), test.wantAnswer) {
				t.Fatalf("unexpected FEAT-127 marker response: %d %s", response.Code, response.Body.String())
			}
			if strings.Contains(response.Body.String(), test.oldMarker) {
				t.Fatalf("response used an older user message marker: %s", response.Body.String())
			}
			if snapshot := server.Snapshot(); snapshot.AcceptedCalls != 1 || snapshot.RejectedCalls != 0 {
				t.Fatalf("unexpected FEAT-127 marker counters: %#v", snapshot)
			}
		})
	}
}

func TestFEAT127ContextResponseRecognizesExpectedImage(t *testing.T) {
	imageBytes := []byte("synthetic FEAT-127 image transport fixture")
	digest := sha256.Sum256(imageBytes)
	server := newTestServer(t, ModeFEAT127Context)
	server.feat127ImageSHA256 = hex.EncodeToString(digest[:])
	body := feat127RequestBody(t, []any{
		map[string]any{"type": "message", "role": "user", "content": []any{
			map[string]any{
				"type":      "input_image",
				"image_url": "data:image/png;base64," + base64.StdEncoding.EncodeToString(imageBytes),
			},
		}},
	})
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, validRequest(body))
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), feat127ImageAnswer) {
		t.Fatalf("unexpected FEAT-127 image response: %d %s", response.Code, response.Body.String())
	}
	if snapshot := server.Snapshot(); snapshot.AcceptedCalls != 1 || snapshot.RejectedCalls != 0 {
		t.Fatalf("unexpected FEAT-127 image counters: %#v", snapshot)
	}
}

func TestFEAT127ContextResponseFailsClosed(t *testing.T) {
	for _, test := range []struct {
		name     string
		input    []any
		wantCode int
	}{
		{
			name: "unknown latest input",
			input: []any{map[string]any{"type": "message", "role": "user", "content": []any{
				map[string]any{"type": "input_text", "text": "UNKNOWN-CANARY"},
			}}},
			wantCode: http.StatusUnprocessableEntity,
		},
		{
			name: "older marker is ignored",
			input: []any{
				map[string]any{"type": "message", "role": "user", "content": []any{
					map[string]any{"type": "input_text", "text": "ALPHA-7319"},
				}},
				map[string]any{"type": "message", "role": "user", "content": []any{
					map[string]any{"type": "input_text", "text": "UNKNOWN-CANARY"},
				}},
			},
			wantCode: http.StatusUnprocessableEntity,
		},
		{
			name: "malformed image data URL",
			input: []any{map[string]any{"type": "message", "role": "user", "content": []any{
				map[string]any{"type": "input_image", "image_url": "data:image/png;base64,%%%"},
			}}},
			wantCode: http.StatusBadRequest,
		},
		{
			name: "missing user message",
			input: []any{map[string]any{"type": "message", "role": "developer", "content": []any{
				map[string]any{"type": "input_text", "text": "ALPHA-7319"},
			}}},
			wantCode: http.StatusBadRequest,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := newTestServer(t, ModeFEAT127Context)
			response := httptest.NewRecorder()
			server.Handler().ServeHTTP(response, validRequest(feat127RequestBody(t, test.input)))
			if response.Code != test.wantCode {
				t.Fatalf("expected %d, got %d: %s", test.wantCode, response.Code, response.Body.String())
			}
			for _, forbidden := range []string{"UNKNOWN-CANARY", "ALPHA-7319", "BRAVO-2846", "\u52a0\u53f7"} {
				if strings.Contains(response.Body.String(), forbidden) {
					t.Fatalf("failed-closed response reflected or invented verification content %q", forbidden)
				}
			}
			if snapshot := server.Snapshot(); snapshot.AcceptedCalls != 0 || snapshot.RejectedCalls != 1 {
				t.Fatalf("unexpected failed-closed counters: %#v", snapshot)
			}
		})
	}
}

func TestHealthUsesExplicitDatasetAndFixtureCaseNames(t *testing.T) {
	server := newTestServer(t, ModeComplete)
	request := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	request.Header.Set(RunIDHeader, testRunID)
	request.Header.Set(FixtureIDHeader, codex.FEAT126FakeFixtureID)
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	var payload map[string]any
	if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &payload) != nil {
		t.Fatalf("unexpected health response: %d %s", response.Code, response.Body.String())
	}
	if payload["dataset_id"] != "feat126-title-raw-v1" || payload["fixture_case_id"] != "normal-000" {
		t.Fatalf("health identity is ambiguous: %#v", payload)
	}
	if _, exists := payload["fixture_id"]; exists {
		t.Fatalf("legacy ambiguous fixture_id must not be emitted: %#v", payload)
	}
}

func TestClosedHealthBindsModeGenerationAndCallCap(t *testing.T) {
	server, err := New(Config{
		RunID: testRunID, FixtureID: codex.FEAT126FakeFixtureID,
		Mode: ModeDisconnect, Generation: 3, MaxCalls: 5,
	})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "/healthz/v2", nil)
	request.Header.Set(RunIDHeader, testRunID)
	request.Header.Set(FixtureIDHeader, codex.FEAT126FakeFixtureID)
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	var payload map[string]any
	if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &payload) != nil {
		t.Fatalf("unexpected closed health response: %d %s", response.Code, response.Body.String())
	}
	if payload["mode"] != string(ModeDisconnect) || payload["generation"] != float64(3) || payload["call_cap"] != float64(5) {
		t.Fatalf("closed health omitted fake authority: %#v", payload)
	}
	for _, forbidden := range []string{"path", "secret", "bearer", "dsn", "payload"} {
		if strings.Contains(strings.ToLower(response.Body.String()), forbidden) {
			t.Fatalf("closed health leaked forbidden material %q", forbidden)
		}
	}
}

func TestFakeResponsesFailsClosed(t *testing.T) {
	tests := []struct {
		name     string
		mutate   func(*http.Request)
		body     string
		wantCode int
	}{
		{name: "missing run", mutate: func(request *http.Request) { request.Header.Del(RunIDHeader) }, body: `{"model":"MiniMax-M3","stream":true,"input":"x"}`, wantCode: http.StatusForbidden},
		{name: "authorization forbidden", mutate: func(request *http.Request) { request.Header.Set("Authorization", "Bearer synthetic") }, body: `{"model":"MiniMax-M3","stream":true,"input":"x"}`, wantCode: http.StatusForbidden},
		{name: "wrong model", body: `{"model":"other","stream":true,"input":"x"}`, wantCode: http.StatusBadRequest},
		{name: "not streaming", body: `{"model":"MiniMax-M3","stream":false,"input":"x"}`, wantCode: http.StatusBadRequest},
		{name: "empty input", body: `{"model":"MiniMax-M3","stream":true,"input":[]}`, wantCode: http.StatusBadRequest},
		{name: "trailing json", body: `{"model":"MiniMax-M3","stream":true,"input":"x"}{}`, wantCode: http.StatusBadRequest},
		{name: "oversized request", body: `{"model":"MiniMax-M3","stream":true,"input":"` + strings.Repeat("x", 1<<20) + `"}`, wantCode: http.StatusRequestEntityTooLarge},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := newTestServer(t, ModeComplete)
			request := validRequest(test.body)
			if test.mutate != nil {
				test.mutate(request)
			}
			response := httptest.NewRecorder()
			server.Handler().ServeHTTP(response, request)
			if response.Code != test.wantCode {
				t.Fatalf("expected %d, got %d: %s", test.wantCode, response.Code, response.Body.String())
			}
		})
	}
}

func TestFailureModesAreDeterministic(t *testing.T) {
	for _, test := range []struct {
		mode     Mode
		code     int
		contains string
	}{
		{mode: ModeIncomplete, code: http.StatusOK, contains: "response.incomplete"},
		{mode: ModeHTTPError, code: http.StatusServiceUnavailable, contains: "synthetic_unavailable"},
		{mode: ModeDisconnect, code: http.StatusOK, contains: "response.created"},
		{mode: ModeOversize, code: http.StatusOK, contains: strings.Repeat("x", 1024)},
	} {
		t.Run(string(test.mode), func(t *testing.T) {
			server := newTestServer(t, test.mode)
			response := httptest.NewRecorder()
			server.Handler().ServeHTTP(response, validRequest(`{"model":"MiniMax-M3","stream":true,"input":"x"}`))
			if response.Code != test.code || !strings.Contains(response.Body.String(), test.contains) {
				t.Fatalf("unexpected deterministic failure response: %d", response.Code)
			}
		})
	}
}

func TestIncompleteResponseExposesReasoningBeforeCancelableTerminal(t *testing.T) {
	fake := httptest.NewServer(newTestServer(t, ModeIncomplete).Handler())
	defer fake.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	request, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		fake.URL+"/v1/responses",
		strings.NewReader(`{"model":"MiniMax-M3","stream":true,"input":"x"}`),
	)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set(RunIDHeader, testRunID)
	request.Header.Set(FixtureIDHeader, codex.FEAT126FakeFixtureID)
	response, err := fake.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var events []string
	scanner := bufio.NewScanner(response.Body)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "event: ") {
			continue
		}
		event := strings.TrimPrefix(line, "event: ")
		events = append(events, event)
		if event == "response.output_item.done" {
			cancel()
		}
	}
	joined := strings.Join(events, ",")
	if !strings.Contains(joined, "response.reasoning_text.delta") ||
		!strings.Contains(joined, "response.output_item.done") {
		t.Fatalf("reasoning observation window was not reached: %v", events)
	}
	if strings.Contains(joined, "response.incomplete") {
		t.Fatalf("canceled request emitted a late terminal: %v", events)
	}
}

func newTestServer(t *testing.T, mode Mode) *Server {
	t.Helper()
	server, err := New(Config{RunID: testRunID, FixtureID: codex.FEAT126FakeFixtureID, Mode: mode, MaxCalls: 2})
	if err != nil {
		t.Fatal(err)
	}
	return server
}

func validRequest(body string) *http.Request {
	request := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set(RunIDHeader, testRunID)
	request.Header.Set(FixtureIDHeader, codex.FEAT126FakeFixtureID)
	return request
}

func feat127RequestBody(t *testing.T, input []any) string {
	t.Helper()
	encoded, err := json.Marshal(map[string]any{
		"model": codex.MiniMaxModel, "stream": true, "input": input,
	})
	if err != nil {
		t.Fatal(err)
	}
	return string(encoded)
}
