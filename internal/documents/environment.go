// Package documents verifies application-owned document resources. It never
// executes commands, installs packages, reads user documents or grants paths.
package documents

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	wire "github.com/36Dge/yijie-agent-host/internal/contracts/documentruntime"
)

type Environment struct {
	mu             sync.Mutex
	root, expected string
	state          string
	checkState     bool
	manifest       wire.BundleManifest
	fingerprints   map[string]string
}

func Limits() wire.Limits {
	return wire.Limits{GeneratedFileBytes: 10 << 20, TextSourceBytes: 1 << 20, MaxPages: 50, MaxSheets: 10, MaxCells: 12000, MaxVisualTiles: 32, MaxArtifacts: 12, StepSoftTimeoutSeconds: 180, JobSoftTimeoutSeconds: 900}
}
func Unavailable(code string) wire.EnvironmentResponse {
	return wire.EnvironmentResponse{SchemaVersion: 1, Status: "unavailable", Code: code, Platform: "macos-arm64", Guides: []wire.Guide{}, Limits: Limits()}
}

func New(root, expected string, state ...string) *Environment {
	e := &Environment{root: root, expected: expected}
	if len(state) > 0 {
		e.state = state[0]
		e.checkState = true
	}
	return e
}
func validDigest(value string) bool {
	b, e := hex.DecodeString(value)
	return e == nil && len(b) == 32 && strings.ToLower(value) == value
}

func (e *Environment) Snapshot(ctx context.Context, format string) wire.EnvironmentResponse {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.root == "" || !validDigest(e.expected) {
		return Unavailable("missing")
	}
	if runtime.GOOS != "darwin" || runtime.GOARCH != "arm64" {
		return Unavailable("incompatible")
	}
	if e.checkState {
		canonical, err := filepath.EvalSymlinks(e.state)
		info, statErr := os.Stat(e.state)
		if err != nil || statErr != nil || !filepath.IsAbs(e.state) || canonical != e.state || !info.IsDir() || info.Mode().Perm() != 0700 {
			return Unavailable("not_ready")
		}
	}
	if format != "" && !wire.FormatAllowed(format) {
		return Unavailable("incompatible")
	}
	if err := e.verify(ctx); err != nil {
		return Unavailable("integrity_failed")
	}
	m := e.manifest
	guides := []wire.Guide{}
	for _, g := range m.Guides {
		if format == "" || format == g.Format {
			guides = append(guides, wire.Guide{Format: g.Format, Path: filepath.Join(e.root, g.Path)})
		}
	}
	if len(guides) == 0 {
		return Unavailable("incompatible")
	}
	result := wire.EnvironmentResponse{SchemaVersion: 1, Status: "ready", Code: "ok", BundleId: m.BundleId, Platform: m.Platform, PythonPath: filepath.Join(e.root, m.Python), HelperPath: filepath.Join(e.root, m.Helper), RendererPath: filepath.Join(e.root, m.Renderer), Guides: guides, Limits: m.Limits}
	b, err := json.Marshal(result)
	if err != nil || len(b) > 16<<10 {
		return Unavailable("incompatible")
	}
	return result
}

func local(name string) bool {
	return filepath.IsLocal(name) && filepath.Clean(name) == name && !strings.ContainsAny(name, "\\\x00")
}
func fingerprint(info os.FileInfo) string {
	return fmt.Sprintf("%d:%d:%v", info.Size(), info.Mode(), info.Sys())
}

func (e *Environment) verify(ctx context.Context) error {
	if !filepath.IsAbs(e.root) || filepath.Clean(e.root) != e.root {
		return errors.New("bundle root invalid")
	}
	resolved, err := filepath.EvalSymlinks(e.root)
	if err != nil || resolved != e.root {
		return errors.New("bundle root unavailable")
	}
	p := filepath.Join(e.root, "manifest.json")
	info, err := os.Lstat(p)
	if err != nil || !info.Mode().IsRegular() || info.Size() > 8<<20 {
		return errors.New("manifest unavailable")
	}
	raw, err := os.ReadFile(p)
	if err != nil {
		return err
	}
	sum := sha256.Sum256(raw)
	if hex.EncodeToString(sum[:]) != e.expected {
		return errors.New("manifest mismatch")
	}
	var m wire.BundleManifest
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&m) != nil || decoder.Decode(new(any)) != io.EOF {
		return errors.New("manifest invalid")
	}
	if m.SchemaVersion != 1 || !validDigest(m.BundleId) || m.Platform != "macos-arm64" || m.MinimumMacos != "14.0" || m.PythonVersion != "3.12.16" || m.LibreofficeVersion != "26.8.2" || m.Limits != Limits() || len(m.Files) == 0 || len(m.Files) > 30000 || len(m.Guides) != 9 || len(m.Fonts) != 2 {
		return errors.New("bundle incompatible")
	}
	entries := make(map[string]wire.BundleFile, len(m.Files))
	for _, f := range m.Files {
		if !local(f.Path) || !validDigest(f.Sha256) || f.SizeBytes < 0 || f.SizeBytes > 2<<30 {
			return errors.New("bundle entry invalid")
		}
		if _, ok := entries[f.Path]; ok {
			return errors.New("duplicate bundle entry")
		}
		entries[f.Path] = f
	}
	seen := map[string]bool{}
	for _, g := range m.Guides {
		if !wire.FormatAllowed(g.Format) || seen[g.Format] {
			return errors.New("guide mismatch")
		}
		seen[g.Format] = true
		if f, ok := entries[g.Path]; !ok || f.Kind != "file" {
			return errors.New("guide missing")
		}
	}
	for _, name := range append([]string{m.Python, m.Helper, m.Renderer, m.Libreoffice}, m.Fonts...) {
		if f, ok := entries[name]; !ok || f.Kind != "file" {
			return errors.New("runtime file missing")
		}
	}
	for _, name := range []string{m.Python, m.Renderer, m.Libreoffice} {
		st, er := os.Stat(filepath.Join(e.root, name))
		if er != nil || st.Mode()&0111 == 0 {
			return errors.New("runtime executable unavailable")
		}
	}
	observed := map[string]string{}
	err = filepath.WalkDir(e.root, func(path string, dir os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if dir.IsDir() {
			return nil
		}
		name, er := filepath.Rel(e.root, path)
		if er != nil {
			return er
		}
		if name == "manifest.json" {
			return nil
		}
		f, ok := entries[name]
		if !ok {
			return errors.New("unlisted bundle file")
		}
		st, er := os.Lstat(path)
		if er != nil {
			return er
		}
		stamp := fingerprint(st)
		if e.fingerprints != nil && e.fingerprints[name] == stamp {
			observed[name] = stamp
			return nil
		}
		var actual string
		if f.Kind == "symlink" && st.Mode()&os.ModeSymlink != 0 {
			target, er := os.Readlink(path)
			if er != nil {
				return er
			}
			if target != f.LinkTarget || filepath.IsAbs(target) {
				return errors.New("bundle link mismatch")
			}
			to, er := filepath.EvalSymlinks(path)
			if er != nil || !strings.HasPrefix(to, e.root+string(filepath.Separator)) {
				return errors.New("bundle link outside root")
			}
			value := sha256.Sum256([]byte(target))
			actual = hex.EncodeToString(value[:])
		} else if f.Kind == "file" && st.Mode().IsRegular() && st.Size() == f.SizeBytes {
			file, er := os.Open(path)
			if er != nil {
				return er
			}
			h := sha256.New()
			_, er = io.Copy(h, file)
			closeErr := file.Close()
			if er != nil {
				return er
			}
			if closeErr != nil {
				return closeErr
			}
			actual = hex.EncodeToString(h.Sum(nil))
		} else {
			return errors.New("bundle file changed")
		}
		if actual != f.Sha256 {
			return errors.New("bundle digest mismatch")
		}
		st, er = os.Lstat(path)
		if er != nil {
			return er
		}
		observed[name] = fingerprint(st)
		return nil
	})
	if err != nil {
		return err
	}
	if len(observed) != len(entries) {
		return errors.New("bundle files missing")
	}
	e.manifest = m
	e.fingerprints = observed
	return nil
}
