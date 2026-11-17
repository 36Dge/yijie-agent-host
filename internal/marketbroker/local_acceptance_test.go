package marketbroker

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLocalMarketAcceptanceRequiresItsOwnStorage(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	isolated := filepath.Join(root, ".local", "market-local-acceptance", "ordinary-run", "host-home", "market-worker", "library-home")
	if !ValidLocalAcceptance("ordinary-run", isolated) {
		t.Fatal("isolated namespace refused")
	}
	for _, home := range []string{filepath.Join(root, ".local", "demo-fast", "host-home", "market-worker", "library-home"), filepath.Join(root, ".local", "market-local-acceptance", "different-run", "host-home", "market-worker", "library-home")} {
		if ValidLocalAcceptance("ordinary-run", home) {
			t.Fatal("another namespace accepted")
		}
		if _, err := NewLocalAcceptanceOwner("unused-manifest", home, "ordinary-run"); err == nil {
			t.Fatal("ordinary storage acquired acceptance owner")
		}
	}
	if err := os.MkdirAll(filepath.Dir(filepath.Dir(isolated)), 0700); err != nil {
		t.Fatal(err)
	}
	owner, err := NewLocalAcceptanceOwner("unused-manifest", isolated, "ordinary-run")
	if err != nil || owner.localAcceptance != "ordinary-run" || owner.kimiModelKey != "" || owner.started {
		t.Fatal("acceptance assembled outside intended lifecycle", err)
	}
	if NewOwner("unused-manifest").localAcceptance != "" {
		t.Fatal("ordinary owner inherited acceptance")
	}
	if localAcceptanceArgument() != "--broker-local-acceptance-stdio" {
		t.Fatal("source argument differs")
	}
}
