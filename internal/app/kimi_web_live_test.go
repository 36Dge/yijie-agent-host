package app

// Opt-in, one-shot supplier acceptance. This is a deterministic MCP client,
// not a model or Native/Runtime impersonation acceptance claim. It uses the
// actual product worker and both existing approval decisions. No product code
// or credential handling is replaced. Never include this in ordinary CI.
import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	wire "github.com/36Dge/yijie-agent-host/internal/contracts/marketbrokercontrol"
	provider "github.com/36Dge/yijie-agent-host/internal/contracts/marketprovider"
	selection "github.com/36Dge/yijie-agent-host/internal/contracts/marketselection"
	"github.com/36Dge/yijie-agent-host/internal/marketbroker"
	"github.com/google/uuid"
	"github.com/santhosh-tekuri/jsonschema/v6"
)

const kimiLiveWorkerSHA = "ebf7c834c95f4614365981aa7b99d04dcff37eb4d709cf4f84d0b04a8e073404"
const kimiLiveURL = "https://www.iana.org/help/example-domains"
const kimiLiveQuery = "IANA example domains"

type kimiLiveFrame struct {
	ID     json.RawMessage `json:"id,omitempty"`
	Method string          `json:"method,omitempty"`
	Params json.RawMessage `json:"params,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  json.RawMessage `json:"error,omitempty"`
}
type kimiLiveMCP struct {
	client                    *http.Client
	endpoint, bearer, session string // Private capabilities: never written to evidence.
	version                   string
	sequence                  int
}

func (m *kimiLiveMCP) send(ctx context.Context, method string, body any) (*http.Response, error) {
	u, err := url.Parse(m.endpoint)
	if err != nil || u.Scheme != "http" || u.Hostname() != "127.0.0.1" || u.Port() == "" || !strings.HasPrefix(u.Path, "/mcp/") || u.RawQuery != "" {
		return nil, errors.New("invalid local gateway")
	}
	var data []byte
	if body != nil {
		data, err = json.Marshal(body)
		if err != nil {
			return nil, errors.New("invalid local request")
		}
	}
	req, err := http.NewRequestWithContext(ctx, method, m.endpoint, bytes.NewReader(data))
	if err != nil {
		return nil, errors.New("local request unavailable")
	}
	req.Header.Set("Authorization", "Bearer "+m.bearer)
	req.Header.Set("Accept", "application/json, text/event-stream")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if m.session != "" {
		req.Header.Set("Mcp-Session-Id", m.session)
		req.Header.Set("MCP-Protocol-Version", m.version)
	}
	r, err := m.client.Do(req)
	if err != nil {
		return nil, errors.New("local gateway transport unavailable")
	}
	return r, nil
}
func kimiLiveRead(r *http.Response, visit func(kimiLiveFrame) error) error {
	defer r.Body.Close()
	if r.StatusCode != http.StatusOK {
		return errors.New("local gateway status unavailable")
	}
	if strings.Contains(r.Header.Get("Content-Type"), "text/event-stream") {
		scanner := bufio.NewScanner(io.LimitReader(r.Body, 2<<20))
		scanner.Buffer(make([]byte, 4096), 256<<10)
		var data []string
		for scanner.Scan() {
			line := scanner.Text()
			if line == "" {
				if len(data) > 0 {
					payload := strings.Join(data, "\n")
					data = nil
					// rmcp SEP-1699 priming is a valid SSE control event, not JSON-RPC.
					if payload == "" {
						continue
					}
					var f kimiLiveFrame
					if json.Unmarshal([]byte(payload), &f) != nil {
						return errors.New("invalid local SSE frame")
					}
					if err := visit(f); err != nil {
						return err
					}
				}
				continue
			}
			if strings.HasPrefix(line, "data:") {
				data = append(data, strings.TrimPrefix(strings.TrimPrefix(line, "data:"), " "))
			}
		}
		if scanner.Err() != nil {
			return errors.New("local SSE stream ended before completion")
		}
		return nil
	}
	var f kimiLiveFrame
	if json.NewDecoder(io.LimitReader(r.Body, 256<<10)).Decode(&f) != nil {
		return errors.New("invalid local JSON frame")
	}
	return visit(f)
}
func (m *kimiLiveMCP) notify(ctx context.Context, body any) error {
	r, err := m.send(ctx, "POST", body)
	if err != nil {
		return err
	}
	defer r.Body.Close()
	if r.StatusCode != http.StatusAccepted {
		return errors.New("local acknowledgement missing")
	}
	return nil
}
func (m *kimiLiveMCP) request(ctx context.Context, method string, params any) (json.RawMessage, error) {
	m.sequence++
	id := m.sequence
	r, err := m.send(ctx, "POST", map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params})
	if err != nil {
		return nil, err
	}
	if m.session == "" {
		m.session = r.Header.Get("Mcp-Session-Id")
	}
	var result json.RawMessage
	count := 0
	err = kimiLiveRead(r, func(f kimiLiveFrame) error {
		var echoed int
		if f.Method != "" || len(f.Error) > 0 || json.Unmarshal(f.ID, &echoed) != nil || echoed != id {
			return errors.New("unexpected local MCP reply")
		}
		count++
		result = f.Result
		return nil
	})
	if err != nil {
		return nil, err
	}
	if count != 1 || len(result) == 0 {
		return nil, errors.New("local MCP result missing")
	}
	return result, nil
}
func kimiLiveID() wire.CanonicalId { return wire.CanonicalId(uuid.NewString()) }

func kimiLiveApprovalMetadata(raw json.RawMessage) (wire.ElicitationMetadata, error) {
	var params struct {
		Meta map[string]json.RawMessage `json:"_meta"`
	}
	var metadata wire.ElicitationMetadata
	if json.Unmarshal(raw, &params) != nil || params.Meta == nil {
		return metadata, errors.New("approval metadata unavailable")
	}
	// Match the existing Codex rmcp client restore_context_meta behavior:
	// progressToken is transport bookkeeping, not the closed application locator.
	delete(params.Meta, "progressToken")
	encoded, err := json.Marshal(params.Meta)
	if err != nil || json.Unmarshal(encoded, &metadata) != nil {
		return metadata, errors.New("invalid approval locator")
	}
	return metadata, nil
}

func kimiLiveSave(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err = f.Write(data); err != nil {
		return err
	}
	return f.Sync()
}
func kimiLiveDiagnostics(home string) ([]map[string]any, int, error) {
	paths, err := filepath.Glob(filepath.Join(home, "diagnostics", "provider", "*.jsonl"))
	if err != nil {
		return nil, 0, err
	}
	var events []map[string]any
	sent := 0
	for _, p := range paths {
		b, err := os.ReadFile(p)
		if err != nil {
			return nil, 0, err
		}
		for _, line := range bytes.Split(bytes.TrimSpace(b), []byte{'\n'}) {
			if len(line) == 0 {
				continue
			}
			var event map[string]any
			if json.Unmarshal(line, &event) != nil {
				return nil, 0, errors.New("invalid diagnostic")
			}
			event["operation_id"] = strings.TrimSuffix(filepath.Base(p), ".jsonl")
			events = append(events, event)
			if event["stage"] == "web_http_send" && event["event"] == "request" {
				sent++
			}
		}
	}
	return events, sent, nil
}

// Ordinary local protocol examples; this never starts a worker or loads a key.
func TestFEAT162LiveReaderNormalJSONAndSSE(t *testing.T) {
	for _, sample := range []struct{ kind, body string }{
		{"application/json", `{"jsonrpc":"2.0","id":1,"result":{"ok":true}}`},
		{"text/event-stream", ": keepalive\n\nevent: message\nid: 1/0\ndata: {\"jsonrpc\":\"2.0\",\"id\":1,\"result\":{\"ok\":true}}\n\n"},
		// rmcp's normal stream begins with an empty-data retry/priming event.
		{"text/event-stream", "id: 0/0\nretry: 3000\ndata:\n\nevent: message\nid: 1/0\ndata: {\"jsonrpc\":\"2.0\",\"id\":1,\"result\":{\"ok\":true}}\n\n"},
	} {
		count := 0
		r := &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{sample.kind}}, Body: io.NopCloser(strings.NewReader(sample.body))}
		if err := kimiLiveRead(r, func(f kimiLiveFrame) error {
			count++
			if string(f.ID) != "1" || string(f.Result) != `{"ok":true}` {
				return errors.New("ordinary frame differs")
			}
			return nil
		}); err != nil || count != 1 {
			t.Fatal("normal protocol parsing failed")
		}
	}
	id := kimiLiveID()
	raw, _ := json.Marshal(map[string]any{"_meta": map[string]any{"yijieMarketCallRef": id, "yijieKind": "gateway_call_approval", "progressToken": 0}})
	metadata, err := kimiLiveApprovalMetadata(raw)
	if err != nil || metadata.YijieMarketCallRef != id {
		t.Fatal("normal MCP progress metadata did not preserve application locator")
	}
}

func TestFEAT162LiveMinimalSearchFetch(t *testing.T) {
	mode := os.Getenv("YIJIE_FEAT162_LIVE_TEST")
	preflight := mode == "local-preflight-no-credentials-no-supplier-calls"
	if !preflight && mode != "approved-search1-fetch1-cny002-no-model" {
		t.Skip("explicit one-shot user approval required")
	}
	expectedWorkerSHA := kimiLiveWorkerSHA // Historical paid run remains pinned and closed.
	if preflight {
		expectedWorkerSHA = os.Getenv("YIJIE_FEAT162_PREFLIGHT_WORKER_SHA256")
		decoded, decodeErr := hex.DecodeString(expectedWorkerSHA)
		if decodeErr != nil || len(decoded) != sha256.Size || expectedWorkerSHA != strings.ToLower(expectedWorkerSHA) {
			t.Fatal("reviewed canonical preflight worker SHA256 required")
		}
	}
	root := os.Getenv("YIJIE_FEAT162_LIVE_ROOT")
	manifest := os.Getenv("YIJIE_MARKET_BROKER_TEST_MANIFEST")
	canonical := os.Getenv("YIJIE_FEAT162_TOOLS_JSON")
	if !filepath.IsAbs(root) || !filepath.IsAbs(manifest) || !filepath.IsAbs(canonical) {
		t.Fatal("absolute reviewed paths required")
	}
	marker, err := os.OpenFile(filepath.Join(root, "started.json"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal("one-shot run already reserved or unavailable; do not retry")
	}
	reservation := "{\"search_max\":1,\"fetch_max\":1,\"budget_cny\":\"0.02\",\"model_max\":0,\"retry_max\":0}\n"
	if preflight {
		reservation = "{\"mode\":\"local_preflight\",\"search_max\":0,\"fetch_max\":0,\"budget_cny\":\"0.00\",\"model_max\":0}\n"
	}
	_, err = marker.WriteString(reservation)
	if err == nil {
		err = marker.Sync()
	}
	marker.Close()
	if err != nil {
		t.Fatal("one-shot budget reservation unavailable")
	}
	report := map[string]any{"started_at": time.Now().UTC().Format(time.RFC3339Nano), "status": "STOPPED", "stage": "setup", "model_calls": 0, "search_allowances_consumed": 0, "fetch_allowances_consumed": 0, "budget_cny": "0.02", "retry_count": 0, "real_native_or_model_turn": false, "worker_sha256": expectedWorkerSHA}
	report["local_preflight"] = preflight
	if preflight {
		report["budget_cny"] = "0.00"
	}
	var steps []map[string]any
	home := filepath.Join(root, "host-home", "market-worker", "library-home")
	defer func() {
		report["finished_at"] = time.Now().UTC().Format(time.RFC3339Nano)
		report["steps"] = steps
		events, sent, e := kimiLiveDiagnostics(home)
		report["supplier_http_sends"] = sent
		report["diagnostics"] = events
		if e != nil {
			report["diagnostic_status"] = "unavailable"
		}
		if e = kimiLiveSave(filepath.Join(root, "result.json"), report); e != nil {
			t.Error("acceptance evidence write failed")
		}
	}()
	fail := func(stage string) { report["stage"] = stage; t.Fatal(stage + "; stopped without retry") }
	mb, err := os.ReadFile(manifest)
	if err != nil {
		fail("manifest_read")
	}
	var artifact struct {
		Binary        string `json:"binary"`
		SHA           string `json:"sha256"`
		External      bool   `json:"externalCallsEnabled"`
		Qualification bool   `json:"qualificationOnly"`
	}
	if json.Unmarshal(mb, &artifact) != nil || artifact.SHA != expectedWorkerSHA || !artifact.External || artifact.Qualification {
		fail("canonical_worker_identity")
	}
	bb, err := os.ReadFile(artifact.Binary)
	if err != nil {
		fail("worker_read")
	}
	sum := sha256.Sum256(bb)
	if hex.EncodeToString(sum[:]) != expectedWorkerSHA {
		fail("worker_digest")
	}
	var descriptors []map[string]any
	raw, err := os.ReadFile(canonical)
	if err != nil || json.Unmarshal(raw, &descriptors) != nil || len(descriptors) != 2 {
		fail("canonical_descriptors")
	}
	expected := map[string]map[string]any{}
	validators := map[string]*jsonschema.Schema{}
	for _, desc := range descriptors {
		name, ok := desc["name"].(string)
		if !ok || (name != "kimi_web_search" && name != "kimi_web_fetch") {
			fail("canonical_tool_name")
		}
		expected[name] = desc
		compiler := jsonschema.NewCompiler()
		id := "https://example.invalid/feat162/" + name + ".json"
		if compiler.AddResource(id, desc["outputSchema"]) != nil {
			fail("canonical_schema")
		}
		schema, err := compiler.Compile(id)
		if err != nil {
			fail("canonical_schema_compile")
		}
		validators[name] = schema
	}
	key := "ordinary-local-preflight-value"
	if !preflight {
		key, err = loadKimiAPIKey()
		if err != nil || key == "" {
			fail("existing_model_credential_unavailable")
		}
	}
	if err = os.Mkdir(filepath.Join(root, "host-home"), 0700); err != nil {
		fail("private_host_home")
	}
	owner, err := marketbroker.NewOwnerWithModelCredential(manifest, home, key)
	key = ""
	if err != nil {
		fail("owner_assembly")
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		e := owner.Close(ctx)
		report["normal_eof_confirmed"] = e == nil && owner.NormalExitConfirmed()
		if e != nil {
			report["status"] = "STOPPED"
			report["cleanup"] = "STOP_PENDING; owner retained, no signals"
			t.Error("normal EOF not confirmed; no force termination")
		} else {
			report["cleanup"] = "normal EOF exit confirmed"
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Second)
	defer cancel()
	process := wire.ProcessBinding{HostInstanceId: kimiLiveID(), RuntimeGeneration: kimiLiveID()}
	init, err := owner.Initialize(ctx, wire.InitializeRequest{SchemaVersion: 1, RequestId: kimiLiveID(), Method: "initialize", Payload: wire.InitializePayload{OperationId: kimiLiveID(), Process: process}})
	if err != nil || !init.Data.ExternalCallsEnabled {
		fail("owner_initialize")
	}
	scope := wire.ScopeBinding{OwnerUserId: kimiLiveID(), TenantId: kimiLiveID(), NativeProcessEpoch: kimiLiveID(), AuthorizationRevision: 1, AuthorizationExpiresAtUnixMs: wire.UnixMilliseconds(time.Now().Add(4 * time.Minute).UnixMilli())}
	ref := wire.SelectionRef{InstallationId: kimiLiveID(), Revision: 1, Generation: 1}
	payload := provider.ProviderPayload{OperationId: kimiLiveID(), HostInstanceId: process.HostInstanceId, Scope: scope, Binding: provider.ProviderBinding{ServiceId: "kimi-web", Reference: ref, CredentialRef: kimiLiveID()}}
	probe, err := owner.Probe(ctx, provider.ProbeRequest{SchemaVersion: 1, RequestId: kimiLiveID(), Method: "provider_probe", Payload: payload})
	if err != nil {
		fail("local_prepare")
	}
	for probe.Data.OperationState == "starting" {
		select {
		case <-ctx.Done():
			fail("local_prepare_timeout")
		case <-time.After(10 * time.Millisecond):
		}
		probe, err = owner.Probe(ctx, provider.ProbeRequest{SchemaVersion: 1, RequestId: kimiLiveID(), Method: "provider_probe", Payload: payload})
		if err != nil {
			fail("local_prepare_poll")
		}
	}
	if probe.Data.OperationState != "succeeded" || !probe.Data.ExecutionAvailable || probe.Data.Qualification != "qualified" {
		fail("local_prepare_not_ready")
	}
	thread := wire.NativeId("feat162-deterministic-no-model-thread")
	turn := wire.NativeId("feat162-deterministic-no-model-turn")
	operation := kimiLiveID()
	refs := selection.Selection{ref}
	digest, err := selection.SelectionDigest(string(operation), refs)
	if err != nil {
		fail("selection_digest")
	}
	prepared, err := owner.Prepare(ctx, wire.PrepareRequest{SchemaVersion: 1, RequestId: kimiLiveID(), Method: "prepare", Payload: wire.PreparePayload{OperationId: kimiLiveID(), Context: wire.ContextBinding{Process: process, Scope: scope, AgentSessionId: kimiLiveID(), NativeThreadId: &thread}, Snapshot: selection.SelectionSnapshot{SchemaVersion: 1, TurnOperationId: operation, Selection: refs, SelectionDigest: selection.SelectionDigestValue(digest)}, TtlMs: 120000}})
	if err != nil {
		fail("lease_prepare")
	}
	bound, err := owner.BindTurn(ctx, wire.BindTurnRequest{SchemaVersion: 1, RequestId: kimiLiveID(), Method: "bind_turn", Payload: wire.BindTurnPayload{OperationId: kimiLiveID(), Binding: prepared.Data.Binding, ExpectedRevision: prepared.Data.Revision, NativeThreadId: thread, NativeTurnId: turn}})
	if err != nil {
		fail("deterministic_client_binding")
	}
	env, err := owner.EnvironmentForRuntime(nil, process)
	if err != nil || len(env) != 1 {
		fail("private_gateway_capability")
	}
	_, bearer, ok := strings.Cut(env[0], "=")
	if !ok {
		fail("private_gateway_capability")
	}
	transport := &http.Transport{Proxy: nil, DisableKeepAlives: true}
	defer transport.CloseIdleConnections()
	m := &kimiLiveMCP{client: &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, endpoint: string(init.Data.GatewayUrl) + "/" + string(bound.Data.Binding.CapabilityRef), bearer: bearer, version: "2025-06-18"}
	initial, err := m.request(ctx, "initialize", map[string]any{"protocolVersion": "2025-06-18", "capabilities": map[string]any{"elicitation": map[string]any{"form": map[string]any{}}}, "clientInfo": map[string]any{"name": "feat162-approved-no-model-acceptance", "version": "1"}})
	if err != nil || m.session == "" {
		if err != nil {
			report["mcp_initialize_error"] = err.Error()
		}
		report["mcp_session_present"] = m.session != ""
		fail("mcp_initialize")
	}
	var negotiated struct {
		Version string `json:"protocolVersion"`
	}
	if json.Unmarshal(initial, &negotiated) != nil || (negotiated.Version != "2025-06-18" && negotiated.Version != "2025-11-25") {
		fail("mcp_protocol_version")
	}
	m.version = negotiated.Version
	if m.notify(ctx, map[string]any{"jsonrpc": "2.0", "method": "notifications/initialized"}) != nil {
		fail("mcp_initialized")
	}
	listed, err := m.request(ctx, "tools/list", map[string]any{})
	if err != nil {
		fail("mcp_list")
	}
	var tools struct {
		Tools []map[string]any `json:"tools"`
	}
	if json.Unmarshal(listed, &tools) != nil || len(tools.Tools) != 2 {
		fail("mcp_tool_count")
	}
	for _, tool := range tools.Tools {
		name, _ := tool["name"].(string)
		if expected[name] == nil || !reflect.DeepEqual(tool["inputSchema"], expected[name]["inputSchema"]) || !reflect.DeepEqual(tool["outputSchema"], expected[name]["outputSchema"]) {
			fail("mcp_schema_identity")
		}
	}
	events, sent, err := kimiLiveDiagnostics(home)
	if err != nil || sent != 0 || len(events) != 2 {
		fail("zero_http_local_prepare")
	}
	streamCtx, stopStream := context.WithCancel(ctx)
	defer stopStream()
	stream, err := m.send(streamCtx, "GET", nil)
	if err != nil || stream.StatusCode != http.StatusOK {
		if stream != nil {
			stream.Body.Close()
		}
		fail("mcp_callback_stream")
	}
	defer stream.Body.Close()
	callbacks := make(chan kimiLiveFrame, 4)
	streamDone := make(chan error, 1)
	go func() {
		streamDone <- kimiLiveRead(stream, func(f kimiLiveFrame) error {
			// Normal cancellation/progress notices are not new approval intents.
			if len(f.ID) == 0 && (f.Method == "notifications/cancelled" || f.Method == "notifications/progress") {
				return nil
			}
			select {
			case callbacks <- f:
				return nil
			case <-streamCtx.Done():
				return errors.New("local stream closed")
			}
		})
	}()
	defer func() {
		stopStream()
		stream.Body.Close()
		select {
		case <-streamDone:
		case <-time.After(3 * time.Second):
			report["status"] = "STOPPED"
			t.Error("local reader cleanup pending")
		}
	}()
	cases := []struct {
		Name, Tool string
		Args       map[string]any
		Approve    bool
	}{{"deny_fetch", "kimi_web_fetch", map[string]any{"url": kimiLiveURL}, false}, {"search", "kimi_web_search", map[string]any{"query": kimiLiveQuery, "limit": float64(3)}, true}, {"fetch", "kimi_web_fetch", map[string]any{"url": kimiLiveURL}, true}}
	if preflight {
		cases = cases[:1]
	} // Only the denied proposal; no approve_once path.
	for index, tc := range cases {
		report["stage"] = tc.Name
		step := map[string]any{"name": tc.Name, "tool": tc.Tool, "status": "STOPPED"}
		steps = append(steps, step)
		start := time.Now()
		if kimiLiveSave(filepath.Join(root, "progress.json"), report) != nil {
			fail("progress_write")
		}
		callCtx, stopCall := context.WithTimeout(ctx, 30*time.Second)
		resultChannel := make(chan struct {
			Data json.RawMessage
			Err  error
		}, 1)
		go func() {
			data, e := m.request(callCtx, "tools/call", map[string]any{"name": tc.Tool, "arguments": tc.Args, "_meta": map[string]any{"threadId": thread, "x-codex-turn-metadata": map[string]any{"turn_id": turn}}})
			resultChannel <- struct {
				Data json.RawMessage
				Err  error
			}{data, e}
		}()
		approved := false
		var result json.RawMessage
		for result == nil {
			select {
			case callback := <-callbacks:
				if callback.Method != "elicitation/create" || len(callback.ID) == 0 || approved {
					stopCall()
					fail("unexpected_approval_callback")
				}
				metadata, metadataErr := kimiLiveApprovalMetadata(callback.Params)
				if metadataErr != nil {
					stopCall()
					fail("approval_metadata")
				}
				pending, e := owner.PendingCall(ctx, wire.PendingCallRequest{SchemaVersion: 1, RequestId: kimiLiveID(), Method: "pending_call", Payload: wire.PendingCallPayload{Binding: bound.Data.Binding, NativeThreadId: thread, NativeTurnId: turn, CallRef: metadata.YijieMarketCallRef}})
				if e != nil || pending.Data.State != wire.CallStatePending || pending.Data.Identity.ServiceId != "kimi-web" || string(pending.Data.Identity.ToolName) != tc.Tool || pending.Data.Identity.Reference != ref || pending.Data.Review.ArgumentsJson == nil {
					stopCall()
					fail("pending_review")
				}
				var arguments map[string]any
				if json.Unmarshal([]byte(*pending.Data.Review.ArgumentsJson), &arguments) != nil || !reflect.DeepEqual(arguments, tc.Args) {
					stopCall()
					fail("frozen_arguments")
				}
				step["call_ref"] = pending.Data.Identity.CallRef
				decision := wire.DecisionReject
				action := "decline"
				if tc.Approve {
					budgetKey := tc.Name + "_allowances_consumed"
					if report[budgetKey] != 0 || index == 0 || steps[0]["status"] != "PASS" {
						stopCall()
						fail("one_call_budget")
					}
					report[budgetKey] = 1
					report["steps"] = steps
					if kimiLiveSave(filepath.Join(root, "progress.json"), report) != nil {
						stopCall()
						fail("budget_reservation_write")
					}
					decision = wire.DecisionApproveOnce
					action = "accept"
				}
				decided, e := owner.DecideCall(ctx, wire.DecideCallRequest{SchemaVersion: 1, RequestId: kimiLiveID(), Method: "decide_call", Payload: wire.DecideCallPayload{OperationId: kimiLiveID(), Identity: pending.Data.Identity, ExpectedRevision: pending.Data.Revision, ApprovalRef: kimiLiveID(), DecisionId: kimiLiveID(), Decision: decision}})
				if e != nil || decided.Data.Decision == nil || *decided.Data.Decision != decision {
					stopCall()
					fail("owner_decision")
				}
				approved = true
				if m.notify(callCtx, map[string]any{"jsonrpc": "2.0", "id": callback.ID, "result": map[string]any{"action": action, "content": map[string]any{}}}) != nil {
					stopCall()
					fail("elicitation_decision")
				}
			case returned := <-resultChannel:
				if returned.Err != nil || !approved {
					stopCall()
					fail("tool_result_unavailable")
				}
				result = returned.Data
			case <-callCtx.Done():
				stopCall()
				fail("tool_call_timeout")
			}
		}
		stopCall()
		step["elapsed_ms"] = time.Since(start).Milliseconds()
		if len(result) > 65536 {
			fail("mcp_result_budget")
		}
		var envelope struct {
			IsError bool `json:"isError"`
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
			Structured map[string]any `json:"structuredContent"`
		}
		if json.Unmarshal(result, &envelope) != nil {
			fail("tool_result_shape")
		}
		all, total, e := kimiLiveDiagnostics(home)
		if e != nil {
			fail("diagnostics_read")
		}
		var callEvents []map[string]any
		count := 0
		http200 := false
		for _, event := range all {
			if event["operation_id"] == string(step["call_ref"].(wire.CanonicalId)) {
				callEvents = append(callEvents, event)
				if event["stage"] == "web_http_send" && event["event"] == "request" {
					count++
				}
				if event["stage"] == "web_http_response" && event["httpStatus"] == float64(200) {
					http200 = true
				}
			}
		}
		step["http_sends"] = count
		step["diagnostics"] = callEvents
		if !tc.Approve {
			if !envelope.IsError || count != 0 || total != 0 || len(envelope.Content) != 1 || envelope.Content[0].Text != wire.GatewayErrorOwnerRejectedMessage {
				fail("denial_zero_http")
			}
			step["status"] = "PASS"
			report["denial_zero_http"] = "PASS"
			continue
		}
		step["result"] = envelope.Structured
		if envelope.IsError || count != 1 || total != index || !http200 {
			fail(tc.Name + "_supplier_or_admission_failed")
		}
		if validators[tc.Tool].Validate(envelope.Structured) != nil || len(envelope.Content) != 1 || envelope.Content[0].Type != "text" {
			fail("canonical_result_schema")
		}
		var textResult map[string]any
		if json.Unmarshal([]byte(envelope.Content[0].Text), &textResult) != nil || !reflect.DeepEqual(textResult, envelope.Structured) {
			fail("mcp_result_consistency")
		}
		if tc.Name == "search" {
			rows, ok := envelope.Structured["results"].([]any)
			if !ok || len(rows) < 1 || len(rows) > 3 {
				fail("search_nonempty_list")
			}
			related := false
			for _, row := range rows {
				r := row.(map[string]any)
				u, e := url.Parse(r["url"].(string))
				if e != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
					fail("search_url")
				}
				title := strings.TrimSpace(r["title"].(string))
				snippet := strings.TrimSpace(r["snippet"].(string))
				lower := strings.ToLower(title + " " + snippet + " " + u.Host)
				if title != "" && snippet != "" && strings.Contains(lower, "iana") && strings.Contains(lower, "example") {
					related = true
				}
			}
			if !related {
				fail("search_relevance")
			}
			step["result_count"] = len(rows)
		} else {
			markdown, _ := envelope.Structured["markdown"].(string)
			title, _ := envelope.Structured["title"].(string)
			body := strings.ToLower(markdown)
			if envelope.Structured["url"] != kimiLiveURL || strings.TrimSpace(title) == "" || envelope.Structured["truncated"] != false || !strings.Contains(body, "documentation") || !strings.Contains(body, "registration") || !strings.Contains(body, "transfer") || !strings.Contains(body, "example.com") {
				fail("fetch_public_content")
			}
			step["markdown_characters"] = len([]rune(markdown))
		}
		step["status"] = "PASS"
	}
	report["status"] = "PASS"
	report["stage"] = "complete"
	report["calculated_cost_cny"] = "0.02"
	if preflight {
		report["stage"] = "local_preflight_complete"
		report["calculated_cost_cny"] = "0.00"
	}
	report["account_billing_checked"] = false
}
