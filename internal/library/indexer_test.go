package library

import (
	"archive/zip"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestKindFor(t *testing.T) {
	cases := map[string]string{
		"photo.png": "media", "voice.mp3": "media", "clip.mp4": "media",
		"report.pdf": "document", "notes.md": "document", "data.csv": "document",
		"main.go": "file", "app.tsx": "file", "unknown.bin": "",
	}
	for name, want := range cases {
		kind, _ := kindFor(name)
		if kind != want {
			t.Fatalf("%s: kind=%q want %q", name, kind, want)
		}
	}
}

func TestIsTextMime(t *testing.T) {
	if !isTextMime("text/markdown") || !isTextMime("application/json") {
		t.Fatal("text mime detection failed")
	}
	if isTextMime("image/png") {
		t.Fatal("image must not be treated as inline text")
	}
	// Word's type ends in "+xml" but the file is a zip archive; reading it as
	// text put NUL bytes into Postgres and the row was never created.
	if isTextMime("application/vnd.openxmlformats-officedocument.wordprocessingml.document") {
		t.Fatal("docx must not be treated as inline text")
	}
}

func TestInlineTextExtractsDocxAndRejectsBinary(t *testing.T) {
	dir := t.TempDir()
	docx := filepath.Join(dir, "开题报告.docx")
	file, err := os.Create(docx)
	if err != nil {
		t.Fatal(err)
	}
	writer := zip.NewWriter(file)
	part, _ := writer.Create("word/document.xml")
	_, _ = part.Write([]byte(`<w:document xmlns:w="w"><w:body><w:p><w:r><w:t>研究背景</w:t></w:r></w:p></w:body></w:document>`))
	_ = writer.Close()
	_ = file.Close()
	info, _ := os.Stat(docx)
	_, mimeType := kindFor(docx)
	if got := inlineText(docx, mimeType, info.Size()); got != "研究背景" {
		t.Fatalf("docx text = %q", got)
	}

	binary := filepath.Join(dir, "data.txt")
	if err := os.WriteFile(binary, []byte("abc\x00def"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := inlineText(binary, "text/plain", 7); got != "" {
		t.Fatalf("binary content was inlined: %q", got)
	}
}

func TestSkipScanDir(t *testing.T) {
	root := t.TempDir()
	mk := func(rel string, files ...string) string {
		dir := filepath.Join(root, rel)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		for _, name := range files {
			if strings.HasSuffix(name, "/") {
				_ = os.MkdirAll(filepath.Join(dir, name), 0o755)
				continue
			}
			_ = os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o644)
		}
		return dir
	}
	cases := []struct {
		dir  string
		skip bool
	}{
		{mk(".superbot", "tmp.py"), true},
		{mk("pylib", "typing_extensions.py", "lxml-6.1.3.dist-info/"), true},
		{mk("pkg", "__init__.py", "core.py"), true},
		{mk("lxml-6.1.3.dist-info", "RECORD"), true},
		{mk("node_modules"), true},
		{mk("figs", "fig1.png"), false},
		{mk("output", "report.md"), false},
	}
	for _, c := range cases {
		if got := skipScanDir(c.dir, filepath.Base(c.dir)); got != c.skip {
			t.Fatalf("%s: skip=%v want %v", filepath.Base(c.dir), got, c.skip)
		}
	}
}
