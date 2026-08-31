package app

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/36Dge/yijie-agent-host/internal/codex"
	"github.com/36Dge/yijie-agent-host/internal/session"
)

const (
	feat137AppTaskID       = "019c0123-4567-7abc-8123-456789abcdea"
	feat137AppSessionID    = "019c0123-4567-7abc-8123-456789abcdeb"
	feat137AppThreadID     = "019c0123-4567-7abc-8123-456789abcdec"
	feat137AppTurnID       = "019c0123-4567-7abc-8123-456789abcded"
	feat137AppApprovalID   = "019c0123-4567-7abc-8123-456789abcdee"
	feat137AppDecisionID   = "019c0123-4567-7abc-8123-456789abcdef"
	feat137AppOtherID      = "019fbd88-cbc3-7bf1-934d-7b05cd693f54"
	feat137AppStreamID     = "019fbd88-cbc3-7bf1-934d-7b05cd693f55"
	feat137AppOtherStream  = "019fbd88-cbc3-7bf1-934d-7b05cd693f56"
	feat137AppEventID      = "019fbd88-cbc3-7bf1-934d-7b05cd693f57"
	feat137AppSensitiveTag = "/private/FEAT137-SENSITIVE-CANARY"
)

type feat137AppServiceStub struct {
	SessionService

	streamID          string
	replay            []session.Event
	subscribeErr      error
	subscribeCalls    int
	subscribeFor      string
	subscribeStreamID string
	subscribeAfter    uint64

	snapshot     session.PendingApprovalSnapshot
	pendingErr   error
	pendingCalls int
	pendingFor   string

	decisionResult session.ApprovalDecisionResult
	decisionErr    error
	decisionCalls  int
	decisionFor    string
	approvalFor    string
	decisionInput  session.ApprovalDecisionInput
}

func (s *feat137AppServiceStub) SubscribeEventsV6(
	sessionID, streamID string,
	after uint64,
) (string, []session.Event, <-chan session.Event, func(), error) {
	s.subscribeCalls++
	s.subscribeFor = sessionID
	s.subscribeStreamID = streamID
	s.subscribeAfter = after
	if s.subscribeErr != nil {
		return "", nil, nil, nil, s.subscribeErr
	}
	updates := make(chan session.Event)
	close(updates)
	return s.streamID, append([]session.Event(nil), s.replay...), updates, func() {}, nil
}

func (s *feat137AppServiceStub) PendingApprovals(sessionID string) (session.PendingApprovalSnapshot, error) {
	s.pendingCalls++
	s.pendingFor = sessionID
	return s.snapshot, s.pendingErr
}

func (s *feat137AppServiceStub) DecideApproval(
	_ context.Context,
	sessionID, approvalID string,
	input session.ApprovalDecisionInput,
) (session.ApprovalDecisionResult, error) {
	s.decisionCalls++
	s.decisionFor = sessionID
	s.approvalFor = approvalID
	s.decisionInput = input
	return s.decisionResult, s.decisionErr
}

type feat137NonV6Service struct{ SessionService }

func TestFEAT137V6RoutesRequireExactConjunctionAndCapability(t *testing.T) {
	path := "/v6/agent-sessions/" + feat137AppSessionID + "/approvals/pending"
	tests := []struct {
		name   string
		config Config
	}{
		{name: "all disabled", config: Config{Environment: "local"}},
		{
			name:   "FEAT-137 alone",
			config: Config{Environment: "local", FEAT137CommandApprovalEnabled: true},
		},
		{
			name:   "FEAT-134 missing",
			config: Config{Environment: "local", FEAT136CommandToolItemsEnabled: true, FEAT137CommandApprovalEnabled: true},
		},
		{
			name:   "FEAT-136 missing",
			config: Config{Environment: "local", FEAT134StreamingEnabled: true, FEAT137CommandApprovalEnabled: true},
		},
		{
			name: "non local environment",
			config: Config{
				Environment: "production", FEAT134StreamingEnabled: true,
				FEAT136CommandToolItemsEnabled: true, FEAT137CommandApprovalEnabled: true,
				Runtime: codex.Config{CommandApprovalEnabled: true},
			},
		},
		{
			name: "Runtime approval disabled",
			config: Config{
				Environment: "local", FEAT134StreamingEnabled: true,
				FEAT136CommandToolItemsEnabled: true, FEAT137CommandApprovalEnabled: true,
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			stub := newFEAT137AppServiceStub()
			response := httptest.NewRecorder()
			NewHandler(test.config, staticRuntimeStatus{}, stub, "api-token").ServeHTTP(
				response, authorizedRequest(http.MethodGet, path, ""),
			)
			if response.Code != http.StatusNotFound {
				t.Fatalf("v6 approval route enabled outside exact conjunction: %d %s", response.Code, response.Body.String())
			}
		})
	}

	t.Run("service capability required", func(t *testing.T) {
		response := httptest.NewRecorder()
		NewHandler(feat137ExactHandlerConfig(), staticRuntimeStatus{}, &feat137NonV6Service{}, "api-token").ServeHTTP(
			response, authorizedRequest(http.MethodGet, path, ""),
		)
		if response.Code != http.StatusNotFound {
			t.Fatalf("v6 route enabled without v6 service capability: %d", response.Code)
		}
	})

	t.Run("exact conjunction", func(t *testing.T) {
		stub := newFEAT137AppServiceStub()
		response := httptest.NewRecorder()
		newFEAT137AppHandler(stub).ServeHTTP(response, authorizedRequest(http.MethodGet, path, ""))
		if response.Code != http.StatusOK || stub.pendingCalls != 1 {
			t.Fatalf("exact v6 route unavailable: %d %s calls=%d", response.Code, response.Body.String(), stub.pendingCalls)
		}
	})
}

func TestFEAT137V6EventsRejectAmbiguousQueries(t *testing.T) {
	base := "/v6/agent-sessions/" + feat137AppSessionID + "/events"
	invalidTargets := []string{
		base,
		base + "?event_schema_version=5",
		base + "?event_schema_version=6&event_schema_version=6",
		base + "?event_schema_version=6&after_sequence=1",
		base + "?event_schema_version=6&forbidden=a;b",
		base + "?event_schema_version=6&after=",
		base + "?event_schema_version=6&stream_id=" + feat137AppStreamID + "&stream_id=" + feat137AppOtherStream + "&after=1",
		base + "?event_schema_version=6&stream_id=" + feat137AppStreamID + "&after=1&after=2",
	}
	for _, target := range invalidTargets {
		t.Run(target, func(t *testing.T) {
			stub := newFEAT137AppServiceStub()
			response := httptest.NewRecorder()
			newFEAT137AppHandler(stub).ServeHTTP(response, authorizedRequest(http.MethodGet, target, ""))
			if response.Code != http.StatusBadRequest {
				t.Fatalf("ambiguous v6 query accepted: %d %s", response.Code, response.Body.String())
			}
			if stub.subscribeCalls != 0 {
				t.Fatalf("invalid query reached the event authority: calls=%d", stub.subscribeCalls)
			}
		})
	}

	stub := newFEAT137AppServiceStub()
	stub.replay = []session.Event{validFEAT137AppEvent()}
	response := httptest.NewRecorder()
	newFEAT137AppHandler(stub).ServeHTTP(
		response,
		authorizedRequest(http.MethodGet, base+"?event_schema_version=6", ""),
	)
	if response.Code != http.StatusOK || response.Header().Get("X-Yijie-Event-Schema-Version") != "6" ||
		response.Header().Get("X-Yijie-Event-Stream-ID") != feat137AppStreamID ||
		!strings.Contains(response.Body.String(), `"schema_version":6`) ||
		!strings.Contains(response.Body.String(), `"event_id":"`+feat137AppEventID+`"`) {
		t.Fatalf("unexpected negotiated v6 stream: %d headers=%v body=%s", response.Code, response.Header(), response.Body.String())
	}
}

func TestFEAT137V6EventsNormalizeUUIDsAndRejectAmbiguousHeaders(t *testing.T) {
	base := "/v6/agent-sessions/" + strings.ToUpper(feat137AppSessionID) + "/events"
	t.Run("uppercase query UUIDs are normalized", func(t *testing.T) {
		stub := newFEAT137AppServiceStub()
		response := httptest.NewRecorder()
		newFEAT137AppHandler(stub).ServeHTTP(
			response,
			authorizedRequest(http.MethodGet, base+"?event_schema_version=6&stream_id="+strings.ToUpper(feat137AppStreamID)+"&after=1", ""),
		)
		if response.Code != http.StatusOK || stub.subscribeCalls != 1 || stub.subscribeFor != feat137AppSessionID ||
			stub.subscribeStreamID != feat137AppStreamID || stub.subscribeAfter != 1 {
			t.Fatalf("v6 query UUIDs were not normalized: status=%d body=%s calls=%d session=%q stream=%q after=%d",
				response.Code, response.Body.String(), stub.subscribeCalls, stub.subscribeFor, stub.subscribeStreamID, stub.subscribeAfter)
		}
	})

	t.Run("uppercase Last-Event-ID UUID is normalized and overrides query cursor", func(t *testing.T) {
		stub := newFEAT137AppServiceStub()
		request := authorizedRequest(
			http.MethodGet,
			base+"?event_schema_version=6&stream_id=ignored-by-header&after=ignored-by-header",
			"",
		)
		request.Header.Set("Last-Event-ID", strings.ToUpper(feat137AppStreamID)+":7")
		response := httptest.NewRecorder()
		newFEAT137AppHandler(stub).ServeHTTP(response, request)
		if response.Code != http.StatusOK || stub.subscribeCalls != 1 || stub.subscribeFor != feat137AppSessionID ||
			stub.subscribeStreamID != feat137AppStreamID || stub.subscribeAfter != 7 {
			t.Fatalf("v6 Last-Event-ID was not normalized: status=%d body=%s calls=%d session=%q stream=%q after=%d",
				response.Code, response.Body.String(), stub.subscribeCalls, stub.subscribeFor, stub.subscribeStreamID, stub.subscribeAfter)
		}
	})

	for _, test := range []struct {
		name  string
		setup func(http.Header)
	}{
		{
			name: "duplicate Last-Event-ID",
			setup: func(header http.Header) {
				header.Add("Last-Event-ID", feat137AppStreamID+":1")
				header.Add("Last-Event-ID", feat137AppStreamID+":2")
			},
		},
		{
			name: "zero Last-Event-ID UUID",
			setup: func(header http.Header) {
				header.Set("Last-Event-ID", "00000000-0000-0000-0000-000000000000:1")
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			stub := newFEAT137AppServiceStub()
			request := authorizedRequest(http.MethodGet, base+"?event_schema_version=6", "")
			test.setup(request.Header)
			response := httptest.NewRecorder()
			newFEAT137AppHandler(stub).ServeHTTP(response, request)
			if response.Code != http.StatusBadRequest || stub.subscribeCalls != 0 ||
				!strings.Contains(response.Body.String(), `"code":"invalid_event_cursor"`) {
				t.Fatalf("ambiguous v6 event header accepted: status=%d body=%s calls=%d",
					response.Code, response.Body.String(), stub.subscribeCalls)
			}
		})
	}
}

func TestFEAT137V6GETSurfacesRejectHEADBeforeService(t *testing.T) {
	stub := newFEAT137AppServiceStub()
	handler := newFEAT137AppHandler(stub)

	events := httptest.NewRecorder()
	handler.ServeHTTP(events, authorizedRequest(
		http.MethodHead,
		"/v6/agent-sessions/"+feat137AppSessionID+"/events?event_schema_version=6",
		"",
	))
	if events.Code != http.StatusBadRequest || stub.subscribeCalls != 0 ||
		!strings.Contains(events.Body.String(), `"code":"invalid_request"`) {
		t.Fatalf("HEAD entered v6 event service: status=%d body=%s calls=%d", events.Code, events.Body.String(), stub.subscribeCalls)
	}

	pending := httptest.NewRecorder()
	handler.ServeHTTP(pending, authorizedRequest(
		http.MethodHead,
		"/v6/agent-sessions/"+feat137AppSessionID+"/approvals/pending",
		"",
	))
	assertFEAT137AppError(t, pending, http.StatusBadRequest, session.ApprovalErrorInvalidRequest, "approval request is invalid")
	if stub.pendingCalls != 0 {
		t.Fatalf("HEAD entered v6 pending service: calls=%d", stub.pendingCalls)
	}
}

func TestFEAT137PendingHTTPProjectionIsClosedAndOwnerBound(t *testing.T) {
	path := "/v6/agent-sessions/" + feat137AppSessionID + "/approvals/pending"
	t.Run("valid owner-only projection", func(t *testing.T) {
		stub := newFEAT137AppServiceStub()
		response := httptest.NewRecorder()
		newFEAT137AppHandler(stub).ServeHTTP(response, authorizedRequest(http.MethodGet, path, ""))
		if response.Code != http.StatusOK || stub.pendingCalls != 1 || stub.pendingFor != feat137AppSessionID {
			t.Fatalf("pending projection failed: %d %s calls=%d for=%q", response.Code, response.Body.String(), stub.pendingCalls, stub.pendingFor)
		}
		if response.Header().Get("Cache-Control") != "no-store" || strings.Contains(response.Body.String(), feat137AppSensitiveTag) {
			t.Fatalf("pending response is cacheable or leaked content: headers=%v body=%s", response.Header(), response.Body.String())
		}
		assertOpenAPIJSON(t, "PendingApprovalSnapshotV6", response.Body.Bytes())
	})

	t.Run("query rejected before service", func(t *testing.T) {
		stub := newFEAT137AppServiceStub()
		response := httptest.NewRecorder()
		newFEAT137AppHandler(stub).ServeHTTP(response, authorizedRequest(http.MethodGet, path+"?unknown=1", ""))
		assertFEAT137AppError(t, response, http.StatusBadRequest, session.ApprovalErrorInvalidRequest, "approval request is invalid")
		if stub.pendingCalls != 0 {
			t.Fatalf("invalid pending query reached service: %d", stub.pendingCalls)
		}
	})

	t.Run("malformed query rejected before service", func(t *testing.T) {
		stub := newFEAT137AppServiceStub()
		response := httptest.NewRecorder()
		newFEAT137AppHandler(stub).ServeHTTP(response, authorizedRequest(http.MethodGet, path+"?unknown=1;other=2", ""))
		assertFEAT137AppError(t, response, http.StatusBadRequest, session.ApprovalErrorInvalidRequest, "approval request is invalid")
		if stub.pendingCalls != 0 {
			t.Fatalf("malformed pending query reached service: %d", stub.pendingCalls)
		}
	})

	t.Run("uppercase owner UUID is normalized", func(t *testing.T) {
		stub := newFEAT137AppServiceStub()
		response := httptest.NewRecorder()
		newFEAT137AppHandler(stub).ServeHTTP(
			response,
			authorizedRequest(http.MethodGet, "/v6/agent-sessions/"+strings.ToUpper(feat137AppSessionID)+"/approvals/pending", ""),
		)
		if response.Code != http.StatusOK || stub.pendingCalls != 1 || stub.pendingFor != feat137AppSessionID {
			t.Fatalf("uppercase pending owner UUID was not normalized: status=%d body=%s calls=%d for=%q",
				response.Code, response.Body.String(), stub.pendingCalls, stub.pendingFor)
		}
	})

	t.Run("path identity rejected before service", func(t *testing.T) {
		stub := newFEAT137AppServiceStub()
		response := httptest.NewRecorder()
		newFEAT137AppHandler(stub).ServeHTTP(response, authorizedRequest(http.MethodGet, "/v6/agent-sessions/not-a-uuid/approvals/pending", ""))
		assertFEAT137AppError(t, response, http.StatusBadRequest, session.ApprovalErrorInvalidRequest, "approval request is invalid")
		if stub.pendingCalls != 0 {
			t.Fatalf("invalid pending path reached service: %d", stub.pendingCalls)
		}
	})

	shapeTests := []struct {
		name   string
		mutate func(*session.PendingApprovalSnapshot)
	}{
		{name: "wrong schema", mutate: func(value *session.PendingApprovalSnapshot) { value.SchemaVersion = 5 }},
		{name: "zero stream", mutate: func(value *session.PendingApprovalSnapshot) { value.StreamID = "00000000-0000-0000-0000-000000000000" }},
		{name: "noncanonical internal stream", mutate: func(value *session.PendingApprovalSnapshot) { value.StreamID = strings.ToUpper(value.StreamID) }},
		{name: "more than one pending", mutate: func(value *session.PendingApprovalSnapshot) { value.Pending = append(value.Pending, value.Pending[0]) }},
		{name: "cross-session pending", mutate: func(value *session.PendingApprovalSnapshot) { value.Pending[0].AgentSessionID = feat137AppOtherID }},
		{name: "zero approval identity", mutate: func(value *session.PendingApprovalSnapshot) {
			value.Pending[0].ApprovalRequestID = "00000000-0000-0000-0000-000000000000"
		}},
		{name: "wrong revision", mutate: func(value *session.PendingApprovalSnapshot) { value.Pending[0].Revision = 2 }},
		{name: "empty item identity", mutate: func(value *session.PendingApprovalSnapshot) { value.Pending[0].ItemID = "" }},
		{name: "unknown action", mutate: func(value *session.PendingApprovalSnapshot) { value.Pending[0].ActionID = feat137AppSensitiveTag }},
		{name: "unknown workspace", mutate: func(value *session.PendingApprovalSnapshot) { value.Pending[0].WorkspaceScope = feat137AppSensitiveTag }},
		{name: "wrong TTL", mutate: func(value *session.PendingApprovalSnapshot) { value.Pending[0].TTLSeconds = 121 }},
		{name: "wrong expiry window", mutate: func(value *session.PendingApprovalSnapshot) {
			value.Pending[0].ExpiresAt = value.Pending[0].RequestedAt.Add(119 * time.Second)
		}},
		{name: "snapshot outside live window", mutate: func(value *session.PendingApprovalSnapshot) {
			value.SnapshotAt = value.Pending[0].RequestedAt.Add(-time.Second)
		}},
	}
	for _, test := range shapeTests {
		t.Run(test.name, func(t *testing.T) {
			stub := newFEAT137AppServiceStub()
			test.mutate(&stub.snapshot)
			response := httptest.NewRecorder()
			newFEAT137AppHandler(stub).ServeHTTP(response, authorizedRequest(http.MethodGet, path, ""))
			assertFEAT137AppError(t, response, http.StatusInternalServerError, session.ApprovalErrorInternal, "approval processing failed")
			if strings.Contains(response.Body.String(), feat137AppSensitiveTag) {
				t.Fatalf("invalid internal projection leaked content: %s", response.Body.String())
			}
		})
	}
}

func TestFEAT137DecisionHTTPRejectsIncompleteOrAmbiguousInput(t *testing.T) {
	path := feat137DecisionPath(feat137AppSessionID, feat137AppApprovalID)
	valid := validFEAT137DecisionBody("accept_once")
	tests := []struct {
		name          string
		path          string
		body          string
		contentType   string
		mutateHeaders func(http.Header)
		wantCode      string
	}{
		{name: "empty object", path: path, body: `{}`, wantCode: session.ApprovalErrorInvalidRequest},
		{name: "missing schema", path: path, body: strings.Replace(valid, `"schema_version":6,`, "", 1), wantCode: session.ApprovalErrorInvalidRequest},
		{name: "wrong schema", path: path, body: strings.Replace(valid, `"schema_version":6`, `"schema_version":5`, 1), wantCode: session.ApprovalErrorVersionMismatch},
		{name: "missing decision id", path: path, body: strings.Replace(valid, `"decision_id":"`+feat137AppDecisionID+`",`, "", 1), wantCode: session.ApprovalErrorInvalidRequest},
		{name: "zero decision id", path: path, body: strings.Replace(valid, feat137AppDecisionID, "00000000-0000-0000-0000-000000000000", 1), wantCode: session.ApprovalErrorInvalidRequest},
		{name: "missing stream id", path: path, body: strings.Replace(valid, `"expected_stream_id":"`+feat137AppStreamID+`",`, "", 1), wantCode: session.ApprovalErrorInvalidRequest},
		{name: "zero stream id", path: path, body: strings.Replace(valid, feat137AppStreamID, "00000000-0000-0000-0000-000000000000", 1), wantCode: session.ApprovalErrorInvalidRequest},
		{name: "wrong revision", path: path, body: strings.Replace(valid, `"expected_revision":1`, `"expected_revision":2`, 1), wantCode: session.ApprovalErrorInvalidRequest},
		{name: "unknown decision", path: path, body: strings.Replace(valid, "accept_once", "decline", 1), wantCode: session.ApprovalErrorInvalidRequest},
		{name: "unknown field", path: path, body: strings.TrimSuffix(valid, "}") + `,"command":"` + feat137AppSensitiveTag + `"}`, wantCode: session.ApprovalErrorInvalidRequest},
		{name: "trailing JSON", path: path, body: valid + `{}`, wantCode: session.ApprovalErrorInvalidRequest},
		{name: "wrong content type", path: path, body: valid, contentType: "text/plain", wantCode: session.ApprovalErrorInvalidRequest},
		{
			name: "missing content type", path: path, body: valid, wantCode: session.ApprovalErrorInvalidRequest,
			mutateHeaders: func(header http.Header) { header.Del("Content-Type") },
		},
		{
			name: "duplicate content type", path: path, body: valid, wantCode: session.ApprovalErrorInvalidRequest,
			mutateHeaders: func(header http.Header) { header.Add("Content-Type", "application/json") },
		},
		{name: "query forbidden", path: path + "?retry=true", body: valid, wantCode: session.ApprovalErrorInvalidRequest},
		{name: "malformed query forbidden", path: path + "?retry=true;other=false", body: valid, wantCode: session.ApprovalErrorInvalidRequest},
		{name: "invalid session path", path: feat137DecisionPath("not-a-uuid", feat137AppApprovalID), body: valid, wantCode: session.ApprovalErrorInvalidRequest},
		{name: "zero approval path", path: feat137DecisionPath(feat137AppSessionID, "00000000-0000-0000-0000-000000000000"), body: valid, wantCode: session.ApprovalErrorInvalidRequest},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			stub := newFEAT137AppServiceStub()
			request := authorizedRequest(http.MethodPost, test.path, test.body)
			if test.contentType != "" {
				request.Header.Set("Content-Type", test.contentType)
			}
			if test.mutateHeaders != nil {
				test.mutateHeaders(request.Header)
			}
			response := httptest.NewRecorder()
			newFEAT137AppHandler(stub).ServeHTTP(response, request)
			assertFEAT137AppError(t, response, http.StatusBadRequest, test.wantCode, feat137ExpectedErrorMessage(test.wantCode))
			if stub.decisionCalls != 0 {
				t.Fatalf("invalid decision reached service: calls=%d input=%+v", stub.decisionCalls, stub.decisionInput)
			}
			if strings.Contains(response.Body.String(), feat137AppSensitiveTag) {
				t.Fatalf("invalid decision leaked request content: %s", response.Body.String())
			}
		})
	}
}

func TestFEAT137DecisionHTTPProjectionIsStrictlyCorrelated(t *testing.T) {
	for _, decision := range []string{"accept_once", "cancel_current_turn"} {
		t.Run(decision, func(t *testing.T) {
			stub := newFEAT137AppServiceStub()
			stub.decisionResult.Decision = decision
			if decision == "accept_once" {
				stub.decisionResult.Outcome = "accepted_once"
			} else {
				stub.decisionResult.Outcome = "cancelled_current_turn"
			}
			response := httptest.NewRecorder()
			newFEAT137AppHandler(stub).ServeHTTP(
				response,
				authorizedRequest(http.MethodPost, feat137DecisionPath(feat137AppSessionID, feat137AppApprovalID), validFEAT137DecisionBody(decision)),
			)
			if response.Code != http.StatusOK || stub.decisionCalls != 1 || stub.decisionFor != feat137AppSessionID ||
				stub.approvalFor != feat137AppApprovalID || stub.decisionInput.DecisionID != feat137AppDecisionID ||
				stub.decisionInput.ExpectedStreamID != feat137AppStreamID || stub.decisionInput.ExpectedRevision != 1 ||
				stub.decisionInput.Decision != decision {
				t.Fatalf("decision mapping mismatch: status=%d body=%s calls=%d session=%q approval=%q input=%+v",
					response.Code, response.Body.String(), stub.decisionCalls, stub.decisionFor, stub.approvalFor, stub.decisionInput)
			}
			if response.Header().Get("Cache-Control") != "no-store" {
				t.Fatalf("decision response is cacheable: %v", response.Header())
			}
			assertOpenAPIJSON(t, "ApprovalDecisionV6Response", response.Body.Bytes())
		})
	}

	t.Run("uppercase request UUIDs are normalized and JSON media parameters are accepted", func(t *testing.T) {
		stub := newFEAT137AppServiceStub()
		body := validFEAT137DecisionBody("accept_once")
		body = strings.ReplaceAll(body, feat137AppDecisionID, strings.ToUpper(feat137AppDecisionID))
		body = strings.ReplaceAll(body, feat137AppStreamID, strings.ToUpper(feat137AppStreamID))
		request := authorizedRequest(
			http.MethodPost,
			feat137DecisionPath(strings.ToUpper(feat137AppSessionID), strings.ToUpper(feat137AppApprovalID)),
			body,
		)
		request.Header.Set("Content-Type", "application/json; charset=utf-8")
		response := httptest.NewRecorder()
		newFEAT137AppHandler(stub).ServeHTTP(response, request)
		if response.Code != http.StatusOK || stub.decisionCalls != 1 || stub.decisionFor != feat137AppSessionID ||
			stub.approvalFor != feat137AppApprovalID || stub.decisionInput.DecisionID != feat137AppDecisionID ||
			stub.decisionInput.ExpectedStreamID != feat137AppStreamID {
			t.Fatalf("uppercase decision UUIDs were not normalized: status=%d body=%s calls=%d session=%q approval=%q input=%+v",
				response.Code, response.Body.String(), stub.decisionCalls, stub.decisionFor, stub.approvalFor, stub.decisionInput)
		}
	})

	resultTests := []struct {
		name   string
		mutate func(*session.ApprovalDecisionResult)
	}{
		{name: "wrong approval id", mutate: func(value *session.ApprovalDecisionResult) { value.ApprovalRequestID = feat137AppOtherID }},
		{name: "wrong decision id", mutate: func(value *session.ApprovalDecisionResult) { value.DecisionID = feat137AppOtherID }},
		{name: "wrong stream id", mutate: func(value *session.ApprovalDecisionResult) { value.StreamID = feat137AppOtherStream }},
		{name: "zero result identity", mutate: func(value *session.ApprovalDecisionResult) { value.DecisionID = "00000000-0000-0000-0000-000000000000" }},
		{name: "noncanonical internal identity", mutate: func(value *session.ApprovalDecisionResult) { value.DecisionID = strings.ToUpper(value.DecisionID) }},
		{name: "wrong schema", mutate: func(value *session.ApprovalDecisionResult) { value.SchemaVersion = 5 }},
		{name: "wrong revision", mutate: func(value *session.ApprovalDecisionResult) { value.Revision = 1 }},
		{name: "wrong decision", mutate: func(value *session.ApprovalDecisionResult) { value.Decision = feat137AppSensitiveTag }},
		{name: "decision outcome mismatch", mutate: func(value *session.ApprovalDecisionResult) { value.Outcome = "cancelled_current_turn" }},
		{name: "zero resolved time", mutate: func(value *session.ApprovalDecisionResult) { value.ResolvedAt = time.Time{} }},
	}
	for _, test := range resultTests {
		t.Run(test.name, func(t *testing.T) {
			stub := newFEAT137AppServiceStub()
			test.mutate(&stub.decisionResult)
			response := httptest.NewRecorder()
			newFEAT137AppHandler(stub).ServeHTTP(
				response,
				authorizedRequest(http.MethodPost, feat137DecisionPath(feat137AppSessionID, feat137AppApprovalID), validFEAT137DecisionBody("accept_once")),
			)
			assertFEAT137AppError(t, response, http.StatusInternalServerError, session.ApprovalErrorInternal, "approval processing failed")
			if strings.Contains(response.Body.String(), feat137AppSensitiveTag) {
				t.Fatalf("invalid service result leaked content: %s", response.Body.String())
			}
		})
	}
}

func TestFEAT137ApprovalHTTPUsesStableErrors(t *testing.T) {
	decisionPath := feat137DecisionPath(feat137AppSessionID, feat137AppApprovalID)
	tests := []struct {
		name    string
		err     error
		status  int
		code    string
		message string
	}{
		{name: "invalid", err: &session.ApprovalError{Code: session.ApprovalErrorInvalidRequest}, status: 400, code: session.ApprovalErrorInvalidRequest, message: "approval request is invalid"},
		{name: "version", err: &session.ApprovalError{Code: session.ApprovalErrorVersionMismatch}, status: 400, code: session.ApprovalErrorVersionMismatch, message: "approval schema version does not match"},
		{name: "session missing", err: session.ErrNotFound, status: 404, code: "session_not_found", message: "agent session was not found"},
		{name: "approval missing", err: &session.ApprovalError{Code: session.ApprovalErrorNotFound}, status: 404, code: session.ApprovalErrorNotFound, message: "approval request was not found"},
		{name: "stale", err: &session.ApprovalError{Code: session.ApprovalErrorStale}, status: 409, code: session.ApprovalErrorStale, message: "approval request is stale"},
		{name: "expired", err: &session.ApprovalError{Code: session.ApprovalErrorExpired}, status: 409, code: session.ApprovalErrorExpired, message: "approval request expired"},
		{name: "resolved", err: &session.ApprovalError{Code: session.ApprovalErrorAlreadyResolved}, status: 409, code: session.ApprovalErrorAlreadyResolved, message: "approval request was already resolved"},
		{name: "decision conflict", err: &session.ApprovalError{Code: session.ApprovalErrorDecisionConflict}, status: 409, code: session.ApprovalErrorDecisionConflict, message: "approval decision conflicts with the existing decision"},
		{name: "unavailable", err: &session.ApprovalError{Code: session.ApprovalErrorUnavailable}, status: 503, code: session.ApprovalErrorUnavailable, message: "approval authority is unavailable"},
		{name: "internal", err: &session.ApprovalError{Code: session.ApprovalErrorInternal}, status: 500, code: session.ApprovalErrorInternal, message: "approval processing failed"},
		{name: "unknown hidden", err: errors.New(feat137AppSensitiveTag), status: 500, code: session.ApprovalErrorInternal, message: "approval processing failed"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			stub := newFEAT137AppServiceStub()
			stub.decisionErr = test.err
			response := httptest.NewRecorder()
			newFEAT137AppHandler(stub).ServeHTTP(
				response,
				authorizedRequest(http.MethodPost, decisionPath, validFEAT137DecisionBody("accept_once")),
			)
			assertFEAT137AppError(t, response, test.status, test.code, test.message)
			if strings.Contains(response.Body.String(), feat137AppSensitiveTag) {
				t.Fatalf("service error leaked content: %s", response.Body.String())
			}
		})
	}

	t.Run("pending unavailable is closed as internal because GET has no 503", func(t *testing.T) {
		stub := newFEAT137AppServiceStub()
		stub.pendingErr = &session.ApprovalError{Code: session.ApprovalErrorUnavailable}
		response := httptest.NewRecorder()
		newFEAT137AppHandler(stub).ServeHTTP(
			response,
			authorizedRequest(http.MethodGet, "/v6/agent-sessions/"+feat137AppSessionID+"/approvals/pending", ""),
		)
		assertFEAT137AppError(t, response, http.StatusInternalServerError, session.ApprovalErrorInternal, "approval processing failed")
	})

	for _, target := range []struct {
		method string
		path   string
		body   string
	}{
		{method: http.MethodGet, path: "/v6/agent-sessions/" + feat137AppSessionID + "/approvals/pending"},
		{method: http.MethodPost, path: decisionPath, body: validFEAT137DecisionBody("accept_once")},
	} {
		request := httptest.NewRequest(target.method, target.path, strings.NewReader(target.body))
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		newFEAT137AppHandler(newFEAT137AppServiceStub()).ServeHTTP(response, request)
		assertFEAT137AppError(t, response, http.StatusUnauthorized, "unauthorized", "valid Agent Host bearer token required")
		assertOpenAPIJSON(t, "ApprovalUnauthorizedErrorV6", response.Body.Bytes())
	}
}

func newFEAT137AppServiceStub() *feat137AppServiceStub {
	return &feat137AppServiceStub{
		streamID:       feat137AppStreamID,
		snapshot:       validFEAT137PendingSnapshot(),
		decisionResult: validFEAT137DecisionResult(),
	}
}

func newFEAT137AppHandler(service SessionService) http.Handler {
	return NewHandler(feat137ExactHandlerConfig(), staticRuntimeStatus{}, service, "api-token")
}

func feat137ExactHandlerConfig() Config {
	return Config{
		Environment:                    "local",
		FEAT134StreamingEnabled:        true,
		FEAT136CommandToolItemsEnabled: true,
		FEAT137CommandApprovalEnabled:  true,
		Runtime:                        codex.Config{CommandApprovalEnabled: true},
	}
}

func validFEAT137PendingSnapshot() session.PendingApprovalSnapshot {
	requestedAt := time.Date(2026, 8, 30, 12, 0, 0, 0, time.UTC)
	return session.PendingApprovalSnapshot{
		SchemaVersion: 6,
		StreamID:      feat137AppStreamID,
		SnapshotAt:    requestedAt.Add(time.Second),
		Pending: []session.PendingApproval{{
			ApprovalRequestID: feat137AppApprovalID,
			Revision:          1,
			TaskID:            feat137AppTaskID,
			AgentSessionID:    feat137AppSessionID,
			CodexThreadID:     feat137AppThreadID,
			TurnID:            feat137AppTurnID,
			ItemID:            "command-item-safe",
			ActionID:          "git_repository_check",
			WorkspaceScope:    "current_workspace",
			RequestedAt:       requestedAt,
			ExpiresAt:         requestedAt.Add(120 * time.Second),
			TTLSeconds:        120,
		}},
	}
}

func validFEAT137DecisionResult() session.ApprovalDecisionResult {
	return session.ApprovalDecisionResult{
		SchemaVersion:     6,
		ApprovalRequestID: feat137AppApprovalID,
		DecisionID:        feat137AppDecisionID,
		StreamID:          feat137AppStreamID,
		Revision:          2,
		Decision:          "accept_once",
		Outcome:           "accepted_once",
		ResolvedAt:        time.Date(2026, 8, 30, 12, 0, 2, 0, time.UTC),
	}
}

func validFEAT137AppEvent() session.Event {
	message := "safe warning"
	willRetry := false
	return session.Event{
		SchemaVersion:  session.EventSchemaVersionV6,
		EventID:        feat137AppEventID,
		StreamID:       feat137AppStreamID,
		Sequence:       1,
		OccurredAt:     time.Date(2026, 8, 30, 12, 0, 0, 0, time.UTC),
		TaskID:         feat137AppTaskID,
		AgentSessionID: feat137AppSessionID,
		CodexThreadID:  feat137AppThreadID,
		EventType:      session.EventWarning,
		Payload:        session.EventPayload{Message: &message, WillRetry: &willRetry},
	}
}

func feat137DecisionPath(sessionID, approvalID string) string {
	return "/v6/agent-sessions/" + sessionID + "/approvals/" + approvalID + "/decision"
}

func validFEAT137DecisionBody(decision string) string {
	return `{"schema_version":6,"decision_id":"` + feat137AppDecisionID +
		`","expected_stream_id":"` + feat137AppStreamID +
		`","expected_revision":1,"decision":"` + decision + `"}`
}

func assertFEAT137AppError(t *testing.T, response *httptest.ResponseRecorder, status int, code, message string) {
	t.Helper()
	if response.Code != status {
		t.Fatalf("approval error status=%d want=%d body=%s", response.Code, status, response.Body.String())
	}
	if response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("approval error is cacheable: %v", response.Header())
	}
	var body struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode approval error: %v; body=%s", err, response.Body.String())
	}
	if body.Error.Code != code || body.Error.Message != message {
		t.Fatalf("approval error got=(%q,%q), want=(%q,%q)", body.Error.Code, body.Error.Message, code, message)
	}
}

func feat137ExpectedErrorMessage(code string) string {
	if code == session.ApprovalErrorVersionMismatch {
		return "approval schema version does not match"
	}
	return "approval request is invalid"
}
