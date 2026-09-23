package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/36Dge/yijie-agent-host/internal/session"
	"github.com/google/uuid"
	"github.com/santhosh-tekuri/jsonschema/v6"
)

const recoveryTestToken = "synthetic-recovery-bearer-no-real-credential"

func recoverySchema(t *testing.T, name string) *jsonschema.Schema {
	t.Helper()
	file, err := filepath.Abs("../../api/schemas/scheduled-task-recovery.schema.json")
	if err != nil {
		t.Fatal(err)
	}
	schema, err := jsonschema.NewCompiler().Compile(file + "#/$defs/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return schema
}

func recoveryRequest(t *testing.T, handler http.Handler, target, token, body string, status int, schema *jsonschema.Schema) map[string]any {
	t.Helper()
	r := httptest.NewRequest(http.MethodGet, target, strings.NewReader(body))
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != status {
		t.Fatalf("%s status %d: %s", target, w.Code, w.Body.String())
	}
	if w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("query response may be cached")
	}
	var value map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &value); err != nil {
		t.Fatal(err)
	}
	if err := schema.Validate(value); err != nil {
		t.Fatal(err)
	}
	return value
}

func TestFEAT155RecoveryHTTPProducerAndResponderProvenance(t *testing.T) {
	store, err := session.OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	// Real service/Store, with no Runtime object or Provider/process involved.
	service := session.NewService(nil, store, session.NewEventHub(8, 8), nil)
	mappingSchema, operationSchema := recoverySchema(t, "SessionMapping"), recoverySchema(t, "TurnOperationResult")
	for _, state := range []string{"pending", "accepted", "uncertain"} {
		t.Run(state, func(t *testing.T) {
			taskID, sessionID, threadID, operationID, turnID := uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString()
			if err := store.Reserve(session.Record{TaskID: taskID, AgentSessionID: sessionID, Cwd: t.TempDir(), Trace: session.TraceContext{TenantID: "private-trace-tenant", UserID: "private-trace-user"}}); err != nil {
				t.Fatal(err)
			}
			config := Config{Environment: "local", ScheduledRecoveryEnabled: true}
			handler := NewHandler(config, staticRuntimeStatus{}, service, recoveryTestToken)
			mappingPath := "/v1/tasks/" + taskID + "/agent-session-mapping"
			reserved := recoveryRequest(t, handler, mappingPath, recoveryTestToken, "", 200, mappingSchema)
			if reserved["mapping_state"] != "reserved" {
				t.Fatal(reserved)
			}
			if _, err := store.BindThread(sessionID, threadID, "synthetic-runtime-session", "synthetic-model", "synthetic-provider"); err != nil {
				t.Fatal(err)
			}
			digest := strings.Repeat("b", 64)
			if _, _, err := store.PrepareTurnOperation(sessionID, operationID, digest, session.TraceContext{}); err != nil {
				t.Fatal(err)
			}
			if state == "accepted" {
				if _, err := store.AcceptTurnOperation(sessionID, operationID, digest, turnID); err != nil {
					t.Fatal(err)
				}
			}
			if state == "uncertain" {
				if _, err := store.MarkTurnOperationUncertain(sessionID, operationID, digest, "runtime_request_failed"); err != nil {
					t.Fatal(err)
				}
			}
			operationPath := "/v1/agent-sessions/" + sessionID + "/turn-operations/" + operationID
			for _, nonce := range []string{"", uuid.NewString(), uuid.NewString()} {
				config.InstanceNonce = nonce
				handler = NewHandler(config, staticRuntimeStatus{}, service, recoveryTestToken)
				mapping := recoveryRequest(t, handler, mappingPath, recoveryTestToken, "", 200, mappingSchema)
				operation := recoveryRequest(t, handler, operationPath, recoveryTestToken, "", 200, operationSchema)
				if mapping["mapping_state"] != "bound" || mapping["codex_thread_id"] != threadID || mapping["task_id"] != taskID || mapping["agent_session_id"] != sessionID {
					t.Fatal(mapping)
				}
				if operation["state"] != state || operation["operation_id"] != operationID || operation["agent_session_id"] != sessionID {
					t.Fatal(operation)
				}
				if state == "accepted" && operation["turn_id"] != turnID {
					t.Fatal("lost original turn")
				}
				for _, value := range []map[string]any{mapping, operation} {
					if nonce == "" {
						if _, exists := value["responding_host_instance_id"]; exists {
							t.Fatal("invented instance provenance")
						}
					} else if value["responding_host_instance_id"] != nonce {
						t.Fatal(value)
					}
					for _, field := range []string{"cwd", "trace", "input_digest", "model", "model_provider", "failure_code", "execution_generation", "native_terminal_status"} {
						if _, exists := value[field]; exists {
							t.Fatalf("private field exposed: %s", field)
						}
					}
				}
			}
		})
	}
}

func TestFEAT155RecoveryHTTPAuthorityAndMissingFacts(t *testing.T) {
	store, err := session.OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	service := session.NewService(nil, store, session.NewEventHub(8, 8), nil)
	handler := NewHandler(Config{Environment: "local", ScheduledRecoveryEnabled: true}, staticRuntimeStatus{}, service, recoveryTestToken)
	schema := recoverySchema(t, "RecoveryError")
	for _, target := range []string{"/v1/tasks/" + uuid.NewString() + "/agent-session-mapping", "/v1/agent-sessions/" + uuid.NewString() + "/turn-operations/" + uuid.NewString()} {
		for _, token := range []string{"", "ordinary-wrong-bearer"} {
			recoveryRequest(t, handler, target, token, "", 401, schema)
		}
		missing := recoveryRequest(t, handler, target, recoveryTestToken, "", 404, schema)
		if missing["error"].(map[string]any)["code"] != "recovery_record_not_found" {
			t.Fatal(missing)
		}
		recoveryRequest(t, handler, target+"?tenant_id=ordinary-value", recoveryTestToken, "", 400, schema)
		recoveryRequest(t, handler, target, recoveryTestToken, "{}", 400, schema)
	}
	for _, id := range []string{"not-an-id", uuid.Nil.String(), "15500000-0000-4000-8000-00000000000A"} {
		recoveryRequest(t, handler, "/v1/tasks/"+id+"/agent-session-mapping", recoveryTestToken, "", 400, schema)
		recoveryRequest(t, handler, "/v1/agent-sessions/"+uuid.NewString()+"/turn-operations/"+id, recoveryTestToken, "", 400, schema)
	}
	// Normal Store shutdown exercises unavailable service without corruption,
	// permission changes, Runtime termination or injected transport failures.
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	value := recoveryRequest(t, handler, "/v1/tasks/"+uuid.NewString()+"/agent-session-mapping", recoveryTestToken, "", 503, schema)
	if value["error"].(map[string]any)["message"] != "recovery facts are unavailable" {
		t.Fatal(value)
	}
}

func TestFEAT155RecoveryExactLocalRegistration(t *testing.T) {
	for _, pair := range [][2]string{{"local", "demo_fast"}, {"local", ""}, {"production", "demo_fast"}, {"local ", "demo_fast"}, {"", "demo_fast"}} {
		t.Setenv("YIJIE_ENV", pair[0])
		t.Setenv("YIJIE_LOCAL_PROFILE", pair[1])
		want := pair[0] == "local" && pair[1] == "demo_fast"
		if scheduledRecoveryEnabled() != want {
			t.Fatal(pair)
		}
	}
	for _, config := range []Config{{Environment: "local"}, {Environment: "production", ScheduledRecoveryEnabled: true}} {
		handler := NewHandler(config, staticRuntimeStatus{}, session.NewService(nil, nil, session.NewEventHub(8, 8), nil), recoveryTestToken)
		for _, target := range []string{"/v1/tasks/" + uuid.NewString() + "/agent-session-mapping", "/v1/agent-sessions/" + uuid.NewString() + "/turn-operations/" + uuid.NewString()} {
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, target, nil))
			if w.Code != 404 {
				t.Fatalf("disabled recovery route registered: %d", w.Code)
			}
		}
	}
}
