package session

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/36Dge/yijie-agent-host/internal/codex"
	expert "github.com/36Dge/yijie-agent-host/internal/contracts/experts"
	market "github.com/36Dge/yijie-agent-host/internal/contracts/markethost"
	"github.com/36Dge/yijie-agent-host/internal/skills"
	"github.com/google/uuid"
)

// Normal in-process Skills API projection; no executable, model or subprocess.
type feat158ResourceProjection struct {
	roots  []string
	writes int
}

func (*feat158ResourceProjection) ListSkills(context.Context, []string, bool) ([]codex.SkillsListEntry, error) {
	return []codex.SkillsListEntry{}, nil
}
func (r *feat158ResourceProjection) SetSkillsExtraRoots(_ context.Context, roots []string) error {
	r.roots = append([]string(nil), roots...)
	return nil
}
func (r *feat158ResourceProjection) WriteSkillConfig(context.Context, string, bool) (bool, error) {
	r.writes++
	return true, nil
}

func TestFEAT158ActualResourceLifecycleReusesCoreJournalAndProtectsReferences(t *testing.T) {
	bundle := os.Getenv("YIJIE_EXPERT_TEST_BUNDLE")
	if bundle == "" {
		t.Skip("actual reviewed bundle not supplied")
	}
	s, _, _, scope, home := feat158Session(t)
	projection := &feat158ResourceProjection{}
	managed := filepath.Join(t.TempDir(), "installed")
	resources, e := skills.NewService(skills.Config{BundleRoot: bundle, ManagedRoot: managed, Runtime: projection, ExpertCatalogDigest: skills.ReviewedExpertCatalogDigest})
	if e != nil {
		t.Fatal(e)
	}
	defer resources.Close()
	s.expertResources = resources
	selection := expert.ExpertSelection{ExpertId: "jd-ops-expert", Version: "0.1.3"}
	prepare := expert.PrivateResourcePayload{HostInstanceId: s.market.instance, Scope: scope, OperationId: market.CanonicalId(uuid.NewString()), Expert: selection}
	prepared, e := s.PrepareExpertResources(context.Background(), prepare)
	if e != nil || prepared.Validate() != nil || prepared.State != expert.OperationStateSucceeded {
		t.Fatal(prepared, e)
	}
	descriptor, e := resources.DescribeExpert(selection)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = resources.ResolveExpert(descriptor.Reference); e != nil {
		t.Fatal(e)
	}
	// The ordinary HTTP-facing method has no private reference-release permit.
	if _, e = resources.Uninstall(context.Background(), skills.UninstallInput{OperationID: uuid.NewString(), SkillID: descriptor.InstallationID}); skills.ErrorCodeOf(e) != skills.CodeBusy {
		t.Fatal("ordinary Skill mutation bypassed expert reference authority", e)
	}
	submission := marketSubmission(t, home)
	trigger, _ := feat158Grant(t, s, scope, submission, 1, &descriptor.Reference)
	remove := prepare
	remove.OperationId = market.CanonicalId(uuid.NewString())
	if _, e = s.RemoveExpertResources(context.Background(), remove); ExpertFailureCode(e) != expert.ErrorCodeResourceInUse {
		t.Fatal("registered immutable grant did not retain expert bytes", e)
	}
	received, e := s.SubmitExpert(context.Background(), trigger)
	if e != nil {
		t.Fatal(e)
	}
	feat158Complete(t, s, received)
	if _, e = s.RemoveExpertResources(context.Background(), remove); ExpertFailureCode(e) != expert.ErrorCodeResourceInUse {
		t.Fatal("confirmed binding did not retain expert bytes", e)
	}
	input := expert.ApplyPayload{OperationId: market.CanonicalId(uuid.NewString()), HostInstanceId: s.market.instance, Scope: scope, LocalSessionId: submission.LocalSessionId, AgentSessionId: *received.Receipt.AgentSessionId, NativeThreadId: expert.NativeId(*received.Receipt.NativeThreadId), ExpectedBindingRevision: 1, BindingRevision: 2, Expert: nil, Intent: market.ModelIntent{ProfileId: submission.Intent.ProfileId, ExpectedRevision: 1}, PermissionMode: market.PermissionModeAsk, MarketSnapshot: submission.Snapshot}
	if out, e := s.ApplyExpertBinding(context.Background(), input); e != nil || out.State != expert.OperationStateSucceeded {
		t.Fatal(out, e)
	}
	removed, e := s.RemoveExpertResources(context.Background(), remove)
	if e != nil || removed.State != expert.OperationStateSucceeded {
		t.Fatal(removed, e)
	}
	if _, e = resources.ResolveExpert(descriptor.Reference); e == nil {
		t.Fatal("released resource remained installed")
	}
	replay, e := s.PrepareExpertResources(context.Background(), prepare)
	if e != nil || !reflect.DeepEqual(replay, prepared) {
		t.Fatal("historical prepare receipt was replaced by current state", replay, e)
	}
	if _, e = resources.ResolveExpert(descriptor.Reference); e == nil {
		t.Fatal("reading/replaying original receipt reinstalled removed resources")
	}
	status, e := s.ExpertStatus(context.Background(), expert.PrivateStatusPayload{HostInstanceId: s.market.instance, Scope: scope})
	if e != nil || status.Validate() != nil || status.CatalogRevision != expert.Digest(skills.ReviewedExpertCatalogDigest) || len(status.ResourceOperations) != 1 || status.ResourceOperations[0].OperationId != remove.OperationId {
		t.Fatal(status, e)
	}
	if len(projection.roots) != 0 || projection.writes != 0 {
		t.Fatal("expert lifecycle entered global Skill projection")
	}
	if e = s.store.Close(); e != nil {
		t.Fatal(e)
	}
	reopened, e := OpenStore(home, WithExpertStorage())
	if e != nil {
		t.Fatal(e)
	}
	defer reopened.Close()
	original, e := reopened.readExpertResourceOperation(string(prepare.OperationId))
	if e != nil || original.Receipt.State != expert.OperationStateSucceeded {
		t.Fatal("normal reopen lost original resource receipt", e)
	}
}
