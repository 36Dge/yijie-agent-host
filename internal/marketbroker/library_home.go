package marketbroker

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
)

var ErrLibraryHome = errors.New("market worker owner storage is unavailable")

func prepareLibraryHome(home string) error {
	if !filepath.IsAbs(home) || filepath.Clean(home) != home || filepath.Base(home) != "library-home" || filepath.Base(filepath.Dir(home)) != "market-worker" {
		return ErrLibraryHome
	}
	root := filepath.Dir(filepath.Dir(home))
	canonical, err := filepath.EvalSymlinks(root)
	if err != nil || canonical != root {
		return ErrLibraryHome
	}
	for i, path := range []string{root, filepath.Dir(home), home} {
		info, err := os.Lstat(path)
		if errors.Is(err, os.ErrNotExist) && i != 0 {
			if err = os.Mkdir(path, 0700); err != nil && !errors.Is(err, os.ErrExist) {
				return ErrLibraryHome
			}
			info, err = os.Lstat(path)
		}
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0077 != 0 {
			return ErrLibraryHome
		}
		owner, ok := info.Sys().(*syscall.Stat_t)
		if !ok || int(owner.Uid) != os.Geteuid() {
			return ErrLibraryHome
		}
	}
	return nil
}
