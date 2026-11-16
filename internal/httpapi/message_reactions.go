package httpapi

import (
	"errors"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"superbot/internal/store"
)

func (s *Server) messageRoutes(r chi.Router) {
	r.Patch("/{messageID}", s.handleUpdateMessage)
	r.Delete("/{messageID}", s.handleDeleteMessage)
	r.Post("/{messageID}/pin", s.handlePinMessage)
	r.Post("/{messageID}/vote", s.handleVotePoll)
	r.Post("/{messageID}/reactions", s.handleToggleMessageReaction)
}

type toggleMessageReactionRequest struct {
	Emoji  string `json:"emoji"`
	Action string `json:"action"`
}

func (s *Server) handleToggleMessageReaction(w http.ResponseWriter, r *http.Request) {
	messageID, ok := parseUUIDParam(w, r, "messageID")
	if !ok {
		return
	}
	var req toggleMessageReactionRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	emoji := strings.TrimSpace(req.Emoji)
	if emoji == "" || utf8.RuneCountInString(emoji) > 32 {
		writeError(w, http.StatusBadRequest, "invalid_request", "emoji is invalid")
		return
	}
	user := userFrom(r)
	if _, err := s.db.GetMessageForOwner(r.Context(), store.GetMessageForOwnerParams{
		ID:        messageID,
		CreatedBy: user.ID,
	}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "not_found", "message not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", "could not load message")
		return
	}
	switch req.Action {
	case "remove":
		if err := s.db.RemoveMessageReaction(r.Context(), store.RemoveMessageReactionParams{
			MessageID: messageID,
			UserID:    user.ID,
			Emoji:     emoji,
		}); err != nil {
			writeError(w, http.StatusInternalServerError, "internal", "could not remove reaction")
			return
		}
	default:
		if err := s.db.AddMessageReaction(r.Context(), store.AddMessageReactionParams{
			MessageID: messageID,
			UserID:    user.ID,
			Emoji:     emoji,
		}); err != nil {
			writeError(w, http.StatusInternalServerError, "internal", "could not add reaction")
			return
		}
	}
	rows, err := s.db.ListMessageReactions(r.Context(), store.ListMessageReactionsParams{
		Column1: []uuid.UUID{messageID},
		UserID:  user.ID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not load reactions")
		return
	}
	out := make([]messageReactionDTO, 0, len(rows))
	for _, row := range rows {
		out = append(out, messageReactionDTO{
			Emoji:   row.Emoji,
			Count:   row.ReactionCount,
			Reacted: row.Reacted,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"reactions": out})
}
