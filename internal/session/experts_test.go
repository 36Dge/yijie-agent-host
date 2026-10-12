package session

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/36Dge/yijie-agent-host/internal/codex"
	model "github.com/36Dge/yijie-agent-host/internal/contracts/chatmodels"
	expert "github.com/36Dge/yijie-agent-host/internal/contracts/experts"
	broker "github.com/36Dge/yijie-agent-host/internal/contracts/marketbrokercontrol"
	market "github.com/36Dge/yijie-agent-host/internal/contracts/markethost"
	"github.com/36Dge/yijie-agent-host/internal/skills"
	"github.com/google/uuid"
	bolt "go.etcd.io/bbolt"
)

type feat158Resources struct{ calls int }

func (*marketSessionRuntime) ExpertInstructionsAvailable() bool { return true }

func (r *feat158Resources) ResolveExpert(ref expert.ExpertRef) (skills.ResolvedExpert, error) {
	r.calls++
	return skills.ResolvedExpert{Reference: ref, Role: "Benign fixture role never stored in adapter receipts.", Index: "[{\"Path\":\"/ordinary-reference.txt\"}]"}, nil
}
func feat158Ref() *expert.ExpertRef {
	return &expert.ExpertRef{ExpertId: "reference-expert", Version: "0.1.0", ArchiveSha256: expert.Digest(strings.Repeat("a", 64)), RoleSha256: expert.Digest(strings.Repeat("b", 64)), KnowledgeManifestSha256: expert.Digest(strings.Repeat("c", 64))}
}
func feat158Session(t *testing.T) (*Service, *marketSessionRuntime, *feat158Resources, market.ScopeBinding, string) {
	t.Helper()
	home, e := canonicalDirectory(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	store, e := OpenStore(home, WithExpertStorage())
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { _ = store.Close() })
	instance := market.CanonicalId(uuid.NewString())
	runtime := &marketSessionRuntime{process: broker.ProcessBinding{HostInstanceId: instance, RuntimeGeneration: market.CanonicalId(uuid.NewString())}}
	resources := &feat158Resources{}
	service := NewService(runtime, store, NewEventHub(32, 16), nil, WithMarketAuthority(string(instance)), WithExpertResources(resources))
	scope := market.ScopeBinding{OwnerUserId: market.CanonicalId(uuid.NewString()), TenantId: market.CanonicalId(uuid.NewString()), NativeProcessEpoch: market.CanonicalId(uuid.NewString()), AuthorizationRevision: 1, AuthorizationExpiresAtUnixMs: broker.UnixMilliseconds(time.Now().Add(time.Minute).UnixMilli())}
	if _, e := service.MarketHello(market.HelloPayload{HostInstanceId: instance, OwnerUserId: scope.OwnerUserId, TenantId: scope.TenantId, NativeProcessEpoch: scope.NativeProcessEpoch}); e != nil {
		t.Fatal(e)
	}
	return service, runtime, resources, scope, home
}
func feat158Grant(t *testing.T, s *Service, scope market.ScopeBinding, v market.Submission, revision expert.Revision, ref *expert.ExpertRef) (market.SubmitPayload, expert.TurnExpertSnapshot) {
	t.Helper()
	snapshot, e := expert.FreezeSnapshot(v.Snapshot.TurnOperationId, revision, ref)
	if e != nil {
		t.Fatal(e)
	}
	granted, e := s.RegisterExpertGrant(expert.PrivateGrantPayload{OperationId: market.CanonicalId(uuid.NewString()), HostInstanceId: s.market.instance, Scope: scope, Submission: expert.PrivateSubmission{Submission: v, ExpertSnapshot: snapshot}})
	if e != nil {
		t.Fatal(e)
	}
	if granted.ExpertSnapshotDigest != snapshot.SnapshotDigest {
		t.Fatal("grant lost frozen expert snapshot")
	}
	return market.SubmitPayload{GrantRef: granted.Grant.GrantRef, TurnOperationId: v.Snapshot.TurnOperationId}, snapshot
}
func feat158Complete(t *testing.T, s *Service, r expert.PrivateReceipt) {
	t.Helper()
	s.HandleNotification(RuntimeNotificationTurnCompleted, rawJSON(t, map[string]any{"threadId": *r.Receipt.NativeThreadId, "turn": map[string]any{"id": *r.Receipt.NativeTurnId, "status": "completed"}}))
}

func TestFEAT158ExpertSubmitModelSwitchRemoveAndNextTurn(t *testing.T) {
	s, r, resources, scope, home := feat158Session(t)
	v := marketSubmission(t, home)
	trigger, snapshot := feat158Grant(t, s, scope, v, 1, feat158Ref())
	received, e := s.SubmitExpert(context.Background(), trigger)
	if e != nil || received.Validate() != nil || received.Receipt.State != market.SubmissionStateAccepted || !reflect.DeepEqual(received.ExpertSnapshot, snapshot) {
		t.Fatal(received, e)
	}
	replay, e := s.SubmitExpert(context.Background(), trigger)
	if e != nil || !reflect.DeepEqual(received, replay) || r.turns != 1 {
		t.Fatal("expert replay dispatched again", e)
	}
	if _, e = s.SubmitMarket(context.Background(), trigger); e == nil {
		t.Fatal("bare submit consumed expert grant")
	}
	feat158Complete(t, s, received)
	selected, e := s.SelectChatModel(context.Background(), string(*received.Receipt.AgentSessionId), model.SelectRequest{SchemaVersion: 1, OperationId: uuid.NewString(), ExpectedRevision: 1, ProfileId: model.ProfileIdMinimaxM3HighV1})
	if e != nil || selected.Revision != 2 || resources.calls != 2 {
		t.Fatal("model switch did not resolve retained expert", selected, e, resources.calls)
	}
	remove := expert.ApplyPayload{OperationId: market.CanonicalId(uuid.NewString()), HostInstanceId: s.market.instance, Scope: scope, LocalSessionId: v.LocalSessionId, AgentSessionId: *received.Receipt.AgentSessionId, NativeThreadId: expert.NativeId(*received.Receipt.NativeThreadId), ExpectedBindingRevision: 1, BindingRevision: 2, Expert: nil, Intent: market.ModelIntent{ProfileId: model.ProfileIdMinimaxM3HighV1, ExpectedRevision: 2}, PermissionMode: market.PermissionModeAsk, MarketSnapshot: v.Snapshot}
	applied, e := s.ApplyExpertBinding(context.Background(), remove)
	if e != nil || applied.Validate() != nil || applied.State != expert.OperationStateSucceeded || applied.BindingRevision != 2 || applied.Expert != nil {
		t.Fatal(applied, e)
	}
	switchCount := len(r.order)
	again, e := s.ApplyExpertBinding(context.Background(), remove)
	if e != nil || !reflect.DeepEqual(again, applied) || len(r.order) != switchCount {
		t.Fatal("apply operation was repeated", again, e)
	}
	binding, e := s.store.expertBinding(string(remove.AgentSessionId))
	if e != nil || binding.Revision != 2 || binding.Expert != nil || binding.PendingOperation != "" {
		t.Fatal(binding, e)
	}
	v = nextMarketTurn(t, v)
	v.AgentSessionId = received.Receipt.AgentSessionId
	v.Intent = remove.Intent
	next, _ := feat158Grant(t, s, scope, v, 2, nil)
	ordinary, e := s.SubmitExpert(context.Background(), next)
	if e != nil || ordinary.ExpertSnapshot.Expert != nil || string(*ordinary.Receipt.NativeThreadId) != string(remove.NativeThreadId) || r.turns != 2 || resources.calls != 2 {
		t.Fatal("removal changed thread or retained expert", ordinary, e, r.turns, resources.calls)
	}
	read, e := s.ReadExpertSubmission(expert.SubmissionReadPayload{HostInstanceId: s.market.instance, Scope: scope, TaskId: v.TaskId, LocalSessionId: v.LocalSessionId, TurnOperationId: v.Snapshot.TurnOperationId})
	if e != nil || !reflect.DeepEqual(read, ordinary) {
		t.Fatal(read, e)
	}
	raw, e := os.ReadFile(filepath.Join(home, "sessions.db"))
	if e != nil || strings.Contains(string(raw), "Benign fixture role never stored") || strings.Contains(string(raw), "ordinary market test message") {
		t.Fatal("raw content entered operational storage", e)
	}
}
func TestFEAT158StoreNormalReopenPreservesSnapshotAndRequiresExpertReader(t *testing.T) {
	s, _, _, scope, home := feat158Session(t)
	v := marketSubmission(t, home)
	trigger, snapshot := feat158Grant(t, s, scope, v, 1, feat158Ref())
	receipt, e := s.SubmitExpert(context.Background(), trigger)
	if e != nil {
		t.Fatal(e)
	}
	feat158Complete(t, s, receipt)
	if e = s.store.Close(); e != nil {
		t.Fatal(e)
	}
	old, e := OpenStore(home, WithMarketStorage())
	if e == nil {
		_ = old.Close()
		t.Fatal("non-expert writer accepted expert storage")
	}
	reopened, e := OpenStore(home, WithExpertStorage())
	if e != nil {
		t.Fatal(e)
	}
	defer reopened.Close()
	entry, e := reopened.readMarketOperation(string(v.Snapshot.TurnOperationId))
	if e != nil || !reflect.DeepEqual(entry.ExpertSnapshot, &snapshot) {
		t.Fatal("normal reopen lost frozen expert snapshot", entry, e)
	}
	binding, e := reopened.expertBinding(string(*receipt.Receipt.AgentSessionId))
	if e != nil || binding.Revision != 1 || !reflect.DeepEqual(binding.Expert, snapshot.Expert) {
		t.Fatal(binding, e)
	}
}
func TestFEAT158ExistingOrdinaryBindingAdoptionUsesSameThread(t *testing.T) {
	s, r, _, scope, home := feat158Session(t)
	v := marketSubmission(t, home)
	id, thread := uuid.NewString(), uuid.NewString()
	profile, _ := codex.ModelProfile(string(v.Intent.ProfileId))
	if e := s.store.Reserve(Record{TaskID: string(v.TaskId), AgentSessionID: id, Cwd: home, ModelProfile: string(v.Intent.ProfileId), ModelRevision: 1, ModelSelectionState: "ready"}); e != nil {
		t.Fatal(e)
	}
	if _, e := s.store.BindThread(id, thread, "", profile.Model, profile.Provider); e != nil {
		t.Fatal(e)
	}
	input := expert.ApplyPayload{OperationId: market.CanonicalId(uuid.NewString()), HostInstanceId: s.market.instance, Scope: scope, LocalSessionId: v.LocalSessionId, AgentSessionId: market.CanonicalId(id), NativeThreadId: expert.NativeId(thread), BindingRevision: 1, Expert: feat158Ref(), Intent: market.ModelIntent{ProfileId: v.Intent.ProfileId, ExpectedRevision: 1}, PermissionMode: market.PermissionModeAsk, MarketSnapshot: v.Snapshot}
	out, e := s.ApplyExpertBinding(context.Background(), input)
	if e != nil || out.State != expert.OperationStateSucceeded || out.NativeThreadId != input.NativeThreadId || strings.Join(r.order, ",") != "clear,switch" {
		t.Fatal(out, e, r.order)
	}
	record, e := s.store.Get(id)
	if e != nil || record.Purpose != purposeMarket || record.MarketLocalSessionID != string(v.LocalSessionId) {
		t.Fatal(record, e)
	}
	if _, e = s.ResumeSession(context.Background(), id, TraceContext{}); e == nil {
		t.Fatal("legacy resume ignored expert context")
	}
}

// A pending apply journal is an ordinary intermediate state, not an injected
// Runtime failure. Completed model receipts remain readable during that phase.
func TestFEAT158CompletedModelReplayPrecedesPendingExpertPreparation(t *testing.T) {
	s, r, resources, scope, home := feat158Session(t)
	submission := marketSubmission(t, home)
	trigger, _ := feat158Grant(t, s, scope, submission, 1, feat158Ref())
	received, err := s.SubmitExpert(context.Background(), trigger)
	if err != nil {
		t.Fatal(err)
	}
	feat158Complete(t, s, received)
	sessionID := string(*received.Receipt.AgentSessionId)
	request := model.SelectRequest{SchemaVersion: 1, OperationId: uuid.NewString(), ExpectedRevision: 1, ProfileId: model.ProfileIdMinimaxM3HighV1}
	completed, err := s.SelectChatModel(context.Background(), sessionID, request)
	if err != nil {
		t.Fatal(err)
	}
	record, err := s.store.Get(sessionID)
	if err != nil {
		t.Fatal(err)
	}
	pending := expertOperation{Version: 1, ScopeHash: s.marketScopeHash(scope), IntentDigest: strings.Repeat("d", 64), Receipt: expert.ApplyReceipt{
		OperationId: market.CanonicalId(uuid.NewString()), HostInstanceId: s.market.instance, LocalSessionId: submission.LocalSessionId, AgentSessionId: *received.Receipt.AgentSessionId,
		NativeThreadId: expert.NativeId(*received.Receipt.NativeThreadId), ExpectedBindingRevision: 1, BindingRevision: 1, State: expert.OperationStatePending, Expert: nil,
	}}
	if err = s.store.beginExpertOperation(record, pending); err != nil {
		t.Fatal(err)
	}
	beforeCalls, beforeOrder := resources.calls, len(r.order)
	replay, err := s.SelectChatModel(context.Background(), sessionID, request)
	if err != nil || !reflect.DeepEqual(replay, completed) || resources.calls != beforeCalls || len(r.order) != beforeOrder {
		t.Fatal("terminal receipt consulted later expert readiness or executed again", replay, err)
	}
	next := model.SelectRequest{SchemaVersion: 1, OperationId: uuid.NewString(), ExpectedRevision: 2, ProfileId: model.ProfileIdKimiK3MaxV1}
	if _, err = s.SelectChatModel(context.Background(), sessionID, next); ExpertFailureCode(err) != expert.ErrorCodeOperationPending {
		t.Fatal("new model operation bypassed pending expert preparation", err)
	}
	current, err := s.store.Get(sessionID)
	if err != nil || !reflect.DeepEqual(current, record) {
		t.Fatal("failed expert preparation changed model/session state", err)
	}
	if err = s.store.db.View(func(tx *bolt.Tx) error {
		if tx.Bucket(modelOperationsBucket).Get(turnOperationKey(sessionID, next.OperationId)) != nil {
			t.Fatal("failed preparation created a switching journal")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestFEAT158PendingExpertApplyAndCleanupAreTransactionallyExclusive(t *testing.T) {
	s, _, _, scope, home := feat158Session(t)
	submission := marketSubmission(t, home)
	trigger, _ := feat158Grant(t, s, scope, submission, 1, feat158Ref())
	received, err := s.SubmitExpert(context.Background(), trigger)
	if err != nil {
		t.Fatal(err)
	}
	feat158Complete(t, s, received)
	sessionID := string(*received.Receipt.AgentSessionId)
	record, err := s.store.Get(sessionID)
	if err != nil {
		t.Fatal(err)
	}
	// Pause only the in-memory test's normal storage sequence at the documented
	// pending phase. No process, permission or Runtime failure is manufactured.
	pending := expertOperation{Version: 1, ScopeHash: s.marketScopeHash(scope), IntentDigest: strings.Repeat("e", 64), Receipt: expert.ApplyReceipt{
		OperationId: market.CanonicalId(uuid.NewString()), HostInstanceId: s.market.instance, LocalSessionId: submission.LocalSessionId, AgentSessionId: *received.Receipt.AgentSessionId,
		NativeThreadId: expert.NativeId(*received.Receipt.NativeThreadId), ExpectedBindingRevision: 1, BindingRevision: 1, State: expert.OperationStatePending, Expert: nil,
	}}
	if err = s.store.beginExpertOperation(record, pending); err != nil {
		t.Fatal(err)
	}
	cleanupID := uuid.NewString()
	if _, err = s.store.BeginCleanup(cleanupID, sessionID); !errors.Is(err, ErrTurnActive) {
		t.Fatal("cleanup admitted a pending expert apply", err)
	}
	current, err := s.store.Get(sessionID)
	if err != nil || !reflect.DeepEqual(current, record) {
		t.Fatal("rejected cleanup changed session state", err)
	}
	// Finish the normal synthetic storage sequence, then exercise the opposite
	// ordering: a reserved cleanup blocks the next apply transaction.
	pending.Receipt.State = expert.OperationStateSucceeded
	pending.Receipt.BindingRevision = 2
	if err = s.store.finishExpertOperation(pending); err != nil {
		t.Fatal(err)
	}
	if _, err = s.store.BeginCleanup(cleanupID, sessionID); err != nil {
		t.Fatal(err)
	}
	next := pending
	next.Receipt.OperationId = market.CanonicalId(uuid.NewString())
	next.Receipt.State = expert.OperationStatePending
	next.Receipt.ExpectedBindingRevision = 2
	next.Receipt.BindingRevision = 2
	if err = s.store.beginExpertOperation(record, next); !errors.Is(err, ErrSessionNotUsable) {
		t.Fatal("apply admitted an already reserved cleanup", err)
	}
}

func TestFEAT158FeatureOffReaderPreservesExpertHistoryAndOrdinarySessions(t *testing.T) {
	s, r, _, scope, home := feat158Session(t)
	original := marketSubmission(t, home)
	trigger, _ := feat158Grant(t, s, scope, original, 1, feat158Ref())
	accepted, err := s.SubmitExpert(context.Background(), trigger)
	if err != nil {
		t.Fatal(err)
	}
	feat158Complete(t, s, accepted)
	if err = s.store.Close(); err != nil {
		t.Fatal(err)
	}
	reader, err := OpenStore(home, WithMarketStorage(), WithExpertStorageReader())
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	off := NewService(r, reader, NewEventHub(32, 16), nil, WithMarketAuthority(string(s.market.instance)))
	if _, err = off.MarketHello(market.HelloPayload{HostInstanceId: s.market.instance, OwnerUserId: scope.OwnerUserId, TenantId: scope.TenantId, NativeProcessEpoch: scope.NativeProcessEpoch}); err != nil {
		t.Fatal(err)
	}
	if off.ExpertsEnabled() {
		t.Fatal("compatibility reader enabled expert mutation")
	}
	read, err := off.ReadExpertSubmission(expert.SubmissionReadPayload{HostInstanceId: s.market.instance, Scope: scope, TaskId: original.TaskId, LocalSessionId: original.LocalSessionId, TurnOperationId: original.Snapshot.TurnOperationId})
	if err != nil || !reflect.DeepEqual(read, accepted) {
		t.Fatal("feature-off reader lost original expert receipt", err)
	}
	next := nextMarketTurn(t, original)
	next.AgentSessionId = accepted.Receipt.AgentSessionId
	next.Intent.ExpectedRevision = 1
	before := r.turns
	if _, err = off.SubmitMarket(context.Background(), registerMarket(t, off, scope, next)); err == nil || r.turns != before {
		t.Fatal("feature-off bare submit discarded expert context", err)
	}
	if _, err = off.SelectChatModel(context.Background(), string(*accepted.Receipt.AgentSessionId), model.SelectRequest{SchemaVersion: 1, OperationId: uuid.NewString(), ExpectedRevision: 1, ProfileId: model.ProfileIdMinimaxM3HighV1}); err == nil {
		t.Fatal("feature-off model switch discarded expert context")
	}
	ordinary := marketSubmission(t, home)
	if receipt, err := off.SubmitMarket(context.Background(), registerMarket(t, off, scope, ordinary)); err != nil || receipt.State != market.SubmissionStateAccepted {
		t.Fatal("ordinary session stopped working with compatibility reader", receipt, err)
	}
	if err = reader.db.View(func(tx *bolt.Tx) error {
		if string(tx.Bucket(metadataBucket).Get(storeSchemaVersionKey)) != "10" {
			t.Fatal("compatibility reader downgraded storage")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestFEAT158LegacyNineReaderDoesNotUpgradeUntilWriterIsEnabled(t *testing.T) {
	home := t.TempDir()
	legacy, err := OpenStore(home, WithMarketStorage())
	if err != nil {
		t.Fatal(err)
	}
	// Construct the prior documented candidate format in a fresh fixture. This
	// does not downgrade a schema 10 database or touch user files.
	if err = legacy.db.Update(func(tx *bolt.Tx) error {
		for _, b := range [][]byte{expertBindingsBucket, expertOperationsBucket} {
			if _, e := tx.CreateBucket(b); e != nil {
				return e
			}
		}
		return tx.Bucket(metadataBucket).Put(storeSchemaVersionKey, []byte("9"))
	}); err != nil {
		t.Fatal(err)
	}
	if err = legacy.Close(); err != nil {
		t.Fatal(err)
	}
	reader, err := OpenStore(home, WithMarketStorage(), WithExpertStorageReader())
	if err != nil {
		t.Fatal(err)
	}
	if err = reader.db.View(func(tx *bolt.Tx) error {
		if string(tx.Bucket(metadataBucket).Get(storeSchemaVersionKey)) != "9" || tx.Bucket(expertResourceOperationsBucket) != nil {
			t.Fatal("reader migrated legacy candidate")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err = reader.Close(); err != nil {
		t.Fatal(err)
	}
	writer, err := OpenStore(home, WithExpertStorage())
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	if err = writer.db.View(func(tx *bolt.Tx) error {
		if string(tx.Bucket(metadataBucket).Get(storeSchemaVersionKey)) != "10" || tx.Bucket(expertResourceOperationsBucket) == nil {
			t.Fatal("explicit writer did not complete normal 9 to 10 migration")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
