package session

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/36Dge/yijie-agent-host/internal/codex"
	model "github.com/36Dge/yijie-agent-host/internal/contracts/chatmodels"
	broker "github.com/36Dge/yijie-agent-host/internal/contracts/marketbrokercontrol"
	wire "github.com/36Dge/yijie-agent-host/internal/contracts/markethost"
	selection "github.com/36Dge/yijie-agent-host/internal/contracts/marketselection"
	"github.com/google/uuid"
)

type marketSessionRuntime struct {
	modelTestRuntime
	process         broker.ProcessBinding
	order           []string
	preparedThreads []*string
	turns           int
	deadlines       []time.Time
	toolResult      any
}

func (r *marketSessionRuntime) CloseMarketAdmission(context.Context) error {
	r.order = append(r.order, "close_admission")
	return nil
}

func (r *marketSessionRuntime) MarketProcess() (broker.ProcessBinding, error) { return r.process, nil }
func (r *marketSessionRuntime) PrepareMarket(ctx context.Context, s broker.ScopeBinding, v wire.Submission, _ string, thread *string) (*codex.MarketPrepared, error) {
	deadline, _ := ctx.Deadline()
	r.deadlines = append(r.deadlines, deadline)
	r.order = append(r.order, "prepare")
	r.preparedThreads = append(r.preparedThreads, thread)
	return &codex.MarketPrepared{Scope: s, Snapshot: v.Snapshot, Process: r.process, Config: map[string]any{"mcp_servers": map[string]any{"yijie_market": map[string]any{"enabled": false}}}}, nil
}
func (r *marketSessionRuntime) BeginMarketTurn(_ string, _ *codex.MarketPrepared) error {
	r.order = append(r.order, "begin")
	return nil
}
func (r *marketSessionRuntime) DiscardMarketPrepared(context.Context, *codex.MarketPrepared) error {
	r.order = append(r.order, "discard")
	return nil
}
func (r *marketSessionRuntime) ClearMarketThread(context.Context, string) error {
	r.order = append(r.order, "clear")
	return nil
}
func (r *marketSessionRuntime) ClearMarketConfiguration(ctx context.Context, id, cwd string) (*codex.MarketPrepared, error) {
	_ = r.ClearMarketThread(ctx, id)
	return &codex.MarketPrepared{Config: map[string]any{"mcp_servers": map[string]any{"yijie_market": map[string]any{"enabled": false}}}}, nil
}
func (r *marketSessionRuntime) RevokeMarketAuthority(context.Context, broker.ScopeBinding, *broker.CanonicalId) error {
	r.order = append(r.order, "revoke")
	return nil
}
func (r *marketSessionRuntime) StartThread(ctx context.Context, _ string) (codex.ThreadInfo, error) {
	deadline, _ := ctx.Deadline()
	r.deadlines = append(r.deadlines, deadline)
	r.order = append(r.order, "thread_start")
	p, _ := codex.ModelProfile(codex.ModelProfileID(ctx))
	return codex.ThreadInfo{ID: uuid.NewString(), Model: p.Model, ModelProvider: p.Provider, ReasoningEffort: p.Effort}, nil
}
func (r *marketSessionRuntime) SwitchThread(ctx context.Context, id string) (codex.ThreadInfo, error) {
	r.order = append(r.order, "switch")
	return r.modelTestRuntime.SwitchThread(ctx, id)
}
func (r *marketSessionRuntime) StartTurnV2(ctx context.Context, _ string, _ []codex.UserInput, _ string) (codex.TurnInfo, error) {
	deadline, _ := ctx.Deadline()
	r.deadlines = append(r.deadlines, deadline)
	r.order = append(r.order, "turn_start")
	r.turns++
	return codex.TurnInfo{ID: uuid.NewString()}, nil
}
func (r *marketSessionRuntime) ValidatePermissionMode(mode codex.PermissionMode) error {
	if !mode.Valid() {
		return errors.New("invalid mode")
	}
	return nil
}
func (r *marketSessionRuntime) ListRuntimeApprovals(string) []codex.RuntimeApproval { return nil }

func marketService(t *testing.T) (*Service, *marketSessionRuntime, wire.ScopeBinding, string) {
	t.Helper()
	home, e := canonicalDirectory(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	store, e := OpenStore(home, WithMarketStorage())
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { _ = store.Close() })
	instance := wire.CanonicalId(uuid.NewString())
	rt := &marketSessionRuntime{process: broker.ProcessBinding{HostInstanceId: instance, RuntimeGeneration: wire.CanonicalId(uuid.NewString())}}
	s := NewService(rt, store, NewEventHub(32, 16), nil, WithMarketAuthority(string(instance)))
	scope := wire.ScopeBinding{OwnerUserId: wire.CanonicalId(uuid.NewString()), TenantId: wire.CanonicalId(uuid.NewString()), NativeProcessEpoch: wire.CanonicalId(uuid.NewString()), AuthorizationRevision: 1, AuthorizationExpiresAtUnixMs: broker.UnixMilliseconds(time.Now().Add(time.Minute).UnixMilli())}
	if _, e = s.MarketHello(wire.HelloPayload{HostInstanceId: instance, OwnerUserId: scope.OwnerUserId, TenantId: scope.TenantId, NativeProcessEpoch: scope.NativeProcessEpoch}); e != nil {
		t.Fatal(e)
	}
	return s, rt, scope, home
}
func marketSubmission(t *testing.T, cwd string) wire.Submission {
	t.Helper()
	op := wire.CanonicalId(uuid.NewString())
	refs := selection.Selection{}
	digest, e := selection.SelectionDigest(string(op), refs)
	if e != nil {
		t.Fatal(e)
	}
	var block wire.ContentBlock
	if e = json.Unmarshal([]byte(`{"type":"text","text":"ordinary market test message"}`), &block); e != nil {
		t.Fatal(e)
	}
	return wire.Submission{TaskId: wire.CanonicalId(uuid.NewString()), LocalSessionId: wire.CanonicalId(uuid.NewString()), SubmissionOperationId: wire.CanonicalId(uuid.NewString()), Cwd: cwd, ContentBlocks: []wire.ContentBlock{block}, Intent: wire.ModelIntent{ProfileId: model.ProfileIdKimiK3MaxV1}, PermissionMode: wire.PermissionModeAsk, Snapshot: selection.SelectionSnapshot{SchemaVersion: 1, TurnOperationId: op, Selection: refs, SelectionDigest: selection.SelectionDigestValue(digest)}, Services: []wire.ServiceBinding{}}
}
func registerMarket(t *testing.T, s *Service, scope wire.ScopeBinding, v wire.Submission) wire.SubmitPayload {
	t.Helper()
	g, e := s.RegisterMarketGrant(wire.GrantRegisterPayload{OperationId: wire.CanonicalId(uuid.NewString()), HostInstanceId: s.market.instance, Scope: scope, Submission: v})
	if e != nil {
		t.Fatal(e)
	}
	return wire.SubmitPayload{GrantRef: g.GrantRef, TurnOperationId: v.Snapshot.TurnOperationId}
}
func nextMarketTurn(t *testing.T, v wire.Submission) wire.Submission {
	t.Helper()
	v.SubmissionOperationId = wire.CanonicalId(uuid.NewString())
	v.Snapshot.TurnOperationId = wire.CanonicalId(uuid.NewString())
	d, e := selection.SelectionDigest(string(v.Snapshot.TurnOperationId), v.Snapshot.Selection)
	if e != nil {
		t.Fatal(e)
	}
	v.Snapshot.SelectionDigest = selection.SelectionDigestValue(d)
	return v
}

func TestFEAT157MarketNewTurnAcceptedReplayContinuationAndModelClear(t *testing.T) {
	s, r, scope, home := marketService(t)
	v := marketSubmission(t, home)
	trigger := registerMarket(t, s, scope, v)
	receipt, e := s.SubmitMarket(context.Background(), trigger)
	if e != nil || receipt.State != wire.SubmissionStateAccepted || receipt.ModelRevision != 1 || receipt.Validate() != nil {
		t.Fatal(receipt, e)
	}
	if strings.Join(r.order, ",") != "prepare,thread_start,begin,turn_start" || r.preparedThreads[0] != nil {
		t.Fatal(r.order)
	}
	replay, e := s.SubmitMarket(context.Background(), trigger)
	if e != nil || replay.NativeTurnId == nil || *replay.NativeTurnId != *receipt.NativeTurnId || r.turns != 1 {
		t.Fatal(replay, e)
	}
	// A normal completion followed by an explicit new turn in the same session.
	s.HandleNotification(RuntimeNotificationTurnCompleted, rawJSON(t, map[string]any{"threadId": *receipt.NativeThreadId, "turn": map[string]any{"id": *receipt.NativeTurnId, "status": "completed"}}))
	v = nextMarketTurn(t, v)
	v.AgentSessionId = receipt.AgentSessionId
	v.Intent.ExpectedRevision = 1
	continued, e := s.SubmitMarket(context.Background(), registerMarket(t, s, scope, v))
	if e != nil || continued.State != wire.SubmissionStateAccepted || continued.ModelRevision != 1 || *continued.NativeThreadId != *receipt.NativeThreadId {
		t.Fatal(continued, e)
	}
	if r.preparedThreads[1] == nil || *r.preparedThreads[1] != string(*receipt.NativeThreadId) || strings.Join(r.order[4:], ",") != "prepare,clear,switch,begin,turn_start" {
		t.Fatal(r.order)
	}
	s.HandleNotification(RuntimeNotificationTurnCompleted, rawJSON(t, map[string]any{"threadId": *continued.NativeThreadId, "turn": map[string]any{"id": *continued.NativeTurnId, "status": "completed"}}))
	selected, e := s.SelectChatModel(context.Background(), string(*receipt.AgentSessionId), model.SelectRequest{SchemaVersion: 1, OperationId: uuid.NewString(), ExpectedRevision: 1, ProfileId: model.ProfileIdMinimaxM3HighV1})
	if e != nil || selected.Revision != 2 || strings.Join(r.order[len(r.order)-2:], ",") != "clear,switch" {
		t.Fatal(selected, e, r.order)
	}
	if _, e = s.StartTurnV2(context.Background(), StartTurnV2Input{AgentSessionID: string(*receipt.AgentSessionId), OperationID: uuid.NewString()}); !errors.Is(e, ErrMarketPurpose) {
		t.Fatal(e)
	}
	raw, e := os.ReadFile(filepath.Join(home, "sessions.db"))
	if e != nil || strings.Contains(string(raw), "ordinary market test message") || strings.Contains(string(raw), string(trigger.GrantRef)) {
		t.Fatal("body or grant persisted", e)
	}
	if s.market.bytes != 0 {
		t.Fatal("consumed grant bytes retained")
	}
}

func TestFEAT157MarketExplicitAdoptionAndReadAfterRevocation(t *testing.T) {
	s, r, scope, home := marketService(t)
	v := marketSubmission(t, home)
	id, thread := uuid.NewString(), uuid.NewString()
	p, _ := codex.ModelProfile(string(v.Intent.ProfileId))
	record := Record{TaskID: string(v.TaskId), AgentSessionID: id, Cwd: home, ModelProfile: string(v.Intent.ProfileId), ModelRevision: 1, ModelSelectionState: "ready"}
	if e := s.store.Reserve(record); e != nil {
		t.Fatal(e)
	}
	if _, e := s.store.BindThread(id, thread, "", p.Model, p.Provider); e != nil {
		t.Fatal(e)
	}
	sid := wire.CanonicalId(id)
	v.AgentSessionId = &sid
	v.Intent.ExpectedRevision = 1
	trigger := registerMarket(t, s, scope, v)
	receipt, e := s.SubmitMarket(context.Background(), trigger)
	if e != nil || receipt.State != wire.SubmissionStateAccepted || r.preparedThreads[0] == nil {
		t.Fatal(receipt, e)
	}
	stored, e := s.store.Get(id)
	if e != nil || stored.Purpose != purposeMarket || stored.MarketLocalSessionID != string(v.LocalSessionId) {
		t.Fatal(stored, e)
	}
	result, e := s.RevokeMarketAuthority(context.Background(), wire.RevokeAuthorityPayload{OperationId: wire.CanonicalId(uuid.NewString()), HostInstanceId: s.market.instance, Scope: scope, Reason: "authorization_changed"})
	if e != nil || result.Outcome != "admission_revoked" {
		t.Fatal(result, e)
	}
	read, e := s.ReadMarketOperation(string(v.TaskId), string(v.Snapshot.TurnOperationId))
	if e != nil || read.State != wire.SubmissionStateAccepted {
		t.Fatal(read, e)
	}
	replay, e := s.SubmitMarket(context.Background(), trigger)
	if e != nil || replay.State != wire.SubmissionStateAccepted || r.turns != 1 {
		t.Fatal(replay, e)
	}
}

func (r *marketSessionRuntime) ReadThread(_ context.Context, id string) (json.RawMessage, error) {
	result := r.toolResult
	if result == nil {
		result = map[string]any{"content": []any{map[string]string{"type": "text", "text": "Public company profile"}}}
	}
	return json.Marshal(map[string]any{"id": id, "turns": []any{map[string]any{"id": testTurnID, "status": "completed", "items": []any{map[string]any{"id": "native-tool-item-1", "type": "mcpToolCall", "server": "yijie_market", "tool": "public_lookup", "status": "completed", "result": result}}}}})
}

func TestFEAT157MarketToolProjectionKeepsNativeIdentityWithoutGuessingCall(t *testing.T) {
	s, _, scope, home := marketService(t)
	v := marketSubmission(t, home)
	receipt, e := s.SubmitMarket(context.Background(), registerMarket(t, s, scope, v))
	if e != nil {
		t.Fatal(e)
	}
	result, e := s.ReadMarketTools(context.Background(), string(*receipt.AgentSessionId), testTurnID)
	if e != nil || result.Validate() != nil || len(result.Items) != 1 {
		t.Fatal(result, e)
	}
	item := result.Items[0]
	if item.NativeItemId != "native-tool-item-1" || item.CallRef != nil || item.Service != nil || item.ResultText == nil || *item.ResultText != "Public company profile" || item.State != "completed" {
		t.Fatal(item)
	}
}

func TestFEAT157MarketUnsupportedResultRetainsObservedTool(t *testing.T) {
	s, runtime, scope, home := marketService(t)
	// A well-formed JSON result in an unsupported container shape is still an
	// observed tool item. It must not disappear through the strict wire reader.
	runtime.toolResult = []any{}
	v := marketSubmission(t, home)
	receipt, err := s.SubmitMarket(context.Background(), registerMarket(t, s, scope, v))
	if err != nil {
		t.Fatal(err)
	}
	result, err := s.ReadMarketTools(context.Background(), string(*receipt.AgentSessionId), testTurnID)
	if err != nil || result.Validate() != nil || len(result.Items) != 1 || result.Truncated {
		t.Fatal(result, err)
	}
	item := result.Items[0]
	if item.NativeItemId != "native-tool-item-1" || item.ErrorCode == nil || *item.ErrorCode != "unsupported_result" || item.ResultText != nil || item.State != "completed" {
		t.Fatal(item)
	}
}

func TestFEAT157MarketCompleteStructuredCopyIsNotTruncated(t *testing.T) {
	for _, tc := range []struct {
		name      string
		text      string
		truncated bool
	}{
		{"complete", "{\n  \"rows\": []\n}", false},
		{"different", "{\"rows\":[],\"count\":0}", true},
		{"summary", "No rows returned", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, runtime, scope, home := marketService(t)
			runtime.toolResult = map[string]any{"content": []any{map[string]string{"type": "text", "text": tc.text}}, "structuredContent": map[string]any{"rows": []any{}}}
			v := marketSubmission(t, home)
			receipt, err := s.SubmitMarket(context.Background(), registerMarket(t, s, scope, v))
			if err != nil {
				t.Fatal(err)
			}
			result, err := s.ReadMarketTools(context.Background(), string(*receipt.AgentSessionId), testTurnID)
			if err != nil || len(result.Items) != 1 || result.Validate() != nil {
				t.Fatal(result, err)
			}
			item := result.Items[0]
			if item.Truncated != tc.truncated || item.ResultText == nil || *item.ResultText != tc.text {
				t.Fatal(item)
			}
		})
	}
}

func TestFEAT157MarketGrantRevocationAndShutdownRemainReducing(t *testing.T) {
	s, r, scope, home := marketService(t)
	v := marketSubmission(t, home)
	trigger := registerMarket(t, s, scope, v)
	request := wire.RevokeAuthorityPayload{OperationId: wire.CanonicalId(uuid.NewString()), HostInstanceId: s.market.instance, Scope: scope, Reason: "authorization_changed"}
	response, e := s.RevokeMarketAuthority(context.Background(), request)
	if e != nil || response.Outcome != "admission_revoked" {
		t.Fatal(response, e)
	}
	repeated, e := s.RevokeMarketAuthority(context.Background(), request)
	if e != nil || repeated != response || len(r.order) != 1 {
		t.Fatal(repeated, e, r.order)
	}
	if _, e := s.SubmitMarket(context.Background(), trigger); MarketFailureCode(e) != wire.ErrorCodeGrantExpired {
		t.Fatal(e)
	}
	if s.market.bytes != 0 {
		t.Fatal("revoked grant body retained")
	}
	s.CloseMarketAuthority(context.Background())
	if r.order[len(r.order)-1] != "close_admission" {
		t.Fatal("EOF did not close worker admission")
	}
	if _, e = s.RegisterMarketGrant(wire.GrantRegisterPayload{OperationId: wire.CanonicalId(uuid.NewString()), HostInstanceId: s.market.instance, Scope: scope, Submission: v}); MarketFailureCode(e) != wire.ErrorCodeAuthorityMismatch {
		t.Fatal(e)
	}
}

func TestFEAT157MarketSubmitSharesOneBoundedDeadline(t *testing.T) {
	s, r, scope, home := marketService(t)
	submission := marketSubmission(t, home)
	// A caller with a shorter existing budget keeps it. No sleep, timeout
	// injection, fake executable or external request is involved.
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	expected, _ := ctx.Deadline()
	receipt, err := s.SubmitMarket(ctx, registerMarket(t, s, scope, submission))
	if err != nil || receipt.State != wire.SubmissionStateAccepted || len(r.deadlines) != 3 {
		t.Fatal(receipt, err)
	}
	for _, deadline := range r.deadlines {
		if !deadline.Equal(expected) {
			t.Fatal("a submission stage extended the caller budget")
		}
	}
}

func TestFEAT157MarketCancelledSubmissionDoesNotEnterRuntime(t *testing.T) {
	s, r, scope, home := marketService(t)
	submission := marketSubmission(t, home)
	trigger := registerMarket(t, s, scope, submission)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := s.SubmitMarket(ctx, trigger); MarketFailureCode(err) != wire.ErrorCodeBusy {
		t.Fatal(err)
	}
	if len(r.order) != 0 {
		t.Fatal("cancelled request entered admission or Runtime")
	}
	if _, err := s.store.readMarketOperation(string(trigger.TurnOperationId)); !errors.Is(err, ErrNotFound) {
		t.Fatal("cancelled request created a dispatch receipt", err)
	}
}
