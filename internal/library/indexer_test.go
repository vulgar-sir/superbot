package library

import "testing"

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
}
