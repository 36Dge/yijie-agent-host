package session

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/36Dge/yijie-agent-host/internal/codex"
	expert "github.com/36Dge/yijie-agent-host/internal/contracts/experts"
	market "github.com/36Dge/yijie-agent-host/internal/contracts/markethost"
	"github.com/36Dge/yijie-agent-host/internal/skills"
	"github.com/google/uuid"
)

func originalExpertApply(t *testing.T, s *Service, scope market.ScopeBinding, home string) (Record, expert.ApplyPayload) {
	t.Helper()
	v := marketSubmission(t, home)
	id, thread := uuid.NewString(), uuid.NewString()
	p, _ := codex.ModelProfile(string(v.Intent.ProfileId))
	if e := s.store.Reserve(Record{TaskID: string(v.TaskId), AgentSessionID: id, Cwd: home, ModelProfile: string(v.Intent.ProfileId), ModelRevision: 1, ModelSelectionState: "ready"}); e != nil {
		t.Fatal(e)
	}
	r, e := s.store.BindThread(id, thread, "", p.Model, p.Provider)
	if e != nil {
		t.Fatal(e)
	}
	return r, expert.ApplyPayload{OperationId: market.CanonicalId(uuid.NewString()), HostInstanceId: s.market.instance, Scope: scope, LocalSessionId: v.LocalSessionId, AgentSessionId: market.CanonicalId(id), NativeThreadId: expert.NativeId(thread), BindingRevision: 1, Expert: feat158Ref(), Intent: market.ModelIntent{ProfileId: v.Intent.ProfileId, ExpectedRevision: 1}, PermissionMode: market.PermissionModeAsk, MarketSnapshot: v.Snapshot}
}

// These are normal adapter storage phases. No Runtime failure, executable,
// process termination or actual turn is manufactured by this test.
func TestFEAT158ExplicitBindingRecoveryKeepsOriginalIdentityAcrossNormalReopen(t *testing.T) {
	for _, phase := range []string{"not_received", "pending", "unknown"} {
		t.Run(phase, func(t *testing.T) {
			s, runtime, resources, scope, home := feat158Session(t)
			record, original := originalExpertApply(t, s, scope, home)
			if phase != "not_received" {
				digest, _ := s.marketHMAC("yijie.expert-apply/v1", original)
				op := expertOperation{Version: 1, ScopeHash: s.marketScopeHash(scope), IntentDigest: digest, Receipt: expert.ApplyReceipt{OperationId: original.OperationId, HostInstanceId: original.HostInstanceId, LocalSessionId: original.LocalSessionId, AgentSessionId: original.AgentSessionId, NativeThreadId: original.NativeThreadId, ExpectedBindingRevision: 0, BindingRevision: 0, Expert: original.Expert, State: expert.OperationStatePending}}
				if e := s.store.beginExpertOperation(record, op); e != nil {
					t.Fatal(e)
				}
				if phase == "unknown" {
					op.Receipt.State = expert.OperationStateUnknown
					if e := s.store.finishExpertOperation(op); e != nil {
						t.Fatal(e)
					}
				}
				if _, e := s.ReadExpertOperation(expert.PrivateOperationReadPayload{HostInstanceId: s.market.instance, Scope: scope, LocalSessionId: original.LocalSessionId, AgentSessionId: original.AgentSessionId, OperationId: original.OperationId}); e != nil {
					t.Fatal(e)
				}
				if _, e := s.ApplyExpertBinding(context.Background(), original); e != nil {
					t.Fatal(e)
				}
				if runtime.switches != 0 {
					t.Fatal("read/repeated set executed recovery")
				}
			}
			if e := s.store.Close(); e != nil {
				t.Fatal(e)
			}
			store, e := OpenStore(home, WithExpertStorage())
			if e != nil {
				t.Fatal(e)
			}
			defer store.Close()
			instance := market.CanonicalId(uuid.NewString())
			runtime.process.HostInstanceId = instance
			reopened := NewService(runtime, store, NewEventHub(32, 16), nil, WithMarketAuthority(string(instance)), WithExpertResources(resources))
			current := scope
			current.NativeProcessEpoch = market.CanonicalId(uuid.NewString())
			current.AuthorizationRevision++
			if _, e = reopened.MarketHello(market.HelloPayload{HostInstanceId: instance, OwnerUserId: current.OwnerUserId, TenantId: current.TenantId, NativeProcessEpoch: current.NativeProcessEpoch}); e != nil {
				t.Fatal(e)
			}
			recover := expert.PrivateBindingRecoverPayload{HostInstanceId: instance, Scope: current, Original: original}
			receipt, e := reopened.RecoverExpertBinding(context.Background(), recover)
			if e != nil || receipt.State != expert.OperationStateSucceeded || receipt.HostInstanceId != original.HostInstanceId || receipt.OperationId != original.OperationId || receipt.BindingRevision != 1 || receipt.NativeThreadId != original.NativeThreadId || runtime.switches != 1 || runtime.turns != 0 {
				t.Fatal(receipt, e, runtime.switches, runtime.turns)
			}
			again, e := reopened.RecoverExpertBinding(context.Background(), recover)
			if e != nil || !reflect.DeepEqual(again, receipt) || runtime.switches != 1 {
				t.Fatal("recovery re-executed terminal operation", e)
			}
			binding, e := store.expertBinding(record.AgentSessionID)
			if e != nil || binding.PendingOperation != "" || binding.Revision != 1 {
				t.Fatal(binding, e)
			}
		})
	}
}

func TestFEAT158ExplicitResourceRecoveryRejectsOnlyUnstartedOriginals(t *testing.T) {
	bundle := os.Getenv("YIJIE_EXPERT_TEST_BUNDLE")
	if bundle == "" {
		t.Skip("actual reviewed bundle not supplied")
	}
	for _, phase := range []string{"not_received", "host_pending", "core_completed"} {
		for _, action := range []string{"prepare", "remove"} {
			t.Run(phase+"/"+action, func(t *testing.T) {
				s, _, _, scope, _ := feat158Session(t)
				projection := &feat158ResourceProjection{}
				resources, e := skills.NewService(skills.Config{BundleRoot: bundle, ManagedRoot: filepath.Join(t.TempDir(), "installed"), Runtime: projection, ExpertCatalogDigest: skills.ReviewedExpertCatalogDigest})
				if e != nil {
					t.Fatal(e)
				}
				defer resources.Close()
				s.expertResources = resources
				selected := expert.ExpertSelection{ExpertId: "jd-ops-expert", Version: "0.1.3"}
				descriptor, e := resources.DescribeExpert(selected)
				if e != nil {
					t.Fatal(e)
				}
				original := expert.PrivateResourcePayload{HostInstanceId: s.market.instance, Scope: scope, OperationId: market.CanonicalId(uuid.NewString()), Expert: selected}
				if phase == "host_pending" {
					digest, _ := s.marketHMAC("yijie.expert-resources/v1", []any{action, original})
					op := expertResourceOperation{Version: 1, ScopeHash: s.marketScopeHash(scope), IntentDigest: digest, InstallationID: descriptor.InstallationID, Reference: descriptor.Reference, Receipt: expert.ResourceOperation{OperationId: original.OperationId, Expert: selected, Action: action, State: expert.OperationStatePending}}
					if e = s.store.beginExpertResourceOperation(&op); e != nil {
						t.Fatal(e)
					}
					if _, e = s.ReadExpertResourceOperation(expert.PrivateResourceReadPayload{HostInstanceId: s.market.instance, Scope: scope, OperationId: original.OperationId}); e != nil {
						t.Fatal(e)
					}
					observed, e := resources.ReadExpertResourceOperation(string(original.OperationId), descriptor.InstallationID, action)
					if e != nil || observed.Found {
						t.Fatal("read created a core operation", e)
					}
				}
				if phase == "core_completed" {
					if action == "prepare" {
						_, e = resources.PrepareExpertResources(context.Background(), string(original.OperationId), selected)
					} else {
						_, e = resources.RemoveExpertResources(context.Background(), string(original.OperationId), selected)
					}
					if e != nil {
						t.Fatal(e)
					}
				}
				recovery := expert.PrivateResourceRecoverPayload{HostInstanceId: s.market.instance, Scope: scope, Original: original, Action: action}
				receipt, e := s.RecoverExpertResources(context.Background(), recovery)
				want := expert.OperationStateRejected
				if phase == "core_completed" {
					want = expert.OperationStateSucceeded
				}
				if e != nil || receipt.State != want || receipt.OperationId != original.OperationId {
					t.Fatal(receipt, e)
				}
				again, e := s.RecoverExpertResources(context.Background(), recovery)
				if e != nil || !reflect.DeepEqual(again, receipt) {
					t.Fatal("resource recovery replaced terminal receipt", e)
				}
				if phase != "core_completed" {
					if action == "prepare" {
						_, e = resources.PrepareExpertResources(context.Background(), string(original.OperationId), selected)
					} else {
						_, e = resources.RemoveExpertResources(context.Background(), string(original.OperationId), selected)
					}
					if skills.ErrorCodeOf(e) != skills.CodeOperationConflict {
						t.Fatal("late original mutation was not rejected", e)
					}
					if _, e = resources.ResolveExpert(descriptor.Reference); e == nil {
						t.Fatal("recovery installed an unstarted resource")
					}
				}
				if len(projection.roots) != 0 || projection.writes != 0 {
					t.Fatal("recovery projected global skills")
				}
			})
		}
	}
}

func TestFEAT158BindingRecoveryDoesNotBypassAnotherPendingTurn(t *testing.T) {
	s, runtime, _, scope, home := feat158Session(t)
	record, original := originalExpertApply(t, s, scope, home)
	_, _, e := s.store.prepareModelTurnOperation(record.AgentSessionID, uuid.NewString(), strings.Repeat("a", 64), TraceContext{}, record.ModelProfile)
	if e != nil {
		t.Fatal(e)
	}
	_, e = s.RecoverExpertBinding(context.Background(), expert.PrivateBindingRecoverPayload{HostInstanceId: s.market.instance, Scope: scope, Original: original})
	if e == nil || runtime.switches != 0 || runtime.turns != 0 {
		t.Fatal("recovery bypassed reserved turn", e)
	}
	if _, e = s.store.readExpertOperation(string(original.OperationId)); !errors.Is(e, ErrNotFound) {
		t.Fatal("blocked recovery created an apply journal", e)
	}
}
