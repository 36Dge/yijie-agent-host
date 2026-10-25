package app

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/36Dge/yijie-agent-host/internal/codex"
	wire "github.com/36Dge/yijie-agent-host/internal/contracts/chatmodelsv2"
	"github.com/36Dge/yijie-agent-host/internal/session"
)

type modelV2RouteService struct{ SessionService }

func (*modelV2RouteService) ChatModelsV2Enabled() bool { return true }
func (*modelV2RouteService) ChatModelCatalogV2() wire.Catalog {
	rows := []wire.ModelAvailability{}
	for _, p := range wire.Definitions() {
		rows = append(rows, wire.ModelAvailability{Profile: p, Available: true, Reason: wire.ModelAvailabilityReasonReady})
	}
	return wire.Catalog{SchemaVersion: 2, DefaultProfile: wire.ActiveProfileIdKimiK31ProMaxV1, Models: rows}
}

type modelV2OperationRouteService struct {
	modelV2RouteService
	calls int
}

func (s *modelV2OperationRouteService) SelectChatModelV2(context.Context, string, wire.SelectRequest) (wire.Selection, error) {
	s.calls++
	return wire.Selection{}, session.ErrTurnOperationConflict
}
func (s *modelV2OperationRouteService) ReadChatModelOperationV2(string, string) (wire.ModelOperationReceipt, error) {
	s.calls++
	return wire.ModelOperationReceipt{}, session.ErrNotFound
}
func (s *modelV2OperationRouteService) RecoverChatModelV2(context.Context, string, wire.RecoverRequest) (wire.ModelOperationReceipt, error) {
	s.calls++
	return wire.ModelOperationReceipt{}, session.ErrModelUnknown
}

func TestFEAT164V2OperationRoutesValidateBeforeService(t *testing.T) {
	s := &modelV2OperationRouteService{}
	mux := http.NewServeMux()
	registerChatModelsV2(mux, &sessionHandler{service: s, apiToken: "local-operation-token"})
	const id = "00000000-0000-4000-8000-000000000164"
	base := "/v2/model-chat/agent-sessions/" + id
	body := `{"schema_version":2,"operation_id":"` + id + `","expected_revision":0,"profile_id":"kimi-k3-1-pro-max-v1"}`
	for _, tc := range []struct {
		method, path, body string
		auth               bool
		status, calls      int
	}{
		{"POST", base + "/model", body, false, 401, 0},
		{"POST", base + "/model", strings.Replace(body, "kimi-k3-1-pro-max-v1", "kimi-k3-max-v1", 1), true, 400, 0},
		{"POST", base + "/model", body, true, 409, 1},
		{"GET", base + "/model-operations/" + id + "?x=1", "", true, 400, 1},
		{"GET", base + "/model-operations/" + id, "", true, 404, 2},
		{"POST", base + "/model-operations/00000000-0000-4000-8000-000000000165/recover", body, true, 400, 2},
		{"POST", base + "/model-operations/" + id + "/recover", body, true, 409, 3},
	} {
		r := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
		if tc.auth {
			r.Header.Set("Authorization", "Bearer local-operation-token")
		}
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		var errorBody wire.Error
		if w.Code != tc.status || s.calls != tc.calls || w.Header().Get("Cache-Control") != "no-store" || wire.Decode("Error", w.Body.Bytes(), &errorBody) != nil {
			t.Fatalf("%s %s status=%d calls=%d body=%s", tc.method, tc.path, w.Code, s.calls, w.Body.String())
		}
	}
}
func (*modelV2RouteService) ReadChatModelV2(id string) (wire.Selection, error) {
	old := wire.ProfileIdKimiK3MaxV1
	return wire.Selection{SchemaVersion: 2, AgentSessionId: id, Revision: 3, State: wire.SelectionStateReady, ProfileId: &old, Availability: wire.SelectionAvailabilityRetired}, nil
}
func TestFEAT164ModelRoutesPreserveAuthAndRetiredRead(t *testing.T) {
	mux := http.NewServeMux()
	const token = "ordinary-local-token"
	calls := 0
	mux.HandleFunc("POST /v1/tasks/{task_id}/agent-sessions", func(w http.ResponseWriter, r *http.Request) {
		calls++
		if codex.ModelProfileID(r.Context()) != codex.KimiK31ProfileID {
			t.Error("profile context lost")
		}
		w.WriteHeader(201)
	})
	registerChatModelsV2(mux, &sessionHandler{service: &modelV2RouteService{}, apiToken: token})
	const id = "00000000-0000-4000-8000-000000000164"
	do := func(method, path, profile string, auth bool) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, nil)
		if auth {
			r.Header.Set("Authorization", "Bearer "+token)
		}
		if profile != "" {
			r.Header.Set("X-Yijie-Model-Profile", profile)
		}
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		return w
	}
	if w := do("GET", "/v2/model-chat/catalog", "", false); w.Code != 401 || !strings.Contains(w.Body.String(), `"schema_version":2`) {
		t.Fatal("missing auth admitted")
	}
	if w := do("GET", "/v2/model-chat/catalog", "", true); w.Code != 200 || !strings.Contains(w.Body.String(), "kimi-k3.1-pro") {
		t.Fatal("catalog", w.Code)
	}
	if w := do("GET", "/v2/model-chat/agent-sessions/"+id+"/model", "", true); w.Code != 200 || !strings.Contains(w.Body.String(), "retired") {
		t.Fatal("historical read blocked")
	}
	path := "/v2/model-chat/tasks/" + id + "/agent-sessions"
	for _, tc := range []struct {
		profile string
		status  int
	}{{"kimi-k3-max-v1", 409}, {"future", 400}, {"", 400}, {codex.KimiK31ProfileID, 201}} {
		if w := do("POST", path, tc.profile, true); w.Code != tc.status {
			t.Fatalf("profile admission: %s %d", tc.profile, w.Code)
		}
	}
	if calls != 1 {
		t.Fatal("invalid profile reached producer")
	}
	if w := do("POST", path+"?unexpected=true", codex.KimiK31ProfileID, true); w.Code != 400 || calls != 1 {
		t.Fatal("query reached producer")
	}
}
