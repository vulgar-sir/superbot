package httpapi

import (
	"errors"
	"io"
	"mime"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"superbot/internal/store"
)

type botFileDTO struct {
	Name     string    `json:"name"`
	Path     string    `json:"path"`
	IsDir    bool      `json:"is_dir"`
	Size     int64     `json:"size"`
	Modified time.Time `json:"modified"`
}

func (s *Server) botWorkspaceRoot(botID string) string {
	return filepath.Join(s.cfg.DataDir, "workspaces", botID)
}

func (s *Server) handleListBotFiles(w http.ResponseWriter, r *http.Request) {
	botID, ok := parseUUIDParam(w, r, "botID")
	if !ok {
		return
	}
	if _, err := s.db.GetBot(r.Context(), store.GetBotParams{ID: botID, OwnerID: accountFrom(r)}); err != nil {
		writeError(w, http.StatusNotFound, "not_found", "bot not found")
		return
	}
	listRootFiles(w, r, s.botWorkspaceRoot(botID.String()))
}

// listRootFiles lists one directory under rootPath, confined by os.Root.
func listRootFiles(w http.ResponseWriter, r *http.Request, rootPath string) {
	root, err := os.OpenRoot(rootPath)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"files": []botFileDTO{}, "path": ""})
		return
	}
	defer root.Close()
	relative := strings.TrimPrefix(path.Clean("/"+r.URL.Query().Get("path")), "/")
	if relative == "" || relative == "." {
		relative = ""
	}
	openPath := relative
	if openPath == "" {
		openPath = "."
	}
	directory, err := root.Open(openPath)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "directory not found")
		return
	}
	defer directory.Close()
	entries, err := directory.ReadDir(-1)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not read directory")
		return
	}
	files := make([]botFileDTO, 0, len(entries))
	for _, entry := range entries {
		info, infoErr := entry.Info()
		if infoErr != nil {
			continue
		}
		files = append(files, botFileDTO{
			Name: entry.Name(), Path: path.Join(relative, entry.Name()),
			IsDir: entry.IsDir(), Size: info.Size(), Modified: info.ModTime(),
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"files": files, "path": relative})
}

func (s *Server) handleBotFileContent(w http.ResponseWriter, r *http.Request) {
	botID, ok := parseUUIDParam(w, r, "botID")
	if !ok {
		return
	}
	if _, err := s.db.GetBot(r.Context(), store.GetBotParams{ID: botID, OwnerID: accountFrom(r)}); err != nil {
		writeError(w, http.StatusNotFound, "not_found", "bot not found")
		return
	}
	serveRootFile(w, r, s.botWorkspaceRoot(botID.String()))
}

// serveRootFile streams one file under rootPath, confined by os.Root.
func serveRootFile(w http.ResponseWriter, r *http.Request, rootPath string) {
	root, err := os.OpenRoot(rootPath)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "file not found")
		return
	}
	defer root.Close()
	relative := strings.TrimPrefix(path.Clean("/"+r.URL.Query().Get("path")), "/")
	if relative == "" || relative == "." {
		writeError(w, http.StatusBadRequest, "invalid_request", "path is required")
		return
	}
	file, err := root.Open(relative)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			writeError(w, http.StatusNotFound, "not_found", "file not found")
			return
		}
		writeError(w, http.StatusBadRequest, "invalid_request", "could not open file")
		return
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || info.IsDir() {
		writeError(w, http.StatusBadRequest, "invalid_request", "path is not a file")
		return
	}
	if info.Size() > 25<<20 {
		writeError(w, http.StatusBadRequest, "invalid_request", "file is too large")
		return
	}
	contentType := mime.TypeByExtension(filepath.Ext(relative))
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", `inline; filename="`+path.Base(relative)+`"`)
	_, _ = io.Copy(w, file)
}

// driveRoot is the account's shared drive: one folder every bot of the
// account sees at /shared in its computer.
func (s *Server) driveRoot(accountID string) string {
	return filepath.Join(s.cfg.DataDir, "shared", accountID)
}

func (s *Server) handleListDriveFiles(w http.ResponseWriter, r *http.Request) {
	listRootFiles(w, r, s.driveRoot(accountFrom(r).String()))
}

func (s *Server) handleDriveFileContent(w http.ResponseWriter, r *http.Request) {
	serveRootFile(w, r, s.driveRoot(accountFrom(r).String()))
}
