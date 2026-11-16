package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	defaultReadLimit  = 256 * 1024
	maxReadLimit      = 1024 * 1024
	maxWriteLimit     = 1024 * 1024
	maxListEntries    = 500
	maxSearchResults  = 50
	maxSearchFileSize = 1024 * 1024
)

// WorkspaceTools exposes a directory tree to the model. All paths are
// resolved through os.Root, which rejects traversal and symlink escapes.
// The same implementation is used for the private bot workspace and the team
// shared directory, with different tool-name prefixes.
type WorkspaceTools struct {
	root   *os.Root
	prefix string
	scope  string
}

func NewWorkspaceTools(root *os.Root) *WorkspaceTools {
	return &WorkspaceTools{root: root, prefix: "workspace_", scope: "bot workspace"}
}

func NewSharedTools(root *os.Root) *WorkspaceTools {
	return &WorkspaceTools{root: root, prefix: "shared_", scope: "team shared directory (visible to every member of the team)"}
}

// NewDriveTools exposes the account's shared drive, which every bot of the
// account reads and writes.
func NewDriveTools(root *os.Root) *WorkspaceTools {
	return &WorkspaceTools{root: root, prefix: "drive_", scope: "account shared drive (shared by every bot of this account, mounted at /shared in the computer)"}
}

func (w *WorkspaceTools) All() []Tool {
	return []Tool{
		&listTool{root: w.root, prefix: w.prefix, scope: w.scope},
		&readTool{root: w.root, prefix: w.prefix, scope: w.scope},
		&writeTool{root: w.root, prefix: w.prefix, scope: w.scope},
		&searchTool{root: w.root, prefix: w.prefix, scope: w.scope},
	}
}

func cleanRel(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return ".", nil
	}
	name = strings.ReplaceAll(name, "\\", "/")
	clean := path.Clean(name)
	if clean == ".." || strings.HasPrefix(clean, "../") || path.IsAbs(clean) || strings.Contains(clean, ":") || strings.ContainsRune(clean, '\x00') {
		return "", fmt.Errorf("path %q escapes the workspace", name)
	}
	return clean, nil
}

type listTool struct {
	root   *os.Root
	prefix string
	scope  string
}

func (t *listTool) Name() string { return t.prefix + "list" }
func (t *listTool) Description() string {
	return "List files and directories inside the " + t.scope + ". Paths are relative to its root."
}
func (t *listTool) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"path":{"type":"string","description":"relative directory, default ."}}}`)
}

type listArgs struct {
	Path string `json:"path"`
}

type listEntry struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	Size     int64  `json:"size,omitempty"`
	Modified string `json:"modified,omitempty"`
}

func (t *listTool) Execute(ctx context.Context, raw json.RawMessage) (Result, error) {
	var args listArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return Result{}, fmt.Errorf("invalid arguments: %w", err)
	}
	rel, err := cleanRel(args.Path)
	if err != nil {
		return Result{}, err
	}
	entries, err := fs.ReadDir(t.root.FS(), rel)
	if err != nil {
		return Result{}, fmt.Errorf("list %q: %w", rel, err)
	}
	out := make([]listEntry, 0, len(entries))
	for i, entry := range entries {
		if i >= maxListEntries {
			break
		}
		item := listEntry{Name: entry.Name(), Type: "file"}
		if entry.IsDir() {
			item.Type = "dir"
		} else if info, err := entry.Info(); err == nil {
			item.Size = info.Size()
			item.Modified = info.ModTime().Format(time.RFC3339)
		}
		out = append(out, item)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	body, _ := json.MarshalIndent(out, "", "  ")
	return Result{Content: string(body)}, nil
}

type readTool struct {
	root   *os.Root
	prefix string
	scope  string
}

func (t *readTool) Name() string { return t.prefix + "read" }
func (t *readTool) Description() string {
	return "Read a text file from the " + t.scope + ". Returns an error for binary files or paths outside it."
}
func (t *readTool) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"},"max_bytes":{"type":"integer","description":"default 262144, max 1048576"}},"required":["path"]}`)
}

type readArgs struct {
	Path     string `json:"path"`
	MaxBytes int    `json:"max_bytes"`
}

func (t *readTool) Execute(ctx context.Context, raw json.RawMessage) (Result, error) {
	var args readArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return Result{}, fmt.Errorf("invalid arguments: %w", err)
	}
	rel, err := cleanRel(args.Path)
	if err != nil {
		return Result{}, err
	}
	if rel == "." {
		return Result{}, fmt.Errorf("path must point to a file")
	}
	limit := args.MaxBytes
	if limit <= 0 {
		limit = defaultReadLimit
	}
	if limit > maxReadLimit {
		limit = maxReadLimit
	}
	data, err := t.root.ReadFile(rel)
	if err != nil {
		return Result{}, fmt.Errorf("read %q: %w", rel, err)
	}
	truncated := false
	if len(data) > limit {
		data = data[:limit]
		truncated = true
	}
	if looksBinary(data) {
		return Result{}, fmt.Errorf("file %q looks binary; use a shell/python tool in a sandbox for binary files", rel)
	}
	content := string(data)
	if truncated {
		content += fmt.Sprintf("\n\n[truncated at %d bytes]", limit)
	}
	return Result{Content: content}, nil
}

type writeTool struct {
	root   *os.Root
	prefix string
	scope  string
}

func (t *writeTool) Name() string { return t.prefix + "write" }
func (t *writeTool) Description() string {
	return "Create or overwrite a text file in the " + t.scope + ". Parent directories are created automatically."
}
func (t *writeTool) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"},"content":{"type":"string"}},"required":["path","content"]}`)
}

type writeArgs struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

func (t *writeTool) Execute(ctx context.Context, raw json.RawMessage) (Result, error) {
	var args writeArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return Result{}, fmt.Errorf("invalid arguments: %w", err)
	}
	rel, err := cleanRel(args.Path)
	if err != nil {
		return Result{}, err
	}
	if rel == "." {
		return Result{}, fmt.Errorf("path must point to a file")
	}
	if len(args.Content) > maxWriteLimit {
		return Result{}, fmt.Errorf("content is too large (max %d bytes)", maxWriteLimit)
	}
	dir := path.Dir(rel)
	if dir != "." {
		if err := t.root.MkdirAll(dir, 0o755); err != nil {
			return Result{}, fmt.Errorf("create parent directory: %w", err)
		}
	}
	if err := t.root.WriteFile(rel, []byte(args.Content), 0o644); err != nil {
		return Result{}, fmt.Errorf("write %q: %w", rel, err)
	}
	return Result{Content: fmt.Sprintf("wrote %d bytes to %s", len(args.Content), rel)}, nil
}

type searchTool struct {
	root   *os.Root
	prefix string
	scope  string
}

func (t *searchTool) Name() string { return t.prefix + "search" }
func (t *searchTool) Description() string {
	return "Search text files in the " + t.scope + " for a case-insensitive substring and return file paths with line numbers."
}
func (t *searchTool) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"query":{"type":"string"},"path":{"type":"string","description":"relative directory to search, default ."},"max_results":{"type":"integer"}},"required":["query"]}`)
}

type searchArgs struct {
	Query      string `json:"query"`
	Path       string `json:"path"`
	MaxResults int    `json:"max_results"`
}

type searchHit struct {
	Path    string `json:"path"`
	Line    int    `json:"line"`
	Content string `json:"content"`
}

func (t *searchTool) Execute(ctx context.Context, raw json.RawMessage) (Result, error) {
	var args searchArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return Result{}, fmt.Errorf("invalid arguments: %w", err)
	}
	query := strings.ToLower(strings.TrimSpace(args.Query))
	if query == "" {
		return Result{}, fmt.Errorf("query is required")
	}
	start, err := cleanRel(args.Path)
	if err != nil {
		return Result{}, err
	}
	limit := args.MaxResults
	if limit <= 0 || limit > maxSearchResults {
		limit = maxSearchResults
	}

	var hits []searchHit
	walkErr := fs.WalkDir(t.root.FS(), start, func(filePath string, entry fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if entry.IsDir() {
			if entry.Name() == ".git" || entry.Name() == "node_modules" {
				return fs.SkipDir
			}
			return nil
		}
		info, err := entry.Info()
		if err != nil || info.Size() > maxSearchFileSize {
			return nil
		}
		data, err := fs.ReadFile(t.root.FS(), filePath)
		if err != nil || looksBinary(data) {
			return nil
		}
		lines := strings.Split(string(data), "\n")
		for i, line := range lines {
			if strings.Contains(strings.ToLower(line), query) {
				hits = append(hits, searchHit{
					Path:    filePath,
					Line:    i + 1,
					Content: truncateRunes(strings.TrimSpace(line), 300),
				})
				if len(hits) >= limit {
					return fs.SkipAll
				}
			}
		}
		return nil
	})
	if walkErr != nil && walkErr != fs.SkipAll {
		return Result{}, fmt.Errorf("search: %w", walkErr)
	}
	if len(hits) == 0 {
		return Result{Content: "no matches"}, nil
	}
	body, _ := json.MarshalIndent(hits, "", "  ")
	return Result{Content: string(body)}, nil
}

func looksBinary(data []byte) bool {
	probe := data
	if len(probe) > 512 {
		probe = probe[:512]
	}
	if !utf8.Valid(probe) {
		return true
	}
	for _, b := range probe {
		if b == 0 {
			return true
		}
	}
	return false
}

func truncateRunes(s string, limit int) string {
	runes := []rune(s)
	if len(runes) <= limit {
		return s
	}
	return string(runes[:limit]) + "..."
}
