package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"superbot/internal/store"
)

func (s *Server) teamRoutes(r chi.Router) {
	r.Get("/", s.handleListTeams)
	r.Post("/", s.handleCreateTeam)
	r.Route("/{teamID}", func(r chi.Router) {
		r.Get("/", s.handleGetTeam)
		r.Patch("/", s.handleUpdateTeam)
		r.Patch("/placement", s.handleUpdateTeamPlacement)
		r.Patch("/hidden", s.handleSetTeamHidden)
		r.Patch("/unread", s.handleSetTeamUnread)
		r.Delete("/", s.handleDeleteTeam)
		r.Put("/bots", s.handleSetTeamBots)
	})
}

type teamBotDTO struct {
	Bot      botDTO `json:"bot"`
	Position int32  `json:"position"`
	Role     string `json:"role"`
}

type teamDTO struct {
	ID                 uuid.UUID       `json:"id"`
	Name               string          `json:"name"`
	Description        string          `json:"description"`
	Mode               string          `json:"mode"`
	Config             json.RawMessage `json:"config"`
	Bots               []teamBotDTO    `json:"bots"`
	SectionID          *uuid.UUID      `json:"section_id"`
	Position           int32           `json:"position"`
	Pinned             bool            `json:"pinned"`
	Hidden             bool            `json:"hidden"`
	MarkedUnread       bool            `json:"marked_unread"`
	RoomConversationID *uuid.UUID      `json:"room_conversation_id"`
	CreatedAt          time.Time       `json:"created_at"`
	UpdatedAt          time.Time       `json:"updated_at"`
}

func toTeamDTO(team store.Team, rows []store.ListTeamBotsRow) teamDTO {
	config := team.Config
	if len(config) == 0 {
		config = json.RawMessage(`{}`)
	}
	bots := make([]teamBotDTO, 0, len(rows))
	for _, row := range rows {
		bots = append(bots, teamBotDTO{
			Bot:      toBotDTO(row.Bot),
			Position: row.Position,
			Role:     row.Role,
		})
	}
	return teamDTO{
		ID:                 team.ID,
		Name:               team.Name,
		Description:        team.Description,
		Mode:               team.Mode,
		Config:             config,
		Bots:               bots,
		SectionID:          team.SectionID,
		Position:           team.Position,
		Pinned:             team.Pinned,
		Hidden:             team.Hidden,
		MarkedUnread:       team.MarkedUnread,
		RoomConversationID: team.RoomConversationID,
		CreatedAt:          team.CreatedAt,
		UpdatedAt:          team.UpdatedAt,
	}
}

type teamRequest struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Mode        string          `json:"mode"`
	Config      json.RawMessage `json:"config"`
	BotIDs      []uuid.UUID     `json:"bot_ids"`
}

func (s *Server) handleListTeams(w http.ResponseWriter, r *http.Request) {
	teams, err := s.db.ListTeams(r.Context(), accountFrom(r))
	if err != nil {
		s.logger.Error("list teams failed", "error", err)
		writeError(w, http.StatusInternalServerError, "internal", "could not list teams")
		return
	}
	out := make([]teamDTO, 0, len(teams))
	for _, team := range teams {
		rows, err := s.db.ListTeamBots(r.Context(), team.ID)
		if err != nil {
			rows = nil
		}
		out = append(out, toTeamDTO(team, rows))
	}
	writeJSON(w, http.StatusOK, map[string]any{"teams": out})
}

func (s *Server) handleCreateTeam(w http.ResponseWriter, r *http.Request) {
	var req teamRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	accountID := accountFrom(r)
	req.Name = strings.TrimSpace(req.Name)
	req.Description = strings.TrimSpace(req.Description)
	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "team name is required")
		return
	}
	if req.Mode == "" {
		req.Mode = "roundtable"
	}
	if req.Mode != "roundtable" && req.Mode != "pipeline" && req.Mode != "router" && req.Mode != "group" {
		writeError(w, http.StatusBadRequest, "invalid_request", "mode must be roundtable, pipeline, router or group")
		return
	}
	botIDs, err := s.validateTeamBotIDs(r, accountID, req.BotIDs)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	if req.Mode == "group" {
		if len(botIDs) == 0 {
			writeError(w, http.StatusBadRequest, "invalid_request", "a group needs at least one bot")
			return
		}
		if len(botIDs) > 6 {
			writeError(w, http.StatusBadRequest, "invalid_request", "a group can contain at most 6 bots")
			return
		}
	}

	tx, err := s.db.Pool.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not create team")
		return
	}
	defer tx.Rollback(r.Context())
	q := s.db.WithTx(tx)
	team, err := q.CreateTeam(r.Context(), store.CreateTeamParams{
		OwnerID:     accountID,
		Name:        req.Name,
		Description: req.Description,
		Mode:        req.Mode,
		Config:      rawOrEmptyObject(req.Config),
	})
	if err != nil {
		s.logger.Error("create team failed", "error", err)
		writeError(w, http.StatusInternalServerError, "internal", "could not create team")
		return
	}
	for position, botID := range botIDs {
		if err := q.AddTeamBot(r.Context(), store.AddTeamBotParams{
			TeamID:   team.ID,
			BotID:    botID,
			Position: int32(position),
			Role:     "member",
		}); err != nil {
			s.logger.Error("add team bot failed", "error", err)
			writeError(w, http.StatusInternalServerError, "internal", "could not attach bots")
			return
		}
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not create team")
		return
	}
	if req.Mode == "group" {
		conv, convErr := s.chat.CreateTeamConversation(r.Context(), accountID, team.ID, team.Name)
		if convErr != nil {
			s.logger.Error("create group room failed", "team_id", team.ID, "error", convErr)
		} else {
			team.RoomConversationID = &conv.ID
		}
	}
	rows, _ := s.db.ListTeamBots(r.Context(), team.ID)
	writeJSON(w, http.StatusCreated, map[string]any{"team": toTeamDTO(team, rows)})
}

func (s *Server) handleGetTeam(w http.ResponseWriter, r *http.Request) {
	teamID, ok := parseUUIDParam(w, r, "teamID")
	if !ok {
		return
	}
	team, err := s.db.GetTeam(r.Context(), store.GetTeamParams{ID: teamID, OwnerID: accountFrom(r)})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "not_found", "team not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", "could not load team")
		return
	}
	rows, _ := s.db.ListTeamBots(r.Context(), team.ID)
	writeJSON(w, http.StatusOK, map[string]any{"team": toTeamDTO(team, rows)})
}

func (s *Server) handleUpdateTeam(w http.ResponseWriter, r *http.Request) {
	teamID, ok := parseUUIDParam(w, r, "teamID")
	if !ok {
		return
	}
	accountID := accountFrom(r)
	existing, err := s.db.GetTeam(r.Context(), store.GetTeamParams{ID: teamID, OwnerID: accountID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "not_found", "team not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", "could not load team")
		return
	}
	var req teamRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "team name is required")
		return
	}
	if req.Mode == "" {
		req.Mode = existing.Mode
	}
	if req.Mode != "roundtable" && req.Mode != "pipeline" && req.Mode != "router" && req.Mode != "group" {
		writeError(w, http.StatusBadRequest, "invalid_request", "mode must be roundtable, pipeline, router or group")
		return
	}
	if req.Mode == "group" {
		rows, err := s.db.ListTeamBots(r.Context(), existing.ID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal", "could not load team members")
			return
		}
		if len(rows) == 0 {
			writeError(w, http.StatusBadRequest, "invalid_request", "a group needs at least one bot")
			return
		}
		if len(rows) > 6 {
			writeError(w, http.StatusBadRequest, "invalid_request", "a group can contain at most 6 bots")
			return
		}
	}
	team, err := s.db.UpdateTeam(r.Context(), store.UpdateTeamParams{
		ID:          existing.ID,
		OwnerID:     accountID,
		Name:        req.Name,
		Description: strings.TrimSpace(req.Description),
		Mode:        req.Mode,
		Config:      rawOrEmptyObject(req.Config),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not update team")
		return
	}
	rows, _ := s.db.ListTeamBots(r.Context(), team.ID)
	writeJSON(w, http.StatusOK, map[string]any{"team": toTeamDTO(team, rows)})
}

type teamPlacementRequest struct {
	SectionID *uuid.UUID `json:"section_id"`
	Position  int32      `json:"position"`
	Pinned    bool       `json:"pinned"`
}

func (s *Server) handleUpdateTeamPlacement(w http.ResponseWriter, r *http.Request) {
	teamID, ok := parseUUIDParam(w, r, "teamID")
	if !ok {
		return
	}
	accountID := accountFrom(r)
	existing, err := s.db.GetTeam(r.Context(), store.GetTeamParams{ID: teamID, OwnerID: accountID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "not_found", "team not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", "could not load team")
		return
	}
	var req teamPlacementRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	if err := s.ensureSectionOwned(r.Context(), accountID, req.SectionID); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	team, err := s.db.UpdateTeamPlacement(r.Context(), store.UpdateTeamPlacementParams{
		ID:        existing.ID,
		OwnerID:   accountID,
		SectionID: req.SectionID,
		Position:  clampPosition(req.Position),
		Pinned:    req.Pinned,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not update team placement")
		return
	}
	rows, _ := s.db.ListTeamBots(r.Context(), team.ID)
	writeJSON(w, http.StatusOK, map[string]any{"team": toTeamDTO(team, rows)})
}

type setTeamUnreadRequest struct {
	Unread bool `json:"unread"`
}

func (s *Server) handleSetTeamUnread(w http.ResponseWriter, r *http.Request) {
	teamID, ok := parseUUIDParam(w, r, "teamID")
	if !ok {
		return
	}
	var req setTeamUnreadRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	accountID := accountFrom(r)
	if _, err := s.db.GetTeam(r.Context(), store.GetTeamParams{ID: teamID, OwnerID: accountID}); err != nil {
		writeError(w, http.StatusNotFound, "not_found", "team not found")
		return
	}
	if err := s.db.SetTeamMarkedUnread(r.Context(), store.SetTeamMarkedUnreadParams{
		ID:           teamID,
		OwnerID:      accountID,
		MarkedUnread: req.Unread,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not update team unread")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleSetTeamHidden(w http.ResponseWriter, r *http.Request) {
	teamID, ok := parseUUIDParam(w, r, "teamID")
	if !ok {
		return
	}
	var req setHiddenRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	team, err := s.db.SetTeamHidden(r.Context(), store.SetTeamHiddenParams{
		ID:      teamID,
		OwnerID: accountFrom(r),
		Hidden:  req.Hidden,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "not_found", "team not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", "could not update team visibility")
		return
	}
	rows, _ := s.db.ListTeamBots(r.Context(), team.ID)
	writeJSON(w, http.StatusOK, map[string]any{"team": toTeamDTO(team, rows)})
}

func (s *Server) handleDeleteTeam(w http.ResponseWriter, r *http.Request) {
	teamID, ok := parseUUIDParam(w, r, "teamID")
	if !ok {
		return
	}
	n, err := s.db.DeleteTeam(r.Context(), store.DeleteTeamParams{ID: teamID, OwnerID: accountFrom(r)})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not delete team")
		return
	}
	if n == 0 {
		writeError(w, http.StatusNotFound, "not_found", "team not found")
		return
	}
	writeNoContent(w)
}

type setTeamBotsRequest struct {
	Bots []struct {
		BotID    uuid.UUID `json:"bot_id"`
		Position int32     `json:"position"`
		Role     string    `json:"role"`
	} `json:"bots"`
}

func (s *Server) handleSetTeamBots(w http.ResponseWriter, r *http.Request) {
	teamID, ok := parseUUIDParam(w, r, "teamID")
	if !ok {
		return
	}
	accountID := accountFrom(r)
	team, err := s.db.GetTeam(r.Context(), store.GetTeamParams{ID: teamID, OwnerID: accountID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "not_found", "team not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", "could not load team")
		return
	}
	var req setTeamBotsRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	ids := make([]uuid.UUID, 0, len(req.Bots))
	for _, item := range req.Bots {
		ids = append(ids, item.BotID)
	}
	validIDs, err := s.validateTeamBotIDs(r, accountID, ids)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	if team.Mode == "group" && len(validIDs) > 6 {
		writeError(w, http.StatusBadRequest, "invalid_request", "a group can contain at most 6 bots")
		return
	}
	valid := make(map[uuid.UUID]bool, len(validIDs))
	for _, id := range validIDs {
		valid[id] = true
	}

	tx, err := s.db.Pool.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not update team bots")
		return
	}
	defer tx.Rollback(r.Context())
	q := s.db.WithTx(tx)
	if err := q.ClearTeamBots(r.Context(), team.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not update team bots")
		return
	}
	seen := make(map[uuid.UUID]bool)
	for _, item := range req.Bots {
		if !valid[item.BotID] || seen[item.BotID] {
			continue
		}
		seen[item.BotID] = true
		role := strings.TrimSpace(item.Role)
		if role == "" {
			role = "member"
		}
		if err := q.AddTeamBot(r.Context(), store.AddTeamBotParams{
			TeamID:   team.ID,
			BotID:    item.BotID,
			Position: item.Position,
			Role:     role,
		}); err != nil {
			writeError(w, http.StatusInternalServerError, "internal", "could not update team bots")
			return
		}
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not update team bots")
		return
	}
	rows, _ := s.db.ListTeamBots(r.Context(), team.ID)
	writeJSON(w, http.StatusOK, map[string]any{"team": toTeamDTO(team, rows)})
}

func (s *Server) validateTeamBotIDs(r *http.Request, userID uuid.UUID, ids []uuid.UUID) ([]uuid.UUID, error) {
	if len(ids) > 12 {
		return nil, errors.New("a team can contain at most 12 bots")
	}
	out := make([]uuid.UUID, 0, len(ids))
	seen := make(map[uuid.UUID]bool)
	for _, id := range ids {
		if seen[id] {
			continue
		}
		seen[id] = true
		if _, err := s.db.GetBot(r.Context(), store.GetBotParams{ID: id, OwnerID: userID}); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return nil, errors.New("one of the selected bots does not exist")
			}
			return nil, errors.New("could not validate bots")
		}
		out = append(out, id)
	}
	return out, nil
}
