package codex

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"
	"time"
)

const feat137CanonicalApprovalParams = `{
	"threadId":"thread-137",
	"turnId":"turn-137",
	"itemId":"item-137",
	"startedAtMs":137000,
	"command":"git rev-parse --is-inside-work-tree",
	"commandActions":[{"type":"unknown","command":"git rev-parse --is-inside-work-tree"}],
	"cwd":"/workspace",
	"approvalId":null,
	"environmentId":"host-local",
	"availableDecisions":["accept","cancel"]
}`

type feat137ApprovalHandler struct {
	result   CommandApprovalResult
	requests chan CommandApprovalRequest
	written  chan CommandApprovalResponseWriteResult
	resolved chan CommandApprovalResolved
	closed   chan string
}

func newFEAT137ApprovalHandler(result CommandApprovalResult) *feat137ApprovalHandler {
	return &feat137ApprovalHandler{
		result:   result,
		requests: make(chan CommandApprovalRequest, 8),
		written:  make(chan CommandApprovalResponseWriteResult, 8),
		resolved: make(chan CommandApprovalResolved, 8),
		closed:   make(chan string, 8),
	}
}

func (h *feat137ApprovalHandler) HandleCommandApproval(
	_ context.Context,
	request CommandApprovalRequest,
) CommandApprovalResult {
	h.requests <- request
	return h.result
}

func (h *feat137ApprovalHandler) HandleCommandApprovalResponseWritten(result CommandApprovalResponseWriteResult) {
	h.written <- result
}

func (h *feat137ApprovalHandler) HandleCommandApprovalResolved(resolved CommandApprovalResolved) {
	h.resolved <- resolved
}

func (h *feat137ApprovalHandler) HandleCommandApprovalGenerationClosed(generation string) {
	h.closed <- generation
}

func TestFEAT137ExactServerRequestEnvelopeAndTypedRequestID(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want bool
	}{
		{
			name: "numeric id",
			raw:  `{"id":7,"method":"item/commandExecution/requestApproval","params":{}}`,
			want: true,
		},
		{
			name: "string id",
			raw:  `{"params":{},"method":"item/commandExecution/requestApproval","id":"7"}`,
			want: true,
		},
		{
			name: "unknown field",
			raw:  `{"id":7,"method":"item/commandExecution/requestApproval","params":{},"jsonrpc":"2.0"}`,
		},
		{
			name: "duplicate id",
			raw:  `{"id":7,"id":8,"method":"item/commandExecution/requestApproval","params":{}}`,
		},
		{
			name: "missing params",
			raw:  `{"id":7,"method":"item/commandExecution/requestApproval"}`,
		},
		{
			name: "trailing value",
			raw:  `{"id":7,"method":"item/commandExecution/requestApproval","params":{}} {}`,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := exactServerRequestEnvelope([]byte(test.raw)); got != test.want {
				t.Fatalf("exactServerRequestEnvelope() = %v, want %v", got, test.want)
			}
		})
	}

	numeric, err := requestIDKey(json.RawMessage(`7`))
	if err != nil {
		t.Fatal(err)
	}
	text, err := requestIDKey(json.RawMessage(`"7"`))
	if err != nil {
		t.Fatal(err)
	}
	if numeric != "n:7" || text != "s:7" || numeric == text {
		t.Fatalf("request id JSON type was not preserved: numeric=%q string=%q", numeric, text)
	}
	for _, invalid := range []json.RawMessage{json.RawMessage(`true`), json.RawMessage(`7.5`), json.RawMessage(`{}`)} {
		if _, err := requestIDKey(invalid); err == nil {
			t.Fatalf("requestIDKey accepted invalid id %s", invalid)
		}
	}
}

func TestFEAT137ClientPreservesTypedRequestIDOnResponse(t *testing.T) {
	hostWrites, runtimeReads := io.Pipe()
	hostReads, runtimeWrites := io.Pipe()
	seen := make(chan serverRequest, 2)
	client := NewClient(runtimeReads, hostReads, 1<<20, 8, nil, nil)
	client.setServerRequestHandler(func(_ context.Context, request serverRequest) serverRequestResult {
		seen <- request
		return serverRequestResult{respond: true, value: map[string]string{"decision": "cancel"}}
	})
	client.Start()
	t.Cleanup(func() {
		_ = runtimeWrites.Close()
		_ = client.CloseInput()
	})

	decoder := json.NewDecoder(hostWrites)
	for _, test := range []struct {
		wireID  string
		wantKey string
	}{
		{wireID: `7`, wantKey: "n:7"},
		{wireID: `"7"`, wantKey: "s:7"},
	} {
		request := []byte(`{"id":` + test.wireID + `,"method":"item/commandExecution/requestApproval","params":{}}` + "\n")
		if _, err := runtimeWrites.Write(request); err != nil {
			t.Fatal(err)
		}
		select {
		case got := <-seen:
			if got.idKey != test.wantKey || !got.exactEnvelope {
				t.Fatalf("unexpected decoded request: key=%q exact=%v", got.idKey, got.exactEnvelope)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("timed out waiting for decoded server request")
		}
		var response struct {
			ID     json.RawMessage `json:"id"`
			Result struct {
				Decision string `json:"decision"`
			} `json:"result"`
		}
		if err := decoder.Decode(&response); err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(response.ID, json.RawMessage(test.wireID)) || response.Result.Decision != "cancel" {
			t.Fatalf("typed response id or decision drifted: id=%s result=%+v", response.ID, response.Result)
		}
	}
}

func TestFEAT137CommandApprovalReportsResponseAfterTransportWrite(t *testing.T) {
	hostWrites, runtimeReads := io.Pipe()
	hostReads, runtimeWrites := io.Pipe()
	config := DefaultConfig()
	config.CommandApprovalEnabled = true
	manager := NewManager(config, nil)
	handler := newFEAT137ApprovalHandler(CommandApprovalResult{Respond: true, Decision: "cancel"})
	if err := manager.SetCommandApprovalHandler(handler); err != nil {
		t.Fatal(err)
	}
	fatal := make(chan error, 1)
	client := NewClient(runtimeReads, hostReads, 1<<20, 8, nil, func(err error) { fatal <- err })
	client.setServerRequestHandler(manager.handleServerRequest)
	client.Start()
	t.Cleanup(func() {
		_ = runtimeWrites.Close()
		_ = client.CloseInput()
	})

	var params bytes.Buffer
	if err := json.Compact(&params, []byte(feat137CanonicalApprovalParams)); err != nil {
		t.Fatal(err)
	}
	wire := []byte(`{"id":7,"method":"item/commandExecution/requestApproval","params":` +
		params.String() + `}` + "\n")
	if _, err := runtimeWrites.Write(wire); err != nil {
		t.Fatal(err)
	}
	select {
	case request := <-handler.requests:
		if request.RequestIDKey != "n:7" || request.ThreadID != "thread-137" {
			t.Fatalf("unexpected approval request before response write: %+v", request)
		}
	case err := <-fatal:
		t.Fatalf("client failed before mapping approval request: %v", err)
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for approval request")
	}
	select {
	case result := <-handler.written:
		t.Fatalf("response-write callback ran before transport write completed: %+v", result)
	default:
	}

	var response struct {
		ID     int64 `json:"id"`
		Result struct {
			Decision string `json:"decision"`
		} `json:"result"`
	}
	if err := json.NewDecoder(hostWrites).Decode(&response); err != nil {
		t.Fatal(err)
	}
	if response.ID != 7 || response.Result.Decision != "cancel" {
		t.Fatalf("unexpected Runtime response: %+v", response)
	}
	select {
	case result := <-handler.written:
		if !result.Succeeded || result.RuntimeGeneration != manager.runtimeGeneration ||
			result.RequestIDKey != "n:7" || result.ThreadID != "thread-137" {
			t.Fatalf("response-write callback lost correlation: %+v", result)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for committed response-write callback")
	}
}

func TestFEAT137CommandApprovalParamsClosedDecoder(t *testing.T) {
	params, err := decodeCommandApprovalParams(json.RawMessage(feat137CanonicalApprovalParams))
	if err != nil {
		t.Fatalf("decode canonical params: %v", err)
	}
	if params.ThreadID != "thread-137" || params.TurnID != "turn-137" || params.ItemID != "item-137" ||
		params.StartedAtMS != 137000 || params.Command != "git rev-parse --is-inside-work-tree" ||
		params.Cwd != "/workspace" || params.EnvironmentID == nil || *params.EnvironmentID != "host-local" ||
		len(params.CommandActions) != 1 || params.CommandActions[0] != (CommandApprovalAction{
		Type: "unknown", Command: "git rev-parse --is-inside-work-tree",
	}) {
		t.Fatalf("canonical params mapped incorrectly: %+v", params)
	}

	tests := []struct {
		name string
		raw  string
	}{
		{name: "unknown reason", raw: strings.Replace(feat137CanonicalApprovalParams, `"approvalId":null`, `"reason":"why","approvalId":null`, 1)},
		{name: "missing started time", raw: strings.Replace(feat137CanonicalApprovalParams, `"startedAtMs":137000,`, "", 1)},
		{name: "fractional started time", raw: strings.Replace(feat137CanonicalApprovalParams, `137000`, `137000.5`, 1)},
		{name: "non-null approval id", raw: strings.Replace(feat137CanonicalApprovalParams, `"approvalId":null`, `"approvalId":"approval"`, 1)},
		{name: "empty environment", raw: strings.Replace(feat137CanonicalApprovalParams, `"environmentId":"host-local"`, `"environmentId":""`, 1)},
		{name: "two command actions", raw: strings.Replace(feat137CanonicalApprovalParams,
			`[{"type":"unknown","command":"git rev-parse --is-inside-work-tree"}]`,
			`[{"type":"unknown","command":"git rev-parse --is-inside-work-tree"},{"type":"unknown","command":"other"}]`, 1)},
		{name: "unknown command action field", raw: strings.Replace(feat137CanonicalApprovalParams,
			`{"type":"unknown","command":"git rev-parse --is-inside-work-tree"}`,
			`{"type":"unknown","command":"git rev-parse --is-inside-work-tree","extra":true}`, 1)},
		{name: "duplicate thread id", raw: strings.Replace(feat137CanonicalApprovalParams,
			`"threadId":"thread-137"`, `"threadId":"thread-137","threadId":"thread-other"`, 1)},
		{name: "duplicate action command", raw: strings.Replace(feat137CanonicalApprovalParams,
			`"command":"git rev-parse --is-inside-work-tree"}],`,
			`"command":"git rev-parse --is-inside-work-tree","command":"other"}],`, 1)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := decodeCommandApprovalParams(json.RawMessage(test.raw)); err == nil {
				t.Fatal("closed decoder accepted invalid command approval params")
			}
		})
	}
}

func TestFEAT137CommandApprovalMapper(t *testing.T) {
	for _, decision := range []string{"accept", "cancel"} {
		t.Run(decision, func(t *testing.T) {
			config := DefaultConfig()
			config.CommandApprovalEnabled = true
			manager := NewManager(config, nil)
			handler := newFEAT137ApprovalHandler(CommandApprovalResult{Respond: true, Decision: decision})
			if err := manager.SetCommandApprovalHandler(handler); err != nil {
				t.Fatal(err)
			}
			result := manager.handleServerRequest(context.Background(), serverRequest{
				id:            json.RawMessage(`7`),
				idKey:         "n:7",
				method:        RuntimeMethodCommandApproval,
				params:        json.RawMessage(feat137CanonicalApprovalParams),
				exactEnvelope: true,
			})
			if !result.respond || result.rpcError != nil {
				t.Fatalf("unexpected mapped result: %+v", result)
			}
			encoded, err := json.Marshal(result.value)
			if err != nil {
				t.Fatal(err)
			}
			if string(encoded) != `{"decision":"`+decision+`"}` {
				t.Fatalf("unexpected Runtime decision response: %s", encoded)
			}
			request := <-handler.requests
			if request.RuntimeGeneration == "" || request.RuntimeGeneration != manager.runtimeGeneration ||
				request.RequestIDKey != "n:7" || request.ThreadID != "thread-137" ||
				request.TurnID != "turn-137" || request.ItemID != "item-137" || request.StartedAtMS != 137000 ||
				request.Command != "git rev-parse --is-inside-work-tree" || request.Cwd != "/workspace" ||
				request.EnvironmentID == nil || *request.EnvironmentID != "host-local" || len(request.CommandActions) != 1 {
				t.Fatalf("approval mapper lost authority fields: %+v", request)
			}
		})
	}

	config := DefaultConfig()
	config.CommandApprovalEnabled = true
	manager := NewManager(config, nil)
	handler := newFEAT137ApprovalHandler(CommandApprovalResult{Respond: true, Decision: "accept"})
	if err := manager.SetCommandApprovalHandler(handler); err != nil {
		t.Fatal(err)
	}
	for _, request := range []serverRequest{
		{
			idKey: "n:8", method: RuntimeMethodCommandApproval,
			params: json.RawMessage(feat137CanonicalApprovalParams), exactEnvelope: false,
		},
		{
			idKey: "n:9", method: RuntimeMethodCommandApproval,
			params: json.RawMessage(`{"threadId":"thread-137"}`), exactEnvelope: true,
		},
	} {
		result := manager.handleServerRequest(context.Background(), request)
		encoded, err := json.Marshal(result.value)
		if err != nil {
			t.Fatal(err)
		}
		if !result.respond || result.rpcError != nil || string(encoded) != `{"decision":"cancel"}` {
			t.Fatalf("invalid approval did not fail closed: %+v %s", result, encoded)
		}
	}
	select {
	case request := <-handler.requests:
		if request.RuntimeGeneration != manager.runtimeGeneration || request.RequestIDKey != "n:9" ||
			request.ThreadID != "" || request.Command != "" {
			t.Fatalf("malformed params did not reach only the replay authority identity: %+v", request)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("malformed exact-envelope approval did not close its typed replay identity")
	}
	select {
	case request := <-handler.requests:
		t.Fatalf("non-exact approval unexpectedly reached replay authority: %+v", request)
	default:
	}

	t.Run("invalid handler decision is not forwarded", func(t *testing.T) {
		invalidManager := NewManager(config, nil)
		invalidHandler := newFEAT137ApprovalHandler(CommandApprovalResult{
			Respond: true, Decision: "acceptForSession",
		})
		if err := invalidManager.SetCommandApprovalHandler(invalidHandler); err != nil {
			t.Fatal(err)
		}
		result := invalidManager.handleServerRequest(context.Background(), serverRequest{
			idKey: "n:10", method: RuntimeMethodCommandApproval,
			params: json.RawMessage(feat137CanonicalApprovalParams), exactEnvelope: true,
		})
		if !result.respond || result.rpcError == nil || result.rpcError.Code != -32603 || result.value != nil {
			t.Fatalf("invalid decision escaped the response allowlist: %+v", result)
		}
	})

	t.Run("disabled gate is method not found", func(t *testing.T) {
		disabled := NewManager(DefaultConfig(), nil)
		result := disabled.handleServerRequest(context.Background(), serverRequest{
			idKey: "n:11", method: RuntimeMethodCommandApproval,
			params: json.RawMessage(feat137CanonicalApprovalParams), exactEnvelope: true,
		})
		if !result.respond || result.rpcError == nil || result.rpcError.Code != methodNotFoundCode || result.value != nil {
			t.Fatalf("disabled approval gate did not fail closed: %+v", result)
		}
	})
}

func TestFEAT137DoesNotTightenExistingDynamicToolEnvelope(t *testing.T) {
	config := DefaultConfig()
	config.DynamicToolsEnabled = true
	manager := NewManager(config, nil)
	called := false
	if err := manager.SetDynamicToolHandler(func(_ context.Context, call DynamicToolCall) DynamicToolResult {
		called = true
		return DynamicToolResult{Success: true, Text: call.CallID}
	}); err != nil {
		t.Fatal(err)
	}
	result := manager.handleServerRequest(context.Background(), serverRequest{
		idKey:         "s:existing-dynamic-tool",
		method:        RuntimeMethodDynamicToolCall,
		params:        json.RawMessage(`{"threadId":"thread","turnId":"turn","callId":"call","tool":"generate_image","arguments":{}}`),
		exactEnvelope: false,
	})
	if !called || !result.respond || result.rpcError != nil {
		t.Fatalf("FEAT-137 changed the existing dynamic-tool envelope behavior: called=%v result=%+v", called, result)
	}
}

func TestFEAT137ResolutionAckMappingPreservesTypedRequestID(t *testing.T) {
	config := DefaultConfig()
	config.CommandApprovalEnabled = true
	manager := NewManager(config, nil)
	handler := newFEAT137ApprovalHandler(CommandApprovalResult{})
	if err := manager.SetCommandApprovalHandler(handler); err != nil {
		t.Fatal(err)
	}
	genericNotifications := make(chan string, 1)
	if err := manager.SetNotificationHandler(func(method string, _ json.RawMessage) {
		genericNotifications <- method
	}); err != nil {
		t.Fatal(err)
	}

	for _, test := range []struct {
		params  string
		wantKey string
	}{
		{params: `{"requestId":7,"threadId":"thread-137"}`, wantKey: "n:7"},
		{params: `{"requestId":"7","threadId":"thread-137"}`, wantKey: "s:7"},
	} {
		manager.handleNotification(RuntimeNotificationServerRequestResolved, json.RawMessage(test.params))
		select {
		case resolved := <-handler.resolved:
			if resolved.RuntimeGeneration != manager.runtimeGeneration || resolved.RequestIDKey != test.wantKey ||
				resolved.ThreadID != "thread-137" {
				t.Fatalf("resolution mapping drifted: %+v", resolved)
			}
		default:
			t.Fatal("matching resolution notification was not mapped")
		}
	}

	for _, malformed := range []string{
		`{"requestId":7,"threadId":"thread-137","extra":true}`,
		`{"requestId":7,"requestId":8,"threadId":"thread-137"}`,
		`{"requestId":{},"threadId":"thread-137"}`,
		`{"requestId":7,"threadId":""}`,
		`{"requestId":7}`,
	} {
		manager.handleNotification(RuntimeNotificationServerRequestResolved, json.RawMessage(malformed))
	}
	select {
	case resolved := <-handler.resolved:
		t.Fatalf("malformed resolution reached handler: %+v", resolved)
	default:
	}
	select {
	case method := <-genericNotifications:
		t.Fatalf("approval resolution leaked to generic notification handler: %q", method)
	default:
	}
}

type feat137WireRequest struct {
	ID     json.RawMessage `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
}

type feat137WireHarness struct {
	manager   *Manager
	requests  <-chan feat137WireRequest
	responses chan<- json.RawMessage
}

func newFEAT137WireHarness(t *testing.T, commandApprovalEnabled, dynamicToolsEnabled bool) feat137WireHarness {
	t.Helper()
	hostWrites, runtimeReads := io.Pipe()
	hostReads, runtimeWrites := io.Pipe()
	requests := make(chan feat137WireRequest, 4)
	responses := make(chan json.RawMessage, 4)
	client := NewClient(runtimeReads, hostReads, 1<<20, 8, nil, nil)
	client.Start()
	go func() {
		decoder := json.NewDecoder(hostWrites)
		encoder := json.NewEncoder(runtimeWrites)
		for {
			var request feat137WireRequest
			if err := decoder.Decode(&request); err != nil {
				return
			}
			requests <- request
			result, ok := <-responses
			if !ok {
				return
			}
			if err := encoder.Encode(struct {
				ID     json.RawMessage `json:"id"`
				Result json.RawMessage `json:"result"`
			}{ID: request.ID, Result: result}); err != nil {
				return
			}
		}
	}()

	config := DefaultConfig()
	config.RequestTimeout = 2 * time.Second
	config.MiniMax = MiniMaxConfig{Enabled: true, APIKey: "unit-test-only"}
	config.CommandApprovalEnabled = commandApprovalEnabled
	config.DynamicToolsEnabled = dynamicToolsEnabled
	manager := NewManager(config, nil)
	if dynamicToolsEnabled {
		if err := manager.SetDynamicToolHandler(func(context.Context, DynamicToolCall) DynamicToolResult {
			return DynamicToolResult{Success: false, Text: "not called"}
		}); err != nil {
			t.Fatal(err)
		}
	}
	manager.mu.Lock()
	manager.client = client
	manager.status.State = StateReady
	manager.status.Ready = true
	manager.mu.Unlock()

	t.Cleanup(func() {
		close(responses)
		_ = client.CloseInput()
		_ = runtimeWrites.Close()
	})
	return feat137WireHarness{
		manager: manager, requests: requests, responses: responses,
	}
}

func (h feat137WireHarness) nextRequest(t *testing.T) feat137WireRequest {
	t.Helper()
	select {
	case request := <-h.requests:
		return request
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for in-memory Runtime request")
		return feat137WireRequest{}
	}
}

func TestFEAT137SessionWireUsesOnRequestReadOnlyPolicy(t *testing.T) {
	harness := newFEAT137WireHarness(t, true, false)
	workspace := t.TempDir()
	threadDone := make(chan error, 1)
	go func() {
		_, err := harness.manager.StartThread(context.Background(), workspace)
		threadDone <- err
	}()
	threadRequest := harness.nextRequest(t)
	if threadRequest.Method != RuntimeMethodThreadStart {
		t.Fatalf("unexpected thread method: %q", threadRequest.Method)
	}
	var threadParams map[string]any
	if err := json.Unmarshal(threadRequest.Params, &threadParams); err != nil {
		t.Fatal(err)
	}
	if threadParams["approvalPolicy"] != SessionApprovalPolicyOnRequest ||
		threadParams["sandbox"] != SessionSandbox ||
		threadParams["developerInstructions"] != feat137ManagedInstructions ||
		threadParams["ephemeral"] != false {
		t.Fatalf("thread/start policy drifted: %s", threadRequest.Params)
	}
	if _, present := threadParams["dynamicTools"]; present {
		t.Fatalf("command approval profile enabled dynamic tools: %s", threadRequest.Params)
	}
	harness.responses <- json.RawMessage(`{
		"thread":{"id":"thread-137","sessionId":"session-137","turns":[]},
		"model":"MiniMax-M3","modelProvider":"minimax"
	}`)
	if err := <-threadDone; err != nil {
		t.Fatal(err)
	}

	resumeDone := make(chan error, 1)
	go func() {
		_, err := harness.manager.ResumeThread(context.Background(), "thread-137")
		resumeDone <- err
	}()
	resumeRequest := harness.nextRequest(t)
	if resumeRequest.Method != RuntimeMethodThreadResume {
		t.Fatalf("unexpected resume method: %q", resumeRequest.Method)
	}
	var resumeParams map[string]any
	if err := json.Unmarshal(resumeRequest.Params, &resumeParams); err != nil {
		t.Fatal(err)
	}
	if resumeParams["threadId"] != "thread-137" ||
		resumeParams["approvalPolicy"] != SessionApprovalPolicyOnRequest ||
		resumeParams["sandbox"] != SessionSandbox ||
		resumeParams["developerInstructions"] != feat137ManagedInstructions {
		t.Fatalf("thread/resume policy drifted: %s", resumeRequest.Params)
	}
	harness.responses <- json.RawMessage(`{
		"thread":{"id":"thread-137","sessionId":"session-137","turns":[]},
		"model":"MiniMax-M3","modelProvider":"minimax"
	}`)
	if err := <-resumeDone; err != nil {
		t.Fatal(err)
	}

	turnDone := make(chan error, 1)
	go func() {
		_, err := harness.manager.StartTurn(context.Background(), "thread-137", "read only", "none")
		turnDone <- err
	}()
	turnRequest := harness.nextRequest(t)
	if turnRequest.Method != RuntimeMethodTurnStart {
		t.Fatalf("unexpected turn method: %q", turnRequest.Method)
	}
	var turnParams map[string]any
	if err := json.Unmarshal(turnRequest.Params, &turnParams); err != nil {
		t.Fatal(err)
	}
	sandbox, ok := turnParams["sandboxPolicy"].(map[string]any)
	if turnParams["approvalPolicy"] != SessionApprovalPolicyOnRequest || !ok ||
		sandbox["type"] != "readOnly" || sandbox["networkAccess"] != false {
		t.Fatalf("turn/start policy drifted: %s", turnRequest.Params)
	}
	harness.responses <- json.RawMessage(`{"turn":{"id":"turn-137","status":"inProgress"}}`)
	if err := <-turnDone; err != nil {
		t.Fatal(err)
	}

	if harness.manager.Snapshot().ExperimentalAPI {
		t.Fatal("stable command approval profile enabled experimentalApi")
	}
	defaultManager := NewManager(DefaultConfig(), nil)
	if defaultManager.sessionApprovalPolicy() != SessionApprovalPolicy ||
		sessionTurnSandboxPolicy() != (readOnlySandboxPolicy{Type: "readOnly", NetworkAccess: false}) {
		t.Fatal("default read-only/never policy drifted")
	}
}

func TestFEAT137GateOffPreservesBaselineSessionWire(t *testing.T) {
	t.Run("resume and turns omit v6 overrides", func(t *testing.T) {
		harness := newFEAT137WireHarness(t, false, false)
		workspace := t.TempDir()
		threadDone := make(chan error, 1)
		go func() {
			_, err := harness.manager.StartThread(context.Background(), workspace)
			threadDone <- err
		}()
		threadRequest := harness.nextRequest(t)
		var threadParams map[string]any
		if err := json.Unmarshal(threadRequest.Params, &threadParams); err != nil {
			t.Fatal(err)
		}
		if threadRequest.Method != RuntimeMethodThreadStart ||
			threadParams["approvalPolicy"] != SessionApprovalPolicy ||
			threadParams["sandbox"] != SessionSandbox ||
			threadParams["developerInstructions"] !=
				"Runtime Baseline 2 is read-only for workspace and operating-system actions. Do not modify files or request elevated permissions." {
			t.Fatalf("gate-off thread/start baseline drifted: %s", threadRequest.Params)
		}
		if _, present := threadParams["dynamicTools"]; present {
			t.Fatalf("gate-off baseline unexpectedly registered dynamic tools: %s", threadRequest.Params)
		}
		harness.responses <- json.RawMessage(`{
			"thread":{"id":"thread-137","sessionId":"session-137","turns":[]},
			"model":"MiniMax-M3","modelProvider":"minimax"
		}`)
		if err := <-threadDone; err != nil {
			t.Fatal(err)
		}

		resumeDone := make(chan error, 1)
		go func() {
			_, err := harness.manager.ResumeThread(context.Background(), "thread-137")
			resumeDone <- err
		}()
		resumeRequest := harness.nextRequest(t)
		var resumeParams map[string]any
		if err := json.Unmarshal(resumeRequest.Params, &resumeParams); err != nil {
			t.Fatal(err)
		}
		if resumeRequest.Method != RuntimeMethodThreadResume || len(resumeParams) != 1 ||
			resumeParams["threadId"] != "thread-137" {
			t.Fatalf("gate-off thread/resume added v6 overrides: %s", resumeRequest.Params)
		}
		harness.responses <- json.RawMessage(`{
			"thread":{"id":"thread-137","sessionId":"session-137","turns":[]},
			"model":"MiniMax-M3","modelProvider":"minimax"
		}`)
		if err := <-resumeDone; err != nil {
			t.Fatal(err)
		}

		turnDone := make(chan error, 1)
		go func() {
			_, err := harness.manager.StartTurn(context.Background(), "thread-137", "read only", "none")
			turnDone <- err
		}()
		turnRequest := harness.nextRequest(t)
		var turnParams map[string]any
		if err := json.Unmarshal(turnRequest.Params, &turnParams); err != nil {
			t.Fatal(err)
		}
		if turnRequest.Method != RuntimeMethodTurnStart || len(turnParams) != 3 {
			t.Fatalf("gate-off turn/start added v6 overrides: %s", turnRequest.Params)
		}
		if _, present := turnParams["approvalPolicy"]; present {
			t.Fatalf("gate-off turn/start serialized approvalPolicy: %s", turnRequest.Params)
		}
		if _, present := turnParams["sandboxPolicy"]; present {
			t.Fatalf("gate-off turn/start serialized sandboxPolicy: %s", turnRequest.Params)
		}
		harness.responses <- json.RawMessage(`{"turn":{"id":"turn-137","status":"inProgress"}}`)
		if err := <-turnDone; err != nil {
			t.Fatal(err)
		}

		input, err := TextUserInput("read only v2")
		if err != nil {
			t.Fatal(err)
		}
		turnV2Done := make(chan error, 1)
		go func() {
			_, err := harness.manager.StartTurnV2(context.Background(), "thread-137", []UserInput{input}, "none")
			turnV2Done <- err
		}()
		turnV2Request := harness.nextRequest(t)
		var turnV2Params map[string]any
		if err := json.Unmarshal(turnV2Request.Params, &turnV2Params); err != nil {
			t.Fatal(err)
		}
		if turnV2Request.Method != RuntimeMethodTurnStart || len(turnV2Params) != 3 {
			t.Fatalf("gate-off turn/start v2 added v6 overrides: %s", turnV2Request.Params)
		}
		if _, present := turnV2Params["approvalPolicy"]; present {
			t.Fatalf("gate-off turn/start v2 serialized approvalPolicy: %s", turnV2Request.Params)
		}
		if _, present := turnV2Params["sandboxPolicy"]; present {
			t.Fatalf("gate-off turn/start v2 serialized sandboxPolicy: %s", turnV2Request.Params)
		}
		harness.responses <- json.RawMessage(`{"turn":{"id":"turn-137-v2","status":"inProgress"}}`)
		if err := <-turnV2Done; err != nil {
			t.Fatal(err)
		}
	})

	t.Run("dynamic tool thread start keeps baseline exception", func(t *testing.T) {
		harness := newFEAT137WireHarness(t, false, true)
		workspace := t.TempDir()
		threadDone := make(chan error, 1)
		go func() {
			_, err := harness.manager.StartThread(context.Background(), workspace)
			threadDone <- err
		}()
		request := harness.nextRequest(t)
		var params map[string]any
		if err := json.Unmarshal(request.Params, &params); err != nil {
			t.Fatal(err)
		}
		tools, toolsOK := params["dynamicTools"].([]any)
		instructions, instructionsOK := params["developerInstructions"].(string)
		if request.Method != RuntimeMethodThreadStart || params["approvalPolicy"] != SessionApprovalPolicy ||
			params["sandbox"] != SessionSandbox || !toolsOK || len(tools) != 1 || !instructionsOK ||
			!strings.Contains(instructions, "explicitly allowed in this read-only session") ||
			!strings.Contains(instructions, "Never call it for ordinary image viewing or analysis") {
			t.Fatalf("gate-off dynamic-tool thread/start baseline drifted: %s", request.Params)
		}
		harness.responses <- json.RawMessage(`{
			"thread":{"id":"thread-dynamic","sessionId":"session-dynamic","turns":[]},
			"model":"MiniMax-M3","modelProvider":"minimax"
		}`)
		if err := <-threadDone; err != nil {
			t.Fatal(err)
		}
	})
}
