package skills

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
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
	var chosen *ManifestSkill
	for _, entry := range catalog.manifest.Skills {
		if isExpertResource(entry) {
			copy := entry
			chosen = &copy
			break
		}
	}
	if chosen == nil || chosen.Version != "0.1.3" || len(catalog.manifest.Skills) != 39 {
		t.Fatal("candidate differs from the reviewed 38 Skills plus expert 0.1.3 catalog")
	}
	service, err := NewService(Config{BundleRoot: bundle, ManagedRoot: filepath.Join(t.TempDir(), "installed"), Runtime: newTestRuntime(chosen.RuntimeName), ExpertCatalogDigest: authority})
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	manifest := testManifestProjection{revision: catalog.revision, Skills: []ManifestSkill{*chosen}}
	state, err := service.Install(context.Background(), installInput(manifest, uuid.NewString()))
	if err != nil || state.RuntimeVisible {
		t.Fatal(state, err)
	}
	resolved, err := service.ResolveExpert(feat158ReferenceFor(t, *chosen))
	if err != nil || len(resolved.Role) == 0 || len(resolved.Index) == 0 {
		t.Fatal("actual managed candidate resource qualification", err)
	}
	t.Logf("verified actual candidate %s %s: role=%d bytes; short index=%d bytes; no runtime global projection", chosen.Expert.ExpertId, chosen.Version, len(resolved.Role), len(resolved.Index))
}
