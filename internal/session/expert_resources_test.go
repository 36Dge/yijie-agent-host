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
	selections := []expert.ExpertSelection{{ExpertId: "jd-ops-expert", Version: "0.1.3"}}
	switch os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") {
	case "":
	case "first-three", "first-three-repair", "second-group", "third-group", "fourth-group", "fifth-group", "sixth-group", "seventh-group", "thirteen-first", "thirteen-second", "thirteen-second-repair", "thirteen-third", "thirteen-fourth", "thirteen-fifth", "thirteen-sixth", "thirteen-sixth-repair":
		selections = append(selections,
			expert.ExpertSelection{ExpertId: "xiaohongshu-copywriter", Version: "0.1.0"},
			expert.ExpertSelection{ExpertId: "agentgrid-comic-storyboard-director", Version: "0.1.0"},
			expert.ExpertSelection{ExpertId: "cross-border-ecommerce-operations-expert", Version: "0.1.0"},
		)
	default:
		t.Fatal("unsupported actual candidate expectation")
	}
	if value := os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG"); value == "first-three-repair" || value == "second-group" || value == "third-group" || value == "fourth-group" || value == "fifth-group" || value == "sixth-group" || (value == "seventh-group" || (value == "thirteen-first" || (value == "thirteen-second" || (value == "thirteen-second-repair" || (value == "thirteen-third" || (value == "thirteen-fourth" || (value == "thirteen-fifth" || (value == "thirteen-sixth" || value == "thirteen-sixth-repair")))))))) {
		selections = append(selections, expert.ExpertSelection{ExpertId: "xiaohongshu-copywriter", Version: "0.1.1"}, expert.ExpertSelection{ExpertId: "cross-border-ecommerce-operations-expert", Version: "0.1.1"})
	}
	if value := os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG"); value == "second-group" || value == "third-group" || value == "fourth-group" || value == "fifth-group" || value == "sixth-group" || (value == "seventh-group" || (value == "thirteen-first" || (value == "thirteen-second" || (value == "thirteen-second-repair" || (value == "thirteen-third" || (value == "thirteen-fourth" || (value == "thirteen-fifth" || (value == "thirteen-sixth" || value == "thirteen-sixth-repair")))))))) {
		selections = append(selections, expert.ExpertSelection{ExpertId: "tradebuddy-baojia", Version: "0.1.0"}, expert.ExpertSelection{ExpertId: "report-viz-expert", Version: "0.1.0"}, expert.ExpertSelection{ExpertId: "report-viz-expert", Version: "0.1.1"})
	}
	if os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "third-group" || os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "fourth-group" || os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "fifth-group" || os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "sixth-group" || (os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "seventh-group" || (os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "thirteen-first" || (os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "thirteen-second" || (os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "thirteen-second-repair" || (os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "thirteen-third" || (os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "thirteen-fourth" || (os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "thirteen-fifth" || (os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "thirteen-sixth" || os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "thirteen-sixth-repair")))))))) {
		selections = append(selections, expert.ExpertSelection{ExpertId: "chuhaijiang-tiktok-shop-operations-expert", Version: "0.1.0"})
	}
	if os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "fourth-group" || os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "fifth-group" || os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "sixth-group" || (os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "seventh-group" || (os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "thirteen-first" || (os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "thirteen-second" || (os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "thirteen-second-repair" || (os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "thirteen-third" || (os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "thirteen-fourth" || (os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "thirteen-fifth" || (os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "thirteen-sixth" || os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "thirteen-sixth-repair")))))))) {
		selections = append(selections, expert.ExpertSelection{ExpertId: "linkfox-expert-aigc-imagegen-mannequin-to-model", Version: "0.1.0"})
	}
	if os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "fifth-group" || os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "sixth-group" || (os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "seventh-group" || (os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "thirteen-first" || (os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "thirteen-second" || (os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "thirteen-second-repair" || (os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "thirteen-third" || (os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "thirteen-fourth" || (os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "thirteen-fifth" || (os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "thirteen-sixth" || os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "thirteen-sixth-repair")))))))) {
		selections = append(selections, expert.ExpertSelection{ExpertId: "linkfox-expert-aigc-videogen-viral-replicate", Version: "0.1.0"})
	}
	if os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "sixth-group" || (os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "seventh-group" || (os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "thirteen-first" || (os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "thirteen-second" || (os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "thirteen-second-repair" || (os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "thirteen-third" || (os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "thirteen-fourth" || (os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "thirteen-fifth" || (os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "thirteen-sixth" || os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "thirteen-sixth-repair")))))))) {
		selections = append(selections, expert.ExpertSelection{ExpertId: "linkfox-expert-1688-sourcing-expert", Version: "0.1.0"})
	}
	if os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "seventh-group" || (os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "thirteen-first" || (os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "thirteen-second" || (os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "thirteen-second-repair" || (os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "thirteen-third" || (os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "thirteen-fourth" || (os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "thirteen-fifth" || (os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "thirteen-sixth" || os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "thirteen-sixth-repair"))))))) {
		selections = append(selections, expert.ExpertSelection{ExpertId: "market-researcher", Version: "0.1.0"})
	}
	if os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "thirteen-first" || (os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "thirteen-second" || (os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "thirteen-second-repair" || (os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "thirteen-third" || (os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "thirteen-fourth" || (os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "thirteen-fifth" || (os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "thirteen-sixth" || os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "thirteen-sixth-repair")))))) {
		selections = append(selections, expert.ExpertSelection{ExpertId: "product-management", Version: "0.1.0"}, expert.ExpertSelection{ExpertId: "novel-creator", Version: "0.1.0"})
	}
	if os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "thirteen-second" || (os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "thirteen-second-repair" || (os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "thirteen-third" || (os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "thirteen-fourth" || (os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "thirteen-fifth" || (os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "thirteen-sixth" || os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "thirteen-sixth-repair"))))) {
		selections = append(selections, expert.ExpertSelection{ExpertId: "tb-tm-ops-expert", Version: "0.1.0"}, expert.ExpertSelection{ExpertId: "pdd-ops-expert", Version: "0.1.0"})
	}
	if os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "thirteen-second-repair" || (os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "thirteen-third" || (os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "thirteen-fourth" || (os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "thirteen-fifth" || (os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "thirteen-sixth" || os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "thirteen-sixth-repair")))) {
		selections = append(selections, expert.ExpertSelection{ExpertId: "tb-tm-ops-expert", Version: "0.1.1"}, expert.ExpertSelection{ExpertId: "pdd-ops-expert", Version: "0.1.1"})
	}
	if os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "thirteen-third" || (os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "thirteen-fourth" || (os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "thirteen-fifth" || (os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "thirteen-sixth" || os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "thirteen-sixth-repair"))) {
		selections = append(selections, expert.ExpertSelection{ExpertId: "data-analyst-pro", Version: "0.1.0"}, expert.ExpertSelection{ExpertId: "data-analyst-pro", Version: "0.1.1"}, expert.ExpertSelection{ExpertId: "data-analyst-pro", Version: "0.1.2"}, expert.ExpertSelection{ExpertId: "ui-designer", Version: "0.1.0"}, expert.ExpertSelection{ExpertId: "ui-designer", Version: "0.1.1"})
	}
	if os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "thirteen-fourth" || (os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "thirteen-fifth" || (os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "thirteen-sixth" || os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "thirteen-sixth-repair")) {
		selections = append(selections, expert.ExpertSelection{ExpertId: "linkfox-expert-aigc-videogen-sale", Version: "0.1.0"})
	}
	if os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "thirteen-fifth" || (os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "thirteen-sixth" || os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "thirteen-sixth-repair") {
		selections = append(selections, expert.ExpertSelection{ExpertId: "equity-research", Version: "0.1.0"}, expert.ExpertSelection{ExpertId: "equity-research", Version: "0.1.1"}, expert.ExpertSelection{ExpertId: "equity-research", Version: "0.1.2"}, expert.ExpertSelection{ExpertId: "strategy-backtest-expert", Version: "0.1.0"}, expert.ExpertSelection{ExpertId: "strategy-backtest-expert", Version: "0.1.1"})
	}
	if os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "thirteen-sixth" || os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "thirteen-sixth-repair" {
		selections = append(selections, expert.ExpertSelection{ExpertId: "moss-industry-research", Version: "0.1.0"})
	}
	if os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "thirteen-sixth-repair" {
		selections = append(selections, expert.ExpertSelection{ExpertId: "moss-industry-research", Version: "0.1.1"})
	}
	if os.Getenv("YIJIE_EXPERT_TEST_OPS_RESPONSIVE_REPAIR") == "true" {
		if os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") != "thirteen-sixth-repair" {
			t.Fatal("responsive repair requires the complete prior expert catalog")
		}
		selections = append(selections, expert.ExpertSelection{ExpertId: "tb-tm-ops-expert", Version: "0.1.2"}, expert.ExpertSelection{ExpertId: "pdd-ops-expert", Version: "0.1.2"})
	}
	if os.Getenv("YIJIE_EXPERT_TEST_OPS_LABEL_REPAIR") == "true" {
		if os.Getenv("YIJIE_EXPERT_TEST_OPS_RESPONSIVE_REPAIR") != "true" {
			t.Fatal("label repair requires all prior responsive versions")
		}
		selections = append(selections, expert.ExpertSelection{ExpertId: "tb-tm-ops-expert", Version: "0.1.3"}, expert.ExpertSelection{ExpertId: "pdd-ops-expert", Version: "0.1.3"})
	}
	for _, selection := range selections {
		t.Run(string(selection.ExpertId), func(t *testing.T) {
			feat158ActualResourceLifecycle(t, bundle, selection)
		})
	}
}

func feat158ActualResourceLifecycle(t *testing.T, bundle string, selection expert.ExpertSelection) {
	t.Helper()
	s, _, _, scope, home := feat158Session(t)
	projection := &feat158ResourceProjection{}
	managed := filepath.Join(t.TempDir(), "installed")
	resources, e := skills.NewService(skills.Config{BundleRoot: bundle, ManagedRoot: managed, Runtime: projection, ExpertCatalogDigest: skills.ReviewedExpertCatalogDigest})
	if e != nil {
		t.Fatal(e)
	}
	defer resources.Close()
	s.expertResources = resources
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

// Normal upgrade with an existing live binding. Both versions use the same
// installer and exact-reference resolver; no old files or records are replaced.
func TestFEAT158ActualVersionCoexistencePreservesBoundExpert(t *testing.T) {
	bundle := os.Getenv("YIJIE_EXPERT_TEST_BUNDLE")
	if bundle == "" || (os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") != "first-three-repair" && os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") != "second-group" && os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") != "third-group" && os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") != "fourth-group" && os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") != "fifth-group" && os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") != "sixth-group" && (os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") != "seventh-group" && (os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") != "thirteen-first" && (os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") != "thirteen-second" && (os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") != "thirteen-second-repair" && os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") != "thirteen-third" && (os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") != "thirteen-fourth" && (os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") != "thirteen-fifth" && (os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") != "thirteen-sixth" && os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") != "thirteen-sixth-repair")))))))) {
		t.Skip("reviewed repair candidate not supplied")
	}
	ids := []expert.ExpertId{"xiaohongshu-copywriter", "cross-border-ecommerce-operations-expert"}
	if value := os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG"); value == "second-group" || value == "third-group" || value == "fourth-group" || value == "fifth-group" || value == "sixth-group" || (value == "seventh-group" || (value == "thirteen-first" || (value == "thirteen-second" || (value == "thirteen-second-repair" || (value == "thirteen-third" || (value == "thirteen-fourth" || (value == "thirteen-fifth" || (value == "thirteen-sixth" || value == "thirteen-sixth-repair")))))))) {
		ids = append(ids, "report-viz-expert")
	}
	for _, id := range ids {
		t.Run(string(id), func(t *testing.T) {
			s, _, _, scope, home := feat158Session(t)
			resources, err := skills.NewService(skills.Config{BundleRoot: bundle, ManagedRoot: filepath.Join(t.TempDir(), "installed"), Runtime: &feat158ResourceProjection{}, ExpertCatalogDigest: skills.ReviewedExpertCatalogDigest})
			if err != nil {
				t.Fatal(err)
			}
			defer resources.Close()
			s.expertResources = resources
			prepare := expert.PrivateResourcePayload{HostInstanceId: s.market.instance, Scope: scope, OperationId: market.CanonicalId(uuid.NewString()), Expert: expert.ExpertSelection{ExpertId: id, Version: "0.1.0"}}
			if _, err = s.PrepareExpertResources(context.Background(), prepare); err != nil {
				t.Fatal(err)
			}
			old, err := resources.DescribeExpert(prepare.Expert)
			if err != nil {
				t.Fatal(err)
			}
			before, err := resources.ResolveExpert(old.Reference)
			if err != nil {
				t.Fatal(err)
			}
			submission := marketSubmission(t, home)
			trigger, _ := feat158Grant(t, s, scope, submission, 1, &old.Reference)
			received, err := s.SubmitExpert(context.Background(), trigger)
			if err != nil {
				t.Fatal(err)
			}
			feat158Complete(t, s, received)
			prepare.OperationId = market.CanonicalId(uuid.NewString())
			prepare.Expert.Version = "0.1.1"
			if out, err := s.PrepareExpertResources(context.Background(), prepare); err != nil || out.State != expert.OperationStateSucceeded {
				t.Fatal(out, err)
			}
			updated, err := resources.DescribeExpert(prepare.Expert)
			if err != nil || updated.InstallationID == old.InstallationID {
				t.Fatal("versions share an installation", err)
			}
			if _, err := resources.ResolveExpert(updated.Reference); err != nil {
				t.Fatal(err)
			}
			after, err := resources.ResolveExpert(old.Reference)
			if err != nil || !reflect.DeepEqual(before, after) {
				t.Fatal("retained exact resource changed", err)
			}
			prepare.OperationId = market.CanonicalId(uuid.NewString())
			prepare.Expert.Version = "0.1.0"
			if _, err := s.RemoveExpertResources(context.Background(), prepare); ExpertFailureCode(err) != expert.ErrorCodeResourceInUse {
				t.Fatal("old binding lost removal protection", err)
			}
		})
	}
}
