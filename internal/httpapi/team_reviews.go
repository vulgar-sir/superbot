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

type teamReviewDTO struct {
	ID               uuid.UUID  `json:"id"`
	TeamBotID        uuid.UUID  `json:"team_bot_id"`
	Kind             string     `json:"kind"`
	ResourceID       *uuid.UUID `json:"resource_id"`
	Title            string     `json:"title"`
	Description      string     `json:"description"`
	Content          string     `json:"content"`
	Status           string     `json:"status"`
	SubmittedByBotID *uuid.UUID `json:"submitted_by_bot_id"`
	ReviewedByUserID *uuid.UUID `json:"reviewed_by_user_id"`
	CreatedAt        time.Time  `json:"created_at"`
	ReviewedAt       *time.Time `json:"reviewed_at"`
}

func toTeamReviewDTO(review store.TeamReview) teamReviewDTO {
	return teamReviewDTO{
		ID: review.ID, TeamBotID: review.TeamBotID, Kind: review.Kind,
		ResourceID: review.ResourceID, Title: review.Title, Description: review.Description,
		Content: review.Content, Status: review.Status,
		SubmittedByBotID: review.SubmittedByBotID, ReviewedByUserID: review.ReviewedByUserID,
		CreatedAt: review.CreatedAt, ReviewedAt: review.ReviewedAt,
	}
}

func (s *Server) teamReviewRoutes(r chi.Router) {
	r.Get("/", s.handleListTeamReviews)
	r.Post("/{reviewID}", s.handleResolveTeamReview)
}

func (s *Server) handleListTeamReviews(w http.ResponseWriter, r *http.Request) {
	status := strings.TrimSpace(r.URL.Query().Get("status"))
	if status != "" && status != "pending" && status != "approved" && status != "rejected" {
		writeError(w, http.StatusBadRequest, "invalid_request", "invalid review status")
		return
	}
	rows, err := s.db.ListTeamReviews(r.Context(), store.ListTeamReviewsParams{
		OwnerID: accountFrom(r), Column2: status, Limit: 100,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not list team reviews")
		return
	}
	out := make([]teamReviewDTO, 0, len(rows))
	for _, row := range rows {
		out = append(out, toTeamReviewDTO(row))
	}
	writeJSON(w, http.StatusOK, map[string]any{"reviews": out})
}

func (s *Server) handleResolveTeamReview(w http.ResponseWriter, r *http.Request) {
	reviewID, ok := parseUUIDParam(w, r, "reviewID")
	if !ok {
		return
	}
	var req struct {
		Decision string `json:"decision"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	if req.Decision != "approve" && req.Decision != "reject" {
		writeError(w, http.StatusBadRequest, "invalid_request", "decision must be approve or reject")
		return
	}
	review, err := s.db.GetTeamReviewForOwner(r.Context(), store.GetTeamReviewForOwnerParams{
		ID: reviewID, OwnerID: accountFrom(r),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "not_found", "review not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", "could not load review")
		return
	}
	if review.Status != "pending" {
		writeError(w, http.StatusConflict, "conflict", "review is already resolved")
		return
	}
	if review.Kind == "skill" && review.ResourceID != nil {
		if req.Decision == "approve" {
			if _, err := s.skills.Approve(r.Context(), accountFrom(r), *review.ResourceID); err != nil {
				writeError(w, http.StatusInternalServerError, "internal", "could not approve skill")
				return
			}
			if s.teamAutoPublish(r, review.TeamBotID) {
				current, _ := s.skills.ListForBot(r.Context(), review.TeamBotID)
				ids := make([]uuid.UUID, 0, len(current)+1)
				for _, item := range current {
					ids = append(ids, item.ID)
				}
				ids = append(ids, *review.ResourceID)
				if err := s.skills.SetBotSkills(r.Context(), accountFrom(r), review.TeamBotID, ids); err != nil {
					s.logger.Warn("attach approved skill failed", "error", err, "review_id", review.ID)
				}
			}
		} else {
			if _, err := s.skills.Disable(r.Context(), accountFrom(r), *review.ResourceID); err != nil {
				s.logger.Warn("disable rejected skill failed", "error", err, "review_id", review.ID)
			}
		}
	}
	status := "approved"
	if req.Decision == "reject" {
		status = "rejected"
	}
	user := userFrom(r)
	updated, err := s.db.ResolveTeamReview(r.Context(), store.ResolveTeamReviewParams{
		ID: review.ID, OwnerID: accountFrom(r), Status: status, ReviewedByUserID: &user.ID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not resolve review")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"review": toTeamReviewDTO(updated)})
}

func (s *Server) teamAutoPublish(r *http.Request, teamBotID uuid.UUID) bool {
	bot, err := s.db.GetBot(r.Context(), store.GetBotParams{ID: teamBotID, OwnerID: accountFrom(r)})
	if err != nil {
		return false
	}
	var settings struct {
		AutoPublish bool `json:"auto_publish"`
	}
	_ = json.Unmarshal(bot.TeamSettings, &settings)
	return settings.AutoPublish
}
