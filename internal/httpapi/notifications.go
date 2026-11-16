package httpapi

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"superbot/internal/store"
)

type notificationDTO struct {
	ID           uuid.UUID       `json:"id"`
	Kind         string          `json:"kind"`
	Title        string          `json:"title"`
	Body         string          `json:"body"`
	ResourceType string          `json:"resource_type"`
	ResourceID   string          `json:"resource_id"`
	Metadata     json.RawMessage `json:"metadata"`
	Read         bool            `json:"read"`
	CreatedAt    time.Time       `json:"created_at"`
}

func toNotificationDTO(n store.Notification) notificationDTO {
	return notificationDTO{
		ID: n.ID, Kind: n.Kind, Title: n.Title, Body: n.Body,
		ResourceType: n.ResourceType, ResourceID: n.ResourceID,
		Metadata: n.Metadata, Read: n.ReadAt != nil, CreatedAt: n.CreatedAt,
	}
}

func (s *Server) notificationRoutes(r chi.Router) {
	r.Get("/", s.handleListNotifications)
	r.Get("/unread-count", s.handleUnreadNotificationCount)
	r.Post("/read-all", s.handleMarkAllNotificationsRead)
	r.Post("/{notificationID}/read", s.handleMarkNotificationRead)
}

func (s *Server) handleListNotifications(w http.ResponseWriter, r *http.Request) {
	unreadOnly := r.URL.Query().Get("unread") == "1"
	items, err := s.db.ListNotifications(r.Context(), store.ListNotificationsParams{
		OwnerID:    accountFrom(r),
		Limit:      parseLimit(r.URL.Query().Get("limit"), 30, 100),
		UnreadOnly: pgtype.Bool{Bool: unreadOnly, Valid: true},
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not load notifications")
		return
	}
	out := make([]notificationDTO, 0, len(items))
	for _, item := range items {
		out = append(out, toNotificationDTO(item))
	}
	count, _ := s.db.CountUnreadNotifications(r.Context(), accountFrom(r))
	writeJSON(w, http.StatusOK, map[string]any{"notifications": out, "unread": count})
}

func (s *Server) handleUnreadNotificationCount(w http.ResponseWriter, r *http.Request) {
	count, err := s.db.CountUnreadNotifications(r.Context(), accountFrom(r))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not count notifications")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"unread": count})
}

func (s *Server) handleMarkNotificationRead(w http.ResponseWriter, r *http.Request) {
	id, ok := parseUUIDParam(w, r, "notificationID")
	if !ok {
		return
	}
	n, err := s.db.MarkNotificationRead(r.Context(), store.MarkNotificationReadParams{
		ID: id, OwnerID: accountFrom(r),
	})
	if err != nil || n == 0 {
		writeError(w, http.StatusNotFound, "not_found", "notification not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "read"})
}

func (s *Server) handleMarkAllNotificationsRead(w http.ResponseWriter, r *http.Request) {
	if err := s.db.MarkAllNotificationsRead(r.Context(), accountFrom(r)); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not mark notifications")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "read"})
}
