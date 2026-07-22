package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/36Dge/yijie-agent-host/internal/codex"
	"github.com/36Dge/yijie-agent-host/internal/security"
	"github.com/36Dge/yijie-agent-host/internal/session"
)

const ServiceName = "yijie-agent-host"

type Config struct {
	Environment string
	Port        string
	HostHome    string
	Runtime     codex.Config
}

type RuntimeStatusProvider interface {
	Snapshot() codex.Status
}

type Status struct {
	Service     string       `json:"service"`
	Status      string       `json:"status"`
	Environment string       `json:"environment"`
	RuntimeMode string       `json:"runtime_mode"`
	Runtime     codex.Status `json:"runtime"`
}

func LoadConfig() (Config, error) {
	runtimeConfig := codex.DefaultConfig()
	runtimeConfig.BinaryPath = os.Getenv("YIJIE_CODEX_BINARY")
	runtimeConfig.ManifestPath = os.Getenv("YIJIE_CODEX_MANIFEST")
	runtimeConfig.CodexHome = os.Getenv("YIJIE_CODEX_HOME")

	provider := os.Getenv("YIJIE_MODEL_PROVIDER")
	miniMaxKey, err := loadMiniMaxAPIKey()
	if err != nil {
		return Config{}, err
	}
	switch provider {
	case "":
		if miniMaxKey != "" {
			return Config{}, errors.New("YIJIE_MODEL_PROVIDER=minimax is required when a MiniMax key is configured")
		}
	case codex.MiniMaxProviderID:
		if miniMaxKey == "" {
			return Config{}, errors.New("MiniMax provider requires YIJIE_MINIMAX_API_KEY or YIJIE_MINIMAX_API_KEY_FILE")
		}
		runtimeConfig.MiniMax = codex.MiniMaxConfig{Enabled: true, APIKey: miniMaxKey}
	default:
		return Config{}, fmt.Errorf("unsupported YIJIE_MODEL_PROVIDER %q", provider)
	}

	if runtimeConfig.StartupTimeout, err = durationEnv("YIJIE_CODEX_STARTUP_TIMEOUT", runtimeConfig.StartupTimeout); err != nil {
		return Config{}, err
	}
	if runtimeConfig.RequestTimeout, err = durationEnv("YIJIE_CODEX_REQUEST_TIMEOUT", runtimeConfig.RequestTimeout); err != nil {
		return Config{}, err
	}
	if runtimeConfig.ShutdownTimeout, err = durationEnv("YIJIE_CODEX_SHUTDOWN_TIMEOUT", runtimeConfig.ShutdownTimeout); err != nil {
		return Config{}, err
	}
	if runtimeConfig.MaxMessageBytes, err = intEnv("YIJIE_CODEX_MAX_MESSAGE_BYTES", runtimeConfig.MaxMessageBytes); err != nil {
		return Config{}, err
	}
	if runtimeConfig.WriteQueueDepth, err = intEnv("YIJIE_CODEX_WRITE_QUEUE_DEPTH", runtimeConfig.WriteQueueDepth); err != nil {
		return Config{}, err
	}
	if runtimeConfig.StderrTailBytes, err = intEnv("YIJIE_CODEX_STDERR_TAIL_BYTES", runtimeConfig.StderrTailBytes); err != nil {
		return Config{}, err
	}

	hostHome := os.Getenv("YIJIE_AGENT_HOST_HOME")
	if runtimeConfig.MiniMax.Enabled {
		if hostHome == "" || !filepath.IsAbs(hostHome) {
			return Config{}, errors.New("YIJIE_AGENT_HOST_HOME must be absolute when MiniMax is enabled")
		}
		if runtimeConfig.CodexHome != "" && filepath.Clean(hostHome) == filepath.Clean(runtimeConfig.CodexHome) {
			return Config{}, errors.New("YIJIE_AGENT_HOST_HOME and YIJIE_CODEX_HOME must be separate directories")
		}
	}

	return Config{
		Environment: env("YIJIE_ENV", "local"),
		Port:        env("YIJIE_AGENT_HOST_PORT", "18080"),
		HostHome:    hostHome,
		Runtime:     runtimeConfig,
	}, nil
}

type SessionService interface {
	StartSession(context.Context, session.StartSessionInput) (session.Record, error)
	ResumeSession(context.Context, string, session.TraceContext) (session.Record, error)
	GetSession(string) (session.Record, error)
	StartTurn(context.Context, session.StartTurnInput) (codex.TurnInfo, error)
	InterruptTurn(context.Context, string, string, session.TraceContext) error
	SubscribeEvents(string, string, uint64) (string, []session.Event, <-chan session.Event, func(), error)
}

func NewHandler(config Config, runtime RuntimeStatusProvider, sessions SessionService, apiToken string) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{
			"service": ServiceName,
			"status":  "ok",
		})
	})
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, _ *http.Request) {
		status := runtime.Snapshot()
		if !status.Ready {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{
				"status":        "not_ready",
				"runtime_state": status.State,
			})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{
			"status":        "ready",
			"runtime_state": status.State,
		})
	})
	mux.HandleFunc("GET /v1/status", func(w http.ResponseWriter, _ *http.Request) {
		runtimeStatus := runtime.Snapshot()
		serviceStatus := "degraded"
		if runtimeStatus.Ready {
			serviceStatus = "ok"
		}
		writeJSON(w, http.StatusOK, Status{
			Service:     ServiceName,
			Status:      serviceStatus,
			Environment: config.Environment,
			RuntimeMode: "managed-stdio",
			Runtime:     runtimeStatus,
		})
	})
	if sessions != nil {
		handler := &sessionHandler{service: sessions, apiToken: apiToken}
		mux.Handle("POST /v1/tasks/{task_id}/agent-sessions", handler.authorize(http.HandlerFunc(handler.startSession)))
		mux.Handle("POST /v1/agent-sessions/{agent_session_id}/resume", handler.authorize(http.HandlerFunc(handler.resumeSession)))
		mux.Handle("GET /v1/agent-sessions/{agent_session_id}", handler.authorize(http.HandlerFunc(handler.getSession)))
		mux.Handle("POST /v1/agent-sessions/{agent_session_id}/turns", handler.authorize(http.HandlerFunc(handler.startTurn)))
		mux.Handle("POST /v1/agent-sessions/{agent_session_id}/turns/{turn_id}/interrupt", handler.authorize(http.HandlerFunc(handler.interruptTurn)))
		mux.Handle("GET /v1/agent-sessions/{agent_session_id}/events", handler.authorize(http.HandlerFunc(handler.events)))
	}
	return mux
}

type sessionHandler struct {
	service  SessionService
	apiToken string
}

type traceRequest struct {
	TraceID   string `json:"trace_id,omitempty"`
	RequestID string `json:"request_id,omitempty"`
	TenantID  string `json:"tenant_id,omitempty"`
	UserID    string `json:"user_id,omitempty"`
}

func (h *sessionHandler) authorize(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !security.TokenMatches(h.apiToken, r.Header.Get("Authorization")) {
			writeAPIError(w, http.StatusUnauthorized, "unauthorized", "valid Agent Host bearer token required")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (h *sessionHandler) startSession(w http.ResponseWriter, r *http.Request) {
	var request struct {
		traceRequest
		Cwd string `json:"cwd"`
	}
	if err := decodeRequest(w, r, &request); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_request", "request body is invalid")
		return
	}
	record, err := h.service.StartSession(r.Context(), session.StartSessionInput{
		TaskID: r.PathValue("task_id"),
		Cwd:    request.Cwd,
		Trace:  request.traceRequest.context(),
	})
	if err != nil {
		writeSessionError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"session": sessionView(record)})
}

func (h *sessionHandler) resumeSession(w http.ResponseWriter, r *http.Request) {
	var request traceRequest
	if err := decodeRequest(w, r, &request); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_request", "request body is invalid")
		return
	}
	record, err := h.service.ResumeSession(
		r.Context(),
		r.PathValue("agent_session_id"),
		request.context(),
	)
	if err != nil {
		writeSessionError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"session": sessionView(record)})
}

func (h *sessionHandler) getSession(w http.ResponseWriter, r *http.Request) {
	record, err := h.service.GetSession(r.PathValue("agent_session_id"))
	if err != nil {
		writeSessionError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"session": sessionView(record)})
}

func (h *sessionHandler) startTurn(w http.ResponseWriter, r *http.Request) {
	var request struct {
		traceRequest
		Input           string `json:"input"`
		ReasoningEffort string `json:"reasoning_effort,omitempty"`
	}
	if err := decodeRequest(w, r, &request); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_request", "request body is invalid")
		return
	}
	turn, err := h.service.StartTurn(r.Context(), session.StartTurnInput{
		AgentSessionID:  r.PathValue("agent_session_id"),
		Input:           request.Input,
		ReasoningEffort: request.ReasoningEffort,
		Trace:           request.traceRequest.context(),
	})
	if err != nil {
		writeSessionError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]string{"turn_id": turn.ID})
}

func (h *sessionHandler) interruptTurn(w http.ResponseWriter, r *http.Request) {
	var request traceRequest
	if err := decodeRequest(w, r, &request); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_request", "request body is invalid")
		return
	}
	if err := h.service.InterruptTurn(
		r.Context(),
		r.PathValue("agent_session_id"),
		r.PathValue("turn_id"),
		request.context(),
	); err != nil {
		writeSessionError(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusNoContent)
}

func (h *sessionHandler) events(w http.ResponseWriter, r *http.Request) {
	streamID, after, err := eventCursor(r)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_event_cursor", "event cursor is invalid")
		return
	}
	actualStreamID, replay, updates, cancel, err := h.service.SubscribeEvents(
		r.PathValue("agent_session_id"), streamID, after,
	)
	if err != nil {
		writeSessionError(w, err)
		return
	}
	defer cancel()
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeAPIError(w, http.StatusInternalServerError, "streaming_unsupported", "streaming is unavailable")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Accel-Buffering", "no")
	w.Header().Set("X-Yijie-Event-Stream-ID", actualStreamID)
	w.WriteHeader(http.StatusOK)
	for _, event := range replay {
		if err := writeSSEEvent(w, event); err != nil {
			return
		}
	}
	flusher.Flush()
	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()
	for {
		select {
		case event, open := <-updates:
			if !open {
				return
			}
			if err := writeSSEEvent(w, event); err != nil {
				return
			}
			flusher.Flush()
		case <-heartbeat.C:
			if _, err := io.WriteString(w, ": heartbeat\n\n"); err != nil {
				return
			}
			flusher.Flush()
		case <-r.Context().Done():
			return
		}
	}
}

func (r traceRequest) context() session.TraceContext {
	return session.TraceContext{
		TraceID: r.TraceID, RequestID: r.RequestID, TenantID: r.TenantID, UserID: r.UserID,
	}
}

func decodeRequest(w http.ResponseWriter, r *http.Request, destination any) error {
	if contentType := r.Header.Get("Content-Type"); contentType != "" && !strings.HasPrefix(contentType, "application/json") {
		return errors.New("Content-Type must be application/json")
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("request body must contain one JSON value")
	}
	return nil
}

func eventCursor(r *http.Request) (string, uint64, error) {
	streamID := r.URL.Query().Get("stream_id")
	afterText := r.URL.Query().Get("after")
	if lastEventID := r.Header.Get("Last-Event-ID"); lastEventID != "" {
		parts := strings.Split(lastEventID, ":")
		if len(parts) != 2 {
			return "", 0, errors.New("invalid Last-Event-ID")
		}
		streamID = parts[0]
		afterText = parts[1]
	}
	if afterText == "" {
		return streamID, 0, nil
	}
	after, err := strconv.ParseUint(afterText, 10, 64)
	return streamID, after, err
}

func writeSSEEvent(w io.Writer, event session.Event) error {
	data, err := json.Marshal(event)
	if err != nil {
		return err
	}
	if bytes.ContainsAny(data, "\r\n") {
		return errors.New("encoded SSE event contains a line break")
	}
	_, err = fmt.Fprintf(w, "id: %s:%d\nevent: %s\ndata: %s\n\n", event.StreamID, event.Sequence, event.EventType, data)
	return err
}

func sessionView(record session.Record) map[string]any {
	return map[string]any{
		"task_id":          record.TaskID,
		"agent_session_id": record.AgentSessionID,
		"codex_thread_id":  record.CodexThreadID,
		"active_turn_id":   record.ActiveTurnID,
		"state":            record.State,
		"cwd":              record.Cwd,
		"model":            record.Model,
		"model_provider":   record.ModelProvider,
		"failure_code":     record.FailureCode,
		"created_at":       record.CreatedAt,
		"updated_at":       record.UpdatedAt,
	}
}

func writeSessionError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, session.ErrNotFound):
		writeAPIError(w, http.StatusNotFound, "session_not_found", "agent session was not found")
	case errors.Is(err, session.ErrTaskExists):
		writeAPIError(w, http.StatusConflict, "task_session_exists", "task already has an agent session")
	case errors.Is(err, session.ErrTurnActive):
		writeAPIError(w, http.StatusConflict, "turn_active", "agent session already has an active turn")
	case errors.Is(err, session.ErrTurnNotActive):
		writeAPIError(w, http.StatusConflict, "turn_not_active", "turn is not active for this agent session")
	case errors.Is(err, session.ErrSessionNotUsable):
		writeAPIError(w, http.StatusConflict, "session_not_usable", "agent session cannot perform this operation")
	case errors.Is(err, session.ErrStreamChanged):
		writeAPIError(w, http.StatusConflict, "event_stream_changed", "event stream changed after Host restart")
	case errors.Is(err, session.ErrReplayUnavailable):
		writeAPIError(w, http.StatusConflict, "event_replay_unavailable", "requested events are no longer available")
	case errors.Is(err, session.ErrInvalidSequence):
		writeAPIError(w, http.StatusBadRequest, "invalid_event_cursor", "event cursor is invalid")
	case errors.Is(err, session.ErrInvalidArgument):
		writeAPIError(w, http.StatusBadRequest, "invalid_request", "request parameters are invalid")
	case errors.Is(err, session.ErrRuntimeRequest):
		writeAPIError(w, http.StatusBadGateway, "runtime_request_failed", "Codex Runtime request failed")
	default:
		writeAPIError(w, http.StatusInternalServerError, "internal_error", "Agent Host operation failed")
	}
}

func writeAPIError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]any{
		"error": map[string]string{"code": code, "message": message},
	})
}

func loadMiniMaxAPIKey() (string, error) {
	direct := os.Getenv("YIJIE_MINIMAX_API_KEY")
	filePath := os.Getenv("YIJIE_MINIMAX_API_KEY_FILE")
	if direct != "" && filePath != "" {
		return "", errors.New("configure only one MiniMax API key source")
	}
	if direct != "" {
		if strings.TrimSpace(direct) != direct || strings.ContainsAny(direct, "\r\n\x00") {
			return "", errors.New("YIJIE_MINIMAX_API_KEY is invalid")
		}
		return direct, nil
	}
	if filePath == "" {
		return "", nil
	}
	if !filepath.IsAbs(filePath) {
		return "", errors.New("YIJIE_MINIMAX_API_KEY_FILE must be absolute")
	}
	info, err := os.Lstat(filePath)
	if err != nil {
		return "", fmt.Errorf("stat MiniMax API key file: %w", err)
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0 {
		return "", errors.New("MiniMax API key file must be regular and accessible only by its owner")
	}
	content, err := os.ReadFile(filePath)
	if err != nil {
		return "", fmt.Errorf("read MiniMax API key file: %w", err)
	}
	if len(content) > 16<<10 {
		return "", errors.New("MiniMax API key file exceeds 16 KiB")
	}
	key := strings.TrimSpace(string(content))
	if key == "" || strings.ContainsAny(key, "\r\n\x00") {
		return "", errors.New("MiniMax API key file is invalid")
	}
	return key, nil
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		return
	}
}

func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func durationEnv(key string, fallback time.Duration) (time.Duration, error) {
	value := os.Getenv(key)
	if value == "" {
		return fallback, nil
	}
	parsed, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf("parse %s: %w", key, err)
	}
	return parsed, nil
}

func intEnv(key string, fallback int) (int, error) {
	value := os.Getenv(key)
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("parse %s: %w", key, err)
	}
	return parsed, nil
}
