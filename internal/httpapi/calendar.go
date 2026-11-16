package httpapi

import (
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"superbot/internal/store"
)

type calendarEventDTO struct {
	ID          uuid.UUID `json:"id"`
	Title       string    `json:"title"`
	Description string    `json:"description"`
	Location    string    `json:"location"`
	StartsAt    time.Time `json:"starts_at"`
	EndsAt      time.Time `json:"ends_at"`
	CreatedAt   time.Time `json:"created_at"`
}

func toCalendarEventDTO(event store.CalendarEvent) calendarEventDTO {
	return calendarEventDTO{
		ID: event.ID, Title: event.Title, Description: event.Description, Location: event.Location,
		StartsAt: event.StartsAt, EndsAt: event.EndsAt, CreatedAt: event.CreatedAt,
	}
}

func (s *Server) calendarRoutes(r chi.Router) {
	r.Get("/", s.handleListCalendarEvents)
	r.Post("/", s.handleCreateCalendarEvent)
	r.Delete("/{eventID}", s.handleDeleteCalendarEvent)
}

func (s *Server) handleListCalendarEvents(w http.ResponseWriter, r *http.Request) {
	events, err := s.db.ListCalendarEvents(r.Context(), store.ListCalendarEventsParams{
		OwnerID: accountFrom(r), EndsAt: time.Now().Add(-24 * time.Hour),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not list calendar")
		return
	}
	out := make([]calendarEventDTO, 0, len(events))
	for _, event := range events {
		out = append(out, toCalendarEventDTO(event))
	}
	writeJSON(w, http.StatusOK, map[string]any{"events": out})
}

func (s *Server) handleCreateCalendarEvent(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Title       string `json:"title"`
		Description string `json:"description"`
		Location    string `json:"location"`
		StartsAt    string `json:"starts_at"`
		EndsAt      string `json:"ends_at"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	title := strings.TrimSpace(req.Title)
	if title == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "title is required")
		return
	}
	start, err := time.Parse(time.RFC3339, strings.TrimSpace(req.StartsAt))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "starts_at must be RFC3339")
		return
	}
	end, err := time.Parse(time.RFC3339, strings.TrimSpace(req.EndsAt))
	if err != nil || !end.After(start) {
		writeError(w, http.StatusBadRequest, "invalid_request", "ends_at must be after starts_at")
		return
	}
	userID := userFrom(r).ID
	event, err := s.db.CreateCalendarEvent(r.Context(), store.CreateCalendarEventParams{
		OwnerID: accountFrom(r), Title: title, Description: strings.TrimSpace(req.Description),
		Location: strings.TrimSpace(req.Location), StartsAt: start, EndsAt: end, CreatedByUserID: &userID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not create calendar event")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"event": toCalendarEventDTO(event)})
}

func (s *Server) handleDeleteCalendarEvent(w http.ResponseWriter, r *http.Request) {
	eventID, ok := parseUUIDParam(w, r, "eventID")
	if !ok {
		return
	}
	n, err := s.db.DeleteCalendarEvent(r.Context(), store.DeleteCalendarEventParams{
		ID: eventID, OwnerID: accountFrom(r),
	})
	if err != nil || n == 0 {
		writeError(w, http.StatusNotFound, "not_found", "calendar event not found")
		return
	}
	writeNoContent(w)
}
