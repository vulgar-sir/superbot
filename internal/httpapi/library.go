package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	libindex "superbot/internal/library"
	"superbot/internal/store"
)

type libraryItemDTO struct {
	ID             uuid.UUID       `json:"id"`
	BotID          uuid.UUID       `json:"bot_id"`
	Kind           string          `json:"kind"`
	Title          string          `json:"title"`
	Description    string          `json:"description"`
	MimeType       string          `json:"mime_type"`
	ConversationID *uuid.UUID      `json:"conversation_id"`
	MessageID      *uuid.UUID      `json:"message_id"`
	Metadata       json.RawMessage `json:"metadata"`
	FileURL        string          `json:"file_url,omitempty"`
	CreatedAt      time.Time       `json:"created_at"`
	UpdatedAt      time.Time       `json:"updated_at"`
}

func toLibraryItemDTO(item store.LibraryItem) libraryItemDTO {
	meta := item.Metadata
	if len(meta) == 0 {
		meta = json.RawMessage(`{}`)
	}
	fileURL := ""
	if item.FilePath != "" {
		fileURL = "/api/library/" + item.ID.String() + "/file"
	}
	return libraryItemDTO{
		ID:             item.ID,
		BotID:          item.BotID,
		Kind:           item.Kind,
		Title:          item.Title,
		Description:    item.Description,
		MimeType:       item.MimeType,
		ConversationID: item.ConversationID,
		MessageID:      item.MessageID,
		Metadata:       meta,
		FileURL:        fileURL,
		CreatedAt:      item.CreatedAt,
		UpdatedAt:      item.UpdatedAt,
	}
}

func (s *Server) handleListBotLibrary(w http.ResponseWriter, r *http.Request) {
	botID, ok := parseUUIDParam(w, r, "botID")
	if !ok {
		return
	}
	accountID := accountFrom(r)
	if _, err := s.db.GetBot(r.Context(), store.GetBotParams{ID: botID, OwnerID: accountID}); err != nil {
		writeError(w, http.StatusNotFound, "not_found", "bot not found")
		return
	}
	items, err := s.db.ListLibraryItemsForBot(r.Context(), store.ListLibraryItemsForBotParams{
		OwnerID: accountID, BotID: botID, Limit: 100,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not load library")
		return
	}
	out := make([]libraryItemDTO, 0, len(items))
	for _, item := range items {
		out = append(out, toLibraryItemDTO(item))
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out})
}

func (s *Server) libraryRoutes(r chi.Router) {
	r.Get("/{itemID}", s.handleGetLibraryItem)
	r.Get("/{itemID}/file", s.handleGetLibraryFile)
	r.Get("/{itemID}/download", s.handleDownloadLibraryItem)
	r.Get("/{itemID}/preview", s.handleLibraryPreview)
	r.Delete("/{itemID}", s.handleDeleteLibraryItem)
}

func (s *Server) handleUploadLibraryFile(w http.ResponseWriter, r *http.Request) {
	botID, ok := parseUUIDParam(w, r, "botID")
	if !ok {
		return
	}
	accountID := accountFrom(r)
	if _, err := s.db.GetBot(r.Context(), store.GetBotParams{ID: botID, OwnerID: accountID}); err != nil {
		writeError(w, http.StatusNotFound, "not_found", "bot not found")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 25<<20)
	if err := r.ParseMultipartForm(25 << 20); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "file is too large or malformed")
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "file is required")
		return
	}
	defer file.Close()
	mimeType := strings.TrimSpace(header.Header.Get("Content-Type"))
	if mimeType == "" || mimeType == "application/octet-stream" {
		mimeType = mime.TypeByExtension(filepath.Ext(header.Filename))
	}
	if mimeType == "" {
		mimeType = "application/octet-stream"
	}
	extension := strings.ToLower(filepath.Ext(header.Filename))
	if len(extension) > 12 {
		extension = ""
	}
	for _, r := range extension[1:] {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') {
			extension = ""
			break
		}
	}
	directory := filepath.Join(s.cfg.DataDir, "library")
	if err := os.MkdirAll(directory, 0o750); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not create library directory")
		return
	}
	filePath := filepath.Join(directory, uuid.NewString()+extension)
	target, err := os.Create(filePath)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not store file")
		return
	}
	if _, err := io.Copy(target, io.LimitReader(file, 25<<20)); err != nil {
		target.Close()
		_ = os.Remove(filePath)
		writeError(w, http.StatusInternalServerError, "internal", "could not store file")
		return
	}
	if err := target.Close(); err != nil {
		_ = os.Remove(filePath)
		writeError(w, http.StatusInternalServerError, "internal", "could not store file")
		return
	}
	kind := "file"
	if strings.HasPrefix(mimeType, "image/") || strings.HasPrefix(mimeType, "audio/") || strings.HasPrefix(mimeType, "video/") {
		kind = "media"
	}
	title := strings.TrimSpace(header.Filename)
	if len([]rune(title)) > 200 {
		title = string([]rune(title)[:200])
	}
	metadata, _ := json.Marshal(map[string]string{"original_name": title})
	item, err := s.db.CreateLibraryItem(r.Context(), store.CreateLibraryItemParams{
		OwnerID:     accountID,
		BotID:       botID,
		Kind:        kind,
		Title:       title,
		Description: "",
		Content:     "",
		MimeType:    mimeType,
		FilePath:    filePath,
		Metadata:    metadata,
	})
	if err != nil {
		_ = os.Remove(filePath)
		writeError(w, http.StatusInternalServerError, "internal", "could not create library item")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"item": toLibraryItemDTO(item)})
}

func (s *Server) handleGetLibraryFile(w http.ResponseWriter, r *http.Request) {
	itemID, ok := parseUUIDParam(w, r, "itemID")
	if !ok {
		return
	}
	item, err := s.db.GetLibraryItem(r.Context(), store.GetLibraryItemParams{
		ID: itemID, OwnerID: accountFrom(r),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "not_found", "library item not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", "could not load library item")
		return
	}
	if strings.TrimSpace(item.FilePath) == "" {
		writeError(w, http.StatusNotFound, "not_found", "library item has no file")
		return
	}
	path, found := s.libraryFilePath(item)
	if !found {
		writeError(w, http.StatusNotFound, "not_found", "library file not found")
		return
	}
	disposition := "inline"
	if r.URL.Query().Get("download") != "" || !inlineSafe(libraryFileType(item, path)) {
		disposition = "attachment"
	}
	w.Header().Set("Content-Disposition", contentDisposition(disposition, libindex.FileName(item)))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	http.ServeFile(w, r, path)
}

// handleDownloadLibraryItem saves any Library item as a file: the stored
// file itself, or for documents and pages their text as .md (or the type
// they were saved as).
func (s *Server) handleDownloadLibraryItem(w http.ResponseWriter, r *http.Request) {
	itemID, ok := parseUUIDParam(w, r, "itemID")
	if !ok {
		return
	}
	item, err := s.db.GetLibraryItem(r.Context(), store.GetLibraryItemParams{
		ID: itemID, OwnerID: accountFrom(r),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "not_found", "library item not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", "could not load library item")
		return
	}
	name := libindex.FileName(item)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if strings.TrimSpace(item.FilePath) != "" {
		path, found := s.libraryFilePath(item)
		if !found {
			writeError(w, http.StatusNotFound, "not_found", "library file not found")
			return
		}
		w.Header().Set("Content-Disposition", contentDisposition("attachment", name))
		http.ServeFile(w, r, path)
		return
	}
	if item.Content == "" {
		writeError(w, http.StatusNotFound, "not_found", "library item has nothing to download")
		return
	}
	contentType := mime.TypeByExtension(libindex.TextExtension(item.MimeType))
	if contentType == "" || !strings.HasPrefix(contentType, "text/") && !strings.Contains(contentType, "json") {
		contentType = "text/plain; charset=utf-8"
	} else if !strings.Contains(contentType, "charset") {
		contentType += "; charset=utf-8"
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", contentDisposition("attachment", name))
	_, _ = io.WriteString(w, item.Content)
}

// libraryFilePath is the item's stored file when it exists inside the data
// directory; anything outside it is never served.
func (s *Server) libraryFilePath(item store.LibraryItem) (string, bool) {
	root := filepath.Clean(s.cfg.DataDir)
	path := filepath.Clean(item.FilePath)
	if !strings.HasPrefix(path, root+string(os.PathSeparator)) {
		return "", false
	}
	if info, err := os.Stat(path); err != nil || info.IsDir() {
		return "", false
	}
	return path, true
}

func libraryFileType(item store.LibraryItem, path string) string {
	if byExt := mime.TypeByExtension(filepath.Ext(path)); byExt != "" {
		return byExt
	}
	return item.MimeType
}

func (s *Server) handleGetLibraryItem(w http.ResponseWriter, r *http.Request) {
	itemID, ok := parseUUIDParam(w, r, "itemID")
	if !ok {
		return
	}
	item, err := s.db.GetLibraryItem(r.Context(), store.GetLibraryItemParams{
		ID: itemID, OwnerID: accountFrom(r),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "not_found", "library item not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", "could not load library item")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"item":    toLibraryItemDTO(item),
		"content": item.Content,
	})
}

func (s *Server) handleDeleteLibraryItem(w http.ResponseWriter, r *http.Request) {
	itemID, ok := parseUUIDParam(w, r, "itemID")
	if !ok {
		return
	}
	item, loadErr := s.db.GetLibraryItem(r.Context(), store.GetLibraryItemParams{
		ID: itemID, OwnerID: accountFrom(r),
	})
	n, err := s.db.DeleteLibraryItem(r.Context(), store.DeleteLibraryItemParams{
		ID: itemID, OwnerID: accountFrom(r),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not delete library item")
		return
	}
	if n == 0 {
		writeError(w, http.StatusNotFound, "not_found", "library item not found")
		return
	}
	// Remove the stored file once the row is gone; ignore anything outside the
	// data directory or already-missing files.
	if loadErr == nil && strings.TrimSpace(item.FilePath) != "" {
		root := filepath.Clean(s.cfg.DataDir)
		path := filepath.Clean(item.FilePath)
		if strings.HasPrefix(path, root+string(os.PathSeparator)) {
			if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
				s.logger.Warn("remove library file failed", "item_id", itemID, "error", err)
			}
		}
	}
	writeNoContent(w)
}
