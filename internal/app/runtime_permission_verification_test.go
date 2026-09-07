package app

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPermissionVerificationPolicyRequiresLocalMeter(t *testing.T) {
	const policy = "Files in the review directory need explicit human review before writing."
	path := filepath.Join(t.TempDir(), "review-policy.md")
	if err := os.WriteFile(path, []byte(policy), 0o600); err != nil {
		t.Fatal(err)
	}
	value, err := loadPermissionVerificationPolicy(true, "http://127.0.0.1:18083/v1", path)
	if err != nil || value != policy {
		t.Fatalf("ordinary policy was not preserved: %v", err)
	}
	if _, err := loadPermissionVerificationPolicy(false, "http://127.0.0.1:18083/v1", path); err == nil {
		t.Fatal("policy must not enable the local feature")
	}
	if _, err := loadPermissionVerificationPolicy(true, "", path); err == nil {
		t.Fatal("policy must not bypass paid-request metering")
	}
	value, err = loadPermissionVerificationPolicy(false, "", "")
	if err != nil || value != "" {
		t.Fatal("absent policy must leave the default profile unchanged")
	}
}
