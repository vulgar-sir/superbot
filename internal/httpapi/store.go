package httpapi

import (
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"superbot/internal/store"
)

type storeTemplateDTO struct {
	ID           uuid.UUID `json:"id"`
	Name         string    `json:"name"`
	Description  string    `json:"description"`
	Version      int32     `json:"version"`
	InstallCount int32     `json:"install_count"`
	OwnerName    string    `json:"owner_name"`
	CreatedAt    time.Time `json:"created_at"`
}

func (s *Server) storeRoutes(r chi.Router) {
	r.Get("/templates", s.handleListStoreTemplates)
	r.Post("/templates/{templateID}/install", s.handleInstallStoreTemplate)
}

func (s *Server) handleListStoreTemplates(w http.ResponseWriter, r *http.Request) {
	items, err := s.db.ListStoreBotTemplates(r.Context(), store.ListStoreBotTemplatesParams{Limit: 100, Offset: 0})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not list store templates")
		return
	}
	out := make([]storeTemplateDTO, 0, len(items))
	for _, item := range items {
		out = append(out, storeTemplateDTO{
			ID: item.ID, Name: item.Name, Description: item.Description, Version: item.Version,
			InstallCount: item.InstallCount, OwnerName: item.OwnerName, CreatedAt: item.CreatedAt,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"templates": out})
}

func (s *Server) handleInstallStoreTemplate(w http.ResponseWriter, r *http.Request) {
	templateID, ok := parseUUIDParam(w, r, "templateID")
	if !ok {
		return
	}
	template, err := s.db.GetPublicBotTemplate(r.Context(), templateID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "not_found", "template not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", "could not load template")
		return
	}
	if err := s.db.IncrementBotTemplateInstall(r.Context(), templateID); err != nil {
		s.logger.Warn("increment template install failed", "error", err)
	}
	writeJSON(w, http.StatusOK, map[string]any{"template": template.Template, "name": template.Name})
}
