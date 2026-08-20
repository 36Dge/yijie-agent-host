package artifact

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const (
	testSession  = "019fbd88-cbc3-7bf1-934d-7b05cd693f53"
	testArtifact = "019fbd88-cbc3-7bf1-934d-7b05cd693f70"
	testAck      = "019fbd88-cbc3-7bf1-934d-7b05cd693f80"
)

func TestStoreEncryptsReadsAcknowledgesAndExpires(t *testing.T) {
	now := time.Date(2026, time.August, 20, 2, 0, 0, 0, time.UTC)
	root := filepath.Join(t.TempDir(), "artifact-spool")
	store, err := OpenStore(root, Options{TTL: time.Hour, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	content := []byte("safe synthetic content")
	digest := sha256.Sum256(content)
	if err := store.Put(Manifest{
		SessionID: testSession, ArtifactID: testArtifact, Kind: "file", DisplayName: "result.txt",
		MediaType: "text/plain", Content: content, StagedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	sealedPath := filepath.Join(root, testSession, testArtifact+".content.sealed")
	sealed, err := os.ReadFile(sealedPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(sealed), string(content)) {
		t.Fatal("plaintext escaped into the spool")
	}
	if info, err := os.Stat(sealedPath); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("spool file is not owner-only: info=%v err=%v", info, err)
	}
	resource, err := store.Get(testSession, testArtifact, Content)
	if err != nil || string(resource.Bytes) != string(content) || resource.SHA256 != fmt.Sprintf("%x", digest) {
		t.Fatalf("resource mismatch: %+v err=%v", resource, err)
	}
	request := Acknowledgement{AckID: testAck, SizeBytes: int64(len(content)), SHA256: resource.SHA256, LocalCommittedAt: now.Add(time.Minute)}
	receipt, err := store.Acknowledge(testSession, testArtifact, request)
	if err != nil || receipt.CleanupStatus != "completed" {
		t.Fatalf("ack failed: %+v err=%v", receipt, err)
	}
	if _, err := os.Stat(sealedPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("ack did not remove encrypted staging: %v", err)
	}
	replayed, err := store.Acknowledge(testSession, testArtifact, request)
	if err != nil || replayed != receipt {
		t.Fatalf("ack replay mismatch: %+v err=%v", replayed, err)
	}
	conflict := request
	conflict.SizeBytes++
	if _, err := store.Acknowledge(testSession, testArtifact, conflict); !errors.Is(err, ErrAckConflict) {
		t.Fatalf("expected ack conflict, got %v", err)
	}
	if _, err := store.Get(testSession, testArtifact, Content); !errors.Is(err, ErrExpired) {
		t.Fatalf("expected expired tombstone, got %v", err)
	}
	now = now.Add(2 * time.Hour)
	store.CleanupExpired()
	if _, err := store.Get(testSession, testArtifact, Content); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected tombstone cleanup, got %v", err)
	}
}

func TestStoreRejectsTraversalLimitsAndCrossSessionReads(t *testing.T) {
	root := filepath.Join(t.TempDir(), "artifact-spool")
	store, err := OpenStore(root, Options{SessionLimit: 8, GlobalLimit: 8})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	invalid := Manifest{SessionID: testSession, ArtifactID: testArtifact, Kind: "file", DisplayName: "../secret", MediaType: "text/plain", Content: []byte("x")}
	if err := store.Put(invalid); !errors.Is(err, ErrInvalid) {
		t.Fatalf("expected traversal rejection, got %v", err)
	}
	invalid.DisplayName = "safe.txt"
	invalid.Content = []byte("123456789")
	if err := store.Put(invalid); !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("expected capacity rejection, got %v", err)
	}
	invalid.Content = []byte("safe")
	if err := store.Put(invalid); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Get("019fbd88-cbc3-7bf1-934d-7b05cd693f54", testArtifact, Content); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-session read leaked existence: %v", err)
	}
}

func TestOpenStoreClearsUnrecoverablePreviousProcessSpool(t *testing.T) {
	root := filepath.Join(t.TempDir(), "artifact-spool")
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	stale := filepath.Join(root, "stale.sealed")
	if err := os.WriteFile(stale, []byte("old process ciphertext"), 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := OpenStore(root, Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if _, err := os.Stat(stale); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("stale spool survived restart cleanup: %v", err)
	}
}

func TestIntegrityFailureRevokesResource(t *testing.T) {
	root := filepath.Join(t.TempDir(), "artifact-spool")
	store, err := OpenStore(root, Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.Put(Manifest{
		SessionID: testSession, ArtifactID: testArtifact, Kind: "file", DisplayName: "safe.txt",
		MediaType: "text/plain", Content: []byte("verified"),
	}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, testSession, testArtifact+".content.sealed")
	sealed, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	sealed[len(sealed)-1] ^= 0xff
	if err := os.WriteFile(path, sealed, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Get(testSession, testArtifact, Content); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("expected integrity failure, got %v", err)
	}
	if _, err := store.Get(testSession, testArtifact, Content); !errors.Is(err, ErrExpired) {
		t.Fatalf("integrity failure did not revoke resource: %v", err)
	}
}
