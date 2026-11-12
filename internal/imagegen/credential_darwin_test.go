//go:build darwin && cgo

package imagegen

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// With no opt-in this reads a nonexistent temporary scope. The optional local
// probe reads the operator's existing scope without displaying or changing a
// credential. It never invokes Seedream or prints the key.
func TestArkCredentialNonInteractiveRead(t *testing.T) {
	home := os.Getenv("YIJIE_IMAGE_CREDENTIAL_PROBE_HOME")
	if home == "" {
		home = t.TempDir()
	} else if canonical, err := filepath.EvalSymlinks(home); err != nil || !filepath.IsAbs(home) || canonical != home {
		t.Fatal("credential probe requires the exact canonical operator scope")
	}
	start := time.Now()
	key, err := ArkCredential(home)(context.Background())
	readable := err == nil && key != ""
	key = ""
	if err != nil && ErrorCode(err) != ErrorAuthentication {
		t.Fatal("unexpected credential result", ErrorCode(err))
	}
	if time.Since(start) > 5*time.Second {
		t.Fatal("non-interactive query did not return promptly")
	}
	t.Logf("credential_readable=%t; elapsed_ms=%d; no provider request", readable, time.Since(start).Milliseconds())
}
