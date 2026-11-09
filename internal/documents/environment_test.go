package documents

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	wire "github.com/36Dge/yijie-agent-host/internal/contracts/documentruntime"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDocumentEnvironmentRealBundle(t *testing.T) {
	root := os.Getenv("YIJIE_DOCUMENT_TEST_BUNDLE")
	if root == "" {
		t.Skip("requires canonical document bundle")
	}
	raw, err := os.ReadFile(filepath.Join(root, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(raw)
	env := New(root, fmt.Sprintf("%x", sum))
	started := time.Now()
	all := env.Snapshot(context.Background(), "")
	t.Log("cold bundle verification", time.Since(started))
	if all.Status != "ready" || len(all.Guides) != 9 {
		t.Fatalf("bundle not ready: %s/%s", all.Status, all.Code)
	}
	one := env.Snapshot(context.Background(), "docx")
	if one.Status != "ready" || len(one.Guides) != 1 || one.Guides[0].Format != "docx" {
		t.Fatal("format guide unavailable")
	}
	if result := New("", "").Snapshot(context.Background(), ""); result.Code != "missing" || result.PythonPath != "" {
		t.Fatal("unconfigured environment did not remain unavailable")
	}
}
func TestDocumentInputNormalReceiptAndExpiry(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	const operation = "019fbd88-cbc3-7bf1-934d-7b05cd693f53"
	const attachment = "019fbd88-cbc3-7bf1-934d-7b05cd693f70"
	data := []byte("Normal document input.\n")
	sum := fmt.Sprintf("%x", sha256.Sum256(data))
	folder := filepath.Join(root, "inputs", operation)
	if err := os.MkdirAll(folder, 0700); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(folder, attachment+".txt")
	if err := os.WriteFile(p, data, 0600); err != nil {
		t.Fatal(err)
	}
	receipt := wire.InputReceipt{SchemaVersion: 1, OperationId: operation, AttachmentId: attachment, Sha256: sum, SizeBytes: int64(len(data)), Format: "txt", ExpiresAt: time.Now().Add(time.Hour).Unix()}
	raw, _ := json.Marshal(receipt)
	if err := os.WriteFile(filepath.Join(folder, attachment+".receipt.json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	actual, err := InputReference(root, operation, attachment, sum, int64(len(data)))
	if err != nil || actual != p {
		t.Fatal("source binding unavailable", err)
	}
	// An ordinary expired receipt, no filesystem damage or altered executable.
	receipt.ExpiresAt = time.Now().Add(-time.Hour).Unix()
	raw, _ = json.Marshal(receipt)
	if err := os.WriteFile(filepath.Join(folder, attachment+".receipt.json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = InputReference(root, operation, attachment, sum, int64(len(data))); err == nil {
		t.Fatal("expired input remained usable")
	}
}
