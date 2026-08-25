package skills

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"

	contractschema "github.com/36Dge/yijie-agent-host/api/jsonschema"
	"github.com/dlclark/regexp2"
	"github.com/santhosh-tekuri/jsonschema/v6"
)

const (
	manifestFileName = "bundle-manifest.json"
	maxManifestBytes = 4 << 20
)

var (
	manifestSchemasOnce sync.Once
	manifestSchemas     map[int]*jsonschema.Schema
	manifestSchemasErr  error
)

type Manifest struct {
	SchemaVersion       int             `json:"schema_version"`
	BundleID            string          `json:"bundle_id"`
	BundleVersion       string          `json:"bundle_version"`
	DistributionChannel string          `json:"distribution_channel"`
	Source              BundleSource    `json:"source"`
	Skills              []ManifestSkill `json:"skills"`
}

type BundleSource struct {
	Repository   string `json:"repository"`
	RevisionKind string `json:"revision_kind"`
	Revision     string `json:"revision"`
	TreeSHA256   string `json:"tree_sha256"`
}

type ManifestSkill struct {
	ID               string       `json:"id"`
	RuntimeName      string       `json:"runtime_name"`
	Category         string       `json:"category"`
	Order            int          `json:"order"`
	DisplayName      string       `json:"display_name"`
	Description      string       `json:"description"`
	Version          string       `json:"version"`
	CatalogEntryMode string       `json:"catalog_entry_mode"`
	Entrypoint       string       `json:"entrypoint"`
	Icon             SkillIcon    `json:"icon"`
	Risk             SkillRisk    `json:"risk"`
	Provenance       Provenance   `json:"provenance"`
	License          License      `json:"license"`
	Capabilities     Capabilities `json:"capabilities"`
	Archive          Archive      `json:"archive"`
	Release          Release      `json:"release"`
}

type SkillIcon struct {
	Registry string `json:"registry"`
	Key      string `json:"key"`
}

type SkillRisk struct {
	Level   string   `json:"level"`
	Reasons []string `json:"reasons"`
}

type Provenance struct {
	SourceType      string `json:"source_type"`
	SourceReference string `json:"source_reference"`
	SourceVersion   string `json:"source_version"`
	SourceSHA256    string `json:"source_sha256"`
	ReviewStatus    string `json:"review_status"`
	ReviewedBy      string `json:"reviewed_by"`
	ReviewedAt      string `json:"reviewed_at"`
}

type License struct {
	Expression           string `json:"expression"`
	RedistributionStatus string `json:"redistribution_status"`
	AuthorizationScope   string `json:"authorization_scope"`
	EvidenceReference    string `json:"evidence_reference"`
	ReviewedBy           string `json:"reviewed_by"`
	ReviewedAt           string `json:"reviewed_at"`
}

type Capabilities struct {
	ExecutionMode string   `json:"execution_mode"`
	Network       string   `json:"network"`
	Filesystem    string   `json:"filesystem"`
	RequiredTools []string `json:"required_tools"`
}

type Archive struct {
	Path                  string `json:"path"`
	SHA256                string `json:"sha256"`
	CompressedSizeBytes   int64  `json:"compressed_size_bytes"`
	UncompressedSizeBytes int64  `json:"uncompressed_size_bytes"`
	FileCount             int    `json:"file_count"`
}

type Release struct {
	CatalogStatus     string `json:"catalog_status"`
	MaintenanceStatus string `json:"maintenance_status"`
	BlockedReason     string `json:"blocked_reason"`
}

type catalog struct {
	manifest Manifest
	revision string
	byID     map[string]ManifestSkill
}

func loadCatalog(bundleRoot string) (catalog, error) {
	root, err := os.OpenRoot(bundleRoot)
	if err != nil {
		return catalog{}, errorWithCode(CodeBundleMissing, err)
	}
	defer root.Close()

	info, err := root.Lstat(manifestFileName)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return catalog{}, errorWithCode(CodeBundleMissing, err)
		}
		return catalog{}, errorWithCode(CodeManifestInvalid, err)
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0o022 != 0 ||
		info.Size() < 1 || info.Size() > maxManifestBytes {
		return catalog{}, errorWithCode(CodeManifestInvalid, errors.New("manifest file is invalid"))
	}
	manifestFile, err := root.Open(manifestFileName)
	if err != nil {
		return catalog{}, errorWithCode(CodeManifestInvalid, err)
	}
	defer manifestFile.Close()
	openedInfo, err := manifestFile.Stat()
	if err != nil || !openedInfo.Mode().IsRegular() || openedInfo.Mode().Perm()&0o022 != 0 || openedInfo.Size() != info.Size() {
		return catalog{}, errorWithCode(CodeManifestInvalid, errors.New("manifest file changed during validation"))
	}
	raw, err := io.ReadAll(io.LimitReader(manifestFile, maxManifestBytes+1))
	if err != nil || len(raw) < 1 || len(raw) > maxManifestBytes {
		return catalog{}, errorWithCode(CodeManifestInvalid, errors.New("manifest file is invalid"))
	}
	instance, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	if err != nil {
		return catalog{}, errorWithCode(CodeManifestInvalid, errors.New("manifest JSON is invalid"))
	}
	var header struct {
		SchemaVersion int `json:"schema_version"`
	}
	if err := json.Unmarshal(raw, &header); err != nil {
		return catalog{}, errorWithCode(CodeManifestInvalid, errors.New("manifest JSON is invalid"))
	}
	schema, err := compiledManifestSchema(header.SchemaVersion)
	if err != nil {
		return catalog{}, errorWithCode(CodeManifestInvalid, err)
	}
	if err := schema.Validate(instance); err != nil {
		return catalog{}, errorWithCode(CodeManifestInvalid, fmt.Errorf("manifest does not match Skill Bundle Manifest v%d", header.SchemaVersion))
	}
	var manifest Manifest
	if err := strictJSON(raw, &manifest); err != nil {
		return catalog{}, errorWithCode(CodeManifestInvalid, err)
	}
	byID := make(map[string]ManifestSkill, len(manifest.Skills))
	runtimeNames := make(map[string]struct{}, len(manifest.Skills))
	archivePaths := make(map[string]struct{}, len(manifest.Skills))
	for _, skill := range manifest.Skills {
		if _, exists := byID[skill.ID]; exists {
			return catalog{}, errorWithCode(CodeManifestInvalid, errors.New("duplicate Skill id"))
		}
		if _, exists := runtimeNames[skill.RuntimeName]; exists {
			return catalog{}, errorWithCode(CodeManifestInvalid, errors.New("duplicate Runtime name"))
		}
		mode := catalogEntryMode(skill)
		if mode != "bundled" && mode != "catalog-only" {
			return catalog{}, errorWithCode(CodeManifestInvalid, errors.New("Skill catalog entry mode is invalid"))
		}
		if skill.Archive.Path != "" {
			if _, exists := archivePaths[skill.Archive.Path]; exists {
				return catalog{}, errorWithCode(CodeManifestInvalid, errors.New("duplicate archive path"))
			}
			if _, err := resolveUnder(bundleRoot, skill.Archive.Path); err != nil {
				return catalog{}, errorWithCode(CodeManifestInvalid, err)
			}
			archivePaths[skill.Archive.Path] = struct{}{}
		}
		if skill.Release.CatalogStatus == "installable" {
			if mode != "bundled" || skill.Entrypoint != "SKILL.md" || skill.Archive.Path == "" ||
				skill.Provenance.ReviewStatus != "verified" || skill.License.RedistributionStatus != "verified" ||
				(skill.License.AuthorizationScope != "local-development" && skill.License.AuthorizationScope != "desktop-distribution") {
				return catalog{}, errorWithCode(CodeManifestInvalid, errors.New("installable Skill is not a verified bundled entry"))
			}
		} else if mode == "catalog-only" && (skill.Entrypoint != "" || skill.Archive.Path != "" || skill.Release.BlockedReason == "") {
			return catalog{}, errorWithCode(CodeManifestInvalid, errors.New("catalog-only Skill contains bundled content or lacks a blocked reason"))
		}
		byID[skill.ID] = skill
		runtimeNames[skill.RuntimeName] = struct{}{}
	}
	digest := sha256.Sum256(raw)
	return catalog{manifest: manifest, revision: hex.EncodeToString(digest[:]), byID: byID}, nil
}

func compiledManifestSchema(version int) (*jsonschema.Schema, error) {
	manifestSchemasOnce.Do(func() {
		manifestSchemas = make(map[int]*jsonschema.Schema, 2)
		for schemaVersion, source := range map[int][]byte{
			1: contractschema.SkillBundleManifestV1,
			2: contractschema.SkillBundleManifestV2,
		} {
			compiler := jsonschema.NewCompiler()
			compiler.UseRegexpEngine(compileECMAScriptRegexp)
			compiler.AssertFormat()
			resource := fmt.Sprintf("https://contracts.yijie.ai/skills/skill-bundle-manifest-v%d.schema.json", schemaVersion)
			schemaDocument, err := jsonschema.UnmarshalJSON(bytes.NewReader(source))
			if err != nil {
				manifestSchemasErr = err
				return
			}
			if err := compiler.AddResource(resource, schemaDocument); err != nil {
				manifestSchemasErr = err
				return
			}
			compiled, err := compiler.Compile(resource)
			if err != nil {
				manifestSchemasErr = err
				return
			}
			manifestSchemas[schemaVersion] = compiled
		}
	})
	if manifestSchemasErr != nil {
		return nil, manifestSchemasErr
	}
	schema, ok := manifestSchemas[version]
	if !ok {
		return nil, fmt.Errorf("unsupported Skill Bundle Manifest version %d", version)
	}
	return schema, nil
}

func catalogEntryMode(skill ManifestSkill) string {
	if skill.CatalogEntryMode == "" {
		return "bundled"
	}
	return skill.CatalogEntryMode
}

type ecmaScriptRegexp regexp2.Regexp

func (regexp *ecmaScriptRegexp) MatchString(value string) bool {
	matched, err := (*regexp2.Regexp)(regexp).MatchString(value)
	return err == nil && matched
}

func (regexp *ecmaScriptRegexp) String() string {
	return (*regexp2.Regexp)(regexp).String()
}

func compileECMAScriptRegexp(value string) (jsonschema.Regexp, error) {
	compiled, err := regexp2.Compile(value, regexp2.ECMAScript)
	if err != nil {
		return nil, err
	}
	return (*ecmaScriptRegexp)(compiled), nil
}

func strictJSON(raw []byte, destination any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("JSON contains trailing content")
	}
	return nil
}

func canonicalDirectory(value string, create bool) (string, error) {
	if value == "" || !filepath.IsAbs(value) || filepath.Clean(value) != value {
		return "", errors.New("directory must be canonical and absolute")
	}
	if create {
		if err := os.MkdirAll(value, 0o700); err != nil {
			return "", err
		}
	}
	info, err := os.Lstat(value)
	if err != nil {
		return "", err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return "", errors.New("directory must not be a symlink")
	}
	resolved, err := filepath.EvalSymlinks(value)
	if err != nil || !filepath.IsAbs(resolved) {
		return "", errors.New("directory cannot be canonicalized")
	}
	return filepath.Clean(resolved), nil
}

func resolveUnder(root, relative string) (string, error) {
	if relative == "" || filepath.IsAbs(relative) || strings.Contains(relative, "\\") || strings.ContainsRune(relative, '\x00') {
		return "", errors.New("relative path is invalid")
	}
	normalized := filepath.FromSlash(relative)
	if filepath.Clean(normalized) != normalized || normalized == "." || strings.HasPrefix(normalized, ".."+string(filepath.Separator)) {
		return "", errors.New("relative path escapes its root")
	}
	resolved := filepath.Join(root, normalized)
	relativeToRoot, err := filepath.Rel(root, resolved)
	if err != nil || relativeToRoot == ".." || strings.HasPrefix(relativeToRoot, ".."+string(filepath.Separator)) {
		return "", errors.New("relative path escapes its root")
	}
	return resolved, nil
}

func manifestSkill(catalog catalog, id string) (ManifestSkill, error) {
	skill, ok := catalog.byID[id]
	if !ok {
		return ManifestSkill{}, errorWithCode(CodeNotFound, fmt.Errorf("unknown Skill id"))
	}
	return skill, nil
}
