package codex

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestFEAT164NativeHistoryWriterFence(t *testing.T) {
	home := canonicalOwnedTempDir(t)
	a := acquireTestCodexHomeAuthority(t, home)
	catalog, err := kimiK31Catalog()
	if err != nil {
		t.Fatal(err)
	}
	prior := kimiK31PriorConfig(home)
	writeS1File(t, home, kimiK31CatalogName, catalog)
	writeS1File(t, home, kimiK31ConfigName, prior)
	trust := []byte("# ordinary user configuration retained\n")
	writeS1File(t, home, "config.toml", trust)
	oldArgs, err := nativeConfigArguments(prior)
	if err != nil {
		t.Fatal(err)
	}
	newArgs, err := prepareKimiK31Layer(a)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(oldArgs, newArgs) {
		t.Fatal("writer fence changed native options")
	}
	current := kimiK31Config(home)
	for name, want := range map[string][]byte{kimiK31ConfigName: current, kimiK31CatalogName: catalog, "config.toml": trust} {
		got, err := os.ReadFile(filepath.Join(home, name))
		if err != nil || !bytes.Equal(got, want) {
			t.Fatal("managed upgrade changed unrelated content", name, err)
		}
	}
	// This is the unchanged preflight + exact allowed bytes used by the retained
	// r2 Host, not an old executable pointed at new history or a relaxed policy.
	if _, err := a.preflightManagedFile(kimiK31ConfigName, prior, prior); err == nil {
		t.Fatal("retained writer could replace the new revision")
	}
	if _, err := prepareKimiK31Layer(a); err != nil {
		t.Fatal("normal new-writer reopen", err)
	}
	m := NewManager(Config{ChatModelsEnabled: true, KimiK31Candidate: true}, nil)
	if err := m.validateSelectedChatModelLayer(a); err != nil {
		t.Fatal(err)
	}
}
