package httpapi

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"superbot/internal/store"
)

type routineDTO struct {
	ID        uuid.UUID  `json:"id"`
	TeamID    uuid.UUID  `json:"team_id"`
	Name      string     `json:"name"`
	Cron      string     `json:"cron"`
	Prompt    string     `json:"prompt"`
	Enabled   bool       `json:"enabled"`
	LastRunAt *time.Time `json:"last_run_at"`
	NextRunAt *time.Time `json:"next_run_at"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
}

func toRoutineDTO(routine store.Routine) routineDTO {
	return routineDTO{
		ID:        routine.ID,
		TeamID:    routine.TeamID,
		Name:      routine.Name,
		Cron:      routine.Cron,
		Prompt:    routine.Prompt,
		Enabled:   routine.Enabled,
		LastRunAt: routine.LastRunAt,
		NextRunAt: routine.NextRunAt,
		CreatedAt: routine.CreatedAt,
		UpdatedAt: routine.UpdatedAt,
	}
}

func (s *Server) routineRoutes(r chi.Router) {
	r.Get("/", s.handleListRoutines)
	r.Post("/", s.handleCreateRoutine)
	r.Route("/{routineID}", func(r chi.Router) {
		r.Patch("/", s.handleUpdateRoutine)
		r.Delete("/", s.handleDeleteRoutine)
	})
}

func (s *Server) handleListRoutines(w http.ResponseWriter, r *http.Request) {
	items, err := s.routines.List(r.Context(), accountFrom(r))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not list routines")
		return
	}
	out := make([]routineDTO, 0, len(items))
	for _, item := range items {
		out = append(out, toRoutineDTO(item))
	}
	writeJSON(w, http.StatusOK, map[string]any{"routines": out})
}

type routineCreateRequest struct {
	TeamID  uuid.UUID `json:"team_id"`
	Name    string    `json:"name"`
	Cron    string    `json:"cron"`
	Prompt  string    `json:"prompt"`
	Enabled *bool     `json:"enabled"`
}

func (s *Server) handleCreateRoutine(w http.ResponseWriter, r *http.Request) {
	var req routineCreateRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	routine, err := s.routines.Create(r.Context(), accountFrom(r), req.TeamID, req.Name, req.Cron, req.Prompt, enabled)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"routine": toRoutineDTO(routine)})
}

type routineUpdateRequest struct {
	Name    string `json:"name"`
	Cron    string `json:"cron"`
	Prompt  string `json:"prompt"`
	Enabled *bool  `json:"enabled"`
}

func (s *Server) handleUpdateRoutine(w http.ResponseWriter, r *http.Request) {
	routineID, ok := parseUUIDParam(w, r, "routineID")
	if !ok {
		return
	}
	var req routineUpdateRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	routine, err := s.routines.Update(r.Context(), accountFrom(r), routineID, req.Name, req.Cron, req.Prompt, enabled)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"routine": toRoutineDTO(routine)})
}

func (s *Server) handleDeleteRoutine(w http.ResponseWriter, r *http.Request) {
	routineID, ok := parseUUIDParam(w, r, "routineID")
	if !ok {
		return
	}
	if err := s.routines.Delete(r.Context(), accountFrom(r), routineID); err != nil {
		writeError(w, http.StatusNotFound, "not_found", "routine not found")
		return
	}
	writeNoContent(w)
}
