package artifact

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDocumentStatusUsesNativeAckBeforeSpoolExpiry(t *testing.T) {
	now := time.Now()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	root = filepath.Join(root, "artifact-spool")
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	s, err := OpenStore(root, Options{Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if got := s.Status(testSession, testArtifact); got != "unknown" {
		t.Fatal(got)
	}
	if err := s.Put(Manifest{SessionID: testSession, ArtifactID: testArtifact, Kind: "file", DisplayName: "document.txt", MediaType: "text/plain", Content: []byte("normal content"), StagedAt: now}); err != nil {
		t.Fatal(err)
	}
	if got := s.Status(testSession, testArtifact); got != "staged" {
		t.Fatal(got)
	}
	data, err := s.Get(testSession, testArtifact, Content)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Acknowledge(testSession, testArtifact, Acknowledgement{AckID: testAck, SHA256: data.SHA256, SizeBytes: int64(len(data.Bytes)), LocalCommittedAt: now}); err != nil {
		t.Fatal(err)
	}
	if got := s.Status(testSession, testArtifact); got != "committed" {
		t.Fatal("ACK lost after normal spool cleanup", got)
	}
	if got := s.Status("019fbd88-cbc3-7bf1-934d-7b05cd693f54", testArtifact); got != "unknown" {
		t.Fatal("unrelated session returned a status")
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if got := s.Status(testSession, testArtifact); got != "unknown" {
		t.Fatal("closed generation returned committed")
	}
}
