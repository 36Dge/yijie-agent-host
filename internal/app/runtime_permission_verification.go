package app

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

// This input belongs to the local, metered verification entrypoint. It never
// changes the normal native risk policy or enables a synthetic review producer.
func loadPermissionVerificationPolicy(enabled bool, meterURL, path string) (string, error) {
	if path == "" {
		return "", nil
	}
	if !enabled || meterURL != "http://127.0.0.1:18083/v1" || !filepath.IsAbs(path) {
		return "", errors.New("verification review policy requires the exact local metered profile and an absolute file path")
	}
	file, err := os.Open(path)
	if err != nil {
		return "", errors.New("verification review policy is unavailable")
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return "", errors.New("verification review policy must be a regular file")
	}
	const maxPolicyBytes = 64 << 10
	content, err := io.ReadAll(io.LimitReader(file, maxPolicyBytes+1))
	if err != nil || len(content) > maxPolicyBytes || !utf8.Valid(content) || strings.TrimSpace(string(content)) == "" {
		return "", errors.New("verification review policy must be non-empty UTF-8 within 64 KiB")
	}
	return string(content), nil
}
