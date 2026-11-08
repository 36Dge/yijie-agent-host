package app

import (
	"context"
	"encoding/json"
	"github.com/36Dge/yijie-agent-host/internal/codex"
	wire "github.com/36Dge/yijie-agent-host/internal/contracts/userinput"
	"github.com/google/uuid"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type questionRouteService struct {
	SessionService
	row         wire.UserInputRequest
	answers     int
	lastSession string
}

func (s *questionRouteService) UserInputSnapshot(session string) (wire.UserInputSnapshot, error) {
	s.lastSession = session
	return wire.UserInputSnapshot{Enabled: true, Requests: []wire.UserInputRequest{s.row}}, nil
}
func (s *questionRouteService) GetUserInput(session, id string) (wire.UserInputRequest, error) {
	s.lastSession = session
	return s.row, nil
}
func (s *questionRouteService) TouchUserInput(session, id string) (wire.UserInputRequest, error) {
	s.lastSession = session
	s.row.TimeoutPaused = true
	return s.row, nil
}
func (s *questionRouteService) AnswerUserInput(_ context.Context, session, id string, answer wire.UserInputAnswerRequest) (wire.UserInputRequest, error) {
	s.answers++
	s.lastSession = session
	s.row.State = "answered"
	s.row.Questions = []wire.ToolRequestUserInputQuestion{}
	s.row.OperationId = &answer.OperationId
	return s.row, nil
}
func TestUserInputRoutesAuthenticateAndConformWithoutApproval(t *testing.T) {
	session, id := uuid.NewString(), uuid.New()
	service := &questionRouteService{row: wire.UserInputRequest{RequestId: id, TurnId: "ordinary-turn", ItemId: "ordinary-item", State: "pending", Questions: []wire.ToolRequestUserInputQuestion{{Id: "colour", Header: "颜色", Question: "普通颜色"}}}}
	mux := http.NewServeMux()
	registerUserInput(mux, &sessionHandler{service: service, apiToken: "ordinary-owner-token"}, true)
	call := func(method, path, body string, auth bool) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		if auth {
			r.Header.Set("Authorization", "Bearer ordinary-owner-token")
		}
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		return w
	}
	base := "/v1/agent-sessions/" + session + "/user-input"
	for _, tc := range []struct {
		method, path, body, schema string
		auth                       bool
		status                     int
	}{
		{"GET", base, "", "UserInputError", false, 401},
		{"GET", base, "", "UserInputSnapshot", true, 200},
		{"POST", base + "/" + id.String() + "/touch", "", "UserInputRequest", true, 200},
		{"POST", base + "/" + id.String() + "/answer", `{"operation_id":"` + uuid.NewString() + `","answers":{"colour":{"answers":["普通样例"]}}}`, "UserInputRequest", true, 200},
	} {
		w := call(tc.method, tc.path, tc.body, tc.auth)
		var value any
		if w.Code != tc.status || wire.Decode(tc.schema, w.Body.Bytes(), &value) != nil {
			t.Fatal("question route mismatch", tc.path, w.Code, w.Body.String())
		}
		if w.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("missing no-store")
		}
	}
	if service.answers != 1 || service.lastSession != session {
		t.Fatal("original session lost")
	}
	w := call("POST", base+"/"+id.String()+"/answer", `{"decision":"approve_once"}`, true)
	if w.Code != 400 || service.answers != 1 {
		t.Fatal("approval decision was accepted as an answer")
	}
}

func TestUserInputClosedErrorsConform(t *testing.T) {
	for _, tc := range []struct {
		err    error
		status int
		code   string
	}{
		{codex.ErrUserInputNotFound, 404, "not_found"},
		{codex.ErrUserInputConflict, 409, "conflict"},
		{codex.ErrUserInputInvalid, 400, "invalid_request"},
	} {
		w := httptest.NewRecorder()
		writeUserInput(w, "", nil, tc.err)
		var value map[string]any
		if w.Code != tc.status || wire.Decode("UserInputError", w.Body.Bytes(), &value) != nil {
			t.Fatal("closed error contract mismatch")
		}
		b, _ := json.Marshal(value["error"].(map[string]any)["code"])
		if string(b) != `"`+tc.code+`"` {
			t.Fatal("error meaning changed")
		}
	}
}
