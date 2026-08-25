package skills

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"regexp"
	"sort"
)

const (
	receiptFileName  = ".yijie-install.json"
	disabledFileName = ".yijie-disabled"
)

type installReceipt struct {
	SchemaVersion   int           `json:"schema_version"`
	SkillID         string        `json:"skill_id"`
	RuntimeName     string        `json:"runtime_name"`
	Version         string        `json:"version"`
	ArchiveSHA256   string        `json:"archive_sha256"`
	CatalogRevision string        `json:"catalog_revision"`
	Files           []receiptFile `json:"files"`
}

type receiptFile struct {
	Path   string `json:"path"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

type installation struct {
	exists  bool
	valid   bool
	enabled bool
	receipt installReceipt
	failure ErrorCode
}

func inspectInstallation(managed *os.Root, skill ManifestSkill) installation {
	info, err := managed.Lstat(skill.ID)
	if errors.Is(err, os.ErrNotExist) {
		return installation{}
	}
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0o077 != 0 {
		return installation{exists: true, failure: CodeInstalledFilesCorrupt}
	}

	skillRoot, err := managed.OpenRoot(skill.ID)
	if err != nil {
		return installation{exists: true, failure: CodeInstalledFilesCorrupt}
	}
	defer skillRoot.Close()

	raw, err := skillRoot.ReadFile(receiptFileName)
	if err != nil || len(raw) == 0 || len(raw) > maxManifestBytes {
		return installation{exists: true, failure: CodeInstallReceiptInvalid}
	}
	var receipt installReceipt
	if err := strictJSON(raw, &receipt); err != nil || !validReceiptIdentity(receipt, skill) {
		return installation{exists: true, failure: CodeInstallReceiptInvalid}
	}

	expectedFiles := make(map[string]receiptFile, len(receipt.Files))
	expectedFolded := make(map[string]struct{}, len(receipt.Files))
	expectedDirectories := map[string]struct{}{".": {}}
	for _, file := range receipt.Files {
		if !validArchiveEntryName(file.Path) || file.Size < 0 || !isLowerSHA256(file.SHA256) ||
			file.Path == receiptFileName || file.Path == disabledFileName ||
			(path.Base(file.Path) == "SKILL.md" && file.Path != skill.Entrypoint) {
			return installation{exists: true, failure: CodeInstallReceiptInvalid}
		}
		folded := collisionKey(file.Path)
		if _, duplicate := expectedFiles[file.Path]; duplicate {
			return installation{exists: true, failure: CodeInstallReceiptInvalid}
		}
		if _, collision := expectedFolded[folded]; collision {
			return installation{exists: true, failure: CodeInstallReceiptInvalid}
		}
		expectedFiles[file.Path] = file
		expectedFolded[folded] = struct{}{}
		for directory := path.Dir(file.Path); directory != "."; directory = path.Dir(directory) {
			expectedDirectories[directory] = struct{}{}
		}
	}
	if _, ok := expectedFiles[skill.Entrypoint]; !ok {
		return installation{exists: true, failure: CodeInstallReceiptInvalid}
	}

	seenFiles := make(map[string]struct{}, len(expectedFiles))
	seenFolded := make(map[string]struct{}, len(expectedFiles)+2)
	disabled := false
	walkErr := fs.WalkDir(skillRoot.FS(), ".", func(name string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			if errors.Is(walkErr, os.ErrNotExist) {
				return errorWithCode(CodeInstalledFilesMissing, walkErr)
			}
			return errorWithCode(CodeInstalledFilesCorrupt, walkErr)
		}
		if name == "." {
			return nil
		}
		if name == receiptFileName || name == disabledFileName {
			info, err := entry.Info()
			if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0o077 != 0 {
				return errorWithCode(CodeInstalledFilesCorrupt, errors.New("installed state file is invalid"))
			}
			if name == disabledFileName {
				if info.Size() != 0 {
					return errorWithCode(CodeInstalledFilesCorrupt, errors.New("disabled marker is invalid"))
				}
				disabled = true
			}
			return nil
		}
		if !validArchiveEntryName(name) {
			return errorWithCode(CodeInstalledFilesCorrupt, errors.New("installed path is invalid"))
		}
		folded := collisionKey(name)
		if _, collision := seenFolded[folded]; collision {
			return errorWithCode(CodeInstalledFilesCorrupt, errors.New("installed paths collide"))
		}
		seenFolded[folded] = struct{}{}

		info, err := entry.Info()
		if err != nil {
			return errorWithCode(CodeInstalledFilesCorrupt, err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return errorWithCode(CodeInstalledFilesCorrupt, errors.New("installed symlink is forbidden"))
		}
		if entry.IsDir() {
			if info.Mode().Perm()&0o077 != 0 {
				return errorWithCode(CodeInstalledFilesCorrupt, errors.New("installed directory permissions are not owner-only"))
			}
			if _, expected := expectedDirectories[name]; !expected {
				return errorWithCode(CodeInstalledFilesCorrupt, errors.New("unexpected installed directory"))
			}
			return nil
		}
		if !info.Mode().IsRegular() {
			return errorWithCode(CodeInstalledFilesCorrupt, errors.New("installed special file is forbidden"))
		}
		expected, ok := expectedFiles[name]
		if !ok {
			return errorWithCode(CodeInstalledFilesCorrupt, errors.New("unexpected installed file"))
		}
		if info.Size() != expected.Size || info.Mode().Perm()&0o077 != 0 {
			return errorWithCode(CodeInstalledFilesCorrupt, errors.New("installed file metadata changed"))
		}
		actual, err := hashRootFile(skillRoot, name, expected.Size)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return errorWithCode(CodeInstalledFilesMissing, err)
			}
			return errorWithCode(CodeInstalledFilesCorrupt, err)
		}
		if actual != expected.SHA256 {
			return errorWithCode(CodeInstalledFilesCorrupt, errors.New("installed file digest changed"))
		}
		seenFiles[name] = struct{}{}
		return nil
	})
	if walkErr != nil {
		return installation{exists: true, receipt: receipt, failure: ErrorCodeOf(walkErr)}
	}
	if len(seenFiles) != len(expectedFiles) {
		return installation{exists: true, receipt: receipt, failure: CodeInstalledFilesMissing}
	}
	return installation{exists: true, valid: true, enabled: !disabled, receipt: receipt}
}

func validReceiptIdentity(receipt installReceipt, skill ManifestSkill) bool {
	if receipt.SchemaVersion != 1 || receipt.SkillID != skill.ID || receipt.RuntimeName != skill.RuntimeName ||
		!semanticVersionPattern.MatchString(receipt.Version) || !isLowerSHA256(receipt.ArchiveSHA256) ||
		!isLowerSHA256(receipt.CatalogRevision) || len(receipt.Files) == 0 || len(receipt.Files) > 2048 {
		return false
	}
	if receipt.Version == skill.Version {
		if receipt.ArchiveSHA256 != skill.Archive.SHA256 || len(receipt.Files) != skill.Archive.FileCount {
			return false
		}
		var total int64
		for _, file := range receipt.Files {
			if file.Size < 0 || total > skill.Archive.UncompressedSizeBytes-file.Size {
				return false
			}
			total += file.Size
		}
		if total != skill.Archive.UncompressedSizeBytes {
			return false
		}
	}
	return true
}

var semanticVersionPattern = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(?:-[0-9A-Za-z.-]+)?(?:\+[0-9A-Za-z.-]+)?$`)

func hashRootFile(root *os.Root, name string, expectedSize int64) (string, error) {
	file, err := root.Open(name)
	if err != nil {
		return "", err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() != expectedSize {
		return "", errors.New("installed file changed during validation")
	}
	digest := sha256.New()
	written, err := io.Copy(digest, io.LimitReader(file, expectedSize+1))
	if err != nil || written != expectedSize {
		return "", errors.New("installed file could not be validated")
	}
	return hex.EncodeToString(digest.Sum(nil)), nil
}

func writeReceipt(root *os.Root, receipt installReceipt) error {
	sort.Slice(receipt.Files, func(i, j int) bool { return receipt.Files[i].Path < receipt.Files[j].Path })
	raw, err := json.Marshal(receipt)
	if err != nil {
		return err
	}
	return writeOwnerOnlyFile(root, receiptFileName, append(raw, '\n'))
}

func writeOwnerOnlyFile(root *os.Root, name string, content []byte) error {
	file, err := root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	if _, err = file.Write(content); err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err != nil {
		_ = root.Remove(name)
		return err
	}
	if closeErr != nil {
		_ = root.Remove(name)
		return closeErr
	}
	return nil
}

func isLowerSHA256(value string) bool {
	if len(value) != sha256.Size*2 {
		return false
	}
	decoded, err := hex.DecodeString(value)
	return err == nil && hex.EncodeToString(decoded) == value
}

func safeEntrypointPath(managedRoot string, managed *os.Root, skill ManifestSkill) (string, bool) {
	info, err := managed.Lstat(skill.ID)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return "", false
	}
	skillRoot, err := managed.OpenRoot(skill.ID)
	if err != nil {
		return "", false
	}
	defer skillRoot.Close()
	info, err = skillRoot.Lstat(skill.Entrypoint)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return "", false
	}
	entrypoint := fmt.Sprintf("%s%c%s%c%s", managedRoot, os.PathSeparator, skill.ID, os.PathSeparator, skill.Entrypoint)
	return entrypoint, true
}
