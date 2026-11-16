package session

import (
	"context"
	"testing"

	"github.com/36Dge/yijie-agent-host/internal/codex"
	web "github.com/36Dge/yijie-agent-host/internal/contracts/builtinweb"
	expert "github.com/36Dge/yijie-agent-host/internal/contracts/experts"
	market "github.com/36Dge/yijie-agent-host/internal/contracts/markethost"
	"github.com/google/uuid"
)

type builtinSessionRuntime struct {
	*marketSessionRuntime
	root, turn string
	pending    market.MarketApproval
	decisions  int
}

func (r *builtinSessionRuntime) ListMarketApprovals(string) []market.MarketApproval {
	return []market.MarketApproval{r.pending}
}
func (r *builtinSessionRuntime) DecideMarketApproval(context.Context, string, market.ApprovalDecidePayload) (market.MarketApproval, error) {
	r.decisions++
	return r.pending, nil
}

func (r *builtinSessionRuntime) BuiltinWebOwner(context.Context, string, string) (string, string, error) {
	return r.root, r.turn, nil
}
func (r *builtinSessionRuntime) BuiltinWebControl(context.Context, string, []byte) (web.ControlAck, error) {
	return web.ControlAck{State: "applied"}, nil
}
func TestFEAT175BuiltinWebUsesCurrentExpertAndStop(t *testing.T) {
	for _, offline := range []bool{false, true} {
		t.Run(map[bool]string{false: "ordinary", true: "offline"}[offline], func(t *testing.T) {
			s, base, _, scope, home := feat158Session(t)
			runtime := &builtinSessionRuntime{marketSessionRuntime: base}
			s.runtime = runtime
			ref := feat158Ref()
			if offline {
				p := web.ExpertPolicies[2]
				ref.ExpertId = expert.ExpertId(p.ExpertID)
				ref.Version = expert.Version(p.Version)
				ref.RoleSha256 = expert.Digest(p.RoleSHA256)
			}
			v := marketSubmission(t, home)
			trigger, _ := feat158Grant(t, s, scope, v, 1, ref)
			receipt, err := s.SubmitExpert(context.Background(), trigger)
			if err != nil {
				t.Fatal(err)
			}
			runtime.root = string(*receipt.Receipt.NativeThreadId)
			runtime.turn = string(*receipt.Receipt.NativeTurnId)
			process := uuid.MustParse(string(scope.NativeProcessEpoch))
			binding := uuid.New()
			s.builtinWeb.process = process
			s.builtinWeb.binding = binding
			s.builtinWeb.installed = true
			operation, _ := uuid.NewV7()
			q := web.WebAdmissionRequest{ProcessEpoch: process, BindingEpoch: binding, OperationId: operation.String(), ThreadId: runtime.root, TurnId: runtime.turn}
			admitted, err := s.AdmitBuiltinWeb(context.Background(), q)
			approval, call := market.CanonicalId(uuid.NewString()), market.CanonicalId(uuid.NewString())
			runtime.pending = market.MarketApproval{ApprovalId: approval, State: market.ApprovalStatePending, Identity: market.CallIdentity{ServiceId: "kimi-web", CallRef: call}}
			_, legacyErr := s.DecideMarketApproval(context.Background(), market.ApprovalDecidePayload{OperationId: market.CanonicalId(uuid.NewString()), HostInstanceId: s.market.instance, Scope: scope, AgentSessionId: *receipt.Receipt.AgentSessionId, ApprovalId: approval, CallRef: call, ExpectedRevision: 1, DecisionId: market.CanonicalId(uuid.NewString()), Decision: "approve_once"})
			if offline {
				if err == nil {
					t.Fatal("offline expert admitted retrieval")
				}
				if MarketFailureCode(legacyErr) != market.ErrorCodePermissionDenied || runtime.decisions != 0 {
					t.Fatal("offline policy missed legacy web call", legacyErr)
				}
				return
			}
			if legacyErr != nil || runtime.decisions != 1 {
				t.Fatal("ordinary expert lost existing decision semantics", legacyErr)
			}
			if err != nil || admitted.RootTurnId != runtime.turn {
				t.Fatal("ordinary expert admission", err)
			}
			s.ObserveBuiltinWeb(codex.BuiltinWebTurnEvent{ThreadID: runtime.root, TurnID: runtime.turn, State: "stopped"})
			if _, err = s.AdmitBuiltinWeb(context.Background(), q); err == nil {
				t.Fatal("late call admitted after stop")
			}
		})
	}
}
