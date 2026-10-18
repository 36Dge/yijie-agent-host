package skills

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	expertwire "github.com/36Dge/yijie-agent-host/internal/contracts/experts"
)

func isExpertResource(skill ManifestSkill) bool { return skill.AssetKind == "expert_resources" }

func validateExpertCatalogAuthority(current catalog, expectedDigest string) error {
	if expectedDigest != "" && (!isLowerSHA256(expectedDigest) || current.revision != expectedDigest || current.manifest.SchemaVersion != 3) {
		return errorWithCode(CodeManifestInvalid, errors.New("expert catalog differs from its pinned source"))
	}
	found := false
	for _, skill := range current.manifest.Skills {
		if !isExpertResource(skill) {
			continue
		}
		found = true
		if !isLowerSHA256(expectedDigest) || current.revision != expectedDigest || skill.Expert == nil ||
			skill.Expert.LocalUseAuthorization.Status != "local-use-authorized" ||
			skill.Expert.LocalUseAuthorization.EvidenceReference == "" {
			return errorWithCode(CodeManifestInvalid, errors.New("expert catalog local source authority is unavailable"))
		}
	}
	if expectedDigest != "" && !found {
		return errorWithCode(CodeManifestInvalid, errors.New("expert catalog has no reviewed expert resources"))
	}
	return nil
}

func (s *Service) loadCurrentCatalog() (catalog, error) {
	current, err := loadCatalog(s.bundleRoot)
	if err == nil {
		err = validateExpertCatalogAuthority(current, s.expertCatalogDigest)
	}
	return current, err
}

// ResolvedExpert is process-local content from a verified managed installation.
// It is not a wire DTO and must not be logged, persisted in receipts or exposed
// through the renderer. Knowledge bodies are validated but never inlined here.
type ResolvedExpert struct {
	Reference expertwire.ExpertRef
	Role      string
	Index     string
}

// ResolveExpert resolves exact immutable identity, not caller-supplied paths or
// prompt text. Asset availability is separate from native execution readiness.
func (s *Service) ResolveExpert(reference expertwire.ExpertRef) (ResolvedExpert, error) {
	if err := reference.Validate(); err != nil {
		return ResolvedExpert{}, errorWithCode(CodeInvalidRequest, errors.New("expert reference is invalid"))
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return ResolvedExpert{}, errorWithCode(CodeRuntimeUnavailable, errors.New("managed resource service is closed"))
	}
	current, err := s.loadCurrentCatalog()
	if err != nil {
		return ResolvedExpert{}, err
	}
	for _, skill := range current.manifest.Skills {
		if !isExpertResource(skill) || skill.Expert == nil || skill.Expert.ExpertId != string(reference.ExpertId) || skill.Version != string(reference.Version) {
			continue
		}
		return s.resolveExpertLocked(skill, reference)
	}
	return ResolvedExpert{}, errorWithCode(CodeNotFound, errors.New("expert resources unavailable"))
}

func (s *Service) resolveExpertLocked(skill ManifestSkill, reference expertwire.ExpertRef) (ResolvedExpert, error) {
	metadata := skill.Expert
	if metadata == nil || !installableSkill(skill) || string(reference.Version) != skill.Version ||
		string(reference.ArchiveSha256) != skill.Archive.SHA256 || string(reference.RoleSha256) != string(metadata.RoleResource.Sha256) {
		return ResolvedExpert{}, errorWithCode(CodeInvalidRequest, errors.New("expert reference differs from the available version"))
	}
	knowledgeDigest, err := expertwire.KnowledgeManifestDigest(metadata.KnowledgeResources)
	if err != nil || string(reference.KnowledgeManifestSha256) != string(knowledgeDigest) {
		return ResolvedExpert{}, errorWithCode(CodeInvalidRequest, errors.New("expert knowledge identity differs"))
	}
	installed := inspectInstallation(s.managed, skill)
	if !installed.valid || !installed.enabled || installed.receipt.Version != skill.Version ||
		installed.receipt.ArchiveSHA256 != skill.Archive.SHA256 {
		return ResolvedExpert{}, errorWithCode(CodeCapabilityUnavailable, errors.New("expert installation unavailable"))
	}
	root, err := s.managed.OpenRoot(skill.ID)
	if err != nil {
		return ResolvedExpert{}, errorWithCode(CodeInstalledFilesMissing, err)
	}
	defer root.Close()
	role, err := readExpertResource(root, installed.receipt, string(metadata.RoleResource.Path),
		string(metadata.RoleResource.Sha256), int64(metadata.RoleResource.Bytes), 32<<10)
	if err != nil {
		return ResolvedExpert{}, err
	}
	paths := map[string]bool{string(metadata.RoleResource.Path): true}
	type indexEntry struct {
		ID, Title, Version, Status, Path string
	}
	index := make([]indexEntry, 0, len(metadata.KnowledgeResources))
	for _, resource := range metadata.KnowledgeResources {
		path := string(resource.Path)
		if paths[path] {
			return ResolvedExpert{}, errorWithCode(CodeManifestInvalid, errors.New("expert resource paths must be distinct"))
		}
		paths[path] = true
		if _, err := readExpertResource(root, installed.receipt, path, string(resource.Sha256), int64(resource.Bytes), 8<<20); err != nil {
			return ResolvedExpert{}, err
		}
		index = append(index, indexEntry{ID: string(resource.Id), Title: resource.Title,
			Version: string(resource.ContentVersion), Status: string(resource.ContentStatus),
			Path: filepath.Join(s.managedRoot, skill.ID, filepath.FromSlash(path))})
	}
	rawIndex, err := json.Marshal(index)
	if err != nil || len(rawIndex) > 32<<10 {
		return ResolvedExpert{}, errorWithCode(CodeArchiveTooLarge, errors.New("expert resource index exceeds context limit"))
	}
	return ResolvedExpert{Reference: reference, Role: role, Index: string(rawIndex)}, nil
}

func readExpertResource(root *os.Root, receipt installReceipt, path, expectedDigest string, size, limit int64) (string, error) {
	if !validArchiveEntryName(path) || size <= 0 || size > limit || !isLowerSHA256(expectedDigest) {
		return "", errorWithCode(CodeManifestInvalid, errors.New("expert resource declaration is invalid"))
	}
	covered := false
	for _, entry := range receipt.Files {
		if entry.Path == path && entry.Size == size && entry.SHA256 == expectedDigest {
			covered = true
			break
		}
	}
	if !covered {
		return "", errorWithCode(CodeInstallReceiptInvalid, errors.New("expert resource is not covered by installation receipt"))
	}
	info, err := root.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return "", errorWithCode(CodeInstalledFilesMissing, errors.New("expert resource unavailable"))
	}
	file, err := root.Open(path)
	if err != nil {
		return "", errorWithCode(CodeInstalledFilesMissing, err)
	}
	defer file.Close()
	raw, err := io.ReadAll(io.LimitReader(file, size+1))
	if err != nil || int64(len(raw)) != size || !utf8.Valid(raw) || strings.ContainsRune(string(raw), '\x00') {
		return "", errorWithCode(CodeInstalledFilesCorrupt, errors.New("expert text resource unavailable"))
	}
	digest := sha256.Sum256(raw)
	if hex.EncodeToString(digest[:]) != expectedDigest {
		return "", errorWithCode(CodeInstalledFilesCorrupt, errors.New("expert resource content differs"))
	}
	return string(raw), nil
}
