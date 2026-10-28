package artifact

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"io"
	"path"
	"strings"
)

// Mirror the existing Native OOXML admission limits without extracting files.
// The original bytes stay in the existing encrypted spool.
func validOfficeDocument(content []byte, mainPart, mainType string) bool {
	archive, err := zip.NewReader(bytes.NewReader(content), int64(len(content)))
	if err != nil || len(archive.File) == 0 || len(archive.File) > 512 {
		return false
	}
	seen := map[string]bool{}
	total := uint64(0)
	mainFound := false
	typeFound := false
	for _, f := range archive.File {
		if f.FileInfo().IsDir() {
			continue
		}
		name := f.Name
		if seen[name] || strings.Contains(strings.ToLower(name), "vbaproject") || strings.HasSuffix(strings.ToLower(name), ".bin") || (f.Method != zip.Store && f.Method != zip.Deflate) || name == "" || strings.HasPrefix(name, "/") || path.Clean(name) != name || strings.ContainsAny(name, "\\\x00") || strings.HasPrefix(name, "../") || !f.Mode().IsRegular() {
			return false
		}
		seen[name] = true
		if f.UncompressedSize64 > 8<<20 || f.UncompressedSize64 > 100*max(f.CompressedSize64, 1) {
			return false
		}
		total += f.UncompressedSize64
		if total > 32<<20 {
			return false
		}
		reader, err := f.Open()
		if err != nil {
			return false
		}
		raw, err := io.ReadAll(io.LimitReader(reader, (8<<20)+1))
		closeErr := reader.Close()
		if err != nil || closeErr != nil || uint64(len(raw)) != f.UncompressedSize64 {
			return false
		}
		if name == mainPart {
			mainFound = len(raw) > 0
		}
		if name == "[Content_Types].xml" {
			decoder := xml.NewDecoder(bytes.NewReader(raw))
			depth := 0
			for {
				token, e := decoder.Token()
				if e == io.EOF {
					break
				}
				if e != nil {
					return false
				}
				switch t := token.(type) {
				case xml.StartElement:
					depth++
					if depth > 64 {
						return false
					}
					if t.Name.Local == "Override" {
						part, kind := "", ""
						for _, a := range t.Attr {
							if a.Name.Local == "PartName" {
								part = a.Value
							}
							if a.Name.Local == "ContentType" {
								kind = a.Value
							}
						}
						if part == "/"+mainPart && kind == mainType {
							typeFound = true
						}
					}
				case xml.EndElement:
					depth--
				}
			}
		}
	}
	return mainFound && typeFound
}
