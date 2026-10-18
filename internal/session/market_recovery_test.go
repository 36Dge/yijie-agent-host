package session

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	expert "github.com/36Dge/yijie-agent-host/internal/contracts/experts"
	market "github.com/36Dge/yijie-agent-host/internal/contracts/markethost"
	"github.com/google/uuid"
)

// Ordinary native history/status responses; no process failure or storage edits.
type historyRecoveryRuntime struct {
	*marketSessionRuntime
	turn, terminal, current string
	afterRead               func()
}

func (r *historyRecoveryRuntime) ReadThread(_ context.Context, id string) (json.RawMessage, error) {
	return json.Marshal(map[string]any{"id": id, "turns": []any{map[string]any{"id": r.turn, "status": r.terminal, "items": []any{}}}})
}
func (r *historyRecoveryRuntime) ReadThreadStatus(context.Context, string) (string, error) {
	if r.afterRead != nil {
		r.afterRead()
	}
	return r.current, nil
}

func TestFEAT160HistoricalExpertContinuationReusesThreadAndReceipt(t *testing.T) {
	s, runtime, _, scope, home := feat158Session(t)
	v := marketSubmission(t, home)
	trigger, _ := feat158Grant(t, s, scope, v, 1, feat158Ref())
	first, err := s.SubmitExpert(context.Background(), trigger)
	if err != nil {
		t.Fatal(err)
	}
	// Normal close/reopen retains the old running index. Native history is the
	// authoritative terminal fact used when the next explicit turn is submitted.
	if err = s.store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := OpenStore(home, WithExpertStorage())
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	s.store = reopened
	s.runtime = &historyRecoveryRuntime{marketSessionRuntime: runtime, turn: string(*first.Receipt.NativeTurnId), terminal: "interrupted", current: "notLoaded"}
	v = nextMarketTurn(t, v)
	v.AgentSessionId = first.Receipt.AgentSessionId
	v.Intent.ExpectedRevision = 1
	trigger, _ = feat158Grant(t, s, scope, v, 1, feat158Ref())
	next, err := s.SubmitExpert(context.Background(), trigger)
	if err != nil || runtime.turns != 2 || *next.Receipt.NativeThreadId != *first.Receipt.NativeThreadId || !reflect.DeepEqual(next.ExpertSnapshot.Expert, first.ExpertSnapshot.Expert) {
		t.Fatal("same expert/thread continuation failed", err)
	}
	replay, err := s.SubmitExpert(context.Background(), trigger)
	if err != nil || !reflect.DeepEqual(replay, next) || runtime.turns != 2 {
		t.Fatal("duplicate dispatch", err)
	}
	feat158Complete(t, s, next)
	record, err := s.store.Get(string(*next.Receipt.AgentSessionId))
	if err != nil || record.State != StateIdle || record.ActiveTurnID != "" {
		t.Fatal(record, err)
	}
}

func TestFEAT160RecoveryRequiresExactTerminalAndCurrentIdle(t *testing.T) {
	for _, tc := range []struct {
		name, terminal, current string
		same                    bool
	}{
		{"still_running", "inProgress", "active", true},
		{"current_active", "interrupted", "active", true},
		{"other_terminal", "completed", "notLoaded", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, runtime, _, scope, home := feat158Session(t)
			v := marketSubmission(t, home)
			trigger, _ := feat158Grant(t, s, scope, v, 1, feat158Ref())
			first, err := s.SubmitExpert(context.Background(), trigger)
			if err != nil {
				t.Fatal(err)
			}
			turn := string(*first.Receipt.NativeTurnId)
			if !tc.same {
				turn = uuid.NewString()
			}
			s.runtime = &historyRecoveryRuntime{marketSessionRuntime: runtime, turn: turn, terminal: tc.terminal, current: tc.current}
			before, _ := s.store.Get(string(*first.Receipt.AgentSessionId))
			v = nextMarketTurn(t, v)
			v.AgentSessionId = first.Receipt.AgentSessionId
			v.Intent.ExpectedRevision = 1
			trigger, _ = feat158Grant(t, s, scope, v, 1, feat158Ref())
			if _, err = s.SubmitExpert(context.Background(), trigger); ExpertFailureCode(err) != expert.ErrorCodeBusy {
				t.Fatal("expected busy", err)
			}
			after, _ := s.store.Get(before.AgentSessionID)
			if runtime.turns != 1 || !reflect.DeepEqual(before, after) {
				t.Fatal("unproven history changed state")
			}
		})
	}
}

func TestFEAT160NewerNotificationWinsOverHistoricalRead(t *testing.T) {
	s, runtime, _, scope, home := feat158Session(t)
	v := marketSubmission(t, home)
	trigger, _ := feat158Grant(t, s, scope, v, 1, feat158Ref())
	first, err := s.SubmitExpert(context.Background(), trigger)
	if err != nil {
		t.Fatal(err)
	}
	before, _ := s.store.Get(string(*first.Receipt.AgentSessionId))
	s.runtime = &historyRecoveryRuntime{marketSessionRuntime: runtime, turn: before.ActiveTurnID, terminal: "interrupted", current: "idle", afterRead: func() { feat158Complete(t, s, first) }}
	recovered, err := s.reconcileMarketIdle(context.Background(), before)
	if err != nil || recovered.State != StateIdle || recovered.LastTurnStatus != "completed" || recovered.NativeRevision <= before.NativeRevision {
		t.Fatal("stale read overwrote notification", recovered, err)
	}
}

func TestFEAT160ReceiptAbsenceRequiresMappedSessionAndSerializedRead(t *testing.T) {
	s, runtime, _, scope, home := feat158Session(t)
	v := marketSubmission(t, home)
	trigger, _ := feat158Grant(t, s, scope, v, 1, feat158Ref())
	first, err := s.SubmitExpert(context.Background(), trigger)
	if err != nil {
		t.Fatal(err)
	}
	request := expert.SubmissionReadPayload{HostInstanceId: s.market.instance, Scope: scope, TaskId: v.TaskId, LocalSessionId: v.LocalSessionId, TurnOperationId: v.Snapshot.TurnOperationId}
	got, err := s.ReadExpertSubmission(request)
	if err != nil || !reflect.DeepEqual(got, first) {
		t.Fatal("accepted receipt lost", err)
	}
	request.TurnOperationId = market.CanonicalId(uuid.NewString())
	s.modelOperationMu.Lock()
	_, err = s.ReadExpertSubmission(request)
	s.modelOperationMu.Unlock()
	if ExpertFailureCode(err) != expert.ErrorCodeBusy {
		t.Fatal("read raced admission", err)
	}
	if _, err = s.ReadExpertSubmission(request); ExpertFailureCode(err) != expert.ErrorCodeNotFound {
		t.Fatal("mapped genuine absence", err)
	}
	request.TaskId = market.CanonicalId(uuid.NewString())
	if _, err = s.ReadExpertSubmission(request); ExpertFailureCode(err) != expert.ErrorCodeOutcomeUnknown {
		t.Fatal("unmapped absence claimed certainty", err)
	}
	if runtime.turns != 1 {
		t.Fatal("read dispatched a turn")
	}
}
