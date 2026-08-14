package fakeresponses

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"github.com/36Dge/yijie-agent-host/internal/codex"
	"github.com/36Dge/yijie-agent-host/internal/session"
	"github.com/google/uuid"
)

const (
	RunIDHeader     = "X-Yijie-Feat126-Run-Id"
	FixtureIDHeader = "X-Yijie-Feat126-Fixture-Id"
	defaultMaxBytes = int64(1 << 20)
	defaultMaxCalls = uint64(8)
	// S10B-004 must expose a finalized reasoning projection before the synthetic
	// incomplete terminal, so the real Desktop path can exercise cancellation.
	incompleteReasoningSettleDelay = 75 * time.Millisecond
	incompleteInterruptWindow      = 1500 * time.Millisecond
)

type Mode string

const (
	ModeComplete   Mode = "complete"
	ModeIncomplete Mode = "incomplete"
	ModeHTTPError  Mode = "http_error"
	ModeDisconnect Mode = "disconnect"
	ModeOversize   Mode = "oversize"
)

type Config struct {
	RunID           string
	FixtureID       string
	Mode            Mode
	Generation      uint64
	MaxRequestBytes int64
	MaxCalls        uint64
}

type Snapshot struct {
	FixtureID     string `json:"fixture_id"`
	DatasetSHA256 string `json:"dataset_sha256"`
	Mode          Mode   `json:"mode"`
	Generation    uint64 `json:"generation"`
	CallCap       uint64 `json:"call_cap"`
	AcceptedCalls uint64 `json:"accepted_calls"`
	RejectedCalls uint64 `json:"rejected_calls"`
}

type Server struct {
	config   Config
	fixture  session.FEAT126FakeFixture
	accepted atomic.Uint64
	rejected atomic.Uint64
}

func New(config Config) (*Server, error) {
	parsed, err := uuid.Parse(config.RunID)
	if err != nil || parsed == uuid.Nil || parsed.String() != config.RunID {
		return nil, errors.New("run id must be a canonical non-zero UUID")
	}
	if config.FixtureID != codex.FEAT126FakeFixtureID {
		return nil, errors.New("fixture id must use the frozen FEAT-126 fixture")
	}
	if config.Mode == "" {
		config.Mode = ModeComplete
	}
	if config.Generation == 0 {
		config.Generation = 1
	}
	if config.Generation > 64 {
		return nil, errors.New("fake Responses generation is invalid")
	}
	switch config.Mode {
	case ModeComplete, ModeIncomplete, ModeHTTPError, ModeDisconnect, ModeOversize:
	default:
		return nil, errors.New("fake Responses mode is unsupported")
	}
	if config.MaxRequestBytes == 0 {
		config.MaxRequestBytes = defaultMaxBytes
	}
	if config.MaxRequestBytes < 1024 || config.MaxRequestBytes > 4<<20 {
		return nil, errors.New("fake Responses request limit is invalid")
	}
	if config.MaxCalls == 0 {
		config.MaxCalls = defaultMaxCalls
	}
	if config.MaxCalls > 64 {
		return nil, errors.New("fake Responses call limit is invalid")
	}
	fixture, err := session.LoadFEAT126FakeFixture(config.FixtureID)
	if err != nil {
		return nil, fmt.Errorf("load frozen FEAT-126 fixture: %w", err)
	}
	return &Server{config: config, fixture: fixture}, nil
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.health)
	mux.HandleFunc("GET /healthz/v2", s.closedHealth)
	mux.HandleFunc("POST /v1/responses", s.responses)
	return mux
}

func (s *Server) Snapshot() Snapshot {
	return Snapshot{
		FixtureID: s.fixture.ID, DatasetSHA256: s.fixture.DatasetSHA256,
		Mode: s.config.Mode, Generation: s.config.Generation, CallCap: s.config.MaxCalls,
		AcceptedCalls: s.accepted.Load(), RejectedCalls: s.rejected.Load(),
	}
}

func (s *Server) health(w http.ResponseWriter, request *http.Request) {
	if !s.authorized(request) {
		s.reject(w, http.StatusForbidden, "run_mismatch")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status": "ready", "dataset_id": session.FEAT126FixtureDatasetID,
		"fixture_case_id": s.fixture.ID, "dataset_sha256": s.fixture.DatasetSHA256,
	})
}

// closedHealth is the Host-owned private projection used by S10BO1. The
// legacy health endpoint intentionally keeps its original wire shape.
func (s *Server) closedHealth(w http.ResponseWriter, request *http.Request) {
	if !s.authorized(request) {
		s.reject(w, http.StatusForbidden, "run_mismatch")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"schema_version":  1,
		"status":          "ready",
		"run_id":          s.config.RunID,
		"mode":            s.config.Mode,
		"generation":      s.config.Generation,
		"call_cap":        s.config.MaxCalls,
		"accepted_calls":  s.accepted.Load(),
		"rejected_calls":  s.rejected.Load(),
		"dataset_id":      session.FEAT126FixtureDatasetID,
		"fixture_case_id": s.fixture.ID,
		"dataset_sha256":  s.fixture.DatasetSHA256,
	})
}

func (s *Server) responses(w http.ResponseWriter, request *http.Request) {
	if !s.authorized(request) || request.Header.Get("Authorization") != "" {
		s.reject(w, http.StatusForbidden, "request_not_authorized")
		return
	}
	if mediaType := strings.ToLower(strings.TrimSpace(strings.Split(request.Header.Get("Content-Type"), ";")[0])); mediaType != "application/json" {
		s.reject(w, http.StatusUnsupportedMediaType, "content_type_invalid")
		return
	}
	request.Body = http.MaxBytesReader(w, request.Body, s.config.MaxRequestBytes)
	var body struct {
		Model  string          `json:"model"`
		Input  json.RawMessage `json:"input"`
		Stream bool            `json:"stream"`
	}
	decoder := json.NewDecoder(request.Body)
	if err := decoder.Decode(&body); err != nil {
		var maxBytesError *http.MaxBytesError
		if errors.As(err, &maxBytesError) {
			s.reject(w, http.StatusRequestEntityTooLarge, "request_too_large")
			return
		}
		s.reject(w, http.StatusBadRequest, "request_invalid")
		return
	}
	if decoder.Decode(&struct{}{}) == nil {
		s.reject(w, http.StatusBadRequest, "request_invalid")
		return
	}
	if body.Model != codex.MiniMaxModel || !body.Stream || !validInputCategory(body.Input) {
		s.reject(w, http.StatusBadRequest, "request_shape_invalid")
		return
	}
	call := s.accepted.Add(1)
	if call > s.config.MaxCalls {
		s.accepted.Add(^uint64(0))
		s.reject(w, http.StatusTooManyRequests, "call_limit_exceeded")
		return
	}
	if s.config.Mode == ModeHTTPError {
		s.reject(w, http.StatusServiceUnavailable, "synthetic_unavailable")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)
	responseID := fmt.Sprintf("resp-feat126-%02d", call)
	writeSSE(w, "response.created", map[string]any{"type": "response.created", "response": map[string]any{"id": responseID}})
	if s.config.Mode == ModeDisconnect {
		return
	}
	if s.config.Mode == ModeOversize {
		writeSSE(w, "response.output_text.delta", map[string]any{"type": "response.output_text.delta", "delta": strings.Repeat("x", 5<<20)})
		return
	}
	reasoningID := fmt.Sprintf("reasoning-%02d", call)
	writeSSE(w, "response.output_item.added", map[string]any{
		"type": "response.output_item.added", "item": map[string]any{"type": "reasoning", "id": reasoningID, "summary": []any{}},
	})
	writeSSE(w, "response.reasoning_text.delta", map[string]any{
		"type": "response.reasoning_text.delta", "item_id": reasoningID, "content_index": 0, "delta": s.fixture.RawText,
	})
	if s.config.Mode == ModeIncomplete && !waitForRequest(request, incompleteReasoningSettleDelay) {
		return
	}
	writeSSE(w, "response.output_item.done", map[string]any{
		"type": "response.output_item.done", "item": map[string]any{
			"type": "reasoning", "id": reasoningID, "summary": []any{},
			"content": []any{map[string]any{"type": "reasoning_text", "text": s.fixture.RawText}},
		},
	})
	if s.config.Mode == ModeIncomplete {
		if !waitForRequest(request, incompleteInterruptWindow) {
			return
		}
		writeSSE(w, "response.incomplete", map[string]any{
			"type": "response.incomplete", "response": map[string]any{"id": responseID, "incomplete_details": map[string]any{"reason": "synthetic_limit"}},
		})
		return
	}
	messageID := fmt.Sprintf("message-%02d", call)
	writeSSE(w, "response.output_item.added", map[string]any{
		"type": "response.output_item.added", "item": map[string]any{"type": "message", "role": "assistant", "id": messageID, "content": []any{}},
	})
	writeSSE(w, "response.output_text.delta", map[string]any{"type": "response.output_text.delta", "item_id": messageID, "content_index": 0, "delta": s.fixture.AssistantText})
	writeSSE(w, "response.output_item.done", map[string]any{
		"type": "response.output_item.done", "item": map[string]any{
			"type": "message", "role": "assistant", "id": messageID,
			"content": []any{map[string]any{"type": "output_text", "text": s.fixture.AssistantText}},
		},
	})
	writeSSE(w, "response.completed", map[string]any{
		"type": "response.completed", "response": map[string]any{
			"id": responseID, "usage": map[string]any{"input_tokens": 0, "input_tokens_details": nil, "output_tokens": 0, "output_tokens_details": nil, "total_tokens": 0},
		},
	})
}

func waitForRequest(request *http.Request, duration time.Duration) bool {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-request.Context().Done():
		return false
	case <-timer.C:
		return true
	}
}

func (s *Server) authorized(request *http.Request) bool {
	return request.Header.Get(RunIDHeader) == s.config.RunID && request.Header.Get(FixtureIDHeader) == s.config.FixtureID
}

func (s *Server) reject(w http.ResponseWriter, status int, failure string) {
	s.rejected.Add(1)
	writeJSON(w, status, map[string]any{"error": map[string]string{"code": failure}})
}

func validInputCategory(input json.RawMessage) bool {
	if len(input) == 0 || len(input) > 1<<20 {
		return false
	}
	var array []json.RawMessage
	if json.Unmarshal(input, &array) == nil {
		return len(array) > 0 && len(array) <= 128
	}
	var text string
	return json.Unmarshal(input, &text) == nil && strings.TrimSpace(text) != ""
}

func writeSSE(w http.ResponseWriter, event string, data any) {
	encoded, err := json.Marshal(data)
	if err != nil {
		return
	}
	_, _ = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, encoded)
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
