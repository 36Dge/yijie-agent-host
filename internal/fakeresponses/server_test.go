package fakeresponses

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/36Dge/yijie-agent-host/internal/codex"
)

const testRunID = "019fbd88-cbc3-7bf1-934d-7b05cd693f80"

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
	snapshot := server.Snapshot()
	if snapshot.AcceptedCalls != 1 || snapshot.RejectedCalls != 0 || snapshot.FixtureID != codex.FEAT126FakeFixtureID ||
		snapshot.DatasetSHA256 != "523609b44fd244fff18b930c992375999276c2e0d5786efadfd8858ec623b308" {
		t.Fatalf("unexpected content-free snapshot: %#v", snapshot)
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
