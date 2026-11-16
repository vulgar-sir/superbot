package httpapi

import (
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"superbot/internal/store"
)

type botRoutineDTO struct {
	ID           uuid.UUID  `json:"id"`
	BotID        uuid.UUID  `json:"bot_id"`
	Name         string     `json:"name"`
	Schedule     string     `json:"schedule"`
	Timezone     string     `json:"timezone"`
	Timing       string     `json:"timing"`
	Enabled      bool       `json:"enabled"`
	Paused       bool       `json:"paused"`
	Prompt       string     `json:"prompt"`
	LastRunAt    *time.Time `json:"last_run_at"`
	NextRunAt    *time.Time `json:"next_run_at"`
	TriggerToken string     `json:"trigger_token"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
}

func toBotRoutineDTO(r store.BotRoutine) botRoutineDTO {
	return botRoutineDTO{
		ID:           r.ID,
		BotID:        r.BotID,
		Name:         r.Name,
		Schedule:     r.Schedule,
		Timezone:     r.Timezone,
		Timing:       r.Timing,
		Enabled:      r.Enabled,
		Paused:       r.Paused,
		Prompt:       r.Prompt,
		LastRunAt:    r.LastRunAt,
		NextRunAt:    r.NextRunAt,
		TriggerToken: r.TriggerToken,
		CreatedAt:    r.CreatedAt,
		UpdatedAt:    r.UpdatedAt,
	}
}

func (s *Server) botRoutineRoutes(r chi.Router) {
	r.Get("/", s.handleListBotRoutines)
	r.Post("/", s.handleCreateBotRoutine)
	r.Route("/{routineID}", func(r chi.Router) {
		r.Patch("/", s.handleUpdateBotRoutine)
		r.Post("/test", s.handleTestBotRoutine)
		r.Delete("/", s.handleDeleteBotRoutine)
	})
}

func (s *Server) handleListBotRoutines(w http.ResponseWriter, r *http.Request) {
	var botID *uuid.UUID
	if raw := strings.TrimSpace(r.URL.Query().Get("bot_id")); raw != "" {
		parsed, err := uuid.Parse(raw)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_request", "invalid bot_id")
			return
		}
		botID = &parsed
	}
	items, err := s.routines.ListBotRoutines(r.Context(), accountFrom(r), botID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	out := make([]botRoutineDTO, 0, len(items))
	for _, item := range items {
		out = append(out, toBotRoutineDTO(item))
	}
	writeJSON(w, http.StatusOK, map[string]any{"routines": out})
}

type botRoutineCreateRequest struct {
	BotID    uuid.UUID `json:"bot_id"`
	Name     string    `json:"name"`
	Schedule string    `json:"schedule"`
	Timezone string    `json:"timezone"`
	Timing   string    `json:"timing"`
	Prompt   string    `json:"prompt"`
	Enabled  *bool     `json:"enabled"`
}

func (s *Server) handleCreateBotRoutine(w http.ResponseWriter, r *http.Request) {
	var req botRoutineCreateRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	routine, err := s.routines.CreateBotRoutine(
		r.Context(), accountFrom(r), req.BotID,
		req.Name, req.Schedule, req.Timezone, req.Timing, req.Prompt, enabled,
	)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"routine": toBotRoutineDTO(routine)})
}

type botRoutineUpdateRequest struct {
	Name     string `json:"name"`
	Schedule string `json:"schedule"`
	Timezone string `json:"timezone"`
	Timing   string `json:"timing"`
	Prompt   string `json:"prompt"`
	Enabled  *bool  `json:"enabled"`
	Paused   *bool  `json:"paused"`
}

func (s *Server) handleUpdateBotRoutine(w http.ResponseWriter, r *http.Request) {
	routineID, ok := parseUUIDParam(w, r, "routineID")
	if !ok {
		return
	}
	existing, err := s.db.GetBotRoutineForOwner(r.Context(), store.GetBotRoutineForOwnerParams{
		ID: routineID, OwnerID: accountFrom(r),
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "routine not found")
		return
	}
	var req botRoutineUpdateRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	enabled := existing.Enabled
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	paused := existing.Paused
	if req.Paused != nil {
		paused = *req.Paused
	}
	name := existing.Name
	if strings.TrimSpace(req.Name) != "" {
		name = req.Name
	}
	schedule := existing.Schedule
	if strings.TrimSpace(req.Schedule) != "" {
		schedule = req.Schedule
	}
	timezone := existing.Timezone
	if strings.TrimSpace(req.Timezone) != "" {
		timezone = req.Timezone
	}
	timing := existing.Timing
	if strings.TrimSpace(req.Timing) != "" {
		timing = req.Timing
	}
	prompt := existing.Prompt
	if strings.TrimSpace(req.Prompt) != "" {
		prompt = req.Prompt
	}
	routine, err := s.routines.UpdateBotRoutine(
		r.Context(), accountFrom(r), routineID,
		name, schedule, timezone, timing, prompt, enabled, paused,
	)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"routine": toBotRoutineDTO(routine)})
}

func (s *Server) handleTestBotRoutine(w http.ResponseWriter, r *http.Request) {
	routineID, ok := parseUUIDParam(w, r, "routineID")
	if !ok {
		return
	}
	if err := s.routines.TestBotRoutine(r.Context(), accountFrom(r), routineID); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "queued"})
}

func (s *Server) handleDeleteBotRoutine(w http.ResponseWriter, r *http.Request) {
	routineID, ok := parseUUIDParam(w, r, "routineID")
	if !ok {
		return
	}
	if err := s.routines.DeleteBotRoutine(r.Context(), accountFrom(r), routineID); err != nil {
		writeError(w, http.StatusNotFound, "not_found", "routine not found")
		return
	}
	writeNoContent(w)
}

// handleRoutineWebhook lets external systems start a routine by its secret
// token URL. It is intentionally unauthenticated (the token is the
// credential), idempotent per call, and refuses paused/disabled routines.
func (s *Server) handleRoutineWebhook(w http.ResponseWriter, r *http.Request) {
	token := strings.TrimSpace(chi.URLParam(r, "token"))
	if token == "" {
		writeError(w, http.StatusNotFound, "not_found", "routine not found")
		return
	}
	routine, err := s.db.GetBotRoutineByTriggerToken(r.Context(), token)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "routine not found")
		return
	}
	if !routine.Enabled || routine.Paused {
		writeError(w, http.StatusConflict, "routine_inactive", "routine is disabled or paused")
		return
	}
	if err := s.routines.Trigger(r.Context(), routine); err != nil {
		s.logger.Error("routine webhook trigger failed", "routine_id", routine.ID, "error", err)
		writeError(w, http.StatusInternalServerError, "internal", "could not trigger routine")
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"status": "queued", "routine_id": routine.ID})
}
