package library

import (
	"encoding/json"
	"mime"
	"path/filepath"
	"strings"

	"superbot/internal/store"
)

// FileName is the name a Library item carries when it leaves the Library (a
// download, a copy into a workspace, a file sent in chat): the original
// upload name when known, else the title, ending in the stored file's own
// extension so its format is recognised. Items without a file are their
// text content, named for its type (.md for documents).
func FileName(item store.LibraryItem) string {
	var meta struct {
		OriginalName string `json:"original_name"`
	}
	_ = json.Unmarshal(item.Metadata, &meta)
	name := strings.TrimSpace(meta.OriginalName)
	if name == "" {
		name = strings.TrimSpace(item.Title)
	}
	if name == "" {
		name = "file"
	}
	ext := filepath.Ext(item.FilePath)
	if strings.TrimSpace(item.FilePath) == "" {
		ext = TextExtension(item.MimeType)
	}
	if ext != "" && !strings.EqualFold(filepath.Ext(name), ext) {
		name += ext
	}
	return name
}

// TextExtension is the file extension for text content of the given type.
func TextExtension(mimeType string) string {
	base, _, err := mime.ParseMediaType(mimeType)
	if err != nil {
		base = strings.ToLower(strings.TrimSpace(mimeType))
	}
	switch base {
	case "text/markdown", "text/x-markdown":
		return ".md"
	case "text/html":
		return ".html"
	case "text/calendar":
		return ".ics"
	case "text/csv":
		return ".csv"
	case "application/json":
		return ".json"
	default:
		return ".txt"
	}
}
