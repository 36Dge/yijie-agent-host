package app

import (
	"encoding/json"
	"github.com/google/uuid"
	"os"
	"path/filepath"
	"testing"
)

func TestFEAT155DraftCandidateConfigIsNativeAndOrdinaryDefaultStaysOff(t *testing.T) {
	t.Setenv(ScheduledCandidateEnvironment, "")
	got, err := loadScheduledCandidate("", "", "", 0)
	if err != nil || got != nil {
		t.Fatal("ordinary profile changed", err)
	}
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	owner, tenant, nonce := uuid.NewString(), uuid.NewString(), uuid.NewString()
	host, runtime, workspace := filepath.Join(root, "host"), filepath.Join(root, "runtime"), filepath.Join(root, "chat", "scheduled-workspaces", tenant, owner)
	for _, dir := range []string{host, runtime, workspace} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	value := ScheduledCandidateConfig{SchemaVersion: 1, OwnerUserID: owner, TenantID: tenant, WorkspaceRoot: workspace}
	bytes, _ := json.Marshal(value)
	t.Setenv(ScheduledCandidateEnvironment, string(bytes))
	t.Setenv("YIJIE_ENV", "local")
	t.Setenv("YIJIE_LOCAL_PROFILE", "demo_fast")
	got, err = loadScheduledCandidate(host, runtime, nonce, os.Getppid())
	if err != nil || got == nil || got.WorkspaceRoot != workspace {
		t.Fatal(got, err)
	}
	if _, err = loadScheduledCandidate(host, runtime, nonce, 0); err == nil {
		t.Fatal("unmanaged candidate accepted")
	}
	if _, err = loadScheduledCandidate(host, host, nonce, os.Getppid()); err == nil {
		t.Fatal("overlapping homes accepted")
	}
	value.OwnerUserID = uuid.NewString()
	bytes, _ = json.Marshal(value)
	t.Setenv(ScheduledCandidateEnvironment, string(bytes))
	if _, err = loadScheduledCandidate(host, runtime, nonce, os.Getppid()); err == nil {
		t.Fatal("scope mismatch accepted")
	}
}
