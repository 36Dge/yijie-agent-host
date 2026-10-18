package skills

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	expertwire "github.com/36Dge/yijie-agent-host/internal/contracts/experts"
	"github.com/google/uuid"
)

const feat158Role = "Organize benign reference notes and identify missing evidence."
const feat158Reference = "A synthetic, non-sensitive reference. Review the source version before answering."

// This fixture uses only ordinary text files and the existing safe packaging
// helper. It creates no attacks, permission faults, executable or subprocess.
func feat158ExpertBundle(t *testing.T) (string, testManifestProjection) {
	t.Helper()
	root, _ := makeTestBundle(t, []testZipEntry{
		{name: "SKILL.md", body: "---\nname: fixture-model-only\ndescription: Benign reference resources.\n---\nManaged reference entry.\n"},
		{name: "role.md", body: feat158Role},
		{name: "references/notes.md", body: feat158Reference},
	}, "0.1.0")
	raw, err := os.ReadFile(filepath.Join(root, manifestFileName))
	if err != nil {
		t.Fatal(err)
	}
	var document map[string]any
	if err := json.Unmarshal(raw, &document); err != nil {
		t.Fatal(err)
	}
	document["schema_version"] = 3
	document["distribution_channel"] = "local-development"
	skill := document["skills"].([]any)[0].(map[string]any)
	skill["asset_kind"] = "expert_resources"
	skill["license"].(map[string]any)["redistribution_status"] = "unverified"
	skill["license"].(map[string]any)["authorization_scope"] = "local-development"
	skill["capabilities"] = map[string]any{"execution_mode": "tool-assisted", "network": "none", "filesystem": "read", "required_tools": []any{}}
	roleDigest, knowledgeDigest := sha256.Sum256([]byte(feat158Role)), sha256.Sum256([]byte(feat158Reference))
	skill["expert"] = map[string]any{
		"expert_id": "fixture-reference-expert", "source_id": "yijie.fixture", "platform": "reference",
		"subtitle": "Benign reference qualification", "tags": []string{"reference"}, "quick_prompts": []string{"Summarize the reference."},
		"knowledge_summary": "One ordinary synthetic reference.", "runtime_projection": "expert_only",
		"local_use_authorization": map[string]any{"status": "local-use-authorized", "evidence_reference": "docs/fixture-local-use.md"},
		"role_resource":           map[string]any{"path": "role.md", "sha256": hex.EncodeToString(roleDigest[:]), "bytes": len(feat158Role)},
		"knowledge_resources": []any{map[string]any{"id": "notes", "title": "Reference notes", "path": "references/notes.md",
			"sha256": hex.EncodeToString(knowledgeDigest[:]), "bytes": len(feat158Reference), "content_version": "0.1.0", "content_status": "reviewed-reference"}},
	}
	raw, err = json.Marshal(document)
	if err != nil || os.WriteFile(filepath.Join(root, manifestFileName), raw, 0o600) != nil {
		t.Fatal("write ordinary local expert manifest")
	}
	catalog, err := loadCatalog(root)
	if err != nil {
		t.Fatal(err)
	}
	return root, testManifestProjection{revision: catalog.revision, Skills: catalog.manifest.Skills}
}

func feat158ReferenceFor(t *testing.T, skill ManifestSkill) expertwire.ExpertRef {
	t.Helper()
	digest, err := expertwire.KnowledgeManifestDigest(skill.Expert.KnowledgeResources)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(map[string]any{
		"expertId": skill.Expert.ExpertId, "version": skill.Version, "archiveSha256": skill.Archive.SHA256,
		"roleSha256": skill.Expert.RoleResource.Sha256, "knowledgeManifestSha256": digest,
	})
	if err != nil {
		t.Fatal(err)
	}
	var reference expertwire.ExpertRef
	if err := json.Unmarshal(raw, &reference); err != nil {
		t.Fatal(err)
	}
	return reference
}

func TestFEAT158ExpertResourcesUseManagedInstallWithoutGlobalProjection(t *testing.T) {
	bundle, manifest := feat158ExpertBundle(t)
	runtime := newTestRuntime(manifest.Skills[0].RuntimeName)
	managed := filepath.Join(t.TempDir(), "installed")
	service, err := NewService(Config{BundleRoot: bundle, ManagedRoot: managed, Runtime: runtime, ExpertCatalogDigest: manifest.revision})
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	state, err := service.Install(context.Background(), installInput(manifest, uuid.NewString()))
	if err != nil || state.InstallationStatus != "installed" || state.RuntimeVisible {
		t.Fatalf("expert managed install: state=%+v err=%v", state, err)
	}
	if len(runtime.roots) != 0 || len(runtime.enabled) != 0 {
		t.Fatal("expert resources entered global Runtime Skill projection")
	}
	reference := feat158ReferenceFor(t, manifest.Skills[0])
	resolved, err := service.ResolveExpert(reference)
	if err != nil || resolved.Role != feat158Role || !strings.Contains(resolved.Index, "Reference notes") ||
		!strings.Contains(resolved.Index, "references/notes.md") || strings.Contains(resolved.Index, feat158Reference) {
		t.Fatal("expert resolution did not produce exact role plus index without knowledge body")
	}
	if err := service.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := NewService(Config{BundleRoot: bundle, ManagedRoot: managed, Runtime: runtime, ExpertCatalogDigest: manifest.revision})
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	again, err := reopened.ResolveExpert(reference)
	if err != nil || again != resolved {
		t.Fatal("normal managed service reopen changed immutable expert resources")
	}
}

func TestFEAT158ExpertRemovalWaitsForReferenceAuthority(t *testing.T) {
	bundle, manifest := feat158ExpertBundle(t)
	service, err := NewService(Config{BundleRoot: bundle, ManagedRoot: filepath.Join(t.TempDir(), "installed"), Runtime: newTestRuntime(manifest.Skills[0].RuntimeName), ExpertCatalogDigest: manifest.revision})
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	if _, err := service.Install(context.Background(), installInput(manifest, uuid.NewString())); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Uninstall(context.Background(), UninstallInput{OperationID: uuid.NewString(), SkillID: manifest.Skills[0].ID}); ErrorCodeOf(err) != CodeBusy {
		t.Fatal("expert removal bypassed the not-yet-attached reference authority")
	}
	if _, err := service.ResolveExpert(feat158ReferenceFor(t, manifest.Skills[0])); err != nil {
		t.Fatal("blocked removal changed the referenced resources")
	}
}

func TestFEAT158UnstartedRejectionJournalFinishesAfterNormalReopen(t *testing.T) {
	bundle, manifest := feat158ExpertBundle(t)
	runtime := newTestRuntime(manifest.Skills[0].RuntimeName)
	managed := filepath.Join(t.TempDir(), "installed")
	config := Config{BundleRoot: bundle, ManagedRoot: managed, Runtime: runtime, ExpertCatalogDigest: manifest.revision}
	service, e := NewService(config)
	if e != nil {
		t.Fatal(e)
	}
	id := uuid.NewString()
	fingerprint := operationFingerprint("expert-unstarted-rejection", id, manifest.Skills[0].ID, "prepare")
	// The original core journal reservation is a normal storage phase. Close
	// normally before recording its explicit rejection; no resource was touched.
	if _, owner, e := service.reserveOperation(id, fingerprint, "install", manifest.Skills[0].ID); e != nil || !owner {
		t.Fatal(owner, e)
	}
	if e = service.Close(); e != nil {
		t.Fatal(e)
	}
	reopened, e := NewService(config)
	if e != nil {
		t.Fatal(e)
	}
	defer reopened.Close()
	read, e := reopened.ReadExpertResourceOperation(id, manifest.Skills[0].ID, "prepare")
	if e != nil || !read.Found || read.Complete || read.InProgress {
		t.Fatal(read, e)
	}
	settled, e := reopened.CancelUnstartedExpertResource(id, manifest.Skills[0].ID, "prepare")
	if e != nil || !settled.Complete || settled.ErrorCode != CodeOperationConflict {
		t.Fatal(settled, e)
	}
	if _, e = reopened.Install(context.Background(), installInput(manifest, id)); ErrorCodeOf(e) != CodeOperationConflict {
		t.Fatal("late original installed a rejected operation", e)
	}
	if _, e = reopened.ResolveExpert(feat158ReferenceFor(t, manifest.Skills[0])); e == nil {
		t.Fatal("rejection installed a resource")
	}
	if len(runtime.roots) != 0 || len(runtime.enabled) != 0 {
		t.Fatal("recovery changed global projection")
	}
}

func TestFEAT158CoreReservedRecoveryUsesNormalRollbackEvidence(t *testing.T) {
	for _, action := range []string{"prepare", "remove"} {
		t.Run(action, func(t *testing.T) {
			bundle, manifest := feat158ExpertBundle(t)
			runtime := newTestRuntime(manifest.Skills[0].RuntimeName)
			config := Config{BundleRoot: bundle, ManagedRoot: filepath.Join(t.TempDir(), "installed"), Runtime: runtime, ExpertCatalogDigest: manifest.revision}
			service, e := NewService(config)
			if e != nil {
				t.Fatal(e)
			}
			id, kind := uuid.NewString(), "install"
			input := installInput(manifest, id)
			fingerprint := operationFingerprint("install", input.OperationID, input.SkillID, input.ExpectedVersion, input.ExpectedArchiveSHA256, input.CatalogRevision)
			if action == "remove" {
				kind = "uninstall"
				fingerprint = operationFingerprint("uninstall", id, input.SkillID)
			}
			record, owner, e := service.reserveOperation(id, fingerprint, kind, input.SkillID)
			if e != nil || !owner {
				t.Fatal(owner, e)
			}
			stage := ""
			if action == "prepare" {
				// Save the normal pre-rename installation phase using the real
				// benign archive extractor, then close normally. Startup must use
				// its existing rollback before explicit rejection is eligible.
				stage = transactionName(".yijie-staging", "", id)
				if _, e = extractArchive(service.bundle, service.managed, stage, manifest.Skills[0], manifest.revision); e != nil {
					t.Fatal(e)
				}
				if e = service.updateOperationPhase(record, "committing", false, false); e != nil {
					t.Fatal(e)
				}
			}
			active, e := service.CancelUnstartedExpertResource(id, input.SkillID, action)
			if e != nil || !active.InProgress || active.Complete {
				t.Fatal("active owner was overwritten", active, e)
			}
			if e = service.Close(); e != nil {
				t.Fatal(e)
			}
			reopened, e := NewService(config)
			if e != nil {
				t.Fatal(e)
			}
			defer reopened.Close()
			if stage != "" {
				if _, e = reopened.managed.Lstat(stage); !os.IsNotExist(e) {
					t.Fatal("canonical rollback retained original staging", e)
				}
			}
			before, e := reopened.ReadExpertResourceOperation(id, input.SkillID, action)
			if e != nil || before.InProgress || before.Complete {
				t.Fatal(before, e)
			}
			result, e := reopened.CancelUnstartedExpertResource(id, input.SkillID, action)
			if e != nil || !result.Complete || result.ErrorCode != CodeOperationConflict {
				t.Fatal(result, e)
			}
			reopened.opMu.Lock()
			same := reopened.operations[id].Fingerprint == fingerprint
			reopened.opMu.Unlock()
			if !same {
				t.Fatal("explicit closing changed original fingerprint")
			}
			if action == "prepare" {
				_, e = reopened.Install(context.Background(), input)
			} else {
				_, e = reopened.RemoveExpertResources(context.Background(), id, expertwire.ExpertSelection{ExpertId: expertwire.ExpertId(manifest.Skills[0].Expert.ExpertId), Version: expertwire.Version(manifest.Skills[0].Version)})
			}
			if ErrorCodeOf(e) != CodeOperationConflict {
				t.Fatal("closed original mutation ran again", e)
			}
			if len(runtime.roots) != 0 || len(runtime.enabled) != 0 {
				t.Fatal("recovery projected resources")
			}
		})
	}
}

func TestFEAT158OrdinarySkillProjectionRemainsUnchanged(t *testing.T) {
	bundle, manifest := makeTestBundle(t, []testZipEntry{{name: "SKILL.md", body: "---\nname: fixture-model-only\ndescription: Ordinary fixture.\n---\nSummarize notes.\n"}}, "0.1.0")
	runtime := newTestRuntime(manifest.Skills[0].RuntimeName)
	service, err := NewService(Config{BundleRoot: bundle, ManagedRoot: filepath.Join(t.TempDir(), "installed"), Runtime: runtime})
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	state, err := service.Install(context.Background(), installInput(manifest, uuid.NewString()))
	if err != nil || !state.RuntimeVisible || len(runtime.roots) != 1 {
		t.Fatal("ordinary Skill projection changed")
	}
}

// Opt-in actual content qualification; installs only into a fresh test directory.
func TestFEAT158ActualCandidateManagedResources(t *testing.T) {
	bundle, authority := os.Getenv("YIJIE_EXPERT_TEST_BUNDLE"), os.Getenv("YIJIE_EXPERT_TEST_MANIFEST_SHA256")
	if bundle == "" || authority == "" {
		t.Skip("actual candidate path and reviewed manifest pin not supplied")
	}
	if authority != ReviewedExpertCatalogDigest || VerifyExpertCandidate(bundle) != nil {
		t.Fatal("actual candidate differs from the compiled reviewed authority")
	}
	catalog, err := loadCatalog(bundle)
	if err != nil {
		t.Fatal(err)
	}
	expected := map[string]string{"jd-ops-expert": "0.1.3"}
	switch os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") {
	case "":
	case "first-three", "first-three-repair", "second-group", "third-group", "fourth-group", "fifth-group", "sixth-group", "seventh-group", "thirteen-first", "thirteen-second", "thirteen-second-repair", "thirteen-third", "thirteen-fourth", "thirteen-fifth", "thirteen-sixth", "thirteen-sixth-repair":
		expected["xiaohongshu-copywriter"] = "0.1.0"
		expected["agentgrid-comic-storyboard-director"] = "0.1.0"
		expected["cross-border-ecommerce-operations-expert"] = "0.1.0"
	default:
		t.Fatal("unsupported actual candidate expectation")
	}
	if value := os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG"); value == "first-three-repair" || value == "second-group" || value == "third-group" || value == "fourth-group" || value == "fifth-group" || value == "sixth-group" || (value == "seventh-group" || (value == "thirteen-first" || (value == "thirteen-second" || (value == "thirteen-second-repair" || (value == "thirteen-third" || (value == "thirteen-fourth" || (value == "thirteen-fifth" || (value == "thirteen-sixth" || value == "thirteen-sixth-repair")))))))) {
		expected["xiaohongshu-copywriter@0.1.1"] = "0.1.1"
		expected["cross-border-ecommerce-operations-expert@0.1.1"] = "0.1.1"
	}
	if value := os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG"); value == "second-group" || value == "third-group" || value == "fourth-group" || value == "fifth-group" || value == "sixth-group" || (value == "seventh-group" || (value == "thirteen-first" || (value == "thirteen-second" || (value == "thirteen-second-repair" || (value == "thirteen-third" || (value == "thirteen-fourth" || (value == "thirteen-fifth" || (value == "thirteen-sixth" || value == "thirteen-sixth-repair")))))))) {
		expected["tradebuddy-baojia"] = "0.1.0"
		expected["report-viz-expert"] = "0.1.0"
		expected["report-viz-expert@0.1.1"] = "0.1.1"
	}
	if os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "third-group" || os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "fourth-group" || os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "fifth-group" || os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "sixth-group" || (os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "seventh-group" || (os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "thirteen-first" || (os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "thirteen-second" || (os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "thirteen-second-repair" || (os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "thirteen-third" || (os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "thirteen-fourth" || (os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "thirteen-fifth" || (os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "thirteen-sixth" || os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "thirteen-sixth-repair")))))))) {
		expected["chuhaijiang-tiktok-shop-operations-expert"] = "0.1.0"
	}
	if os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "fourth-group" || os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "fifth-group" || os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "sixth-group" || (os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "seventh-group" || (os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "thirteen-first" || (os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "thirteen-second" || (os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "thirteen-second-repair" || (os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "thirteen-third" || (os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "thirteen-fourth" || (os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "thirteen-fifth" || (os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "thirteen-sixth" || os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "thirteen-sixth-repair")))))))) {
		expected["linkfox-expert-aigc-imagegen-mannequin-to-model"] = "0.1.0"
	}
	if os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "fifth-group" || os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "sixth-group" || (os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "seventh-group" || (os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "thirteen-first" || (os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "thirteen-second" || (os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "thirteen-second-repair" || (os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "thirteen-third" || (os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "thirteen-fourth" || (os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "thirteen-fifth" || (os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "thirteen-sixth" || os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "thirteen-sixth-repair")))))))) {
		expected["linkfox-expert-aigc-videogen-viral-replicate"] = "0.1.0"
	}
	if os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "sixth-group" || (os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "seventh-group" || (os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "thirteen-first" || (os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "thirteen-second" || (os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "thirteen-second-repair" || (os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "thirteen-third" || (os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "thirteen-fourth" || (os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "thirteen-fifth" || (os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "thirteen-sixth" || os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "thirteen-sixth-repair")))))))) {
		expected["linkfox-expert-1688-sourcing-expert"] = "0.1.0"
	}
	if os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "seventh-group" || (os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "thirteen-first" || (os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "thirteen-second" || (os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "thirteen-second-repair" || (os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "thirteen-third" || (os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "thirteen-fourth" || (os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "thirteen-fifth" || (os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "thirteen-sixth" || os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "thirteen-sixth-repair"))))))) {
		expected["market-researcher"] = "0.1.0"
	}
	if os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "thirteen-first" || (os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "thirteen-second" || (os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "thirteen-second-repair" || (os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "thirteen-third" || (os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "thirteen-fourth" || (os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "thirteen-fifth" || (os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "thirteen-sixth" || os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "thirteen-sixth-repair")))))) {
		expected["product-management"] = "0.1.0"
		expected["novel-creator"] = "0.1.0"
	}
	if os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "thirteen-second" || (os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "thirteen-second-repair" || (os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "thirteen-third" || (os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "thirteen-fourth" || (os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "thirteen-fifth" || (os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "thirteen-sixth" || os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "thirteen-sixth-repair"))))) {
		expected["tb-tm-ops-expert"] = "0.1.0"
		expected["pdd-ops-expert"] = "0.1.0"
	}
	if os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "thirteen-second-repair" || (os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "thirteen-third" || (os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "thirteen-fourth" || (os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "thirteen-fifth" || (os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "thirteen-sixth" || os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "thirteen-sixth-repair")))) {
		expected["tb-tm-ops-expert@0.1.1"] = "0.1.1"
		expected["pdd-ops-expert@0.1.1"] = "0.1.1"
	}
	if os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "thirteen-third" || (os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "thirteen-fourth" || (os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "thirteen-fifth" || (os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "thirteen-sixth" || os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "thirteen-sixth-repair"))) {
		expected["data-analyst-pro"] = "0.1.0"
		expected["data-analyst-pro@0.1.1"] = "0.1.1"
		expected["data-analyst-pro@0.1.2"] = "0.1.2"
		expected["ui-designer@0.1.1"] = "0.1.1"
		expected["ui-designer"] = "0.1.0"
	}
	if os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "thirteen-fourth" || (os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "thirteen-fifth" || (os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "thirteen-sixth" || os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "thirteen-sixth-repair")) {
		expected["linkfox-expert-aigc-videogen-sale"] = "0.1.0"
	}
	if os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "thirteen-fifth" || (os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "thirteen-sixth" || os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "thirteen-sixth-repair") {
		expected["equity-research"] = "0.1.0"
		expected["equity-research@0.1.1"] = "0.1.1"
		expected["equity-research@0.1.2"] = "0.1.2"
		expected["strategy-backtest-expert"] = "0.1.0"
		expected["strategy-backtest-expert@0.1.1"] = "0.1.1"
	}
	if os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "thirteen-sixth" || os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "thirteen-sixth-repair" {
		expected["moss-industry-research"] = "0.1.0"
	}
	if os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "thirteen-sixth-repair" {
		expected["moss-industry-research@0.1.1"] = "0.1.1"
	}
	if os.Getenv("YIJIE_EXPERT_TEST_OPS_RESPONSIVE_REPAIR") == "true" {
		if os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") != "thirteen-sixth-repair" {
			t.Fatal("responsive repair requires the complete prior expert catalog")
		}
		expected["tb-tm-ops-expert@0.1.2"] = "0.1.2"
		expected["pdd-ops-expert@0.1.2"] = "0.1.2"
	}
	if os.Getenv("YIJIE_EXPERT_TEST_OPS_LABEL_REPAIR") == "true" {
		if os.Getenv("YIJIE_EXPERT_TEST_OPS_RESPONSIVE_REPAIR") != "true" {
			t.Fatal("label repair requires all prior responsive resource versions")
		}
		expected["tb-tm-ops-expert@0.1.3"] = "0.1.3"
		expected["pdd-ops-expert@0.1.3"] = "0.1.3"
	}
	var chosen []ManifestSkill
	seen := make(map[string]bool)
	for _, entry := range catalog.manifest.Skills {
		if isExpertResource(entry) {
			id := entry.Expert.ExpertId
			if entry.Version == "0.1.1" || entry.Version == "0.1.2" || (entry.Version == "0.1.3" && (id == "tb-tm-ops-expert" || id == "pdd-ops-expert")) {
				id += "@" + entry.Version
			}
			if expected[id] != entry.Version || seen[id] {
				t.Fatalf("unexpected actual expert identity %s@%s", id, entry.Version)
			}
			seen[id] = true
			chosen = append(chosen, entry)
		}
	}
	if len(chosen) != len(expected) || len(catalog.manifest.Skills) != 38+len(expected) {
		t.Fatal("candidate differs from the reviewed 38 ordinary Skills and exact expert catalog")
	}
	for _, chosen := range chosen {
		t.Run(chosen.Expert.ExpertId, func(t *testing.T) {
			runtime := newTestRuntime(chosen.RuntimeName)
			service, err := NewService(Config{BundleRoot: bundle, ManagedRoot: filepath.Join(t.TempDir(), "installed"), Runtime: runtime, ExpertCatalogDigest: authority})
			if err != nil {
				t.Fatal(err)
			}
			defer service.Close()
			manifest := testManifestProjection{revision: catalog.revision, Skills: []ManifestSkill{chosen}}
			state, err := service.Install(context.Background(), installInput(manifest, uuid.NewString()))
			if err != nil || state.RuntimeVisible {
				t.Fatal(state, err)
			}
			resolved, err := service.ResolveExpert(feat158ReferenceFor(t, chosen))
			if err != nil || len(resolved.Role) == 0 || len(resolved.Index) == 0 {
				t.Fatal("actual managed candidate resource qualification", err)
			}
			if len(runtime.roots) != 0 || len(runtime.enabled) != 0 {
				t.Fatal("actual expert resource entered global Skill projection")
			}
			t.Logf("verified actual candidate %s %s: role=%d bytes; short index=%d bytes; no runtime global projection", chosen.Expert.ExpertId, chosen.Version, len(resolved.Role), len(resolved.Index))
		})
	}
}

// Install the unchanged original expert from its old reviewed catalog, close
// normally, then reopen the same test-managed root with the expanded catalog.
// No daily installation, receipt, database or source package is modified.
func TestFEAT158ActualExpandedCatalogPreservesOriginalInstallation(t *testing.T) {
	oldBundle, newBundle := os.Getenv("YIJIE_EXPERT_TEST_PREVIOUS_BUNDLE"), os.Getenv("YIJIE_EXPERT_TEST_BUNDLE")
	if oldBundle == "" || newBundle == "" {
		t.Skip("previous and expanded reviewed bundles not supplied")
	}
	if value := os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG"); value != "first-three" && value != "first-three-repair" && value != "second-group" && value != "third-group" && value != "fourth-group" && value != "fifth-group" && value != "sixth-group" && (value != "seventh-group" && (value != "thirteen-first" && (value != "thirteen-second" && (value != "thirteen-second-repair" && (value != "thirteen-third" && (value != "thirteen-fourth" && (value != "thirteen-fifth" && (value != "thirteen-sixth" && value != "thirteen-sixth-repair")))))))) {
		t.Fatal("expanded catalog preservation requires the first-three expectation")
	}
	const oldDigest = "4fec45edbe0995d0fa809c11c1fb37189176c6e9fc7fa7e8cce6a31cf1eea201"
	oldCatalog, err := loadCatalog(oldBundle)
	if err != nil || oldCatalog.revision != oldDigest || len(oldCatalog.manifest.Skills) != 39 {
		t.Fatal("previous bundle differs from the reviewed original catalog", err)
	}
	newCatalog, err := loadCatalog(newBundle)
	expectedCount := 42
	if value := os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG"); value == "first-three-repair" || value == "second-group" || value == "third-group" || value == "fourth-group" || value == "fifth-group" || value == "sixth-group" || (value == "seventh-group" || (value == "thirteen-first" || (value == "thirteen-second" || (value == "thirteen-second-repair" || (value == "thirteen-third" || (value == "thirteen-fourth" || (value == "thirteen-fifth" || (value == "thirteen-sixth" || value == "thirteen-sixth-repair")))))))) {
		expectedCount = 44
	}
	if value := os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG"); value == "second-group" || value == "third-group" || value == "fourth-group" || value == "fifth-group" || value == "sixth-group" || (value == "seventh-group" || (value == "thirteen-first" || (value == "thirteen-second" || (value == "thirteen-second-repair" || (value == "thirteen-third" || (value == "thirteen-fourth" || (value == "thirteen-fifth" || (value == "thirteen-sixth" || value == "thirteen-sixth-repair")))))))) {
		expectedCount = 47
	}
	if os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "third-group" || os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "fourth-group" || os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "fifth-group" || os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "sixth-group" || (os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "seventh-group" || (os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "thirteen-first" || (os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "thirteen-second" || (os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "thirteen-second-repair" || (os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "thirteen-third" || (os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "thirteen-fourth" || (os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "thirteen-fifth" || (os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "thirteen-sixth" || os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "thirteen-sixth-repair")))))))) {
		expectedCount = 48
	}
	if os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "fourth-group" || os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "fifth-group" || os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "sixth-group" || (os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "seventh-group" || (os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "thirteen-first" || (os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "thirteen-second" || (os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "thirteen-second-repair" || (os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "thirteen-third" || (os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "thirteen-fourth" || (os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "thirteen-fifth" || (os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "thirteen-sixth" || os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "thirteen-sixth-repair")))))))) {
		expectedCount = 49
	}
	if os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "fifth-group" || os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "sixth-group" || (os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "seventh-group" || (os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "thirteen-first" || (os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "thirteen-second" || (os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "thirteen-second-repair" || (os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "thirteen-third" || (os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "thirteen-fourth" || (os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "thirteen-fifth" || (os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "thirteen-sixth" || os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "thirteen-sixth-repair")))))))) {
		expectedCount = 50
	}
	if os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "sixth-group" || (os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "seventh-group" || (os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "thirteen-first" || (os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "thirteen-second" || (os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "thirteen-second-repair" || (os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "thirteen-third" || (os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "thirteen-fourth" || (os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "thirteen-fifth" || (os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "thirteen-sixth" || os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "thirteen-sixth-repair")))))))) {
		expectedCount = 51
	}
	if os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "seventh-group" || (os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "thirteen-first" || (os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "thirteen-second" || (os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "thirteen-second-repair" || (os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "thirteen-third" || (os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "thirteen-fourth" || (os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "thirteen-fifth" || (os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "thirteen-sixth" || os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "thirteen-sixth-repair"))))))) {
		expectedCount = 52
	}
	if os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "thirteen-first" || (os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "thirteen-second" || (os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "thirteen-second-repair" || (os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "thirteen-third" || (os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "thirteen-fourth" || (os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "thirteen-fifth" || (os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "thirteen-sixth" || os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "thirteen-sixth-repair")))))) {
		expectedCount = 54
	}
	if os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "thirteen-second" || (os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "thirteen-second-repair" || (os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "thirteen-third" || (os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "thirteen-fourth" || (os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "thirteen-fifth" || (os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "thirteen-sixth" || os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "thirteen-sixth-repair"))))) {
		expectedCount = 56
	}
	if os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "thirteen-second-repair" || (os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "thirteen-third" || (os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "thirteen-fourth" || (os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "thirteen-fifth" || (os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "thirteen-sixth" || os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "thirteen-sixth-repair")))) {
		expectedCount = 58
	}
	if os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "thirteen-third" || (os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "thirteen-fourth" || (os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "thirteen-fifth" || (os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "thirteen-sixth" || os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "thirteen-sixth-repair"))) {
		expectedCount = 63
	}
	if os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "thirteen-fourth" || (os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "thirteen-fifth" || (os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "thirteen-sixth" || os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "thirteen-sixth-repair")) {
		expectedCount = 64
	}
	if os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "thirteen-fifth" || (os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "thirteen-sixth" || os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "thirteen-sixth-repair") {
		expectedCount = 69
	}
	if os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "thirteen-sixth" || os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "thirteen-sixth-repair" {
		expectedCount = 70
	}
	if os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") == "thirteen-sixth-repair" {
		expectedCount = 71
	}
	if os.Getenv("YIJIE_EXPERT_TEST_OPS_RESPONSIVE_REPAIR") == "true" {
		if os.Getenv("YIJIE_EXPERT_TEST_EXPECTED_CATALOG") != "thirteen-sixth-repair" {
			t.Fatal("responsive repair requires the complete prior expert catalog")
		}
		expectedCount += 2
	}
	if os.Getenv("YIJIE_EXPERT_TEST_OPS_LABEL_REPAIR") == "true" {
		if os.Getenv("YIJIE_EXPERT_TEST_OPS_RESPONSIVE_REPAIR") != "true" {
			t.Fatal("label repair requires all prior responsive resource versions")
		}
		expectedCount += 2
	}
	if err != nil || newCatalog.revision != ReviewedExpertCatalogDigest || len(newCatalog.manifest.Skills) != expectedCount {
		t.Fatal("expanded bundle differs from the reviewed first-three catalog", err)
	}
	findOriginal := func(c catalog) ManifestSkill {
		for _, item := range c.manifest.Skills {
			if isExpertResource(item) && item.Expert.ExpertId == "jd-ops-expert" && item.Version == "0.1.3" {
				return item
			}
		}
		t.Fatal("original expert 0.1.3 absent")
		return ManifestSkill{}
	}
	original, expanded := findOriginal(oldCatalog), findOriginal(newCatalog)
	reference := feat158ReferenceFor(t, original)
	if !reflect.DeepEqual(original, expanded) || reference != feat158ReferenceFor(t, expanded) {
		t.Fatal("catalog expansion changed original expert metadata or immutable reference")
	}
	managed := filepath.Join(t.TempDir(), "installed")
	runtime := newTestRuntime(original.RuntimeName)
	before, err := NewService(Config{BundleRoot: oldBundle, ManagedRoot: managed, Runtime: runtime, ExpertCatalogDigest: oldDigest})
	if err != nil {
		t.Fatal(err)
	}
	defer before.Close()
	manifest := testManifestProjection{revision: oldCatalog.revision, Skills: []ManifestSkill{original}}
	if state, err := before.Install(context.Background(), installInput(manifest, uuid.NewString())); err != nil || state.RuntimeVisible {
		t.Fatal("original installation failed", state, err)
	}
	resolved, err := before.ResolveExpert(reference)
	if err != nil {
		t.Fatal(err)
	}
	files := func() map[string][32]byte {
		values := make(map[string][32]byte)
		root := filepath.Join(managed, original.ID)
		err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil || entry.IsDir() {
				return walkErr
			}
			raw, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			relative, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			values[relative] = sha256.Sum256(raw)
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		return values
	}
	originalFiles := files()
	if _, ok := originalFiles[receiptFileName]; !ok {
		t.Fatal("original installation receipt missing")
	}
	if err := before.Close(); err != nil {
		t.Fatal(err)
	}
	after, err := NewService(Config{BundleRoot: newBundle, ManagedRoot: managed, Runtime: runtime, ExpertCatalogDigest: ReviewedExpertCatalogDigest})
	if err != nil {
		t.Fatal(err)
	}
	defer after.Close()
	if current, err := after.ResolveExpert(reference); err != nil || current != resolved {
		t.Fatal("expanded catalog could not resolve retained original expert", err)
	}
	if _, err := after.PrepareExpertResources(context.Background(), uuid.NewString(), expertwire.ExpertSelection{ExpertId: "jd-ops-expert", Version: "0.1.3"}); err != nil {
		t.Fatal("normal prepare of unchanged original resource failed", err)
	}
	if !reflect.DeepEqual(originalFiles, files()) {
		t.Fatal("catalog expansion rewrote original installed files or receipt")
	}
	if len(runtime.roots) != 0 || len(runtime.enabled) != 0 {
		t.Fatal("catalog expansion projected expert resources globally")
	}
	t.Logf("normal catalog expansion preserved original ExpertRef and %d installed file digests including its original receipt", len(originalFiles))
}
