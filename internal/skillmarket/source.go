package skillmarket

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	wire "github.com/36Dge/yijie-agent-host/internal/contracts/skillmarketv2"
	legacy "github.com/36Dge/yijie-agent-host/internal/skills"
)

const MaxImportBytes int64 = 64 << 20
const maxFiles = 2048

type sourceFiles map[string][]byte

func digest(data []byte) string { h := sha256.Sum256(data); return hex.EncodeToString(h[:]) }
func canonical(value any) []byte {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(value)
	return bytes.TrimSuffix(b.Bytes(), []byte("\n"))
}
func contentDigest(files sourceFiles) string {
	names := make([]string, 0, len(files))
	for n := range files {
		names = append(names, n)
	}
	sort.Strings(names)
	h := sha256.New()
	for _, n := range names {
		var size [8]byte
		binary.BigEndian.PutUint64(size[:], uint64(len([]byte(n))))
		h.Write(size[:])
		h.Write([]byte(n))
		v := sha256.Sum256(files[n])
		h.Write(v[:])
	}
	return hex.EncodeToString(h.Sum(nil))
}
func ignoredSourcePath(name string) bool {
	for _, p := range strings.Split(name, "/") {
		if p == "__MACOSX" || p == ".DS_Store" {
			return true
		}
	}
	return false
}
func pluginPayload(name string) bool {
	for _, p := range strings.Split(strings.ToLower(name), "/") {
		if p == ".codex-plugin" || p == ".claude-plugin" || p == ".mcp.json" || p == "mcp.json" || p == "plugin.json" || p == "marketplace.json" || p == "hooks.json" {
			return true
		}
	}
	return false
}

// ParseImport only admits ordinary inert files. It reuses the existing managed
// Skill path/collision rules and leaves Markdown/YAML interpretation to native.
func ParseImport(filename string, data []byte) (sourceFiles, error) {
	if len(data) == 0 || int64(len(data)) > MaxImportBytes {
		return nil, fail("import_too_large")
	}
	if strings.EqualFold(filename, "SKILL.md") {
		if len(data) > 1<<20 {
			return nil, fail("import_too_large")
		}
		return sourceFiles{"SKILL.md": append([]byte(nil), data...)}, nil
	}
	if !strings.EqualFold(path.Ext(filename), ".zip") || filepath.Base(filename) != filename {
		return nil, fail("import_format_invalid")
	}
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fail("import_archive_invalid")
	}
	if len(reader.File) > maxFiles*2 {
		return nil, fail("import_too_large")
	}
	files := sourceFiles{}
	seen := map[string]string{}
	directories := map[string]string{}
	var total int64
	for _, entry := range reader.File {
		name := strings.TrimSuffix(entry.Name, "/")
		if !legacy.ValidSourcePath(name) {
			return nil, fail("import_path_invalid")
		}
		if ignoredSourcePath(name) {
			continue
		}
		if entry.Mode()&os.ModeSymlink != 0 || (!entry.FileInfo().IsDir() && !entry.Mode().IsRegular()) {
			return nil, fail("import_entry_invalid")
		}
		key := legacy.SourceCollisionKey(name)
		if old, ok := seen[key]; ok {
			_ = old
			return nil, fail("import_path_collision")
		}
		seen[key] = name
		if entry.FileInfo().IsDir() {
			directories[key] = name
			continue
		}
		if pluginPayload(name) {
			return nil, fail("import_plugin_unsupported")
		}
		if len(files) >= maxFiles || entry.UncompressedSize64 > uint64(MaxImportBytes-total) {
			return nil, fail("import_too_large")
		}
		stream, e := entry.Open()
		if e != nil {
			return nil, fail("import_archive_invalid")
		}
		value, e := io.ReadAll(io.LimitReader(stream, int64(entry.UncompressedSize64)+1))
		closeErr := stream.Close()
		if e != nil || closeErr != nil || uint64(len(value)) != entry.UncompressedSize64 {
			return nil, fail("import_archive_invalid")
		}
		files[name] = value
		total += int64(len(value))
	}
	for n := range files {
		for parent := path.Dir(n); parent != "."; parent = path.Dir(parent) {
			key := legacy.SourceCollisionKey(parent)
			if other, ok := seen[key]; ok {
				if _, isFile := files[other]; isFile {
					return nil, fail("import_path_collision")
				}
			}
			if other, ok := directories[key]; ok && other != parent {
				return nil, fail("import_path_collision")
			}
			directories[key] = parent
		}
	}
	entry := ""
	for n := range files {
		if strings.EqualFold(path.Base(n), "SKILL.md") {
			if entry != "" {
				return nil, fail("import_multiple_skills")
			}
			entry = n
		}
	}
	if entry == "" {
		return nil, fail("import_entrypoint_missing")
	}
	prefix := ""
	if path.Dir(entry) != "." {
		if strings.Count(entry, "/") != 1 {
			return nil, fail("import_layout_invalid")
		}
		prefix = path.Dir(entry) + "/"
	}
	normalized := sourceFiles{}
	for n, v := range files {
		if !strings.HasPrefix(n, prefix) {
			return nil, fail("import_layout_invalid")
		}
		n = strings.TrimPrefix(n, prefix)
		if strings.EqualFold(n, "SKILL.md") {
			n = "SKILL.md"
		}
		normalized[n] = v
	}
	return normalized, nil
}

func sourceMetadata(source wire.CatalogSource) map[string]any {
	return map[string]any{"name": source.PluginName, "skills": "./skills", "mcpServers": map[string]any{}, "interface": map[string]any{"displayName": source.DisplayName, "shortDescription": source.Description, "category": source.Category}}
}
func populateSource(source wire.CatalogSource, files sourceFiles) wire.CatalogSource {
	source.ContentSha256 = contentDigest(files)
	source.SourceRevision = digest(canonical(map[string]any{"recipe": 1, "content": source.ContentSha256, "metadata": sourceMetadata(source)}))
	source.NativePackageVersion = "1.0.0+yj." + source.SourceRevision[:32]
	source.SourcePath = "sources/" + source.PluginName + "/" + source.SourceRevision
	source.Files = []wire.FileEntry{}
	for n, v := range files {
		source.Files = append(source.Files, wire.FileEntry{Path: n, Bytes: int64(len(v)), Sha256: digest(v)})
	}
	sort.Slice(source.Files, func(i, j int) bool { return source.Files[i].Path < source.Files[j].Path })
	if source.RequiredTools == nil {
		source.RequiredTools = []string{}
	}
	return source
}

func readVerifiedFiles(rootPath string, source wire.CatalogSource) (sourceFiles, error) {
	root, err := os.OpenRoot(rootPath)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	files := sourceFiles{}
	var total int64
	for _, entry := range source.Files {
		if !legacy.ValidSourcePath(entry.Path) || entry.Bytes > MaxImportBytes-total {
			return nil, fail("source_invalid")
		}
		info, e := root.Lstat(entry.Path)
		if e != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Size() != entry.Bytes {
			return nil, fail("source_invalid")
		}
		file, e := root.Open(entry.Path)
		if e != nil {
			return nil, e
		}
		value, e := io.ReadAll(io.LimitReader(file, entry.Bytes+1))
		closeErr := file.Close()
		if e != nil || closeErr != nil || int64(len(value)) != entry.Bytes || digest(value) != entry.Sha256 {
			return nil, fail("source_digest_mismatch")
		}
		files[entry.Path] = value
		total += entry.Bytes
	}
	if len(files) != len(source.Files) || contentDigest(files) != source.ContentSha256 {
		return nil, fail("source_digest_mismatch")
	}
	err = fs.WalkDir(root.FS(), ".", func(name string, entry fs.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if name == "." {
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fail("source_invalid")
		}
		if !entry.IsDir() {
			if _, ok := files[name]; !ok {
				return fail("source_invalid")
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return files, nil
}

func syncDirectory(dir string) error {
	file, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer file.Close()
	return file.Sync()
}
func writeDurable(filename string, data []byte) error {
	f, err := os.OpenFile(filename, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	_, e := f.Write(data)
	if e == nil {
		e = f.Sync()
	}
	closeErr := f.Close()
	return errors.Join(e, closeErr)
}
func replaceDurable(filename string, data []byte) error {
	f, err := os.CreateTemp(filepath.Dir(filename), ".skill-catalog-")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	_, err = f.Write(data)
	if err == nil {
		err = f.Sync()
	}
	e := f.Close()
	if err != nil || e != nil {
		return errors.Join(err, e)
	}
	if err = os.Rename(tmp, filename); err != nil {
		return err
	}
	return syncDirectory(filepath.Dir(filename))
}

func materializeSource(marketRoot string, source wire.CatalogSource, files sourceFiles) error {
	expected := populateSource(source, files)
	if expected.SourceRevision != source.SourceRevision || expected.NativePackageVersion != source.NativePackageVersion || expected.ContentSha256 != source.ContentSha256 {
		return fail("source_digest_mismatch")
	}
	relative := "sources/" + source.PluginName + "/" + source.SourceRevision
	if !legacy.ValidSourcePath(relative) {
		return fail("source_invalid")
	}
	dest := filepath.Join(marketRoot, filepath.FromSlash(relative))
	if _, err := os.Lstat(dest); err == nil {
		_, err = readVerifiedFiles(filepath.Join(dest, "skills/main"), source)
		if err != nil {
			return err
		}
		wrapper, e := os.ReadFile(filepath.Join(dest, ".codex-plugin/plugin.json"))
		metadata := sourceMetadata(source)
		metadata["version"] = source.NativePackageVersion
		if e != nil || !bytes.Equal(wrapper, canonical(metadata)) {
			return fail("source_digest_mismatch")
		}
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	parent := filepath.Dir(dest)
	if siblings, err := os.ReadDir(parent); err == nil {
		for _, sibling := range siblings {
			revision := sibling.Name()
			if len(revision) == 64 && revision != source.SourceRevision && revision[:32] == source.SourceRevision[:32] {
				return fail("source_revision_collision")
			}
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.MkdirAll(parent, 0700); err != nil {
		return err
	}
	stage, err := os.MkdirTemp(parent, ".source-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	metadata := sourceMetadata(source)
	metadata["version"] = source.NativePackageVersion
	if err = os.MkdirAll(filepath.Join(stage, ".codex-plugin"), 0700); err != nil {
		return err
	}
	if err = writeDurable(filepath.Join(stage, ".codex-plugin/plugin.json"), canonical(metadata)); err != nil {
		return err
	}
	for n, value := range files {
		target := filepath.Join(stage, "skills/main", filepath.FromSlash(n))
		if err = os.MkdirAll(filepath.Dir(target), 0700); err != nil {
			return err
		}
		if err = writeDurable(target, value); err != nil {
			return err
		}
	}
	err = filepath.WalkDir(stage, func(p string, d fs.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if d.IsDir() {
			return syncDirectory(p)
		}
		return nil
	})
	if err != nil {
		return err
	}
	if err = os.Rename(stage, dest); err != nil {
		return err
	}
	return syncDirectory(parent)
}
