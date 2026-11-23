package app

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/36Dge/yijie-agent-host/internal/codex"
	broker "github.com/36Dge/yijie-agent-host/internal/contracts/marketbrokercontrol"
	wire "github.com/36Dge/yijie-agent-host/internal/contracts/markethost"
	provider "github.com/36Dge/yijie-agent-host/internal/contracts/marketprovider"
	selection "github.com/36Dge/yijie-agent-host/internal/contracts/marketselection"
	"github.com/36Dge/yijie-agent-host/internal/session"
	"github.com/google/uuid"
)

func approvalID() wire.CanonicalId { return wire.CanonicalId(uuid.NewString()) }

type approvalPeer struct {
	conn    net.Conn
	decoder *json.Decoder
	server  *httptest.Server
	token   string
}

type approvalEnvelope struct {
	Version int              `json:"schemaVersion"`
	ID      wire.CanonicalId `json:"requestId"`
	Code    string           `json:"code"`
	Data    json.RawMessage  `json:"data"`
}

func decodeApprovalResponse(t *testing.T, method string, id wire.CanonicalId, decoder *json.Decoder, out any) {
	t.Helper()
	var response approvalEnvelope
	if err := decoder.Decode(&response); err != nil {
		t.Fatal(method, err)
	}
	if response.Version != 1 || response.ID != id || response.Code != "" {
		t.Fatalf("%s rejected: code=%s correlated=%v", method, response.Code, response.ID == id)
	}
	if err := json.Unmarshal(response.Data, out); err != nil {
		t.Fatal(method, err)
	}
}

func (p approvalPeer) control(t *testing.T, method string, payload, out any) {
	t.Helper()
	id := approvalID()
	if err := p.conn.SetDeadline(time.Now().Add(15 * time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := json.NewEncoder(p.conn).Encode(map[string]any{"schemaVersion": 1, "requestId": id, "method": method, "payload": payload}); err != nil {
		t.Fatal(err)
	}
	decodeApprovalResponse(t, method, id, p.decoder, out)
}

func (p approvalPeer) request(t *testing.T, path string, payload, out any) {
	t.Helper()
	method, status, id := http.MethodGet, http.StatusOK, approvalID()
	var requestBody io.Reader
	if payload != nil {
		method, status = http.MethodPost, http.StatusAccepted
		raw, err := json.Marshal(map[string]any{"schemaVersion": 1, "requestId": id, "payload": payload})
		if err != nil {
			t.Fatal(err)
		}
		requestBody = bytes.NewReader(raw)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, method, p.server.URL+"/v1/market-chat/"+path, requestBody)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer "+p.token)
	if method == http.MethodPost {
		request.Header.Set("Content-Type", "application/json")
	} else {
		request.Header.Set("X-Yijie-Market-Request-Id", string(id))
	}
	res, err := p.server.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != status {
		var failure wire.Error
		_ = json.NewDecoder(res.Body).Decode(&failure)
		t.Fatalf("%s %s: %d %s", method, path, res.StatusCode, failure.Code)
	}
	decodeApprovalResponse(t, path, id, json.NewDecoder(res.Body), out)
}

func approvalBusinessCount(t *testing.T) int {
	t.Helper()
	client := http.Client{Timeout: 3 * time.Second}
	res, err := client.Get("http://127.0.0.1:18085/status")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var status struct {
		Kind      string
		ToolCalls []json.RawMessage
	}
	if res.StatusCode != http.StatusOK || json.NewDecoder(res.Body).Decode(&status) != nil || status.Kind != "ordinary-local-protocol" {
		t.Fatal("expected the canonical local MCP peer")
	}
	return len(status.ToolCalls)
}

// Exercise the same private control and authenticated HTTP routes used by Desktop.
// The session service owns the authorization callback and the on-disk Host ledger.
func TestMarketApprovalThroughProductRoutesAndRestart(t *testing.T) {
	if os.Getenv("YIJIE_MARKET_NATIVE_APPROVAL_E2E") != "true" {
		t.Skip("requires the local market approval profile")
	}
	binary, manifest, worker := os.Getenv("YIJIE_CODEX_INTEGRATION_BINARY"), os.Getenv("YIJIE_CODEX_INTEGRATION_MANIFEST"), os.Getenv("YIJIE_MARKET_BROKER_TEST_MANIFEST")
	if binary == "" || manifest == "" || worker == "" {
		t.Fatal("paired artifacts required")
	}
	const childHome = "YIJIE_MARKET_APPROVAL_TEST_HOME"
	const childStage = "YIJIE_MARKET_APPROVAL_TEST_STAGE"
	stage, root := os.Getenv(childStage), os.Getenv(childHome)
	if stage == "" {
		folder, err := os.MkdirTemp("", "market-approval-routes-")
		if err != nil {
			t.Fatal(err)
		}
		root, err = filepath.EvalSymlinks(folder)
		if err != nil {
			t.Fatal(err)
		}
		executable, err := os.Executable()
		if err != nil {
			t.Fatal(err)
		}
		// Independent Host processes cannot share an in-memory approval store.
		// On a failed shutdown, keep the isolated Home and stop before restarting.
		for _, phase := range []string{"initial", "cold-restart"} {
			command := exec.Command(executable, "-test.run=^TestMarketApprovalThroughProductRoutesAndRestart$", "-test.v")
			command.Env = append(os.Environ(), childHome+"="+root, childStage+"="+phase)
			output, err := command.CombinedOutput()
			if command.ProcessState != nil {
				t.Logf("host stage=%s pid=%d\n%s", phase, command.ProcessState.Pid(), output)
			}
			if err != nil {
				t.Fatalf("Host %s: %v; isolated Home retained at %s", phase, err, root)
			}
		}
		if err := os.RemoveAll(root); err != nil {
			t.Fatal(err)
		}
		return
	}
	if (stage != "initial" && stage != "cold-restart") || !filepath.IsAbs(root) {
		t.Fatal("invalid isolated Host stage")
	}
	isolated := filepath.Join(root, ".local", "market-local-acceptance", "approval-modes")
	hostHome, home, cwd := filepath.Join(isolated, "host-home"), filepath.Join(isolated, "codex-home"), filepath.Join(root, "workspace")
	for _, path := range []string{hostHome, home, cwd} {
		if err := os.MkdirAll(path, 0700); err != nil {
			t.Fatal(err)
		}
	}
	scope := broker.ScopeBinding{OwnerUserId: marketID(301), TenantId: marketID(302), AuthorizationRevision: 1}
	binding := provider.ProviderBinding{ServiceId: "market-local-verification", Reference: broker.SelectionRef{InstallationId: marketID(303), Generation: 1, Revision: 1}, CredentialRef: marketID(304)}
	account := marketID(305)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	instance := approvalID()
	scope.NativeProcessEpoch = approvalID()
	scope.AuthorizationExpiresAtUnixMs = broker.UnixMilliseconds(time.Now().Add(4 * time.Minute).UnixMilli())
	if stage == "cold-restart" {
		scope.AuthorizationRevision++
		binding.Reference.Revision++
	}
	cfg := codex.DefaultConfig()
	cfg.BinaryPath, cfg.ManifestPath, cfg.CodexHome = binary, manifest, home
	cfg.StartupTimeout, cfg.RequestTimeout, cfg.ShutdownTimeout = 30*time.Second, 30*time.Second, 15*time.Second
	cfg.ChatModelsEnabled, cfg.KimiK31Candidate, cfg.RuntimePermissionsEnabled = true, true, true
	cfg.NativeToolsEnabled, cfg.InteractionToolsEnabled, cfg.ToolSearchEnabled, cfg.NativeSkillMarketplaceEnabled = true, true, true, true
	cfg.MiniMax = codex.MiniMaxConfig{Enabled: true, APIKey: "ordinary-loopback-minimax"}
	cfg.KimiAPIKey = "ordinary-loopback-kimi"
	cfg.PermissionVerificationBaseURL = "http://127.0.0.1:18083/v1"
	cfg.Market = codex.MarketConfig{Manifest: worker, HostInstanceID: string(instance), LocalAcceptance: "approval-modes", LibraryHome: filepath.Join(hostHome, "market-worker", "library-home")}
	for _, name := range []string{"yijie-builtin", "yijie-user"} {
		dir := filepath.Join(home, "ordinary-markets", name)
		file := filepath.Join(dir, ".agents", "plugins", "marketplace.json")
		if stage == "initial" {
			if err := os.MkdirAll(filepath.Dir(file), 0700); err != nil {
				t.Fatal(err)
			}
			raw, _ := json.Marshal(map[string]any{"name": name, "plugins": []any{}})
			if err := os.WriteFile(file, raw, 0600); err != nil {
				t.Fatal(err)
			}
		}
		cfg.SkillMarketplaceRoots = append(cfg.SkillMarketplaceRoots, dir)
	}
	store, err := session.OpenStore(hostHome, session.WithMarketStorage(), session.WithChatModelV2Storage())
	if err != nil {
		t.Fatal(err)
	}
	runtime := codex.NewManager(cfg, nil)
	service := session.NewService(runtime, store, session.NewEventHub(128, 64), nil, session.WithMarketAuthority(string(instance)))
	completed := make(chan string, 8)
	if err := runtime.SetNotificationHandler(func(method string, raw json.RawMessage) {
		service.HandleNotification(method, raw)
		if method == "turn/completed" {
			var event struct{ Turn struct{ ID, Status string } }
			if json.Unmarshal(raw, &event) == nil {
				completed <- event.Turn.ID + " " + event.Turn.Status
			}
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		closeCtx, stop := context.WithTimeout(context.Background(), 30*time.Second)
		defer stop()
		if err := runtime.Shutdown(closeCtx); err != nil {
			t.Errorf("normal Runtime shutdown pending: %v", err)
			return
		}
		if err := store.Close(); err != nil {
			t.Error(err)
		}
	})
	if err := runtime.Start(ctx); err != nil {
		t.Fatal(err)
	}
	for _, dir := range cfg.SkillMarketplaceRoots {
		if err := runtime.RegisterSkillMarketplace(ctx, dir); err != nil {
			t.Fatal(err)
		}
	}
	runtime.SetSkillSubmissionReady(true)
	parent, child := net.Pipe()
	pumpDone := make(chan error, 1)
	// Parent EOF is how the real Host retires its market authority.
	go func() { pumpDone <- ServeMarketControl(context.Background(), child, child, service) }()
	t.Cleanup(func() {
		_ = parent.Close()
		select {
		case err := <-pumpDone:
			if err != nil {
				t.Error(err)
			}
		case <-time.After(12 * time.Second):
			t.Error("private control did not stop on EOF")
		}
	})
	mux := http.NewServeMux()
	token := uuid.NewString()
	registerMarketRoutes(mux, service, token, string(instance))
	peer := approvalPeer{conn: parent, decoder: json.NewDecoder(parent), server: httptest.NewServer(mux), token: token}
	t.Cleanup(peer.server.Close)
	var hello wire.HelloResult
	peer.control(t, "hello", wire.HelloPayload{HostInstanceId: instance, OwnerUserId: scope.OwnerUserId, TenantId: scope.TenantId, NativeProcessEpoch: scope.NativeProcessEpoch}, &hello)
	if !hello.Ready || hello.NativeApprovalVersion == nil || *hello.NativeApprovalVersion != 1 {
		t.Fatal("Host/Runtime did not negotiate native approvals")
	}
	probe := func() {
		p := provider.ProviderPayload{OperationId: approvalID(), HostInstanceId: instance, Scope: scope, Binding: binding}
		var status provider.ProviderStatus
		peer.control(t, "provider_probe", p, &status)
		for until := time.Now().Add(8 * time.Second); status.OperationState == "starting" && time.Now().Before(until); {
			time.Sleep(10 * time.Millisecond)
			peer.control(t, "provider_auth_poll", p, &status)
		}
		if !status.ExecutionAvailable || status.Binding != binding || status.NativeApprovalVersion == nil || *status.NativeApprovalVersion != 1 {
			t.Fatal("Worker local probe did not qualify", status.OperationState)
		}
	}
	probe()
	rules := func(revoke *wire.ArgsDigest) []wire.RememberedToolRule {
		var result wire.RememberedToolRules
		peer.control(t, "remembered_tool_rules", wire.RememberedRulesPayload{Reference: binding.Reference, ServiceId: binding.ServiceId, CredentialRef: binding.CredentialRef, AuthorityOperationId: account, HostInstanceId: instance, Scope: scope, Revoke: revoke}, &result)
		return result.Rules
	}
	run := func(label string, profile wire.ProfileId, mode wire.PermissionMode, choice, expected broker.Decision, prior *wire.SubmissionReceipt) wire.SubmissionReceipt {
		t.Helper()
		before := approvalBusinessCount(t)
		op := approvalID()
		refs := selection.Selection{binding.Reference}
		digest, err := selection.SelectionDigest(string(op), refs)
		if err != nil {
			t.Fatal(err)
		}
		raw, _ := json.Marshal(map[string]string{"type": "text", "text": "Read the ordinary local sample. LOCAL_CASE_" + label})
		var block wire.ContentBlock
		if err = json.Unmarshal(raw, &block); err != nil {
			t.Fatal(err)
		}
		version := int64(1)
		operations := []wire.CanonicalId{account}
		submission := wire.Submission{TaskId: approvalID(), LocalSessionId: approvalID(), SubmissionOperationId: approvalID(), NativeApprovalVersion: &version, NativeApprovalAuthorityOperations: &operations, Cwd: cwd,
			ContentBlocks: []wire.ContentBlock{block}, PermissionMode: mode, Intent: wire.ModelIntent{ProfileId: profile}, Services: []wire.ServiceBinding{binding}, Snapshot: selection.SelectionSnapshot{SchemaVersion: 1, TurnOperationId: op, Selection: refs, SelectionDigest: selection.SelectionDigestValue(digest)}}
		if prior != nil {
			submission.TaskId, submission.LocalSessionId, submission.AgentSessionId, submission.Intent.ExpectedRevision = prior.TaskId, prior.LocalSessionId, prior.AgentSessionId, prior.ModelRevision
			submission.SubmissionOperationId = op
		}
		var grant wire.GrantRegistration
		peer.control(t, "grant_register", wire.GrantRegisterPayload{OperationId: approvalID(), HostInstanceId: instance, Scope: scope, Submission: submission}, &grant)
		var receipt wire.SubmissionReceipt
		peer.request(t, "submissions", wire.SubmitPayload{GrantRef: grant.GrantRef, TurnOperationId: op}, &receipt)
		if receipt.State != wire.SubmissionStateAccepted || receipt.AgentSessionId == nil || receipt.NativeTurnId == nil {
			t.Fatal("submission did not bind", receipt.State)
		}
		base := "agent-sessions/" + string(*receipt.AgentSessionId)
		if choice != "" {
			var found *wire.MarketApproval
			for until := time.Now().Add(12 * time.Second); found == nil && time.Now().Before(until); {
				var approvals wire.ApprovalSnapshot
				peer.request(t, base+"/approvals", nil, &approvals)
				for _, a := range approvals.Requests {
					if a.Identity.NativeTurnId == *receipt.NativeTurnId && a.State == wire.ApprovalStatePending {
						found = &a
						break
					}
				}
				if found == nil {
					time.Sleep(15 * time.Millisecond)
				}
			}
			if found == nil || found.PersistOptions == nil || !slices.Equal(*found.PersistOptions, []string{"session", "always"}) {
				t.Fatal(label, "missing offered scopes")
			}
			var decided wire.MarketApproval
			peer.control(t, "approval_decide", wire.ApprovalDecidePayload{OperationId: approvalID(), HostInstanceId: instance, Scope: scope, AgentSessionId: *receipt.AgentSessionId, ApprovalId: found.ApprovalId, CallRef: found.Identity.CallRef, ExpectedRevision: found.Revision, DecisionId: approvalID(), Decision: choice}, &decided)
			if decided.Decision == nil || *decided.Decision != choice {
				t.Fatal(label, "decision acknowledgement missing")
			}
		}
		select {
		case v := <-completed:
			if v != string(*receipt.NativeTurnId)+" completed" {
				t.Fatal(label, v)
			}
		case <-time.After(25 * time.Second):
			t.Fatal(label, "turn did not finish")
		}
		var history wire.ToolSnapshot
		peer.request(t, base+"/turns/"+string(*receipt.NativeTurnId)+"/tools?source=history", nil, &history)
		want := 1
		if expected == broker.DecisionReject || expected == broker.DecisionCancel || expected == broker.DecisionNativeAutoReject || expected == broker.DecisionNativeRuntimeReject {
			want = 0
		}
		sent := approvalBusinessCount(t) - before
		if len(history.Items) != 1 || history.Items[0].Correlation == nil {
			t.Fatalf("%s: missing native Item/ledger correlation, items=%d localRequests=%d", label, len(history.Items), sent)
		}
		c := history.Items[0].Correlation
		if c.Reference != binding.Reference || c.TurnOperationId != op || c.NativeTurnId != *receipt.NativeTurnId || c.DecisionReceipt == nil || c.DecisionReceipt.Decision != expected || c.AdmissionConsumed != (want == 1) || sent != want {
			t.Fatalf("%s expected %s, receipt=%+v consumed=%v localRequests=%d", label, expected, c.DecisionReceipt, c.AdmissionConsumed, sent)
		}
		t.Logf("case=%s profile=%s mode=%s source=%s localRequests=%d", label, profile, mode, expected, sent)
		return receipt
	}
	const mini = wire.ProfileId("minimax-m3-high-v1")
	const kimi = wire.ProfileId(codex.KimiK31ProfileID)
	if stage == "initial" {
		run("NATIVE_READ", kimi, "ask", "", broker.DecisionNativeRuntime, nil)
		first := run("ONCE", kimi, "ask", broker.DecisionApproveOnce, broker.DecisionApproveOnce, nil)
		next := run("SESSION", kimi, "ask", broker.DecisionApproveSession, broker.DecisionApproveSession, &first)
		run("SESSION_HIT", kimi, "ask", "", broker.DecisionNativeSession, &next)
		run("ALWAYS", mini, "ask", broker.DecisionApproveAlways, broker.DecisionApproveAlways, nil)
		list := rules(nil)
		if len(list) != 1 || list[0].Tool != "local_record_sample" {
			t.Fatal("missing confirmed local marker rule")
		}
		if err := os.WriteFile(filepath.Join(root, "expected-rule"), []byte(list[0].Key), 0600); err != nil {
			t.Fatal(err)
		}
		return
	}
	before := approvalBusinessCount(t)
	// The saved key is used only to compare the independently read native rule.
	expectedKey, err := os.ReadFile(filepath.Join(root, "expected-rule"))
	if err != nil {
		t.Fatal(err)
	}
	list := rules(nil)
	if len(list) != 1 || string(list[0].Key) != string(expectedKey) || list[0].Tool != "local_record_sample" || approvalBusinessCount(t) != before {
		t.Fatal("restart did not retain the exact rule")
	}
	run("AFTER_RESTART", mini, "ask", "", broker.DecisionNativeAlways, nil)
	if list := rules(&list[0].Key); len(list) != 0 {
		t.Fatal("rule removal not confirmed")
	}
	run("AFTER_REVOKE", mini, "ask", broker.DecisionCancel, broker.DecisionCancel, nil)
	run("FULL", mini, "full", "", broker.DecisionNativeFull, nil)
	run("GUARDIAN_ALLOW", mini, "auto", "", broker.DecisionNativeAuto, nil)
	run("GUARDIAN_DENY", kimi, "auto", "", broker.DecisionNativeAutoReject, nil)

	run("SAVE_FOR_REINSTALL", mini, "ask", broker.DecisionApproveAlways, broker.DecisionApproveAlways, nil)
	if list := rules(nil); len(list) != 1 {
		t.Fatal("original installation rule missing")
	}
	var forgotten provider.ProviderStatus
	request := provider.ProviderPayload{OperationId: approvalID(), HostInstanceId: instance, Scope: scope, Binding: binding}
	peer.control(t, "provider_forget", request, &forgotten)
	for deadline := time.Now().Add(8 * time.Second); forgotten.OperationState == "starting" && time.Now().Before(deadline); {
		time.Sleep(10 * time.Millisecond)
		peer.control(t, "provider_auth_poll", request, &forgotten)
	}
	if forgotten.OperationState != "succeeded" {
		t.Fatal("local installation did not finish normal removal", forgotten.OperationState)
	}
	binding.Reference.Generation++
	binding.Reference.Revision++
	binding.CredentialRef, account = approvalID(), approvalID()
	probe()
	if list := rules(nil); len(list) != 0 {
		t.Fatal("previous installation's approvals appeared after reinstall")
	}
	run("NEW_INSTALLATION", mini, "ask", broker.DecisionReject, broker.DecisionReject, nil)
}
