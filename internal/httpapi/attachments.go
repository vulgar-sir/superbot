package httpapi

import (
	"bytes"
	"errors"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"superbot/internal/store"
)

const maxAttachmentBytes = 10 << 20

func (s *Server) attachmentRoutes(r chi.Router) {
	r.Post("/", s.handleUploadAttachment)
	r.Get("/{attachmentID}", s.handleGetAttachment)
}

func (s *Server) handleUploadAttachment(w http.ResponseWriter, r *http.Request) {
	accountID := accountFrom(r)
	if err := r.ParseMultipartForm(maxAttachmentBytes + (1 << 20)); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "invalid multipart body")
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "file is required")
		return
	}
	defer file.Close()
	if header.Size > maxAttachmentBytes {
		writeError(w, http.StatusBadRequest, "invalid_request", "file is too large (max 10MB)")
		return
	}
	content, err := io.ReadAll(io.LimitReader(file, maxAttachmentBytes+1))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "could not read file")
		return
	}
	if len(content) == 0 {
		writeError(w, http.StatusBadRequest, "invalid_request", "file is empty")
		return
	}
	if len(content) > maxAttachmentBytes {
		writeError(w, http.StatusBadRequest, "invalid_request", "file is too large (max 10MB)")
		return
	}
	filename := filepath.Base(strings.TrimSpace(header.Filename))
	if filename == "" || filename == "." {
		filename = "attachment"
	}
	mimeType := strings.TrimSpace(header.Header.Get("Content-Type"))
	if mimeType == "" || mimeType == "application/octet-stream" {
		mimeType = http.DetectContentType(content)
	}
	if strings.HasPrefix(mimeType, "image/") {
		if cfg, _, err := image.DecodeConfig(bytes.NewReader(content)); err == nil {
			if cfg.Width > 8192 || cfg.Height > 8192 {
				writeError(w, http.StatusBadRequest, "invalid_request", "image dimensions exceed 8192px")
				return
			}
		}
	}
	attachment, err := s.db.CreateAttachment(r.Context(), store.CreateAttachmentParams{
		OwnerID:   accountID,
		Filename:  filename,
		MimeType:  mimeType,
		SizeBytes: int64(len(content)),
		Content:   content,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not save attachment")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"attachment": toAttachmentDTO(attachment)})
}

func (s *Server) handleGetAttachment(w http.ResponseWriter, r *http.Request) {
	attachmentID, ok := parseUUIDParam(w, r, "attachmentID")
	if !ok {
		return
	}
	attachment, err := s.db.GetAttachmentForOwner(r.Context(), store.GetAttachmentForOwnerParams{
		ID:      attachmentID,
		OwnerID: accountFrom(r),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "not_found", "attachment not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", "could not load attachment")
		return
	}
	// Images, audio, video, PDF and plain text open in the browser; anything
	// else (Word, zip, HTML a bot wrote) is downloaded. ?download=1 always
	// downloads.
	disposition := "inline"
	if r.URL.Query().Get("download") != "" || !inlineSafe(attachment.MimeType) {
		disposition = "attachment"
	}
	w.Header().Set("Content-Type", attachment.MimeType)
	w.Header().Set("Content-Length", strconv.FormatInt(int64(len(attachment.Content)), 10))
	w.Header().Set("Content-Disposition", contentDisposition(disposition, attachment.Filename))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "private, max-age=3600")
	_, _ = w.Write(attachment.Content)
}
