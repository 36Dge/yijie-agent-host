package skills

import (
	"context"
	"path/filepath"
	"testing"

	agenthostcontract "github.com/36Dge/yijie-agent-host/internal/contracts"
)

func TestSelectedSkillUsesEnabledVerifiedInstallationAndVersion(t *testing.T) {
	// Reuse only the existing ordinary model-only archive, never attack fixtures.
	bundle, _ := copyPinnedTestBundle(t, "manifest-valid.json", "fixture-model-only-0.1.0.zip")
	catalog, err := loadCatalog(bundle)
	if err != nil {
		t.Fatal(err)
	}
	skill := catalog.manifest.Skills[0]
	service, err := NewService(Config{BundleRoot: bundle, ManagedRoot: filepath.Join(t.TempDir(), "managed"), Runtime: newTestRuntime(skill.RuntimeName)})
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	ctx := context.Background()
	_, err = service.Install(ctx, InstallInput{OperationID: "019fbd88-cbc3-7bf1-934d-7b05cd693f90", SkillID: skill.ID, ExpectedVersion: skill.Version, ExpectedArchiveSHA256: skill.Archive.SHA256, CatalogRevision: catalog.revision})
	if err != nil {
		t.Fatal(err)
	}
	refs := []agenthostcontract.SelectedSkillRef{{Id: skill.ID, Version: skill.Version}}
	inputs, err := service.ResolveSelectedSkills(ctx, refs)
	if err != nil || len(inputs) != 1 {
		t.Fatalf("verified selected input count=%d error=%v", len(inputs), err)
	}
	_, err = service.ResolveSelectedSkills(ctx, []agenthostcontract.SelectedSkillRef{{Id: skill.ID, Version: "9.0.0"}})
	assertCode(t, err, CodeCapabilityUnavailable)
	_, err = service.SetEnabled(ctx, EnabledInput{OperationID: "019fbd88-cbc3-7bf1-934d-7b05cd693f91", SkillID: skill.ID, Enabled: false})
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.ResolveSelectedSkills(ctx, refs)
	assertCode(t, err, CodeCapabilityUnavailable)
}
