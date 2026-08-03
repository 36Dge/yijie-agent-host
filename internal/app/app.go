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
	agenthostcontract "github.com/36Dge/yijie-agent-host/internal/contracts"
	"github.com/36Dge/yijie-agent-host/internal/security"
	"github.com/36Dge/yijie-agent-host/internal/session"
	"github.com/google/uuid"
)

const (
	ServiceName                 = "yijie-agent-host"
	defaultSSEHeartbeatInterval = 15 * time.Second
)

type Config struct {
	Environment           string
	Port                  string
	HostHome              string
	Runtime               codex.Config
	RawReasoningV2Enabled bool
	TitleV2Enabled        bool
	CleanupV2Enabled      bool
	InstanceNonce         string
}

type RuntimeStatusProvider interface {
	Snapshot() codex.Status
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
	rawV2, err := boolEnv("YIJIE_AGENT_HOST_V2_RAW_REASONING_ENABLED", false)
	if err != nil {
		return Config{}, err
	}
	titleV2, err := boolEnv("YIJIE_AGENT_HOST_V2_TITLE_ENABLED", false)
	if err != nil {
		return Config{}, err
	}
	cleanupV2, err := boolEnv("YIJIE_AGENT_HOST_V2_CLEANUP_ENABLED", false)
	if err != nil {
		return Config{}, err
	}
	instanceNonce := strings.TrimSpace(os.Getenv("YIJIE_AGENT_HOST_INSTANCE_NONCE"))
	if instanceNonce != "" {
		parsed, parseErr := uuid.Parse(instanceNonce)
		if parseErr != nil || parsed == uuid.Nil || parsed.String() != instanceNonce {
			return Config{}, errors.New("YIJIE_AGENT_HOST_INSTANCE_NONCE must be a canonical non-zero UUID")
		}
	}

	hostHome := os.Getenv("YIJIE_AGENT_HOST_HOME")
	environment := env("YIJIE_ENV", "local")
	if (rawV2 || titleV2 || cleanupV2) && (environment != "local" || hostHome == "" || !filepath.IsAbs(hostHome)) {
		return Config{}, errors.New("Agent Host v2 draft capabilities require an absolute Host home in the local environment")
	}
	if titleV2 && !runtimeConfig.MiniMax.Enabled {
		return Config{}, errors.New("Agent Host v2 title generation requires the configured pinned model provider")
	}
	if titleV2 {
		return Config{}, errors.New("Agent Host v2 title generation remains disabled because the pinned Runtime cannot capability-disable tools")
	}
	if runtimeConfig.MiniMax.Enabled {
		if hostHome == "" || !filepath.IsAbs(hostHome) {
			return Config{}, errors.New("YIJIE_AGENT_HOST_HOME must be absolute when MiniMax is enabled")
		}
		if runtimeConfig.CodexHome != "" && filepath.Clean(hostHome) == filepath.Clean(runtimeConfig.CodexHome) {
			return Config{}, errors.New("YIJIE_AGENT_HOST_HOME and YIJIE_CODEX_HOME must be separate directories")
		}
	}

	return Config{
		Environment:           environment,
		Port:                  env("YIJIE_AGENT_HOST_PORT", "18080"),
		HostHome:              hostHome,
		Runtime:               runtimeConfig,
		RawReasoningV2Enabled: rawV2,
		TitleV2Enabled:        titleV2,
		CleanupV2Enabled:      cleanupV2,
		InstanceNonce:         instanceNonce,
	}, nil
}

type SessionService interface {
	StartSession(context.Context, session.StartSessionInput) (session.Record, error)
	ResumeSession(context.Context, string, session.TraceContext) (session.Record, error)
	GetSession(string) (session.Record, error)
	StartTurn(context.Context, session.StartTurnInput) (codex.TurnInfo, error)
	InterruptTurn(context.Context, string, string, session.TraceContext) error
	SubscribeEvents(string, string, uint64) (string, []session.Event, <-chan session.Event, func(), error)
	SubscribeEventsV2(string, string, uint64) (string, []session.Event, <-chan session.Event, func(), error)
	GenerateTitle(context.Context, string, string, string) (session.TitleResult, error)
	CleanupSession(context.Context, string, string) (session.CleanupResult, error)
}

func NewHandler(config Config, runtime RuntimeStatusProvider, sessions SessionService, apiToken string) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		setInstanceNonceHeader(w, config.InstanceNonce)
		writeJSON(w, http.StatusOK, agenthostcontract.HealthResponse{
			Service: agenthostcontract.HealthResponseServiceYijieAgentHost,
			Status:  agenthostcontract.HealthResponseStatusOk,
		})
	})
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, _ *http.Request) {
		setInstanceNonceHeader(w, config.InstanceNonce)
		status := runtime.Snapshot()
		if !status.Ready {
			writeJSON(w, http.StatusServiceUnavailable, agenthostcontract.NotReadyResponse{
				Status:       agenthostcontract.NotReady,
				RuntimeState: agenthostcontract.RuntimeState(status.State),
			})
			return
		}
		writeJSON(w, http.StatusOK, agenthostcontract.ReadyResponse{
			Status:       agenthostcontract.ReadyResponseStatusReady,
			RuntimeState: agenthostcontract.ReadyResponseRuntimeStateReady,
		})
	})
	mux.HandleFunc("GET /v1/status", func(w http.ResponseWriter, _ *http.Request) {
		runtimeStatus := runtime.Snapshot()
		serviceStatus := agenthostcontract.AgentHostStatusStatusDegraded
		if runtimeStatus.Ready {
			serviceStatus = agenthostcontract.AgentHostStatusStatusOk
		}
		writeJSON(w, http.StatusOK, agenthostcontract.AgentHostStatus{
			Service:     agenthostcontract.AgentHostStatusServiceYijieAgentHost,
			Status:      serviceStatus,
			Environment: config.Environment,
			RuntimeMode: agenthostcontract.ManagedStdio,
			Runtime:     runtimeStatusView(runtimeStatus),
		})
	})
	if sessions != nil {
		handler := &sessionHandler{
			service: sessions, apiToken: apiToken, heartbeatInterval: defaultSSEHeartbeatInterval,
		}
		mux.Handle("POST /v1/tasks/{task_id}/agent-sessions", handler.authorize(http.HandlerFunc(handler.startSession)))
		mux.Handle("POST /v1/agent-sessions/{agent_session_id}/resume", handler.authorize(http.HandlerFunc(handler.resumeSession)))
		mux.Handle("GET /v1/agent-sessions/{agent_session_id}", handler.authorize(http.HandlerFunc(handler.getSession)))
		mux.Handle("POST /v1/agent-sessions/{agent_session_id}/turns", handler.authorize(http.HandlerFunc(handler.startTurn)))
		mux.Handle("POST /v1/agent-sessions/{agent_session_id}/turns/{turn_id}/interrupt", handler.authorize(http.HandlerFunc(handler.interruptTurn)))
		mux.Handle("GET /v1/agent-sessions/{agent_session_id}/events", handler.authorize(http.HandlerFunc(handler.events)))
		if config.RawReasoningV2Enabled {
			mux.Handle("GET /v2/agent-sessions/{agent_session_id}/events", handler.authorize(http.HandlerFunc(handler.eventsV2)))
		}
		if config.TitleV2Enabled {
			mux.Handle("POST /v2/agent-sessions/{agent_session_id}/title-generations", handler.authorize(http.HandlerFunc(handler.generateTitleV2)))
		}
		if config.CleanupV2Enabled {
			mux.Handle("POST /v2/agent-sessions/{agent_session_id}/cleanup-operations", handler.authorize(http.HandlerFunc(handler.cleanupV2)))
		}
	}
	return mux
}

func setInstanceNonceHeader(w http.ResponseWriter, nonce string) {
	if nonce != "" {
		w.Header().Set("X-Yijie-Host-Instance-Nonce", nonce)
	}
}

type sessionHandler struct {
	service           SessionService
	apiToken          string
	heartbeatInterval time.Duration
}

func (h *sessionHandler) authorize(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !security.TokenMatches(h.apiToken, r.Header.Get("Authorization")) {
			writeAPIError(w, http.StatusUnauthorized, agenthostcontract.ErrorResponseErrorCodeUnauthorized, "valid Agent Host bearer token required")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (h *sessionHandler) startSession(w http.ResponseWriter, r *http.Request) {
	var request agenthostcontract.StartSessionRequest
	if err := decodeRequest(w, r, &request); err != nil {
		writeAPIError(w, http.StatusBadRequest, agenthostcontract.ErrorResponseErrorCodeInvalidRequest, "request body is invalid")
		return
	}
	record, err := h.service.StartSession(r.Context(), session.StartSessionInput{
		TaskID: r.PathValue("task_id"),
		Cwd:    request.Cwd,
		Trace:  traceContext(request.TraceId, request.RequestId, request.TenantId, request.UserId),
	})
	if err != nil {
		writeSessionError(w, err)
		return
	}
	writeSessionResponse(w, http.StatusCreated, record)
}

func (h *sessionHandler) resumeSession(w http.ResponseWriter, r *http.Request) {
	var request agenthostcontract.TraceRequest
	if err := decodeRequest(w, r, &request); err != nil {
		writeAPIError(w, http.StatusBadRequest, agenthostcontract.ErrorResponseErrorCodeInvalidRequest, "request body is invalid")
		return
	}
	record, err := h.service.ResumeSession(
		r.Context(),
		r.PathValue("agent_session_id"),
		traceContext(request.TraceId, request.RequestId, request.TenantId, request.UserId),
	)
	if err != nil {
		writeSessionError(w, err)
		return
	}
	writeSessionResponse(w, http.StatusOK, record)
}

func (h *sessionHandler) getSession(w http.ResponseWriter, r *http.Request) {
	record, err := h.service.GetSession(r.PathValue("agent_session_id"))
	if err != nil {
		writeSessionError(w, err)
		return
	}
	writeSessionResponse(w, http.StatusOK, record)
}

func (h *sessionHandler) startTurn(w http.ResponseWriter, r *http.Request) {
	var request agenthostcontract.StartTurnRequest
	if err := decodeRequest(w, r, &request); err != nil {
		writeAPIError(w, http.StatusBadRequest, agenthostcontract.ErrorResponseErrorCodeInvalidRequest, "request body is invalid")
		return
	}
	turn, err := h.service.StartTurn(r.Context(), session.StartTurnInput{
		AgentSessionID:  r.PathValue("agent_session_id"),
		Input:           request.Input,
		ReasoningEffort: reasoningEffort(request.ReasoningEffort),
		Trace:           traceContext(request.TraceId, request.RequestId, request.TenantId, request.UserId),
	})
	if err != nil {
		writeSessionError(w, err)
		return
	}
	turnID, err := uuid.Parse(turn.ID)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, agenthostcontract.ErrorResponseErrorCodeInternalError, "Agent Host operation failed")
		return
	}
	writeJSON(w, http.StatusAccepted, agenthostcontract.StartTurnResponse{TurnId: turnID})
}

func (h *sessionHandler) interruptTurn(w http.ResponseWriter, r *http.Request) {
	var request agenthostcontract.TraceRequest
	if err := decodeRequest(w, r, &request); err != nil {
		writeAPIError(w, http.StatusBadRequest, agenthostcontract.ErrorResponseErrorCodeInvalidRequest, "request body is invalid")
		return
	}
	if err := h.service.InterruptTurn(
		r.Context(),
		r.PathValue("agent_session_id"),
		r.PathValue("turn_id"),
		traceContext(request.TraceId, request.RequestId, request.TenantId, request.UserId),
	); err != nil {
		writeSessionError(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusNoContent)
}

func (h *sessionHandler) events(w http.ResponseWriter, r *http.Request) {
	h.streamEvents(w, r, h.service.SubscribeEvents)
}

func (h *sessionHandler) eventsV2(w http.ResponseWriter, r *http.Request) {
	if values := r.URL.Query()["event_schema_version"]; len(values) != 1 || values[0] != "2" {
		writeAPIError(w, http.StatusBadRequest, agenthostcontract.ErrorResponseErrorCodeInvalidEventCursor, "event_schema_version=2 is required")
		return
	}
	w.Header().Set("X-Yijie-Event-Schema-Version", "2")
	h.streamEvents(w, r, h.service.SubscribeEventsV2)
}

type eventSubscriber func(string, string, uint64) (string, []session.Event, <-chan session.Event, func(), error)

func (h *sessionHandler) streamEvents(w http.ResponseWriter, r *http.Request, subscribe eventSubscriber) {
	streamID, after, err := eventCursor(r)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, agenthostcontract.ErrorResponseErrorCodeInvalidEventCursor, "event cursor is invalid")
		return
	}
	actualStreamID, replay, updates, cancel, err := subscribe(
		r.PathValue("agent_session_id"), streamID, after,
	)
	if err != nil {
		writeSessionError(w, err)
		return
	}
	defer cancel()
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeAPIError(w, http.StatusInternalServerError, agenthostcontract.ErrorResponseErrorCodeStreamingUnsupported, "streaming is unavailable")
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
	heartbeatInterval := h.heartbeatInterval
	if heartbeatInterval <= 0 {
		heartbeatInterval = defaultSSEHeartbeatInterval
	}
	heartbeat := time.NewTimer(heartbeatInterval)
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
			resetTimer(heartbeat, heartbeatInterval)
		case <-heartbeat.C:
			if _, err := io.WriteString(w, ": heartbeat\n\n"); err != nil {
				return
			}
			flusher.Flush()
			heartbeat.Reset(heartbeatInterval)
		case <-r.Context().Done():
			return
		}
	}
}

func (h *sessionHandler) generateTitleV2(w http.ResponseWriter, r *http.Request) {
	var request agenthostcontract.GenerateTitleV2Request
	if err := decodeRequest(w, r, &request); err != nil {
		writeAPIError(w, http.StatusBadRequest, agenthostcontract.ErrorResponseErrorCodeInvalidRequest, "request body is invalid")
		return
	}
	result, err := h.service.GenerateTitle(r.Context(), r.PathValue("agent_session_id"), request.OperationId.String(), request.Input)
	if err != nil {
		switch {
		case errors.Is(err, session.ErrInvalidArgument):
			writeAPIError(w, http.StatusBadRequest, agenthostcontract.ErrorResponseErrorCodeInvalidRequest, "request parameters are invalid")
		case errors.Is(err, session.ErrNotFound):
			writeAPIError(w, http.StatusNotFound, agenthostcontract.ErrorResponseErrorCodeSessionNotFound, "agent session was not found")
		case errors.Is(err, session.ErrTitleOperationConflict):
			writeNestedError(w, http.StatusConflict, "title_operation_conflict", "title operation conflicts with an existing input")
		case errors.Is(err, session.ErrTitleOutputInvalid):
			writeNestedError(w, http.StatusUnprocessableEntity, "title_output_invalid", "title output is invalid")
		default:
			writeNestedError(w, http.StatusServiceUnavailable, "title_generation_unavailable", "title generation is unavailable")
		}
		return
	}
	operationID, err := uuid.Parse(result.OperationID)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, agenthostcontract.ErrorResponseErrorCodeInternalError, "Agent Host operation failed")
		return
	}
	writeJSON(w, http.StatusOK, agenthostcontract.GenerateTitleV2Response{OperationId: operationID, Title: result.Title})
}

func (h *sessionHandler) cleanupV2(w http.ResponseWriter, r *http.Request) {
	var request agenthostcontract.CleanupAgentSessionV2Request
	if err := decodeRequest(w, r, &request); err != nil {
		writeAPIError(w, http.StatusBadRequest, agenthostcontract.ErrorResponseErrorCodeInvalidRequest, "request body is invalid")
		return
	}
	result, err := h.service.CleanupSession(r.Context(), r.PathValue("agent_session_id"), request.OperationId.String())
	if err != nil {
		if errors.Is(err, session.ErrCleanupConflict) {
			writeNestedError(w, http.StatusConflict, "cleanup_operation_conflict", "cleanup operation conflicts with another session")
			return
		}
		writeSessionError(w, err)
		return
	}
	operationID, err := uuid.Parse(result.OperationID)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, agenthostcontract.ErrorResponseErrorCodeInternalError, "Agent Host operation failed")
		return
	}
	if result.Outcome == "complete" {
		writeJSON(w, http.StatusOK, agenthostcontract.CleanupAgentSessionV2CompletedResponse{
			OperationId: operationID, Outcome: agenthostcontract.CleanupAgentSessionV2CompletedResponseOutcomeComplete,
			Surfaces: agenthostcontract.CleanupCompletedSurfaces{
				RuntimeThreadTree: agenthostcontract.CleanupCompletedSurfacesRuntimeThreadTreeComplete,
				HostMapping:       agenthostcontract.CleanupCompletedSurfacesHostMappingComplete,
				HostReplay:        agenthostcontract.CleanupCompletedSurfacesHostReplayComplete,
			},
		})
		return
	}
	writeJSON(w, http.StatusConflict, agenthostcontract.CleanupAgentSessionV2IncompleteResponse{
		OperationId: operationID, Outcome: agenthostcontract.CleanupAgentSessionV2IncompleteResponseOutcomeIncomplete,
		Surfaces: agenthostcontract.CleanupIncompleteSurfaces{
			RuntimeThreadTree: agenthostcontract.CleanupSurfaceStatus(result.RuntimeThreadTree),
			HostMapping:       agenthostcontract.CleanupSurfaceStatus(result.HostMapping),
			HostReplay:        agenthostcontract.CleanupSurfaceStatus(result.HostReplay),
		},
		Error: agenthostcontract.CleanupIncompleteError{
			Code:       agenthostcontract.CleanupIncomplete,
			ReasonCode: agenthostcontract.CleanupIncompleteErrorReasonCode(result.ReasonCode),
			Message:    "agent session cleanup is incomplete",
		},
	})
}

func resetTimer(timer *time.Timer, interval time.Duration) {
	if !timer.Stop() {
		select {
		case <-timer.C:
		default:
		}
	}
	timer.Reset(interval)
}

func traceContext(traceID, requestID, tenantID, userID *string) session.TraceContext {
	return session.TraceContext{
		TraceID: stringValue(traceID), RequestID: stringValue(requestID),
		TenantID: stringValue(tenantID), UserID: stringValue(userID),
	}
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func reasoningEffort(value *agenthostcontract.StartTurnRequestReasoningEffort) string {
	if value == nil {
		return ""
	}
	return string(*value)
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
	fromLastEventID := false
	if lastEventID := r.Header.Get("Last-Event-ID"); lastEventID != "" {
		parts := strings.Split(lastEventID, ":")
		if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
			return "", 0, errors.New("invalid Last-Event-ID")
		}
		if parts[1][0] == '0' {
			return "", 0, errors.New("invalid Last-Event-ID sequence")
		}
		streamID = parts[0]
		afterText = parts[1]
		fromLastEventID = true
	}
	if afterText == "" {
		return streamID, 0, nil
	}
	after, err := strconv.ParseUint(afterText, 10, 64)
	if err != nil || (fromLastEventID && after == 0) || (after > 0 && streamID == "") {
		return "", 0, errors.New("invalid event cursor")
	}
	return streamID, after, nil
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

func writeSessionResponse(w http.ResponseWriter, status int, record session.Record) {
	response, err := sessionResponse(record)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, agenthostcontract.ErrorResponseErrorCodeInternalError, "Agent Host operation failed")
		return
	}
	writeJSON(w, status, response)
}

func sessionResponse(record session.Record) (agenthostcontract.SessionResponse, error) {
	taskID, err := uuid.Parse(record.TaskID)
	if err != nil {
		return agenthostcontract.SessionResponse{}, fmt.Errorf("invalid persisted task id: %w", err)
	}
	agentSessionID, err := uuid.Parse(record.AgentSessionID)
	if err != nil {
		return agenthostcontract.SessionResponse{}, fmt.Errorf("invalid persisted agent session id: %w", err)
	}
	return agenthostcontract.SessionResponse{Session: agenthostcontract.AgentSession{
		TaskId:         taskID,
		AgentSessionId: agentSessionID,
		CodexThreadId:  record.CodexThreadID,
		ActiveTurnId:   record.ActiveTurnID,
		State:          agenthostcontract.AgentSessionState(record.State),
		Cwd:            record.Cwd,
		Model:          agenthostcontract.AgentSessionModel(record.Model),
		ModelProvider:  agenthostcontract.AgentSessionModelProvider(record.ModelProvider),
		FailureCode:    agenthostcontract.AgentSessionFailureCode(record.FailureCode),
		CreatedAt:      record.CreatedAt,
		UpdatedAt:      record.UpdatedAt,
	}}, nil
}

func runtimeStatusView(status codex.Status) agenthostcontract.RuntimeStatus {
	view := agenthostcontract.RuntimeStatus{
		State:           agenthostcontract.RuntimeState(status.State),
		Ready:           status.Ready,
		Transport:       agenthostcontract.RuntimeStatusTransport(status.Transport),
		ExperimentalApi: agenthostcontract.RuntimeStatusExperimentalApi(status.ExperimentalAPI),
	}
	if status.RuntimeVersion != "" {
		value := agenthostcontract.RuntimeStatusRuntimeVersion(status.RuntimeVersion)
		view.RuntimeVersion = &value
	}
	if status.UpstreamTag != "" {
		value := agenthostcontract.RuntimeStatusUpstreamTag(status.UpstreamTag)
		view.UpstreamTag = &value
	}
	if status.UpstreamCommit != "" {
		value := agenthostcontract.RuntimeStatusUpstreamCommit(status.UpstreamCommit)
		view.UpstreamCommit = &value
	}
	if status.FailureCode != "" {
		value := agenthostcontract.RuntimeStatusFailureCode(status.FailureCode)
		view.FailureCode = &value
	}
	if status.ModelProvider != "" {
		value := agenthostcontract.RuntimeStatusModelProvider(status.ModelProvider)
		view.ModelProvider = &value
	}
	if status.Model != "" {
		value := agenthostcontract.RuntimeStatusModel(status.Model)
		view.Model = &value
	}
	return view
}

func writeSessionError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, session.ErrNotFound):
		writeAPIError(w, http.StatusNotFound, agenthostcontract.ErrorResponseErrorCodeSessionNotFound, "agent session was not found")
	case errors.Is(err, session.ErrTaskExists):
		writeAPIError(w, http.StatusConflict, agenthostcontract.ErrorResponseErrorCodeTaskSessionExists, "task already has an agent session")
	case errors.Is(err, session.ErrTurnActive):
		writeAPIError(w, http.StatusConflict, agenthostcontract.ErrorResponseErrorCodeTurnActive, "agent session already has an active turn")
	case errors.Is(err, session.ErrTurnNotActive):
		writeAPIError(w, http.StatusConflict, agenthostcontract.ErrorResponseErrorCodeTurnNotActive, "turn is not active for this agent session")
	case errors.Is(err, session.ErrSessionNotUsable):
		writeAPIError(w, http.StatusConflict, agenthostcontract.ErrorResponseErrorCodeSessionNotUsable, "agent session cannot perform this operation")
	case errors.Is(err, session.ErrStreamChanged):
		writeAPIError(w, http.StatusConflict, agenthostcontract.ErrorResponseErrorCodeEventStreamChanged, "event stream changed after Host restart")
	case errors.Is(err, session.ErrReplayUnavailable):
		writeAPIError(w, http.StatusConflict, agenthostcontract.ErrorResponseErrorCodeEventReplayUnavailable, "requested events are no longer available")
	case errors.Is(err, session.ErrInvalidSequence):
		writeAPIError(w, http.StatusBadRequest, agenthostcontract.ErrorResponseErrorCodeInvalidEventCursor, "event cursor is invalid")
	case errors.Is(err, session.ErrInvalidArgument):
		writeAPIError(w, http.StatusBadRequest, agenthostcontract.ErrorResponseErrorCodeInvalidRequest, "request parameters are invalid")
	case errors.Is(err, session.ErrRuntimeRequest):
		writeAPIError(w, http.StatusBadGateway, agenthostcontract.ErrorResponseErrorCodeRuntimeRequestFailed, "Codex Runtime request failed")
	default:
		writeAPIError(w, http.StatusInternalServerError, agenthostcontract.ErrorResponseErrorCodeInternalError, "Agent Host operation failed")
	}
}

func writeAPIError(w http.ResponseWriter, status int, code agenthostcontract.ErrorResponseErrorCode, message string) {
	var response agenthostcontract.ErrorResponse
	response.Error.Code = code
	response.Error.Message = message
	writeJSON(w, status, response)
}

func writeNestedError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]any{"error": map[string]string{"code": code, "message": message}})
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

func boolEnv(key string, fallback bool) (bool, error) {
	value := os.Getenv(key)
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return false, fmt.Errorf("parse %s: %w", key, err)
	}
	return parsed, nil
}
