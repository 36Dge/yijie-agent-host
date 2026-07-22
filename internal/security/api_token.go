package security

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const apiTokenFileName = "api-token"

func LoadOrCreateAPIToken(hostHome string) (string, error) {
	path := filepath.Join(hostHome, apiTokenFileName)
	if token, err := readProtectedToken(path); err == nil {
		return token, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}

	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("generate Agent Host API token: %w", err)
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, os.ErrExist) {
		return readProtectedToken(path)
	}
	if err != nil {
		return "", fmt.Errorf("create Agent Host API token: %w", err)
	}
	if _, err := file.WriteString(token + "\n"); err != nil {
		_ = file.Close()
		return "", fmt.Errorf("write Agent Host API token: %w", err)
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return "", fmt.Errorf("sync Agent Host API token: %w", err)
	}
	if err := file.Close(); err != nil {
		return "", fmt.Errorf("close Agent Host API token: %w", err)
	}
	return token, nil
}

func TokenMatches(expected, authorization string) bool {
	const prefix = "Bearer "
	if expected == "" || !strings.HasPrefix(authorization, prefix) {
		return false
	}
	actual := strings.TrimPrefix(authorization, prefix)
	if len(actual) != len(expected) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(actual), []byte(expected)) == 1
}

func readProtectedToken(path string) (string, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", errors.New("Agent Host API token is not a regular file")
	}
	if info.Mode().Perm()&0o077 != 0 {
		return "", errors.New("Agent Host API token permissions must not allow group or other access")
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	if len(content) > 1024 {
		return "", errors.New("Agent Host API token file exceeds 1 KiB")
	}
	token := strings.TrimSpace(string(content))
	if token == "" || strings.ContainsAny(token, "\r\n\x00") {
		return "", errors.New("Agent Host API token file is invalid")
	}
	return token, nil
}
