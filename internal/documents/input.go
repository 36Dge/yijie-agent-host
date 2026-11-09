package documents

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	wire "github.com/36Dge/yijie-agent-host/internal/contracts/documentruntime"
	"os"
	"path/filepath"
	"time"
)

// InputReference consumes the private Native copy only after the request's
// existing session and idempotency checks. No new HTTP byte/path authority.
func InputReference(root, operation, attachment, digest string, size int64) (string, error) {
	if !filepath.IsAbs(root) || !local(operation) || !local(attachment) || !validDigest(digest) {
		return "", errors.New("document input unavailable")
	}
	folder := filepath.Join(root, "inputs", operation)
	resolved, err := filepath.EvalSymlinks(folder)
	if err != nil || resolved != folder {
		return "", errors.New("document input unavailable")
	}
	metadata := filepath.Join(folder, attachment+".receipt.json")
	stat, err := os.Lstat(metadata)
	if err != nil || !stat.Mode().IsRegular() || stat.Size() > 4096 {
		return "", errors.New("document input unavailable")
	}
	data, err := os.ReadFile(metadata)
	if err != nil {
		return "", err
	}
	var receipt wire.InputReceipt
	if json.Unmarshal(data, &receipt) != nil || receipt.SchemaVersion != 1 || receipt.OperationId != operation || receipt.AttachmentId != attachment || receipt.Sha256 != digest || receipt.SizeBytes != size || receipt.ExpiresAt <= time.Now().Unix() || !wire.FormatAllowed(receipt.Format) {
		return "", errors.New("document input receipt mismatch")
	}
	path := filepath.Join(folder, attachment+"."+receipt.Format)
	stat, err = os.Lstat(path)
	if err != nil || !stat.Mode().IsRegular() || stat.Size() != size || size <= 0 || size > 10<<20 {
		return "", errors.New("document input unavailable")
	}
	bytes, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(bytes)
	if hex.EncodeToString(sum[:]) != digest {
		return "", errors.New("document input changed")
	}
	return path, nil
}
