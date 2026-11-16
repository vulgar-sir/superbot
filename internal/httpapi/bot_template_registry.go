package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"superbot/internal/store"
)

type botTemplateDTO struct {
	ID              uuid.UUID       `json:"id"`
	Name            string          `json:"name"`
	Description     string          `json:"description"`
	Version         int32           `json:"version"`
	BotID           *uuid.UUID      `json:"bot_id"`
	Visibility      string          `json:"visibility"`
	Status          string          `json:"status"`
	CreatedByUserID *uuid.UUID      `json:"created_by_user_id"`
	CreatedAt       time.Time       `json:"created_at"`
	UpdatedAt       time.Time       `json:"updated_at"`
	Template        json.RawMessage `json:"template,omitempty"`
}

func toBotTemplateDTO(t store.BotTemplate, includeTemplate bool) botTemplateDTO {
	dto := botTemplateDTO{
		ID: t.ID, Name: t.Name, Description: t.Description, Version: t.Version,
		BotID: t.BotID, Visibility: t.Visibility, Status: t.Status,
		CreatedByUserID: t.CreatedByUserID, CreatedAt: t.CreatedAt, UpdatedAt: t.UpdatedAt,
	}
	if includeTemplate {
		dto.Template = t.Template
	}
	return dto
}

func (s *Server) botTemplateRoutes(r chi.Router) {
	r.Get("/", s.handleListBotTemplates)
	r.Post("/", s.handleCreateBotTemplate)
	r.Get("/{templateID}", s.handleGetBotTemplate)
	r.Delete("/{templateID}", s.handleDeleteBotTemplate)
	r.Route("/{templateID}/acl", func(r chi.Router) {
		r.Get("/", s.handleListTemplateACL)
		r.Put("/", s.handleSetTemplateACL)
		r.Delete("/{userID}", s.handleDeleteTemplateACL)
	})
}

func (s *Server) templateAccess(ctx context.Context, role string, userID uuid.UUID, template store.BotTemplate) (string, bool) {
	if role == "owner" || role == "admin" {
		return "editor", true
	}
	if template.CreatedByUserID != nil && *template.CreatedByUserID == userID {
		return "editor", true
	}
	if acl, err := s.db.GetResourceACLRole(ctx, store.GetResourceACLRoleParams{
		ResourceType: "template", ResourceID: template.ID, UserID: &userID,
	}); err == nil {
		return acl, true
	}
	if template.Visibility == "owner" {
		return "", false
	}
	return "viewer", true
}

func (s *Server) templateForEditor(w http.ResponseWriter, r *http.Request) (store.BotTemplate, bool) {
	templateID, ok := parseUUIDParam(w, r, "templateID")
	if !ok {
		return store.BotTemplate{}, false
	}
	template, err := s.db.GetBotTemplateForOwner(r.Context(), store.GetBotTemplateForOwnerParams{
		ID: templateID, OwnerID: accountFrom(r),
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "template not found")
		return store.BotTemplate{}, false
	}
	role, _ := s.accountRole(r.Context(), accountFrom(r), userFrom(r).ID)
	if access, ok := s.templateAccess(r.Context(), role, userFrom(r).ID, template); !ok || access != "editor" {
		writeError(w, http.StatusForbidden, "forbidden", "template is read-only for this user")
		return store.BotTemplate{}, false
	}
	return template, true
}

func (s *Server) handleListBotTemplates(w http.ResponseWriter, r *http.Request) {
	rows, err := s.db.ListBotTemplates(r.Context(), accountFrom(r))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not list templates")
		return
	}
	role, _ := s.accountRole(r.Context(), accountFrom(r), userFrom(r).ID)
	out := make([]botTemplateDTO, 0, len(rows))
	for _, row := range rows {
		if _, ok := s.templateAccess(r.Context(), role, userFrom(r).ID, row); !ok {
			continue
		}
		out = append(out, toBotTemplateDTO(row, false))
	}
	writeJSON(w, http.StatusOK, map[string]any{"templates": out})
}

func (s *Server) handleCreateBotTemplate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name        string     `json:"name"`
		Description string     `json:"description"`
		BotID       *uuid.UUID `json:"bot_id"`
		Visibility  string     `json:"visibility"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" || req.BotID == nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "name and bot_id are required")
		return
	}
	bot, err := s.db.GetBot(r.Context(), store.GetBotParams{ID: *req.BotID, OwnerID: accountFrom(r)})
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "bot not found")
		return
	}
	raw, err := json.Marshal(s.buildBotTemplate(r.Context(), accountFrom(r), bot))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not encode template")
		return
	}
	visibility := req.Visibility
	if visibility != "owner" && visibility != "public" {
		visibility = "account"
	}
	userID := userFrom(r).ID
	template, err := s.db.CreateBotTemplate(r.Context(), store.CreateBotTemplateParams{
		OwnerID: accountFrom(r), Name: name, Description: strings.TrimSpace(req.Description),
		BotID: req.BotID, Template: raw, Visibility: visibility, CreatedByUserID: &userID,
	})
	if err != nil {
		writeError(w, http.StatusConflict, "conflict", "template name already exists")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"template": toBotTemplateDTO(template, true)})
}

func (s *Server) handleGetBotTemplate(w http.ResponseWriter, r *http.Request) {
	templateID, ok := parseUUIDParam(w, r, "templateID")
	if !ok {
		return
	}
	template, err := s.db.GetBotTemplateForOwner(r.Context(), store.GetBotTemplateForOwnerParams{
		ID: templateID, OwnerID: accountFrom(r),
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "template not found")
		return
	}
	role, _ := s.accountRole(r.Context(), accountFrom(r), userFrom(r).ID)
	if _, ok := s.templateAccess(r.Context(), role, userFrom(r).ID, template); !ok {
		writeError(w, http.StatusNotFound, "not_found", "template not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"template": toBotTemplateDTO(template, true)})
}

func (s *Server) handleDeleteBotTemplate(w http.ResponseWriter, r *http.Request) {
	template, ok := s.templateForEditor(w, r)
	if !ok {
		return
	}
	n, err := s.db.DeleteBotTemplate(r.Context(), store.DeleteBotTemplateParams{
		ID: template.ID, OwnerID: accountFrom(r),
	})
	if err != nil || n == 0 {
		writeError(w, http.StatusNotFound, "not_found", "template not found")
		return
	}
	writeNoContent(w)
}

func (s *Server) handleListTemplateACL(w http.ResponseWriter, r *http.Request) {
	template, ok := s.templateForEditor(w, r)
	if !ok {
		return
	}
	rows, err := s.db.ListResourceACL(r.Context(), store.ListResourceACLParams{
		ResourceType: "template", ResourceID: template.ID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not load template access")
		return
	}
	out := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		out = append(out, map[string]any{"user_id": row.UserID, "email": row.Email, "display_name": row.DisplayName, "role": row.Role})
	}
	writeJSON(w, http.StatusOK, map[string]any{"acl": out})
}

func (s *Server) handleSetTemplateACL(w http.ResponseWriter, r *http.Request) {
	template, ok := s.templateForEditor(w, r)
	if !ok {
		return
	}
	var req struct {
		UserID uuid.UUID `json:"user_id"`
		Role   string    `json:"role"`
	}
	if err := decodeJSON(w, r, &req); err != nil || req.UserID == uuid.Nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "user_id is required")
		return
	}
	if req.Role != "viewer" && req.Role != "editor" && req.Role != "runner" {
		writeError(w, http.StatusBadRequest, "invalid_request", "role must be viewer, editor or runner")
		return
	}
	if _, err := s.db.GetAccountMembership(r.Context(), store.GetAccountMembershipParams{
		AccountID: accountFrom(r), UserID: req.UserID,
	}); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "user is not a member of this account")
		return
	}
	if err := s.db.UpsertResourceACL(r.Context(), store.UpsertResourceACLParams{
		AccountID: accountFrom(r), ResourceType: "template", ResourceID: template.ID,
		UserID: &req.UserID, Role: req.Role,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not update template access")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleDeleteTemplateACL(w http.ResponseWriter, r *http.Request) {
	template, ok := s.templateForEditor(w, r)
	if !ok {
		return
	}
	userID, ok := parseUUIDParam(w, r, "userID")
	if !ok {
		return
	}
	if err := s.db.DeleteResourceACL(r.Context(), store.DeleteResourceACLParams{
		ResourceType: "template", ResourceID: template.ID, UserID: &userID,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not remove template access")
		return
	}
	writeNoContent(w)
}
