package tools

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// UploadsDir is where files people hand a bot (chat attachments, Library
// files it opens) are placed in its workspace, i.e. /workspace/uploads in
// its computer.
const UploadsDir = "uploads"

// maxPlacedFileBytes bounds the files copied into a workspace.
const maxPlacedFileBytes = 25 << 20

// SafeUploadName turns a user-supplied file name into one plain file name:
// no directories, no leading dots (which would hide it), no control
// characters.
func SafeUploadName(name string) string {
	name = strings.ReplaceAll(name, "\\", "/")
	name = path.Base(strings.TrimSpace(name))
	name = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f || r == ':' {
			return '_'
		}
		return r
	}, name)
	name = strings.TrimLeft(name, ". ")
	if name == "" || name == "/" {
		name = "file"
	}
	if runes := []rune(name); len(runes) > 120 {
		ext := filepath.Ext(name)
		if len([]rune(ext)) > 12 {
			ext = ""
		}
		name = string(runes[:120-len([]rune(ext))]) + ext
	}
	return name
}

// PlaceInWorkspace writes data to uploads/<name> in the workspace and returns
// the path relative to the workspace root. Writing the same bytes again is a
// no-op, so it is safe to call on every turn.
func PlaceInWorkspace(root *os.Root, name string, data []byte) (string, error) {
	if root == nil {
		return "", errors.New("no workspace")
	}
	if len(data) > maxPlacedFileBytes {
		return "", errors.New("file is too large to copy into the workspace")
	}
	rel := path.Join(UploadsDir, SafeUploadName(name))
	if existing, err := root.ReadFile(rel); err == nil && bytes.Equal(existing, data) {
		return rel, nil
	}
	if err := root.MkdirAll(UploadsDir, 0o755); err != nil && !errors.Is(err, fs.ErrExist) {
		return "", err
	}
	if err := root.WriteFile(rel, data, 0o644); err != nil {
		return "", err
	}
	return rel, nil
}
