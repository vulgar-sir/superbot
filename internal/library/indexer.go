// Package library indexes files produced by bots (workspace artifacts, chat
// attachments and documents) into the per-bot Library so generated images,
// media, documents and code are discoverable without an explicit save step.
package library

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"mime"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"superbot/internal/store"
)

const (
	maxIndexedFileBytes = 25 << 20
	maxInlineTextBytes  = 64 << 10
	maxScanFiles        = 200
	maxIndexPerScan     = 50
)

var kindByExt = map[string]string{
	".png": "media", ".jpg": "media", ".jpeg": "media", ".gif": "media",
	".webp": "media", ".svg": "media", ".bmp": "media",
	".mp3": "media", ".wav": "media", ".ogg": "media", ".m4a": "media",
	".mp4": "media", ".webm": "media", ".mov": "media",
	".pdf": "document", ".doc": "document", ".docx": "document",
	".xls": "document", ".xlsx": "document", ".ppt": "document", ".pptx": "document",
	".md": "document", ".txt": "document", ".csv": "document",
	".html": "document", ".htm": "document", ".rtf": "document",
	".js": "file", ".ts": "file", ".tsx": "file", ".jsx": "file",
	".py": "file", ".go": "file", ".sh": "file", ".css": "file",
	".json": "file", ".yaml": "file", ".yml": "file", ".sql": "file",
	".rs": "file", ".java": "file", ".c": "file", ".cpp": "file",
}

var extByMime = map[string]string{
	"image/png": ".png", "image/jpeg": ".jpg", "image/gif": ".gif",
	"image/webp": ".webp", "image/svg+xml": ".svg",
	"audio/mpeg": ".mp3", "audio/wav": ".wav", "audio/ogg": ".ogg",
	"video/mp4": ".mp4", "video/webm": ".webm",
	"application/pdf": ".pdf", "text/plain": ".txt", "text/markdown": ".md",
	"application/json": ".json",
}

var skipDirs = map[string]bool{
	".git": true, ".chromium": true, "node_modules": true,
	".cache": true, "__pycache__": true, ".venv": true, "venv": true,
	"site-packages": true,
}

// Indexer creates Library rows for files and documents without overwriting
// existing rows or duplicating repeated scans.
type Indexer struct {
	db      *store.DB
	dataDir string
	logger  *slog.Logger
}

func NewIndexer(db *store.DB, dataDir string, logger *slog.Logger) *Indexer {
	if logger == nil {
		logger = slog.Default()
	}
	return &Indexer{db: db, dataDir: dataDir, logger: logger}
}

func kindFor(path string) (kind, mimeType string) {
	ext := strings.ToLower(filepath.Ext(path))
	kind = kindByExt[ext]
	if kind == "" {
		return "", ""
	}
	mimeType = mime.TypeByExtension(ext)
	if mimeType == "" {
		switch kind {
		case "media":
			mimeType = "application/octet-stream"
		case "document":
			mimeType = "text/plain"
		default:
			mimeType = "text/plain"
		}
	}
	return kind, mimeType
}

func isTextMime(mimeType string) bool {
	return strings.HasPrefix(mimeType, "text/") ||
		strings.Contains(mimeType, "json") ||
		strings.Contains(mimeType, "xml") ||
		strings.Contains(mimeType, "javascript") ||
		strings.Contains(mimeType, "yaml")
}

func (i *Indexer) existing(ctx context.Context, botID uuid.UUID, filePath string) bool {
	_, err := i.db.GetLibraryItemByFilePath(ctx, store.GetLibraryItemByFilePathParams{
		BotID: botID, FilePath: filePath,
	})
	return err == nil
}

func (i *Indexer) existingSource(ctx context.Context, botID uuid.UUID, source, sourceID string) bool {
	_, err := i.db.GetLibraryItemBySource(ctx, store.GetLibraryItemBySourceParams{
		BotID: botID, Source: source, SourceID: sourceID,
	})
	if err == nil {
		return true
	}
	return !errors.Is(err, pgx.ErrNoRows)
}

// IndexFile adds one existing file to the bot Library. It returns true when a
// new row was created.
func (i *Indexer) IndexFile(
	ctx context.Context,
	ownerID, botID uuid.UUID,
	filePath, relativePath, source, sourceID string,
	conversationID, messageID *uuid.UUID,
) (bool, error) {
	info, err := os.Stat(filePath)
	if err != nil || info.IsDir() || info.Size() == 0 || info.Size() > maxIndexedFileBytes {
		return false, nil
	}
	kind, mimeType := kindFor(filePath)
	if kind == "" {
		return false, nil
	}
	absolute, err := filepath.Abs(filePath)
	if err != nil {
		absolute = filePath
	}
	if i.existing(ctx, botID, absolute) {
		return false, nil
	}
	if source != "" && sourceID != "" && i.existingSource(ctx, botID, source, sourceID) {
		return false, nil
	}
	content := ""
	if isTextMime(mimeType) && info.Size() <= maxInlineTextBytes {
		if raw, readErr := os.ReadFile(filePath); readErr == nil {
			content = string(raw)
		}
	}
	metadata, _ := json.Marshal(map[string]any{
		"auto":          true,
		"source":        source,
		"source_id":     sourceID,
		"relative_path": filepath.ToSlash(relativePath),
		"size":          info.Size(),
	})
	titleSource := strings.TrimSpace(relativePath)
	if titleSource == "" {
		titleSource = filePath
	}
	title := filepath.Base(titleSource)
	if len([]rune(title)) > 200 {
		title = string([]rune(title)[:200])
	}
	_, err = i.db.CreateLibraryItem(ctx, store.CreateLibraryItemParams{
		OwnerID:        ownerID,
		BotID:          botID,
		Kind:           kind,
		Title:          title,
		Description:    "自动收录",
		Content:        content,
		MimeType:       mimeType,
		FilePath:       absolute,
		ConversationID: conversationID,
		MessageID:      messageID,
		Metadata:       metadata,
	})
	if err != nil {
		return false, err
	}
	return true, nil
}

// IndexWorkspace scans a bot workspace for generated artifacts and indexes the
// newest matching files (images, media, documents and simple code).
func (i *Indexer) IndexWorkspace(ctx context.Context, ownerID, botID uuid.UUID, root string) int {
	if strings.TrimSpace(root) == "" {
		return 0
	}
	type candidate struct {
		path string
		rel  string
		mod  time.Time
	}
	var candidates []candidate
	_ = filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if entry.IsDir() {
			if skipDirs[entry.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		if len(candidates) >= maxScanFiles {
			return filepath.SkipAll
		}
		if strings.HasPrefix(entry.Name(), ".") {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(entry.Name()))
		kind := kindByExt[ext]
		if kind == "" {
			return nil
		}
		if strings.HasPrefix(entry.Name(), "call_") && ext == ".txt" {
			// Raw tool result dumps are noise; the tool timeline already shows them.
			return nil
		}
		info, statErr := entry.Info()
		if statErr != nil || info.Size() == 0 || info.Size() > maxIndexedFileBytes {
			return nil
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			rel = entry.Name()
		}
		slashRel := filepath.ToSlash(rel)
		if strings.Contains(slashRel, "artifacts/") && kind != "media" {
			// Keep screenshots/media produced by tools, but not text dumps.
			return nil
		}
		candidates = append(candidates, candidate{path: path, rel: rel, mod: info.ModTime()})
		return nil
	})
	sort.Slice(candidates, func(a, b int) bool { return candidates[a].mod.After(candidates[b].mod) })
	created := 0
	for _, item := range candidates {
		if created >= maxIndexPerScan {
			break
		}
		ok, err := i.IndexFile(ctx, ownerID, botID, item.path, item.rel, "workspace", item.rel, nil, nil)
		if err != nil {
			i.logger.Warn("index workspace artifact failed", "path", item.rel, "error", err)
			continue
		}
		if ok {
			created++
		}
	}
	return created
}

// IndexAttachment copies an uploaded chat attachment into the Library storage
// and creates a Library row. Re-sending the same attachment is a no-op.
func (i *Indexer) IndexAttachment(
	ctx context.Context,
	ownerID, botID uuid.UUID,
	attachment store.Attachment,
	messageID *uuid.UUID,
) (bool, error) {
	ext := strings.ToLower(filepath.Ext(attachment.Filename))
	if _, ok := kindByExt[ext]; !ok || len(ext) > 12 {
		ext = extByMime[strings.ToLower(strings.TrimSpace(attachment.MimeType))]
	}
	digest := sha256.Sum256([]byte("attachment|" + attachment.ID.String()))
	name := "auto-" + hex.EncodeToString(digest[:8]) + ext
	directory := filepath.Join(i.dataDir, "library", "auto")
	if err := os.MkdirAll(directory, 0o750); err != nil {
		return false, err
	}
	filePath := filepath.Join(directory, name)
	if _, statErr := os.Stat(filePath); errors.Is(statErr, os.ErrNotExist) {
		if err := os.WriteFile(filePath, attachment.Content, 0o640); err != nil {
			return false, err
		}
	}
	return i.IndexFile(ctx, ownerID, botID, filePath, attachment.Filename, "attachment", attachment.ID.String(), nil, messageID)
}

// IndexDocument adds an account document (including Canvas) to the primary
// bot's Library. Documents are account-level, so the caller supplies the bot.
func (i *Indexer) IndexDocument(ctx context.Context, ownerID, botID uuid.UUID, document store.Document) (bool, error) {
	if i.existingSource(ctx, botID, "document", document.ID.String()) {
		return false, nil
	}
	metadata, _ := json.Marshal(map[string]any{
		"auto":      true,
		"source":    "document",
		"source_id": document.ID.String(),
	})
	kind := "document"
	if document.Kind == "canvas" {
		kind = "document"
	}
	title := strings.TrimSpace(document.Title)
	if title == "" {
		title = "未命名文档"
	}
	_, err := i.db.CreateLibraryItem(ctx, store.CreateLibraryItemParams{
		OwnerID:     ownerID,
		BotID:       botID,
		Kind:        kind,
		Title:       title,
		Description: "自动收录",
		Content:     document.Content,
		MimeType:    "text/markdown",
		Metadata:    metadata,
	})
	if err != nil {
		return false, fmt.Errorf("create document library item: %w", err)
	}
	return true, nil
}
