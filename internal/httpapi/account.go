package httpapi

import (
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"superbot/internal/store"
)

func (s *Server) accountRoutes(r chi.Router) {
	r.Post("/primary-bot", s.handleSetPrimaryBot)
}

type primaryBotRequest struct {
	BotID uuid.UUID `json:"bot_id"`
}

func (s *Server) handleSetPrimaryBot(w http.ResponseWriter, r *http.Request) {
	var req primaryBotRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	if req.BotID == uuid.Nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "bot_id is required")
		return
	}
	user := userFrom(r)
	accountID := accountFrom(r)
	account, accountErr := s.db.GetAccountByID(r.Context(), accountID)
	var oldPrimary *uuid.UUID
	if accountErr == nil {
		oldPrimary = account.PrimaryBotID
	}
	bot, err := s.db.GetBot(r.Context(), store.GetBotParams{ID: req.BotID, OwnerID: accountID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "not_found", "bot not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", "could not load bot")
		return
	}
	if bot.Kind != "personal" {
		writeError(w, http.StatusBadRequest, "team_bot_cannot_be_primary", "Team Bots can't be a primary Bot")
		return
	}

	if err := s.db.SetAccountPrimaryBot(r.Context(), store.SetAccountPrimaryBotParams{
		ID: accountID, PrimaryBotID: &bot.ID,
	}); err != nil {
		s.logger.Error("set account primary bot failed", "error", err, "account_id", accountID, "bot_id", bot.ID)
		writeError(w, http.StatusInternalServerError, "internal", "could not set primary bot")
		return
	}
	if err := s.db.SetPrimaryBot(r.Context(), store.SetPrimaryBotParams{
		ID:           user.ID,
		PrimaryBotID: &bot.ID,
	}); err != nil {
		s.logger.Warn("set user primary bot failed", "error", err, "user_id", user.ID)
	}
	if oldPrimary != nil && *oldPrimary != bot.ID {
		// Replacing the primary Bot moves the old primary's children to the
		// unassigned/Agents area; it never deletes them.
		if err := s.db.DetachBotsCreatedBy(r.Context(), store.DetachBotsCreatedByParams{
			OwnerID:        accountID,
			CreatedByBotID: oldPrimary,
		}); err != nil {
			s.logger.Warn("detach old primary bots failed", "error", err, "user_id", user.ID)
		}
	}
	updated, err := s.db.GetUserByID(r.Context(), user.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not load account")
		return
	}
	updated.PrimaryBotID = &bot.ID
	writeJSON(w, http.StatusOK, map[string]any{
		"user":        toUserDTO(updated),
		"primary_bot": toBotDTO(bot),
	})
}
