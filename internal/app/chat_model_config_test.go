package app

import (
	"os"
	"path/filepath"
	"testing"
)

func TestChatModelCredentialSources(t *testing.T) {
	t.Setenv("YIJIE_KIMI_API_KEY", "")
	t.Setenv("YIJIE_KIMI_API_KEY_FILE", "")
	if key, err := loadKimiAPIKey(); err != nil || key != "" {
		t.Fatal("missing optional model configuration must remain unavailable")
	}
	p := filepath.Join(t.TempDir(), "provider-key")
	if os.WriteFile(p, []byte("declared-local-fixture\n"), 0600) != nil {
		t.Fatal("cannot create owner-only fixture")
	}
	t.Setenv("YIJIE_KIMI_API_KEY_FILE", p)
	if key, err := loadKimiAPIKey(); err != nil || key != "declared-local-fixture" {
		t.Fatal("owner-only source failed")
	}
	t.Setenv("YIJIE_KIMI_API_KEY", "another-declared-fixture")
	if _, err := loadKimiAPIKey(); err == nil {
		t.Fatal("ambiguous credential source accepted")
	}
}
