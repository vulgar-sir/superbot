package httpapi

import (
	"mime"
	"strings"
	"testing"
)

func TestContentDispositionKeepsChineseNames(t *testing.T) {
	header := contentDisposition("attachment", `开题报告 "终稿";v2.docx`)
	if !strings.HasPrefix(header, `attachment; filename="`) {
		t.Fatalf("header = %q", header)
	}
	disposition, params, err := mime.ParseMediaType(header)
	if err != nil {
		t.Fatalf("parse %q: %v", header, err)
	}
	if disposition != "attachment" {
		t.Fatalf("disposition = %q", disposition)
	}
	if params["filename"] != `开题报告 "终稿";v2.docx` {
		t.Fatalf("filename = %q (header %q)", params["filename"], header)
	}
}

func TestInlineSafe(t *testing.T) {
	for mimeType, want := range map[string]bool{
		"image/png":                 true,
		"application/pdf":           true,
		"text/plain; charset=utf-8": true,
		"audio/mpeg":                true,
		"text/html; charset=utf-8":  false,
		"image/svg+xml":             false,
		"application/xml":           false,
		"application/vnd.openxmlformats-officedocument.wordprocessingml.document": false,
		"": false,
	} {
		if got := inlineSafe(mimeType); got != want {
			t.Errorf("inlineSafe(%q) = %v, want %v", mimeType, got, want)
		}
	}
}
