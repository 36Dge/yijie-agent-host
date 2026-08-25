package skills

import (
	"archive/zip"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"golang.org/x/text/cases"
	"golang.org/x/text/unicode/norm"
)

func extractArchive(
	bundle *os.Root,
	managed *os.Root,
	stageName string,
	skill ManifestSkill,
	catalogRevision string,
) (installReceipt, error) {
	archiveInfo, err := bundle.Lstat(skill.Archive.Path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return installReceipt{}, errorWithCode(CodeBundleMissing, err)
		}
		return installReceipt{}, errorWithCode(CodeInstallFailed, err)
	}
	if !archiveInfo.Mode().IsRegular() || archiveInfo.Mode()&os.ModeSymlink != 0 || archiveInfo.Mode().Perm()&0o022 != 0 {
		return installReceipt{}, errorWithCode(CodeArchiveUnsafe, errors.New("archive must be a regular file"))
	}
	if archiveInfo.Size() != skill.Archive.CompressedSizeBytes {
		return installReceipt{}, errorWithCode(CodeArchiveChecksum, errors.New("archive byte length does not match manifest"))
	}

	archiveFile, err := bundle.Open(skill.Archive.Path)
	if err != nil {
		return installReceipt{}, errorWithCode(CodeBundleMissing, err)
	}
	defer archiveFile.Close()
	openedInfo, err := archiveFile.Stat()
	if err != nil || !openedInfo.Mode().IsRegular() || openedInfo.Mode().Perm()&0o022 != 0 || openedInfo.Size() != archiveInfo.Size() {
		return installReceipt{}, errorWithCode(CodeArchiveUnsafe, errors.New("archive changed during validation"))
	}

	digest := sha256.New()
	written, err := io.Copy(digest, io.LimitReader(archiveFile, skill.Archive.CompressedSizeBytes+1))
	if err != nil || written != skill.Archive.CompressedSizeBytes {
		return installReceipt{}, errorWithCode(CodeArchiveChecksum, errors.New("archive could not be hashed"))
	}
	actualDigest := hex.EncodeToString(digest.Sum(nil))
	if subtle.ConstantTimeCompare([]byte(actualDigest), []byte(skill.Archive.SHA256)) != 1 {
		return installReceipt{}, errorWithCode(CodeArchiveChecksum, errors.New("archive digest does not match manifest"))
	}
	if _, err := archiveFile.Seek(0, io.SeekStart); err != nil {
		return installReceipt{}, errorWithCode(CodeInstallFailed, err)
	}
	reader, err := zip.NewReader(archiveFile, archiveInfo.Size())
	if err != nil {
		return installReceipt{}, errorWithCode(CodeArchiveUnsafe, errors.New("archive is not a valid ZIP"))
	}
	if len(reader.File) != skill.Archive.FileCount {
		return installReceipt{}, errorWithCode(CodeArchiveUnsafe, errors.New("archive file count does not match manifest"))
	}

	seen := make(map[string]struct{}, len(reader.File))
	folded := make(map[string]struct{}, len(reader.File))
	var declaredTotal uint64
	entrypointFound := false
	for _, entry := range reader.File {
		if !validArchiveEntryName(entry.Name) {
			return installReceipt{}, errorWithCode(CodeArchiveUnsafe, errors.New("archive path is unsafe"))
		}
		if path.Base(entry.Name) == "SKILL.md" && entry.Name != skill.Entrypoint {
			return installReceipt{}, errorWithCode(CodeArchiveUnsafe, errors.New("archive contains an undeclared Skill entrypoint"))
		}
		if entry.FileInfo().IsDir() || !entry.Mode().IsRegular() || entry.Mode()&os.ModeSymlink != 0 {
			return installReceipt{}, errorWithCode(CodeArchiveUnsafe, errors.New("archive contains a non-regular entry"))
		}
		if _, duplicate := seen[entry.Name]; duplicate {
			return installReceipt{}, errorWithCode(CodeArchiveUnsafe, errors.New("archive contains duplicate paths"))
		}
		foldedName := collisionKey(entry.Name)
		if _, collision := folded[foldedName]; collision {
			return installReceipt{}, errorWithCode(CodeArchiveUnsafe, errors.New("archive paths collide when case-folded"))
		}
		seen[entry.Name] = struct{}{}
		folded[foldedName] = struct{}{}
		if entry.UncompressedSize64 > uint64(skill.Archive.UncompressedSizeBytes) ||
			declaredTotal > uint64(skill.Archive.UncompressedSizeBytes)-entry.UncompressedSize64 {
			return installReceipt{}, errorWithCode(CodeArchiveTooLarge, errors.New("archive expanded size exceeds manifest limit"))
		}
		declaredTotal += entry.UncompressedSize64
		entrypointFound = entrypointFound || entry.Name == skill.Entrypoint
	}
	directories := make(map[string]string)
	for name := range seen {
		for parent := path.Dir(name); parent != "."; parent = path.Dir(parent) {
			if _, fileConflict := seen[parent]; fileConflict {
				return installReceipt{}, errorWithCode(CodeArchiveUnsafe, errors.New("archive file conflicts with a parent directory"))
			}
			if _, foldedConflict := folded[collisionKey(parent)]; foldedConflict {
				return installReceipt{}, errorWithCode(CodeArchiveUnsafe, errors.New("archive path conflicts after normalization"))
			}
			key := collisionKey(parent)
			if existing, collision := directories[key]; collision && existing != parent {
				return installReceipt{}, errorWithCode(CodeArchiveUnsafe, errors.New("archive directories collide after normalization"))
			}
			directories[key] = parent
		}
	}
	if !entrypointFound {
		return installReceipt{}, errorWithCode(CodeArchiveUnsafe, errors.New("archive does not contain SKILL.md"))
	}
	if declaredTotal != uint64(skill.Archive.UncompressedSizeBytes) {
		return installReceipt{}, errorWithCode(CodeArchiveUnsafe, errors.New("archive expanded size does not match manifest"))
	}

	if err := managed.Mkdir(stageName, 0o700); err != nil {
		return installReceipt{}, errorWithCode(CodeInstallFailed, err)
	}
	stage, err := managed.OpenRoot(stageName)
	if err != nil {
		_ = managed.RemoveAll(stageName)
		return installReceipt{}, errorWithCode(CodeInstallFailed, err)
	}
	defer stage.Close()

	receipt := installReceipt{
		SchemaVersion:   1,
		SkillID:         skill.ID,
		RuntimeName:     skill.RuntimeName,
		Version:         skill.Version,
		ArchiveSHA256:   skill.Archive.SHA256,
		CatalogRevision: catalogRevision,
		Files:           make([]receiptFile, 0, len(reader.File)),
	}
	for _, entry := range reader.File {
		if err := extractEntry(stage, entry, &receipt); err != nil {
			_ = managed.RemoveAll(stageName)
			return installReceipt{}, err
		}
	}
	if err := writeReceipt(stage, receipt); err != nil {
		_ = managed.RemoveAll(stageName)
		return installReceipt{}, errorWithCode(CodeInstallFailed, err)
	}
	if err := syncRoot(stage); err != nil {
		_ = managed.RemoveAll(stageName)
		return installReceipt{}, errorWithCode(CodeInstallFailed, err)
	}
	return receipt, nil
}

func extractEntry(stage *os.Root, entry *zip.File, receipt *installReceipt) error {
	directory := path.Dir(entry.Name)
	if directory != "." {
		if err := stage.MkdirAll(directory, 0o700); err != nil {
			return errorWithCode(CodeInstallFailed, err)
		}
	}
	source, err := entry.Open()
	if err != nil {
		return errorWithCode(CodeArchiveUnsafe, err)
	}
	defer source.Close()
	destination, err := stage.OpenFile(entry.Name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return errorWithCode(CodeInstallFailed, err)
	}
	digest := sha256.New()
	written, copyErr := io.Copy(io.MultiWriter(destination, digest), io.LimitReader(source, int64(entry.UncompressedSize64)+1))
	if copyErr == nil && uint64(written) != entry.UncompressedSize64 {
		copyErr = errors.New("archive entry expanded size changed")
	}
	if copyErr == nil {
		var one [1]byte
		if count, readErr := source.Read(one[:]); count != 0 || (readErr != nil && !errors.Is(readErr, io.EOF)) {
			copyErr = errors.New("archive entry exceeds declared size")
		}
	}
	if copyErr == nil {
		copyErr = destination.Sync()
	}
	closeErr := destination.Close()
	if copyErr != nil {
		return errorWithCode(CodeArchiveUnsafe, copyErr)
	}
	if closeErr != nil {
		return errorWithCode(CodeInstallFailed, closeErr)
	}
	receipt.Files = append(receipt.Files, receiptFile{
		Path:   entry.Name,
		Size:   written,
		SHA256: hex.EncodeToString(digest.Sum(nil)),
	})
	return nil
}

func validArchiveEntryName(name string) bool {
	if name == "" || !utf8.ValidString(name) || strings.ContainsRune(name, '\x00') ||
		strings.Contains(name, "\\") || strings.Contains(name, ":") || strings.HasPrefix(name, "/") ||
		filepath.IsAbs(name) || filepath.VolumeName(name) != "" || path.Clean(name) != name || name == "." ||
		len(name) > 512 || name == receiptFileName || name == disabledFileName {
		return false
	}
	for _, component := range strings.Split(name, "/") {
		if component == "" || component == "." || component == ".." || len(component) > 255 {
			return false
		}
		for _, character := range component {
			if character < 0x20 || character == 0x7f {
				return false
			}
		}
	}
	return true
}

func collisionKey(name string) string {
	return cases.Fold().String(norm.NFC.String(name))
}

func syncRoot(root *os.Root) error {
	directory, err := root.Open(".")
	if err != nil {
		return err
	}
	defer directory.Close()
	if err := directory.Sync(); err != nil {
		return fmt.Errorf("sync directory: %w", err)
	}
	return nil
}
