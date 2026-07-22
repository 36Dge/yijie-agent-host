package security

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadOrCreateAPITokenPersistsProtectedToken(t *testing.T) {
	home := t.TempDir()
	first, err := LoadOrCreateAPIToken(home)
	if err != nil {
		t.Fatal(err)
	}
	second, err := LoadOrCreateAPIToken(home)
	if err != nil {
		t.Fatal(err)
	}
	if first == "" || first != second {
		t.Fatal("API token was not persisted")
	}
	if !TokenMatches(first, "Bearer "+first) || TokenMatches(first, "Bearer wrong") {
		t.Fatal("constant-time bearer token comparison returned the wrong result")
	}
	info, err := os.Stat(filepath.Join(home, apiTokenFileName))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o077 != 0 {
		t.Fatalf("API token is accessible to group/other: %o", info.Mode().Perm())
	}
}

func TestLoadOrCreateAPITokenRejectsLoosePermissions(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, apiTokenFileName)
	if err := os.WriteFile(path, []byte("secret\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadOrCreateAPIToken(home); err == nil {
		t.Fatal("expected loose token permissions to fail")
	}
}
