package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"

	"superbot/internal/store"
)

type auditLogDTO struct {
	ID         uuid.UUID       `json:"id"`
	ActorType  string          `json:"actor_type"`
	ActorID    *uuid.UUID      `json:"actor_id"`
	Action     string          `json:"action"`
	TargetType string          `json:"target_type"`
	TargetID   *uuid.UUID      `json:"target_id"`
	Payload    json.RawMessage `json:"payload"`
	CreatedAt  time.Time       `json:"created_at"`
}

func toAuditLogDTO(entry store.AuditLog) auditLogDTO {
	payload := entry.Payload
	if len(payload) == 0 {
		payload = json.RawMessage(`{}`)
	}
	return auditLogDTO{
		ID: entry.ID, ActorType: entry.ActorType, ActorID: entry.ActorID,
		Action: entry.Action, TargetType: entry.TargetType, TargetID: entry.TargetID,
		Payload: payload, CreatedAt: entry.CreatedAt,
	}
}

// audit persists a sensitive action for the current account. Failures are
// intentionally ignored: audit storage must never break the user action.
func (s *Server) audit(ctx context.Context, accountID, actorID uuid.UUID, action, targetType string, targetID *uuid.UUID, payload any) {
	raw, err := json.Marshal(payload)
	if err != nil {
		raw = json.RawMessage(`{}`)
	}
	_, err = s.db.CreateAuditLog(ctx, store.CreateAuditLogParams{
		AccountID: &accountID, ActorType: "user", ActorID: &actorID,
		Action: action, TargetType: targetType, TargetID: targetID, Payload: raw,
	})
	if err != nil {
		s.logger.Warn("audit log write failed", "action", action, "error", err)
	}
}

func (s *Server) handleListAuditLogs(w http.ResponseWriter, r *http.Request) {
	accountID := accountFrom(r)
	role, err := s.accountRole(r.Context(), accountID, userFrom(r).ID)
	if err != nil || (role != "owner" && role != "admin") {
		writeError(w, http.StatusForbidden, "forbidden", "only account owners and admins can view audit logs")
		return
	}
	limit := int32(100)
	if raw := r.URL.Query().Get("limit"); raw != "" {
		if parsed, parseErr := strconv.Atoi(raw); parseErr == nil && parsed > 0 && parsed <= 500 {
			limit = int32(parsed)
		}
	}
	offset := int32(0)
	if raw := r.URL.Query().Get("offset"); raw != "" {
		if parsed, parseErr := strconv.Atoi(raw); parseErr == nil && parsed >= 0 {
			offset = int32(parsed)
		}
	}
	entries, err := s.db.ListAuditLogs(r.Context(), store.ListAuditLogsParams{
		AccountID: &accountID, Limit: limit, Offset: offset,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not list audit logs")
		return
	}
	out := make([]auditLogDTO, 0, len(entries))
	for _, entry := range entries {
		out = append(out, toAuditLogDTO(entry))
	}
	writeJSON(w, http.StatusOK, map[string]any{"logs": out})
}
