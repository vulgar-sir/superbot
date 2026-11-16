package httpapi

import (
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"superbot/internal/store"
)

type clientErrorRequest struct {
	Kind    string `json:"kind"`
	Message string `json:"message"`
	Stack   string `json:"stack"`
	URL     string `json:"url"`
}

func (s *Server) diagnosticRoutes(r chi.Router) {
	r.Post("/client-error", s.handleClientError)
}

func (s *Server) handleClientError(w http.ResponseWriter, r *http.Request) {
	accountID := accountFrom(r)
	if !s.dataSharingEnabled(r.Context(), accountID) {
		writeNoContent(w)
		return
	}
	var req clientErrorRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	message := strings.TrimSpace(req.Message)
	if message == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "message is required")
		return
	}
	kind := strings.TrimSpace(req.Kind)
	if kind == "" {
		kind = "client_error"
	}
	user := userFrom(r)
	err := s.db.CreateDiagnosticEvent(r.Context(), store.CreateDiagnosticEventParams{
		AccountID: &accountID,
		UserID:    &user.ID,
		Kind:      truncateRunes(kind, 64),
		Message:   truncateRunes(message, 500),
		Stack:     truncateRunes(strings.TrimSpace(req.Stack), 4000),
		Url:       truncateRunes(strings.TrimSpace(req.URL), 500),
		UserAgent: truncateRunes(r.UserAgent(), 300),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not record diagnostic event")
		return
	}
	writeNoContent(w)
}

func truncateRunes(value string, limit int) string {
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit])
}
