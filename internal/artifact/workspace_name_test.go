package artifact

import (
	"strings"
	"testing"
)

func TestWorkspaceDisplayNameAndSaveBudget(t *testing.T) {
	for _, tc := range []struct{ hint, file, expected string }{
		{"百度二维码（https://www.baidu.com）", "code.png", "code.png"},
		{"计划（11/23）", "计划.pptx", "计划.pptx"},
		{"  Cafe\u0301 文档  ", "report.docx", "Café 文档"},
		{"", "Cafe\u0301.docx", "Café.docx"},
		{"Version 1.2", "source.pdf", "Version 1.2"},
		{strings.Repeat("图", 84), "file.png", "file.png"},
		{strings.Repeat("图", 83), "file.png", strings.Repeat("图", 83)},
		{strings.Repeat("a", 251) + ".htm", "index.html", "index.html"},
		{"", "报价 \"A\".markdown", "artifact.md"},
		{"", "notes.HTM", "notes.HTM"},
		{"图.JPEG", "photo.jpg", "图.JPEG"},
	} {
		if got := WorkspaceDisplayName(tc.hint, tc.file); got != tc.expected {
			t.Errorf("%q and %q: got %q, want %q", tc.hint, tc.file, got, tc.expected)
		}
	}
}
