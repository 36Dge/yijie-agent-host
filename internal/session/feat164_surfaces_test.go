package session

import (
	"context"
	"errors"
	"github.com/36Dge/yijie-agent-host/internal/codex"
	models "github.com/36Dge/yijie-agent-host/internal/contracts/chatmodelsv2"
	expert "github.com/36Dge/yijie-agent-host/internal/contracts/experts"
	wire "github.com/36Dge/yijie-agent-host/internal/contracts/markethost"
	"github.com/google/uuid"
	"reflect"
	"testing"
)

// Uses the real domain service/store and existing normal in-process runtime double.
// No executable replacement, provider, archive installation or external endpoint.
type feat164MarketRuntime struct {
	*marketSessionRuntime
	paused bool
}

func (*feat164MarketRuntime) ChatModelsV2Enabled() bool { return true }
func (*feat164MarketRuntime) ChatModelsV2Ready() bool   { return true }
func (r *feat164MarketRuntime) ModelAvailable(id string) bool {
	return id == "minimax-m3-high-v1" || (id == codex.KimiK31ProfileID && !r.paused)
}
func TestFEAT164MarketExpertModelWiring(t *testing.T) {
	for _, family := range []string{"market", "expert"} {
		t.Run(family, func(t *testing.T) {
			s, rt, _, scope, home := feat158Session(t)
			r := &feat164MarketRuntime{marketSessionRuntime: rt}
			s.runtime = r
			input := marketSubmission(t, home)
			input.Intent.ProfileId = codex.KimiK31ProfileID
			var receipt wire.SubmissionReceipt
			if family == "market" {
				trigger := registerMarket(t, s, scope, input)
				v, e := s.SubmitMarket(context.Background(), trigger)
				if e != nil {
					t.Fatal(e)
				}
				receipt = v
				again, e := s.SubmitMarket(context.Background(), trigger)
				if e != nil || !reflect.DeepEqual(v, again) {
					t.Fatal("receipt replay", e)
				}
			} else {
				trigger, snapshot := feat158Grant(t, s, scope, input, 1, feat158Ref())
				v, e := s.SubmitExpert(context.Background(), trigger)
				if e != nil {
					t.Fatal(e)
				}
				receipt = v.Receipt
				again, e := s.SubmitExpert(context.Background(), trigger)
				if e != nil || !reflect.DeepEqual(v, again) || !reflect.DeepEqual(v.ExpertSnapshot, snapshot) {
					t.Fatal("expert receipt replay", e)
				}
			}
			if receipt.ProfileId != codex.KimiK31ProfileID || receipt.State != wire.SubmissionStateAccepted || rt.turns != 1 {
				t.Fatal("model/dispatch differs", receipt, rt.turns)
			}
			before, e := s.store.readMarketOperation(string(input.Snapshot.TurnOperationId))
			if e != nil {
				t.Fatal(e)
			}
			r.paused = true
			var read wire.SubmissionReceipt
			if family == "expert" {
				value, err := s.ReadExpertSubmission(expert.SubmissionReadPayload{HostInstanceId: s.market.instance, Scope: scope, TaskId: input.TaskId, LocalSessionId: input.LocalSessionId, TurnOperationId: input.Snapshot.TurnOperationId})
				e, read = err, value.Receipt
			} else {
				read, e = s.ReadMarketOperation(string(input.TaskId), string(input.Snapshot.TurnOperationId))
			}
			if e != nil || read.ProfileId != receipt.ProfileId {
				t.Fatal("paused recovery", e)
			}
			after, _ := s.store.readMarketOperation(string(input.Snapshot.TurnOperationId))
			if !reflect.DeepEqual(before, after) || rt.turns != 1 {
				t.Fatal("read changed original facts")
			}
		})
	}
	for _, family := range []string{"market", "expert"} {
		for _, profile := range []string{"kimi-k3-max-v1", codex.KimiK31ProfileID} {
			t.Run("deny_"+family+"_"+profile, func(t *testing.T) {
				s, rt, _, scope, home := feat158Session(t)
				s.runtime = &feat164MarketRuntime{marketSessionRuntime: rt, paused: true}
				input := marketSubmission(t, home)
				input.Intent.ProfileId = wire.ProfileId(profile)
				var err error
				if family == "market" {
					trigger := registerMarket(t, s, scope, input)
					_, err = s.SubmitMarket(context.Background(), trigger)
				} else {
					trigger, _ := feat158Grant(t, s, scope, input, 1, feat158Ref())
					_, err = s.SubmitExpert(context.Background(), trigger)
				}
				if err == nil || rt.turns != 0 {
					t.Fatal("retired/paused executed", err)
				}
			})
		}
	}
}

// Ordinary stored state + domain notifications; no process/transport injection.
func TestFEAT164SettledMarketExpertModelSelection(t *testing.T) {
	for _, family := range []string{"market", "expert"} {
		t.Run(family, func(t *testing.T) {
			s, rt, _, scope, home := feat158Session(t)
			if err := s.store.Close(); err != nil {
				t.Fatal(err)
			}
			store, err := OpenStore(home, WithExpertStorage(), WithChatModelV2Storage())
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			s.store = store
			runtime := &feat164MarketRuntime{marketSessionRuntime: rt}
			s.runtime = runtime
			input := marketSubmission(t, home)
			input.Intent.ProfileId = codex.KimiK31ProfileID
			var receipt wire.SubmissionReceipt
			if family == "market" {
				receipt, err = s.SubmitMarket(context.Background(), registerMarket(t, s, scope, input))
			} else {
				trigger, _ := feat158Grant(t, s, scope, input, 1, feat158Ref())
				var result expert.PrivateReceipt
				result, err = s.SubmitExpert(context.Background(), trigger)
				receipt = result.Receipt
			}
			if err != nil {
				t.Fatal(err)
			}
			request := models.SelectRequest{SchemaVersion: 2, OperationId: uuid.NewString(), ExpectedRevision: 1, ProfileId: models.ActiveProfileIdMinimaxM3HighV1}
			if _, err = s.SelectChatModelV2(context.Background(), string(*receipt.AgentSessionId), request); !errors.Is(err, ErrTurnActive) {
				t.Fatal("active turn admitted", err)
			}
			s.HandleNotification(RuntimeNotificationTurnCompleted, rawJSON(t, map[string]any{"threadId": *receipt.NativeThreadId, "turn": map[string]any{"id": *receipt.NativeTurnId, "status": "completed"}}))
			before, err := s.store.readMarketOperation(string(input.Snapshot.TurnOperationId))
			if err != nil {
				t.Fatal(err)
			}
			oldOrder := len(rt.order)
			oldTurns := rt.turns
			runtime.paused = true
			selected, err := s.SelectChatModelV2(context.Background(), string(*receipt.AgentSessionId), request)
			if err != nil || selected.State != models.SelectionStateReady || selected.Revision != 2 || *selected.ProfileId != models.ProfileIdMinimaxM3HighV1 {
				t.Fatal(selected, err)
			}
			if !reflect.DeepEqual(rt.order[oldOrder:], []string{"clear", "switch"}) || rt.turns != oldTurns {
				t.Fatal("configuration reused business authority", rt.order, rt.turns)
			}
			after, _ := s.store.readMarketOperation(string(input.Snapshot.TurnOperationId))
			if !reflect.DeepEqual(before, after) {
				t.Fatal("original snapshot or receipt changed")
			}
			orderAfter := len(rt.order)
			replay, err := s.RecoverChatModelV2(context.Background(), string(*receipt.AgentSessionId), modelV2Recover(request))
			if err != nil || replay.State != models.ModelOperationStateCommitted || len(rt.order) != orderAfter {
				t.Fatal("terminal replay reconfigured", err)
			}
		})
	}
}
