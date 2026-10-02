package app

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

// The key stays in the native Host boundary. Only the explicit owner-only
// source is read; source errors never echo key material or file contents.
func loadKimiAPIKey() (string, error) {
	direct, file := os.Getenv("YIJIE_KIMI_API_KEY"), os.Getenv("YIJIE_KIMI_API_KEY_FILE")
	if direct != "" && file != "" {
		return "", errors.New("configure one Kimi key source")
	}
	if direct != "" {
		if len(direct) > 16<<10 || strings.TrimSpace(direct) != direct || strings.ContainsAny(direct, "\x00\r\n") {
			return "", errors.New("invalid Kimi key")
		}
		return direct, nil
	}
	if file == "" {
		return "", nil
	}
	if !filepath.IsAbs(file) {
		return "", errors.New("Kimi key file must be absolute")
	}
	info, e := os.Lstat(file)
	if e != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0 || info.Size() > 16<<10 {
		return "", errors.New("Kimi key file unavailable or not owner-only")
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok || st.Uid != uint32(os.Geteuid()) || st.Nlink != 1 {
		return "", errors.New("Kimi key ownership invalid")
	}
	f, e := os.OpenFile(file, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if e != nil {
		return "", errors.New("Kimi key unavailable")
	}
	defer f.Close()
	opened, e := f.Stat()
	if e != nil || !os.SameFile(info, opened) || opened.Mode().Perm()&0o077 != 0 {
		return "", errors.New("Kimi key source changed")
	}
	b, e := io.ReadAll(io.LimitReader(f, (16<<10)+1))
	if e != nil || len(b) > 16<<10 {
		return "", errors.New("Kimi key unavailable")
	}
	key := strings.TrimSpace(string(b))
	if key == "" || strings.ContainsAny(key, "\x00\r\n") {
		return "", errors.New("invalid Kimi key")
	}
	return key, nil
}
