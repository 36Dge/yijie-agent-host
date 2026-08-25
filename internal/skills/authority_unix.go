//go:build unix

package skills

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
)

// validateDirectoryAuthority prevents a path string handed to Runtime from
// being rebound to an attacker-controlled inode while Host continues using an
// already-open os.Root. Writable ancestors are accepted only when sticky-bit
// protection applies to a child owned by this process.
func validateDirectoryAuthority(value string, requireOwner bool) error {
	current := value
	currentInfo, err := os.Lstat(current)
	if err != nil || !currentInfo.IsDir() || currentInfo.Mode()&os.ModeSymlink != 0 {
		return errors.New("directory authority is invalid")
	}
	currentUID, ok := fileOwnerUID(currentInfo)
	if !ok || requireOwner && currentUID != uint32(os.Geteuid()) {
		return errors.New("directory is not owned by the current user")
	}

	for {
		parent := filepath.Dir(current)
		if parent == current {
			return nil
		}
		parentInfo, err := os.Lstat(parent)
		if err != nil || !parentInfo.IsDir() || parentInfo.Mode()&os.ModeSymlink != 0 {
			return errors.New("directory ancestor authority is invalid")
		}
		parentUID, ok := fileOwnerUID(parentInfo)
		if !ok {
			return errors.New("directory ancestor owner is unavailable")
		}
		if parentInfo.Mode().Perm()&0o022 != 0 {
			sticky := parentInfo.Mode()&os.ModeSticky != 0
			protectedChild := currentUID == uint32(os.Geteuid()) || parentUID == uint32(os.Geteuid())
			if !sticky || !protectedChild {
				return errors.New("directory ancestor can be replaced by another user")
			}
		}
		current = parent
		currentInfo = parentInfo
		currentUID = parentUID
	}
}

func fileOwnerUID(info os.FileInfo) (uint32, bool) {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, false
	}
	return stat.Uid, true
}
