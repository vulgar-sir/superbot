package httpapi

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	"superbot/internal/store"
)

type skillDTO struct {
	ID              uuid.UUID  `json:"id"`
	Name            string     `json:"name"`
	Description     string     `json:"description"`
	DescriptionEn   string     `json:"description_en"`
	Version         string     `json:"version"`
	Source          string     `json:"source"`
	Status          string     `json:"status"`
	CreatedBy       string     `json:"created_by"`
	Visibility      string     `json:"visibility"`
	CreatedByUserID *uuid.UUID `json:"created_by_user_id"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

func toSkillDTO(skill store.Skill) skillDTO {
	return skillDTO{
		ID:              skill.ID,
		Name:            skill.Name,
		Description:     skill.Description,
		DescriptionEn:   skill.DescriptionEn,
		Version:         skill.Version,
		Source:          skill.Source,
		Status:          skill.Status,
		CreatedBy:       skill.CreatedBy,
		Visibility:      skill.Visibility,
		CreatedByUserID: skill.CreatedByUserID,
		CreatedAt:       skill.CreatedAt,
		UpdatedAt:       skill.UpdatedAt,
	}
}

func (s *Server) skillAccess(ctx context.Context, role string, userID uuid.UUID, skill store.Skill) (string, bool) {
	if role == "owner" || role == "admin" {
		return "editor", true
	}
	if skill.CreatedByUserID != nil && *skill.CreatedByUserID == userID {
		return "editor", true
	}
	if acl, err := s.db.GetResourceACLRole(ctx, store.GetResourceACLRoleParams{
		ResourceType: "skill", ResourceID: skill.ID, UserID: &userID,
	}); err == nil {
		return acl, true
	}
	if skill.Visibility == "owner" {
		return "", false
	}
	return "viewer", true
}

func (s *Server) skillForEditor(w http.ResponseWriter, r *http.Request) (store.Skill, bool) {
	skillID, ok := parseUUIDParam(w, r, "skillID")
	if !ok {
		return store.Skill{}, false
	}
	skill, err := s.db.GetSkill(r.Context(), store.GetSkillParams{ID: skillID, OwnerID: accountFrom(r)})
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "skill not found")
		return store.Skill{}, false
	}
	role, _ := s.accountRole(r.Context(), accountFrom(r), userFrom(r).ID)
	if access, ok := s.skillAccess(r.Context(), role, userFrom(r).ID, skill); !ok || access != "editor" {
		writeError(w, http.StatusForbidden, "forbidden", "skill is read-only for this user")
		return store.Skill{}, false
	}
	return skill, true
}

func (s *Server) skillRoutes(r chi.Router) {
	r.Get("/", s.handleListSkills)
	r.Post("/", s.handleCreateSkill)
	r.Post("/{skillID}/approve", s.handleApproveSkill)
	r.Post("/{skillID}/disable", s.handleDisableSkill)
	r.Delete("/{skillID}", s.handleDeleteSkill)
	r.Route("/{skillID}/acl", func(r chi.Router) {
		r.Get("/", s.handleListSkillACL)
		r.Put("/", s.handleSetSkillACL)
		r.Delete("/{userID}", s.handleDeleteSkillACL)
	})
}

func (s *Server) handleListSkills(w http.ResponseWriter, r *http.Request) {
	items, err := s.skills.List(r.Context(), accountFrom(r))
	if err != nil {
		s.logger.Error("list skills failed", "error", err)
		writeError(w, http.StatusInternalServerError, "internal", "could not list skills")
		return
	}
	role, _ := s.accountRole(r.Context(), accountFrom(r), userFrom(r).ID)
	out := make([]skillDTO, 0, len(items))
	for _, skill := range items {
		if _, ok := s.skillAccess(r.Context(), role, userFrom(r).ID, skill); !ok {
			continue
		}
		out = append(out, toSkillDTO(skill))
	}
	writeJSON(w, http.StatusOK, map[string]any{"skills": out})
}

type skillRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Content     string `json:"content"`
	Visibility  string `json:"visibility"`
}

func (s *Server) handleCreateSkill(w http.ResponseWriter, r *http.Request) {
	var req skillRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	skill, err := s.skills.Create(r.Context(), accountFrom(r), req.Name, req.Description, req.Content, "user")
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			writeError(w, http.StatusConflict, "conflict", "a skill with this name already exists")
			return
		}
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	visibility := req.Visibility
	if visibility != "owner" {
		visibility = "account"
	}
	userID := userFrom(r).ID
	_ = s.db.SetSkillAccess(r.Context(), store.SetSkillAccessParams{
		ID: skill.ID, OwnerID: accountFrom(r), Visibility: visibility, CreatedByUserID: &userID,
	})
	skill.Visibility = visibility
	skill.CreatedByUserID = &userID
	writeJSON(w, http.StatusCreated, map[string]any{"skill": toSkillDTO(skill)})
}

func (s *Server) handleApproveSkill(w http.ResponseWriter, r *http.Request) {
	skillID, ok := parseUUIDParam(w, r, "skillID")
	if !ok {
		return
	}
	if _, ok := s.skillForEditor(w, r); !ok {
		return
	}
	skill, err := s.skills.Approve(r.Context(), accountFrom(r), skillID)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "skill not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"skill": toSkillDTO(skill)})
}

func (s *Server) handleDisableSkill(w http.ResponseWriter, r *http.Request) {
	skillID, ok := parseUUIDParam(w, r, "skillID")
	if !ok {
		return
	}
	if _, ok := s.skillForEditor(w, r); !ok {
		return
	}
	skill, err := s.skills.Disable(r.Context(), accountFrom(r), skillID)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "skill not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"skill": toSkillDTO(skill)})
}

func (s *Server) handleDeleteSkill(w http.ResponseWriter, r *http.Request) {
	skillID, ok := parseUUIDParam(w, r, "skillID")
	if !ok {
		return
	}
	if _, ok := s.skillForEditor(w, r); !ok {
		return
	}
	if err := s.skills.Delete(r.Context(), accountFrom(r), skillID); err != nil {
		writeError(w, http.StatusNotFound, "not_found", "skill not found")
		return
	}
	writeNoContent(w)
}

type botSkillDTO struct {
	Skill   skillDTO `json:"skill"`
	Enabled bool     `json:"enabled"`
}

func (s *Server) handleListBotSkills(w http.ResponseWriter, r *http.Request) {
	botID, ok := parseUUIDParam(w, r, "botID")
	if !ok {
		return
	}
	if _, err := s.db.GetBot(r.Context(), store.GetBotParams{ID: botID, OwnerID: accountFrom(r)}); err != nil {
		writeError(w, http.StatusNotFound, "not_found", "bot not found")
		return
	}
	rows, err := s.db.ListBotSkills(r.Context(), botID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not list bot skills")
		return
	}
	out := make([]botSkillDTO, 0, len(rows))
	for _, row := range rows {
		out = append(out, botSkillDTO{Skill: toSkillDTO(row.Skill), Enabled: row.Enabled})
	}
	writeJSON(w, http.StatusOK, map[string]any{"skills": out})
}

func (s *Server) handleSetBotSkills(w http.ResponseWriter, r *http.Request) {
	botID, ok := parseUUIDParam(w, r, "botID")
	if !ok {
		return
	}
	var req struct {
		SkillIDs []uuid.UUID `json:"skill_ids"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	if err := s.skills.SetBotSkills(r.Context(), accountFrom(r), botID, req.SkillIDs); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	rows, _ := s.db.ListBotSkills(r.Context(), botID)
	out := make([]botSkillDTO, 0, len(rows))
	for _, row := range rows {
		out = append(out, botSkillDTO{Skill: toSkillDTO(row.Skill), Enabled: row.Enabled})
	}
	writeJSON(w, http.StatusOK, map[string]any{"skills": out})
}

func (s *Server) handleListSkillACL(w http.ResponseWriter, r *http.Request) {
	skill, ok := s.skillForEditor(w, r)
	if !ok {
		return
	}
	rows, err := s.db.ListResourceACL(r.Context(), store.ListResourceACLParams{
		ResourceType: "skill", ResourceID: skill.ID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not load skill access")
		return
	}
	out := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		out = append(out, map[string]any{"user_id": row.UserID, "email": row.Email, "display_name": row.DisplayName, "role": row.Role})
	}
	writeJSON(w, http.StatusOK, map[string]any{"acl": out})
}

func (s *Server) handleSetSkillACL(w http.ResponseWriter, r *http.Request) {
	skill, ok := s.skillForEditor(w, r)
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
		AccountID: accountFrom(r), ResourceType: "skill", ResourceID: skill.ID,
		UserID: &req.UserID, Role: req.Role,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not update skill access")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleDeleteSkillACL(w http.ResponseWriter, r *http.Request) {
	skill, ok := s.skillForEditor(w, r)
	if !ok {
		return
	}
	userID, ok := parseUUIDParam(w, r, "userID")
	if !ok {
		return
	}
	if err := s.db.DeleteResourceACL(r.Context(), store.DeleteResourceACLParams{
		ResourceType: "skill", ResourceID: skill.ID, UserID: &userID,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not remove skill access")
		return
	}
	writeNoContent(w)
}
