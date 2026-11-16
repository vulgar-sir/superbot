package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"superbot/internal/i18n"
	"superbot/internal/store"
)

type permissionRuleDTO struct {
	ID        uuid.UUID `json:"id"`
	Kind      string    `json:"kind"`
	Pattern   string    `json:"pattern"`
	CreatedAt string    `json:"created_at"`
}

type toolApprovalDTO struct {
	ID             uuid.UUID       `json:"id"`
	ConversationID uuid.UUID       `json:"conversation_id"`
	RunID          uuid.UUID       `json:"run_id"`
	CallID         string          `json:"call_id"`
	ToolName       string          `json:"tool_name"`
	Args           json.RawMessage `json:"args"`
	Status         string          `json:"status"`
	CreatedAt      string          `json:"created_at"`
}

func (s *Server) permissionRoutes(r chi.Router) {
	r.Get("/", s.handleGetPermissions)
	r.Patch("/", s.handleUpdatePermissions)
	r.Post("/rules", s.handleCreatePermissionRule)
	r.Delete("/rules/{ruleID}", s.handleDeletePermissionRule)
}

func (s *Server) approvalRoutes(r chi.Router) {
	r.Get("/", s.handleListToolApprovals)
	r.Post("/{approvalID}", s.handleResolveToolApproval)
}

func (s *Server) handleGetPermissions(w http.ResponseWriter, r *http.Request) {
	user := userFrom(r)
	autoReview, err := s.db.GetUserAutoReview(r.Context(), user.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not load permissions")
		return
	}
	rules, err := s.db.ListPermissionRules(r.Context(), accountFrom(r))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not load permission rules")
		return
	}
	out := make([]permissionRuleDTO, 0, len(rules))
	for _, rule := range rules {
		out = append(out, permissionRuleDTO{
			ID:        rule.ID,
			Kind:      rule.Kind,
			Pattern:   rule.Pattern,
			CreatedAt: rule.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"auto_review": autoReview, "rules": out})
}

type updatePermissionsRequest struct {
	AutoReview *bool `json:"auto_review"`
}

func (s *Server) handleUpdatePermissions(w http.ResponseWriter, r *http.Request) {
	var req updatePermissionsRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	if req.AutoReview == nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "auto_review is required")
		return
	}
	if err := s.db.SetUserAutoReview(r.Context(), store.SetUserAutoReviewParams{
		ID:         accountFrom(r),
		AutoReview: *req.AutoReview,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not update permissions")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"auto_review": *req.AutoReview})
}

type createPermissionRuleRequest struct {
	Kind    string `json:"kind"`
	Pattern string `json:"pattern"`
}

func (s *Server) handleCreatePermissionRule(w http.ResponseWriter, r *http.Request) {
	var req createPermissionRuleRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	if req.Kind != "always_ask" && req.Kind != "allow" {
		writeError(w, http.StatusBadRequest, "invalid_request", "kind must be always_ask or allow")
		return
	}
	pattern := strings.TrimSpace(req.Pattern)
	if pattern == "" || len(pattern) > 500 {
		writeError(w, http.StatusBadRequest, "invalid_request", "pattern is required")
		return
	}
	rule, err := s.db.CreatePermissionRule(r.Context(), store.CreatePermissionRuleParams{
		OwnerID: accountFrom(r),
		Kind:    req.Kind,
		Pattern: pattern,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not create permission rule")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"rule": permissionRuleDTO{
		ID:        rule.ID,
		Kind:      rule.Kind,
		Pattern:   rule.Pattern,
		CreatedAt: rule.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
	}})
}

func (s *Server) handleDeletePermissionRule(w http.ResponseWriter, r *http.Request) {
	ruleID, ok := parseUUIDParam(w, r, "ruleID")
	if !ok {
		return
	}
	if err := s.db.DeletePermissionRule(r.Context(), store.DeletePermissionRuleParams{
		ID:      ruleID,
		OwnerID: accountFrom(r),
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not delete permission rule")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleListToolApprovals(w http.ResponseWriter, r *http.Request) {
	rows, err := s.db.ListPendingToolApprovals(r.Context(), accountFrom(r))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not load approvals")
		return
	}
	conversationFilter := strings.TrimSpace(r.URL.Query().Get("conversation_id"))
	out := make([]toolApprovalDTO, 0, len(rows))
	for _, row := range rows {
		if conversationFilter != "" && row.ConversationID.String() != conversationFilter {
			continue
		}
		out = append(out, toToolApprovalDTO(row))
	}
	writeJSON(w, http.StatusOK, map[string]any{"approvals": out})
}

func toToolApprovalDTO(row store.ToolApproval) toolApprovalDTO {
	return toolApprovalDTO{
		ID:             row.ID,
		ConversationID: row.ConversationID,
		RunID:          row.RunID,
		CallID:         row.CallID,
		ToolName:       row.ToolName,
		Args:           row.Args,
		Status:         row.Status,
		CreatedAt:      row.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
	}
}

type resolveToolApprovalRequest struct {
	Decision string `json:"decision"`
}

func (s *Server) handleResolveToolApproval(w http.ResponseWriter, r *http.Request) {
	approvalID, ok := parseUUIDParam(w, r, "approvalID")
	if !ok {
		return
	}
	var req resolveToolApprovalRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	if req.Decision != "allow" && req.Decision != "deny" {
		writeError(w, http.StatusBadRequest, "invalid_request", "decision must be allow or deny")
		return
	}
	status := "allowed"
	if req.Decision == "deny" {
		status = "denied"
	}
	approval, err := s.db.ResolveToolApproval(r.Context(), store.ResolveToolApprovalParams{
		ID:      approvalID,
		OwnerID: accountFrom(r),
		Status:  status,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "not_found", "approval not found or already resolved")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", "could not resolve approval")
		return
	}
	resume := "none"
	if checkpoint, cpErr := s.db.GetRunCheckpointForApproval(r.Context(), &approvalID); cpErr == nil {
		language := i18n.AccountLanguage(r.Context(), s.db, accountFrom(r))
		content := i18n.Text(language, "工具审批已通过：", "Tool approval granted: ") + approval.ToolName
		if status == "denied" {
			content = i18n.Text(language, "工具审批被拒绝：", "Tool approval denied: ") + approval.ToolName
		}
		meta, _ := json.Marshal(map[string]any{"approval_id": approvalID.String(), "tool_name": approval.ToolName})
		started, resumeErr := s.chat.ResumeAfterApproval(r.Context(), checkpoint, content, meta)
		switch {
		case resumeErr != nil:
			// The decision is recorded; the worker retries the resume when
			// the conversation next goes idle.
			s.logger.Warn("resume after approval failed", "approval_id", approvalID, "error", resumeErr)
			resume = "deferred"
		case started:
			resume = "started"
		default:
			resume = "deferred"
		}
	}
	s.audit(r.Context(), accountFrom(r), userFrom(r).ID, "approval."+status, "tool_approval", &approvalID, map[string]any{
		"tool_name": approval.ToolName,
	})
	writeJSON(w, http.StatusOK, map[string]any{"approval": toToolApprovalDTO(approval), "resume": resume})
}
