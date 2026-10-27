package marketbroker

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
)

// artifact describes a canonical Connectors development build. This is a
// private deployment manifest, not a product/renderer request or release pin.
type artifact struct {
	SchemaVersion        int    `json:"schemaVersion"`
	CandidateOnly        bool   `json:"candidateOnly"`
	Binary               string `json:"binary"`
	SHA256               string `json:"sha256"`
	SizeBytes            int64  `json:"sizeBytes"`
	CodexSource          string `json:"codexSource"`
	ExternalCallsEnabled bool   `json:"externalCallsEnabled"`
	QualificationOnly    *bool  `json:"qualificationOnly"`
	ProviderProfile      string `json:"providerProfile,omitempty"`
}

func loadArtifact(path string) (artifact, error) {
	var value artifact
	if !filepath.IsAbs(path) {
		return value, ErrArtifact
	}
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() > 16384 {
		return value, ErrArtifact
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return value, ErrArtifact
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&value) != nil || decoder.Decode(new(any)) != io.EOF || !value.CandidateOnly || value.QualificationOnly == nil || *value.QualificationOnly || value.CodexSource != "9154f27d0cdb526a5f5ba0913fe3e2448014b4a1" {
		return artifact{}, ErrArtifact
	}
	if !acceptedDeploymentProfile(value) {
		return artifact{}, ErrArtifact
	}
	if !filepath.IsAbs(value.Binary) || filepath.Clean(value.Binary) != value.Binary || filepath.Base(value.Binary) != "yijie-mcp-worker" || filepath.Base(filepath.Dir(value.Binary)) != value.SHA256 || value.SizeBytes <= 0 || value.SizeBytes > 512*1024*1024 {
		return artifact{}, ErrArtifact
	}
	digest, err := hex.DecodeString(value.SHA256)
	if err != nil || len(digest) != sha256.Size || hex.EncodeToString(digest) != value.SHA256 {
		return artifact{}, ErrArtifact
	}
	info, err = os.Lstat(value.Binary)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0111 == 0 || info.Size() != value.SizeBytes {
		return artifact{}, ErrArtifact
	}
	file, err := os.Open(value.Binary)
	if err != nil {
		return artifact{}, ErrArtifact
	}
	defer file.Close()
	hash := sha256.New()
	if _, err = io.Copy(hash, file); err != nil || hex.EncodeToString(hash.Sum(nil)) != value.SHA256 {
		return artifact{}, ErrArtifact
	}
	return value, nil
}

// Deployment capability is not execution admission. OAuth-only artifacts are
// still usable for account management; only a live, explicitly enabled backend
// in the daily profile may satisfy the worker's per-turn prepare checks.
func acceptedDeploymentProfile(value artifact) bool {
	if value.SchemaVersion == 1 {
		return !value.ExternalCallsEnabled && value.ProviderProfile == ""
	}
	if value.SchemaVersion != 2 || !value.ExternalCallsEnabled {
		return false
	}
	switch value.ProviderProfile {
	case "tushare-oauth-v1", "tushare-daily-v1", "generic-mcp-v1":
		return true
	default:
		return false
	}
}

var ErrArtifact = errors.New("market worker canonical artifact unavailable")
