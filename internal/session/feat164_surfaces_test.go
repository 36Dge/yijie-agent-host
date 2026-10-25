package session

import (
	"context"
	"github.com/36Dge/yijie-agent-host/internal/codex"
	expert "github.com/36Dge/yijie-agent-host/internal/contracts/experts"
	wire "github.com/36Dge/yijie-agent-host/internal/contracts/markethost"
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
