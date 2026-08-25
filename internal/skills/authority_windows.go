//go:build windows

package skills

import (
	"errors"
	"os"
	"path/filepath"
)

// Windows ownership is enforced by the Desktop-created private App Data ACL.
// This fallback still rejects reparse-point components and writable mode-bit
// projections; the Tauri consumer must resolve the root from the platform App
// Data API rather than accepting a request path.
func validateDirectoryAuthority(value string, requireOwner bool) error {
	for current := value; ; current = filepath.Dir(current) {
		info, err := os.Lstat(current)
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return errors.New("directory authority is invalid")
		}
		if requireOwner && current == value && info.Mode().Perm()&0o077 != 0 {
			return errors.New("directory is not owner-only")
		}
		parent := filepath.Dir(current)
		if parent == current {
			return nil
		}
	}
}
