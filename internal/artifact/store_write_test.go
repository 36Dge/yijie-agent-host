package artifact

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestStoreWriteConflictPreservesOtherArtifacts(t *testing.T) {
	root := filepath.Join(t.TempDir(), "artifact-spool")
	store, err := OpenStore(root, Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	first := Manifest{SessionID: testSession, ArtifactID: testArtifact, Kind: "file", DisplayName: "first.txt", MediaType: "text/plain", Content: []byte("first result")}
	if err := store.Put(first); err != nil {
		t.Fatal(err)
	}
	second := first
	second.ArtifactID = "019fbd88-cbc3-7bf1-934d-7b05cd693f71"
	// An ordinary same-name directory collision, entirely inside the temporary
	// spool: no permission changes, symlinks, or corrupt content.
	conflict := filepath.Join(root, testSession, second.ArtifactID+".content.sealed")
	if err := os.Mkdir(conflict, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := store.Put(second); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("expected write refusal, got %v", err)
	}
	resource, err := store.Get(testSession, first.ArtifactID, Content)
	if err != nil || string(resource.Bytes) != string(first.Content) {
		t.Fatalf("a later write removed the earlier artifact: %v", err)
	}
	if err := os.Remove(conflict); err != nil {
		t.Fatal(err)
	}
	if err := store.Put(second); err != nil {
		t.Fatalf("normal retry failed: %v", err)
	}
}
