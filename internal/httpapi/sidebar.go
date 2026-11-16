package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"superbot/internal/store"
)

const (
	maxSectionNameRunes = 80
	maxSidebarPosition  = 10000
)

func (s *Server) sectionRoutes(r chi.Router) {
	r.Post("/", s.handleCreateSection)
	r.Patch("/{sectionID}", s.handleUpdateSection)
	r.Delete("/{sectionID}", s.handleDeleteSection)
}

type sidebarSectionDTO struct {
	ID        uuid.UUID `json:"id"`
	Name      string    `json:"name"`
	Position  int32     `json:"position"`
	Collapsed bool      `json:"collapsed"`
	Bots      []botDTO  `json:"bots"`
	Groups    []teamDTO `json:"groups"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func toSidebarSectionDTO(section store.SidebarSection) sidebarSectionDTO {
	return sidebarSectionDTO{
		ID:        section.ID,
		Name:      section.Name,
		Position:  section.Position,
		Collapsed: section.Collapsed,
		Bots:      []botDTO{},
		Groups:    []teamDTO{},
		CreatedAt: section.CreatedAt,
		UpdatedAt: section.UpdatedAt,
	}
}

func (s *Server) handleGetSidebar(w http.ResponseWriter, r *http.Request) {
	accountID := accountFrom(r)
	sections, err := s.db.ListSidebarSections(r.Context(), accountID)
	if err != nil {
		s.logger.Error("list sidebar sections failed", "error", err)
		writeError(w, http.StatusInternalServerError, "internal", "could not load sidebar")
		return
	}
	bots, err := s.db.ListBots(r.Context(), accountID)
	if err != nil {
		s.logger.Error("list sidebar bots failed", "error", err)
		writeError(w, http.StatusInternalServerError, "internal", "could not load sidebar")
		return
	}
	teams, err := s.db.ListTeams(r.Context(), accountID)
	if err != nil {
		s.logger.Error("list sidebar teams failed", "error", err)
		writeError(w, http.StatusInternalServerError, "internal", "could not load sidebar")
		return
	}

	role, _ := s.accountRole(r.Context(), accountID, userFrom(r).ID)
	visibleBots := make(map[uuid.UUID]bool, len(bots))
	for _, bot := range bots {
		if _, ok := s.botAccess(r.Context(), role, userFrom(r).ID, bot); ok {
			visibleBots[bot.ID] = true
		}
	}

	activityRows, err := s.db.ListBotActivitySummaries(r.Context(), accountID)
	if err != nil {
		s.logger.Warn("list bot activity summaries failed", "error", err)
		activityRows = nil
	}
	activityByBot := make(map[uuid.UUID]store.ListBotActivitySummariesRow, len(activityRows))
	for _, row := range activityRows {
		activityByBot[row.BotID] = row
	}

	order := make([]sidebarSectionDTO, 0, len(sections))
	byID := make(map[uuid.UUID]*sidebarSectionDTO, len(sections))
	for _, section := range sections {
		dto := toSidebarSectionDTO(section)
		order = append(order, dto)
	}
	for i := range order {
		byID[order[i].ID] = &order[i]
	}

	unassigned := sidebarSectionDTO{
		Name:   "未分组",
		Bots:   []botDTO{},
		Groups: []teamDTO{},
	}
	for _, bot := range bots {
		if !visibleBots[bot.ID] {
			continue
		}
		dto := toBotDTO(bot)
		if activity, ok := activityByBot[bot.ID]; ok {
			dto.UnreadCount = activity.UnreadCount
			if activity.LatestSummary != "" {
				dto.LatestActivity = &activityPreviewDTO{
					Kind:      "update",
					Summary:   activity.LatestSummary,
					CreatedAt: activity.LatestAt,
				}
				if bot.LastActivityAt == nil || activity.LatestAt.After(*bot.LastActivityAt) {
					dto.LastActivityAt = &activity.LatestAt
				}
			}
		}
		if bot.SectionID != nil {
			if bucket, ok := byID[*bot.SectionID]; ok {
				bucket.Bots = append(bucket.Bots, dto)
				continue
			}
		}
		unassigned.Bots = append(unassigned.Bots, dto)
	}
	for _, team := range teams {
		if team.Mode != "group" {
			continue
		}
		rows, listErr := s.db.ListTeamBots(r.Context(), team.ID)
		if listErr != nil {
			s.logger.Warn("list group members failed", "team_id", team.ID, "error", listErr)
			rows = nil
		}
		dto := toTeamDTO(team, rows)
		if team.SectionID != nil {
			if bucket, ok := byID[*team.SectionID]; ok {
				bucket.Groups = append(bucket.Groups, dto)
				continue
			}
		}
		unassigned.Groups = append(unassigned.Groups, dto)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"sections":   order,
		"unassigned": unassigned,
	})
}

type sectionRequest struct {
	Name      *string `json:"name"`
	Position  *int32  `json:"position"`
	Collapsed *bool   `json:"collapsed"`
}

func (s *Server) handleCreateSection(w http.ResponseWriter, r *http.Request) {
	var req sectionRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	if req.Name == nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "name is required")
		return
	}
	name := strings.TrimSpace(*req.Name)
	if err := validateSectionName(name); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	position := int32(0)
	if req.Position != nil {
		position = clampPosition(*req.Position)
	} else {
		sections, err := s.db.ListSidebarSections(r.Context(), accountFrom(r))
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal", "could not create section")
			return
		}
		position = int32(len(sections))
	}
	section, err := s.db.CreateSidebarSection(r.Context(), store.CreateSidebarSectionParams{
		OwnerID:  accountFrom(r),
		Name:     name,
		Position: position,
	})
	if err != nil {
		s.logger.Error("create sidebar section failed", "error", err)
		writeError(w, http.StatusInternalServerError, "internal", "could not create section")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"section": toSidebarSectionDTO(section)})
}

func (s *Server) handleUpdateSection(w http.ResponseWriter, r *http.Request) {
	sectionID, ok := parseUUIDParam(w, r, "sectionID")
	if !ok {
		return
	}
	accountID := accountFrom(r)
	existing, err := s.db.GetSidebarSection(r.Context(), store.GetSidebarSectionParams{
		ID:      sectionID,
		OwnerID: accountID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "not_found", "section not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", "could not load section")
		return
	}
	var req sectionRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	name := existing.Name
	if req.Name != nil {
		name = strings.TrimSpace(*req.Name)
	}
	if err := validateSectionName(name); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	position := existing.Position
	if req.Position != nil {
		position = clampPosition(*req.Position)
	}
	collapsed := existing.Collapsed
	if req.Collapsed != nil {
		collapsed = *req.Collapsed
	}
	section, err := s.db.UpdateSidebarSection(r.Context(), store.UpdateSidebarSectionParams{
		ID:        sectionID,
		OwnerID:   accountID,
		Name:      name,
		Position:  position,
		Collapsed: collapsed,
	})
	if err != nil {
		s.logger.Error("update sidebar section failed", "error", err)
		writeError(w, http.StatusInternalServerError, "internal", "could not update section")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"section": toSidebarSectionDTO(section)})
}

func (s *Server) handleDeleteSection(w http.ResponseWriter, r *http.Request) {
	sectionID, ok := parseUUIDParam(w, r, "sectionID")
	if !ok {
		return
	}
	n, err := s.db.DeleteSidebarSection(r.Context(), store.DeleteSidebarSectionParams{
		ID:      sectionID,
		OwnerID: accountFrom(r),
	})
	if err != nil {
		s.logger.Error("delete sidebar section failed", "error", err)
		writeError(w, http.StatusInternalServerError, "internal", "could not delete section")
		return
	}
	if n == 0 {
		writeError(w, http.StatusNotFound, "not_found", "section not found")
		return
	}
	writeNoContent(w)
}

func validateSectionName(name string) error {
	if name == "" {
		return errors.New("name is required")
	}
	if len([]rune(name)) > maxSectionNameRunes {
		return errors.New("section name is too long")
	}
	return nil
}

func clampPosition(position int32) int32 {
	if position < 0 {
		return 0
	}
	if position > maxSidebarPosition {
		return maxSidebarPosition
	}
	return position
}

func (s *Server) ensureSectionOwned(ctx context.Context, userID uuid.UUID, sectionID *uuid.UUID) error {
	if sectionID == nil {
		return nil
	}
	if _, err := s.db.GetSidebarSection(ctx, store.GetSidebarSectionParams{
		ID:      *sectionID,
		OwnerID: userID,
	}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return errors.New("section not found")
		}
		return errors.New("could not validate section")
	}
	return nil
}
