package app

import (
	"context"
	"encoding/json"
	"github.com/36Dge/yijie-agent-host/internal/codex"
	wire "github.com/36Dge/yijie-agent-host/internal/contracts/scheduledraft"
	"github.com/36Dge/yijie-agent-host/internal/session"
	"github.com/google/uuid"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// An in-process interface fixture, never an executable/runtime replacement.
type draftProtocolFixture struct {
	starts, turns int
	thread, turn  string
}

func (r *draftProtocolFixture) ScheduledDraftReady() bool { return true }
func (r *draftProtocolFixture) StartThread(context.Context, string) (codex.ThreadInfo, error) {
	r.starts++
	return codex.ThreadInfo{ID: r.thread, Model: codex.MiniMaxModel, ModelProvider: codex.MiniMaxProviderID}, nil
}
func (r *draftProtocolFixture) ResumeThread(context.Context, string) (codex.ThreadInfo, error) {
	return codex.ThreadInfo{ID: r.thread, Model: codex.MiniMaxModel, ModelProvider: codex.MiniMaxProviderID}, nil
}
func (r *draftProtocolFixture) StartTurn(context.Context, string, string, string) (codex.TurnInfo, error) {
	panic("ordinary route must not run")
}
func (r *draftProtocolFixture) StartTurnV2(_ context.Context, _ string, inputs []codex.UserInput, _ string) (codex.TurnInfo, error) {
	r.turns++
	return codex.TurnInfo{ID: r.turn, Status: "inProgress"}, nil
}
func (r *draftProtocolFixture) InterruptTurn(context.Context, string, string) error { return nil }
func (r *draftProtocolFixture) DeleteThread(context.Context, string) error          { return nil }
func TestFEAT155DraftHTTPRealServiceProducerConformance(t *testing.T) {
	home, e := filepath.EvalSymlinks(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	store, e := session.OpenStore(home, session.WithScheduledDraftStorage())
	if e != nil {
		t.Fatal(e)
	}
	defer store.Close()
	workspace, e := filepath.EvalSymlinks(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	runtime := &draftProtocolFixture{thread: uuid.NewString(), turn: uuid.NewString()}
	service := session.NewService(runtime, store, session.NewEventHub(8, 8), nil, session.WithScheduledDraftDirectory(func(string) (string, error) { return workspace, nil }))
	handler := NewHandler(Config{Environment: "local", ScheduledRecoveryEnabled: true}, staticRuntimeStatus{}, service, recoveryTestToken)
	var rows []map[string]any
	request := func(method, path, schema string, value any, want int, token string) map[string]any {
		t.Helper()
		body := ""
		if value != nil {
			b, _ := json.Marshal(value)
			body = string(b)
		}
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		if token != "" {
			r.Header.Set("Authorization", "Bearer "+token)
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != want {
			t.Fatalf("%s: %d %s", path, w.Code, w.Body.String())
		}
		if w.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("cache")
		}
		var result map[string]any
		if e = wire.Decode(schema, w.Body.Bytes(), &result); e != nil {
			t.Fatal(e)
		}
		rows = append(rows, map[string]any{"schema": schema, "value": result, "method": method, "path": path, "status": want})
		return result
	}
	cap := request("GET", "/v1/scheduled-plan-draft-capability", "Capability", nil, 200, recoveryTestToken)
	if cap["available"] != true {
		t.Fatal(cap)
	}
	request("GET", "/v1/scheduled-plan-draft-capability", "Error", nil, 401, "")
	input := wire.CreateRequest{SchemaVersion: 1, PolicyVersion: 1, TaskId: uuid.NewString(), WorkspaceId: uuid.NewString()}
	created := request("POST", "/v1/scheduled-plan-draft-sessions", "SessionReceipt", input, 200, recoveryTestToken)
	request("POST", "/v1/scheduled-plan-draft-sessions", "SessionReceipt", input, 200, recoveryTestToken)
	request("GET", "/v1/scheduled-plan-draft-session-mappings/"+input.TaskId, "RecoveryMapping", nil, 200, recoveryTestToken)
	id := created["agent_session_id"].(string)
	path := "/v1/scheduled-plan-draft-sessions/" + id
	request("POST", path+"/resume", "SessionReceipt", wire.ResumeRequest{SchemaVersion: 1, PolicyVersion: 1}, 200, recoveryTestToken)
	turn := wire.TurnRequest{SchemaVersion: 1, PolicyVersion: 1, OperationId: uuid.NewString(), Text: "每天九点生成总结草案"}
	request("POST", path+"/turns", "TurnReceipt", turn, 200, recoveryTestToken)
	request("POST", path+"/turns", "TurnReceipt", turn, 200, recoveryTestToken)
	request("POST", path+"/resume", "Error", wire.ResumeRequest{SchemaVersion: 1, PolicyVersion: 1}, 409, recoveryTestToken)
	turn.Text = "another"
	request("POST", path+"/turns", "Error", turn, 409, recoveryTestToken)
	request("POST", path+"/resume", "Error", map[string]any{"schema_version": 2, "policy_version": 1}, 400, recoveryTestToken)
	if runtime.starts != 1 || runtime.turns != 1 {
		t.Fatalf("duplicate dispatch %d %d", runtime.starts, runtime.turns)
	}
	if dir := os.Getenv("YIJIE_FEAT155_CONFORMANCE_DIR"); dir != "" {
		b, _ := json.MarshalIndent(rows, "", "  ")
		if e = os.WriteFile(filepath.Join(dir, "host-draft-producer.json"), b, 0600); e != nil {
			t.Fatal(e)
		}
	}
	// The ordinary registration profile does not expose candidate routes.
	hidden := NewHandler(Config{Environment: "local"}, staticRuntimeStatus{}, service, recoveryTestToken)
	w := httptest.NewRecorder()
	hidden.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/v1/scheduled-plan-draft-capability", nil))
	if w.Code != 404 {
		t.Fatal(w.Code)
	}
}
func TestFEAT155DraftHTTPUnavailableHasNoRuntimeOrStoreMigration(t *testing.T) {
	store, e := session.OpenStore(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	defer store.Close()
	service := session.NewService(nil, store, session.NewEventHub(8, 8), nil)
	handler := NewHandler(Config{Environment: "local", ScheduledRecoveryEnabled: true}, staticRuntimeStatus{}, service, recoveryTestToken)
	body, _ := json.Marshal(wire.CreateRequest{SchemaVersion: 1, PolicyVersion: 1, TaskId: uuid.NewString(), WorkspaceId: uuid.NewString()})
	r := httptest.NewRequest("POST", "/v1/scheduled-plan-draft-sessions", strings.NewReader(string(body)))
	r.Header.Set("Authorization", "Bearer "+recoveryTestToken)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	var error wire.Error
	if e = wire.Decode("Error", w.Body.Bytes(), &error); e != nil || error.Code != "storage_disabled" || w.Code != 503 {
		t.Fatalf("%s %v", w.Body.String(), e)
	}
}

func TestFEAT155DraftMappingReadWithoutRuntimeOrDirectory(t *testing.T) {
	home := t.TempDir()
	workspace, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	store, err := session.OpenStore(home, session.WithScheduledDraftStorage())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	runtime := &draftProtocolFixture{thread: uuid.NewString(), turn: uuid.NewString()}
	creator := session.NewService(runtime, store, session.NewEventHub(8, 8), nil, session.WithScheduledDraftDirectory(func(string) (string, error) { return workspace, nil }))
	input := wire.CreateRequest{SchemaVersion: 1, PolicyVersion: 1, TaskId: uuid.NewString(), WorkspaceId: uuid.NewString()}
	receipt, err := creator.CreateScheduledDraft(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	before, err := store.Get(receipt.AgentSessionId)
	if err != nil {
		t.Fatal(err)
	}
	// No Runtime, directory resolver or candidate writer qualification is needed
	// to inspect the existing durable association.
	reader := session.NewService(nil, store, session.NewEventHub(8, 8), nil)
	nonce := uuid.NewString()
	handler := NewHandler(Config{Environment: "local", ScheduledRecoveryEnabled: true, InstanceNonce: nonce}, staticRuntimeStatus{}, reader, recoveryTestToken)
	get := func(path, token string) *httptest.ResponseRecorder {
		request := httptest.NewRequest(http.MethodGet, path, nil)
		if token != "" {
			request.Header.Set("Authorization", "Bearer "+token)
		}
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		return response
	}
	path := "/v1/scheduled-plan-draft-session-mappings/" + input.TaskId
	response := get(path, recoveryTestToken)
	var mapping wire.RecoveryMapping
	if response.Code != 200 || wire.Decode("RecoveryMapping", response.Body.Bytes(), &mapping) != nil || mapping.WorkspaceId != input.WorkspaceId || mapping.AgentSessionId != receipt.AgentSessionId || mapping.CodexThreadId == nil || *mapping.CodexThreadId != runtime.thread || mapping.RespondingHostInstanceId == nil || *mapping.RespondingHostInstanceId != nonce {
		t.Fatalf("invalid read: %d %s", response.Code, response.Body.String())
	}
	if response.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("cacheable recovery")
	}
	if got := get(path, "").Code; got != 401 {
		t.Fatal(got)
	}
	if got := get(path+"?unexpected=1", recoveryTestToken).Code; got != 400 {
		t.Fatal(got)
	}
	missing := get("/v1/scheduled-plan-draft-session-mappings/"+uuid.NewString(), recoveryTestToken)
	var failure wire.Error
	if missing.Code != 404 || wire.Decode("Error", missing.Body.Bytes(), &failure) != nil || failure.Code != "not_found" {
		t.Fatal(missing.Code, missing.Body.String())
	}
	after, err := store.Get(receipt.AgentSessionId)
	if err != nil {
		t.Fatal(err)
	}
	a, _ := json.Marshal(before)
	b, _ := json.Marshal(after)
	if string(a) != string(b) || runtime.starts != 1 || runtime.turns != 0 {
		t.Fatal("query changed facts or called Runtime")
	}
}
