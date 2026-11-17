package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSendFileFromWorkspace(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "out", "figs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "out", "开题报告.docx"), []byte("PK\x03\x04docx"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "empty.txt"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	tool := &sendFileTool{workspace: root}

	for _, path := range []string{"/workspace/out/开题报告.docx", "out/开题报告.docx", "workspace/out/开题报告.docx", `out\开题报告.docx`} {
		file, err := tool.fromWorkspace(path)
		if err != nil {
			t.Fatalf("fromWorkspace(%q): %v", path, err)
		}
		if file.name != "开题报告.docx" || !strings.Contains(file.mimeType, "wordprocessingml") || string(file.data) != "PK\x03\x04docx" {
			t.Fatalf("fromWorkspace(%q) = %q %q %q", path, file.name, file.mimeType, file.data)
		}
	}

	for path, want := range map[string]string{
		"../etc/passwd":      "不在你的电脑工作区",
		"/etc/passwd":        "不在你的电脑工作区",
		"/workspace":         "不在你的电脑工作区",
		"out/figs":           "是目录",
		"empty.txt":          "空文件",
		"out/missing.pdf":    "找不到文件",
		"/workspace/../root": "不在你的电脑工作区",
	} {
		_, err := tool.fromWorkspace(path)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("fromWorkspace(%q) error = %v, want %q", path, err, want)
		}
	}

	if _, err := (&sendFileTool{}).fromWorkspace("a.txt"); err == nil {
		t.Error("expected an error without a workspace")
	}
}

func TestFileType(t *testing.T) {
	for name, want := range map[string]string{
		"a.xlsx": "spreadsheetml",
		"b.PDF":  "application/pdf",
		"c.md":   "text/markdown",
		"d.png":  "image/png",
	} {
		if got := fileType(name, []byte("x")); !strings.Contains(got, want) {
			t.Errorf("fileType(%q) = %q, want %q", name, got, want)
		}
	}
	if got := fileType("blob", []byte("hello")); !strings.HasPrefix(got, "text/plain") {
		t.Errorf("fileType(blob) = %q", got)
	}
}
