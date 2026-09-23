package app

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
)

const ScheduledCandidateEnvironment = "YIJIE_SCHEDULED_CANDIDATE"

// Private native launch descriptor. It selects compatible storage and the
// native-owned directory root, never runtime permission or dispatch authority.
type ScheduledCandidateConfig struct {
	SchemaVersion int    `json:"schema_version"`
	OwnerUserID   string `json:"owner_user_id"`
	TenantID      string `json:"tenant_id"`
	WorkspaceRoot string `json:"workspace_root"`
}

func loadScheduledCandidate(hostHome, codexHome, nonce string, parentPID int) (*ScheduledCandidateConfig, error) {
	raw := os.Getenv(ScheduledCandidateEnvironment)
	if raw == "" {
		return nil, nil
	}
	invalid := errors.New("scheduled native candidate configuration is invalid")
	if len(raw) > 4096 || !scheduledRecoveryEnabled() || parentPID == 0 || !isCanonicalUUID(nonce) {
		return nil, invalid
	}
	var value ScheduledCandidateConfig
	decoder := json.NewDecoder(bytes.NewBufferString(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&value) != nil || decoder.Decode(new(any)) != io.EOF || value.SchemaVersion != 1 || !isCanonicalUUID(value.OwnerUserID) || !isCanonicalUUID(value.TenantID) {
		return nil, invalid
	}
	for _, dir := range []string{value.WorkspaceRoot, hostHome, codexHome} {
		canonical, err := filepath.EvalSymlinks(dir)
		if err != nil || !filepath.IsAbs(dir) || filepath.Clean(dir) != dir || canonical != dir {
			return nil, invalid
		}
		_, err = validateOwnerOnlyDirectoryAuthority(dir, "scheduled candidate directory")
		if err != nil {
			return nil, invalid
		}
	}
	if filepath.Base(value.WorkspaceRoot) != value.OwnerUserID || filepath.Base(filepath.Dir(value.WorkspaceRoot)) != value.TenantID || filepath.Base(filepath.Dir(filepath.Dir(value.WorkspaceRoot))) != "scheduled-workspaces" {
		return nil, invalid
	}
	for _, home := range []string{hostHome, codexHome} {
		if withinDirectory(home, value.WorkspaceRoot) || withinDirectory(value.WorkspaceRoot, home) {
			return nil, invalid
		}
	}
	if withinDirectory(hostHome, codexHome) || withinDirectory(codexHome, hostHome) {
		return nil, invalid
	}
	return &value, nil
}

func withinDirectory(parent, child string) bool {
	relative, err := filepath.Rel(parent, child)
	return err == nil && (relative == "." || (relative != ".." && len(relative) > 0 && relative[0] != '/' && !bytes.HasPrefix([]byte(relative), []byte(".."+string(os.PathSeparator)))))
}
