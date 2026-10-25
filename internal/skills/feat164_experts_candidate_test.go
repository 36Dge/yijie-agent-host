package skills

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	expert "github.com/36Dge/yijie-agent-host/internal/contracts/experts"
	"github.com/google/uuid"
)

func TestFEAT164ImmutableExpertResources(t *testing.T) {
	bundle := os.Getenv("YIJIE_FEAT164_EXPERT_BUNDLE")
	if bundle == "" {
		t.Skip("explicit canonical bundle required; FEAT-164 qualification driver rejects skips")
	}
	if err := VerifyExpertCandidateForProfile(bundle, true); err != nil {
		t.Fatal(err)
	}
	if err := VerifyExpertCandidate(bundle); err == nil {
		t.Fatal("default profile accepted a different provenance")
	}
	catalog, err := loadCatalog(bundle)
	if err != nil || len(catalog.manifest.Skills) != 75 {
		t.Fatal(err)
	}
	if catalog.manifest.Source.RevisionKind != "git-commit" {
		t.Fatal("floating resource source")
	}
	runtime := newTestRuntime("ordinary-test")
	managed := filepath.Join(t.TempDir(), "installed")
	cfg := Config{BundleRoot: bundle, ManagedRoot: managed, Runtime: runtime, ExpertCatalogDigest: ExpertCatalogDigestForProfile(true)}
	service, err := NewService(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	resolved := map[expert.ExpertRef]ResolvedExpert{}
	for _, item := range catalog.manifest.Skills {
		if !isExpertResource(item) {
			continue
		}
		state, e := service.Install(context.Background(), InstallInput{OperationID: uuid.NewString(), SkillID: item.ID, ExpectedVersion: item.Version, ExpectedArchiveSHA256: item.Archive.SHA256, CatalogRevision: catalog.revision})
		if e != nil || state.InstallationStatus != "installed" || state.RuntimeVisible {
			t.Fatal(item.ID, e)
		}
		ref := feat158ReferenceFor(t, item)
		r, e := service.ResolveExpert(ref)
		if e != nil || r.Role == "" || r.Index == "" {
			t.Fatal(item.ID, e)
		}
		resolved[ref] = r
	}
	if len(resolved) != 37 || len(runtime.roots) != 0 || len(runtime.enabled) != 0 {
		t.Fatal("expert/global projection differs")
	}
	if err = service.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := NewService(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	for ref, expected := range resolved {
		actual, e := reopened.ResolveExpert(ref)
		if e != nil || !reflect.DeepEqual(expected, actual) {
			t.Fatal("normal reopen changed exact resources", e)
		}
	}
	t.Log("75 unchanged resources;37 exact expert versions installed/resolved/reopened;0 global expert Skills;0 model calls")
}
