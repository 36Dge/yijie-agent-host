package session

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/36Dge/yijie-agent-host/internal/codex"
	contract "github.com/36Dge/yijie-agent-host/internal/contracts"
	wire "github.com/36Dge/yijie-agent-host/internal/contracts/markethost"
)

type scopedMarketSkills struct {
	expected string
	calls    int
}

func (*scopedMarketSkills) ResolveSelectedSkills(context.Context, []contract.SelectedSkillRef) ([]codex.UserInput, error) {
	return nil, errors.New("actual working directory required")
}
func (r *scopedMarketSkills) ResolveSelectedSkillsInDirectory(_ context.Context, refs []contract.SelectedSkillRef, cwd string) ([]codex.UserInput, error) {
	if cwd != r.expected || len(refs) != 1 || refs[0].Id != "ordinary-sample" || refs[0].Version != "1.0.0" {
		return nil, errors.New("invalid frozen selection")
	}
	r.calls++
	input, err := codex.SkillUserInput("ordinary-sample", cwd+"/SKILL.md")
	return []codex.UserInput{input}, err
}

func TestFEAT174MarketSubmissionKeepsSkillCWDThroughContinuationAndReplay(t *testing.T) {
	s, r, scope, cwd := marketService(t)
	resources := &scopedMarketSkills{expected: cwd}
	s.selectedSkills = resources
	v := marketSubmission(t, cwd)
	var block wire.ContentBlock
	if err := json.Unmarshal([]byte(`{"type":"text","text":"ordinary scoped selection","skills":[{"id":"ordinary-sample","version":"1.0.0"}]}`), &block); err != nil {
		t.Fatal(err)
	}
	v.ContentBlocks = []wire.ContentBlock{block}
	trigger := registerMarket(t, s, scope, v)
	first, err := s.SubmitMarket(context.Background(), trigger)
	if err != nil || first.State != wire.SubmissionStateAccepted || resources.calls != 2 || r.turns != 1 {
		t.Fatal("new market submission lost actual CWD", first, err, resources.calls)
	}
	resources.expected = "unavailable after acceptance"
	if replay, err := s.SubmitMarket(context.Background(), trigger); err != nil || replay.State != wire.SubmissionStateAccepted || resources.calls != 2 || r.turns != 1 {
		t.Fatal("accepted replay resolved or dispatched again", err)
	}
	resources.expected = cwd
	s.HandleNotification(RuntimeNotificationTurnCompleted, rawJSON(t, map[string]any{"threadId": *first.NativeThreadId, "turn": map[string]any{"id": *first.NativeTurnId, "status": "completed"}}))
	v = nextMarketTurn(t, v)
	v.AgentSessionId = first.AgentSessionId
	v.Intent.ExpectedRevision = 1
	second, err := s.SubmitMarket(context.Background(), registerMarket(t, s, scope, v))
	if err != nil || second.State != wire.SubmissionStateAccepted || resources.calls != 4 || r.turns != 2 {
		t.Fatal("market continuation lost actual CWD", second, err, resources.calls)
	}
}

type busySkillRuntime struct {
	*marketSessionRuntime
	busy bool
}

func (r *busySkillRuntime) HoldSkillResources(ctx context.Context) (context.Context, func(), error) {
	if r.busy {
		return ctx, nil, errors.New("normal skill maintenance")
	}
	return ctx, func() {}, nil
}
func TestFEAT174BusyRejectionProofPersistsAndOnlyNewIntentDispatches(t *testing.T) {
	s, r, scope, home := marketService(t)
	gate := &busySkillRuntime{marketSessionRuntime: r, busy: true}
	s.runtime = gate
	v := marketSubmission(t, home)
	trigger := registerMarket(t, s, scope, v)
	if _, err := s.SubmitMarket(context.Background(), trigger); MarketFailureCode(err) != wire.ErrorCodeBusy {
		t.Fatal(err)
	}
	proof, err := s.ReadMarketSubmissionRejection(string(v.TaskId), string(v.Snapshot.TurnOperationId))
	if err != nil || proof.Validate() != nil || proof.Reason != "skill_maintenance_busy" || len(r.order) != 0 {
		t.Fatal("unproven rejection or native side effect", proof, err, r.order)
	}
	// Normal stop/reopen, including the existing model envelope on the next open.
	if err = s.store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err := OpenStore(home, WithMarketStorage(), WithChatModelV2Storage())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	s.store = store
	recovered, err := s.ReadMarketSubmissionRejection(string(v.TaskId), string(v.Snapshot.TurnOperationId))
	if err != nil || recovered != proof {
		t.Fatal("proof lost", err)
	}
	gate.busy = false
	if _, err = s.SubmitMarket(context.Background(), trigger); MarketFailureCode(err) != wire.ErrorCodeBusy || len(r.order) != 0 {
		t.Fatal("old rejection replayed", err)
	}
	changed := v
	changed.Cwd = t.TempDir()
	if _, err = s.SubmitMarket(context.Background(), registerMarket(t, s, scope, changed)); MarketFailureCode(err) != wire.ErrorCodeRequestConflict {
		t.Fatal("changed old intent accepted", err)
	}
	v = nextMarketTurn(t, v)
	receipt, err := s.SubmitMarket(context.Background(), registerMarket(t, s, scope, v))
	if err != nil || receipt.State != wire.SubmissionStateAccepted || r.turns != 1 {
		t.Fatal("explicit new message failed", err)
	}
	if _, err = s.ReadMarketSubmissionRejection(string(v.TaskId), string(v.Snapshot.TurnOperationId)); MarketFailureCode(err) != wire.ErrorCodeNotFound {
		t.Fatal("accepted attempt has false rejection", err)
	}
	t.Log("normal busy gate: zero native admission; durable bound proof; old attempt never redispatched; new explicit intent accepted")
}
