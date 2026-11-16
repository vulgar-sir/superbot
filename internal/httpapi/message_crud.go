package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"

	"superbot/internal/store"
)

const maxEditableMessageBytes = 64 * 1024

func pollOptionIDs(metadata json.RawMessage) (map[string]bool, bool) {
	var meta struct {
		Kind string `json:"kind"`
		Poll struct {
			Options []struct {
				ID string `json:"id"`
			} `json:"options"`
		} `json:"poll"`
	}
	if len(metadata) == 0 || json.Unmarshal(metadata, &meta) != nil {
		return nil, false
	}
	if meta.Kind != "poll" || len(meta.Poll.Options) == 0 {
		return nil, false
	}
	ids := make(map[string]bool, len(meta.Poll.Options))
	for _, option := range meta.Poll.Options {
		if option.ID != "" {
			ids[option.ID] = true
		}
	}
	return ids, len(ids) > 0
}

type updateMessageRequest struct {
	Content string `json:"content"`
}

type pinMessageRequest struct {
	Pinned bool `json:"pinned"`
}

func (s *Server) ownedMessage(w http.ResponseWriter, r *http.Request) (store.Message, bool) {
	messageID, ok := parseUUIDParam(w, r, "messageID")
	if !ok {
		return store.Message{}, false
	}
	message, err := s.db.GetMessageForOwner(r.Context(), store.GetMessageForOwnerParams{
		ID:        messageID,
		CreatedBy: userFrom(r).ID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "not_found", "message not found")
			return store.Message{}, false
		}
		writeError(w, http.StatusInternalServerError, "internal", "could not load message")
		return store.Message{}, false
	}
	if message.DeletedAt != nil {
		writeError(w, http.StatusNotFound, "not_found", "message not found")
		return store.Message{}, false
	}
	return message, true
}

func (s *Server) handleUpdateMessage(w http.ResponseWriter, r *http.Request) {
	var req updateMessageRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	content := strings.TrimSpace(req.Content)
	if content == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "message content is required")
		return
	}
	if len(content) > maxEditableMessageBytes {
		writeError(w, http.StatusBadRequest, "invalid_request", "message is too long")
		return
	}
	message, ok := s.ownedMessage(w, r)
	if !ok {
		return
	}
	if message.SenderType != "user" {
		writeError(w, http.StatusForbidden, "forbidden", "only user messages can be edited")
		return
	}
	updated, err := s.db.UpdateMessageContent(r.Context(), store.UpdateMessageContentParams{
		ID:      message.ID,
		Content: content,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "not_found", "message not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", "could not update message")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"message": s.toMessageDTOs(r.Context(), userFrom(r).ID, []store.Message{updated})[0],
	})
}

func (s *Server) handleDeleteMessage(w http.ResponseWriter, r *http.Request) {
	message, ok := s.ownedMessage(w, r)
	if !ok {
		return
	}
	deleted, err := s.db.SoftDeleteMessage(r.Context(), message.ID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "not_found", "message not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", "could not delete message")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"deleted": true,
		"id":      deleted.ID,
	})
}

type votePollRequest struct {
	OptionID string `json:"option_id"`
	Action   string `json:"action"`
}

func (s *Server) handleVotePoll(w http.ResponseWriter, r *http.Request) {
	var req votePollRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	optionID := strings.TrimSpace(req.OptionID)
	if optionID == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "option_id is required")
		return
	}
	message, ok := s.ownedMessage(w, r)
	if !ok {
		return
	}
	options, isPoll := pollOptionIDs(message.Metadata)
	if !isPoll {
		writeError(w, http.StatusBadRequest, "invalid_request", "message is not a poll")
		return
	}
	if !options[optionID] {
		writeError(w, http.StatusBadRequest, "invalid_request", "option_id is not part of this poll")
		return
	}
	var meta struct {
		Poll struct {
			Multiple bool `json:"multiple"`
		} `json:"poll"`
	}
	_ = json.Unmarshal(message.Metadata, &meta)
	user := userFrom(r)
	if !meta.Poll.Multiple {
		if err := s.db.RemovePollVotesForUser(r.Context(), store.RemovePollVotesForUserParams{
			MessageID: message.ID,
			UserID:    user.ID,
		}); err != nil {
			writeError(w, http.StatusInternalServerError, "internal", "could not update vote")
			return
		}
	}
	action := strings.TrimSpace(req.Action)
	if action == "remove" {
		if err := s.db.RemovePollVote(r.Context(), store.RemovePollVoteParams{
			MessageID: message.ID,
			UserID:    user.ID,
			OptionID:  optionID,
		}); err != nil {
			writeError(w, http.StatusInternalServerError, "internal", "could not remove vote")
			return
		}
	} else {
		if err := s.db.AddPollVote(r.Context(), store.AddPollVoteParams{
			MessageID: message.ID,
			UserID:    user.ID,
			OptionID:  optionID,
		}); err != nil {
			writeError(w, http.StatusInternalServerError, "internal", "could not add vote")
			return
		}
	}
	fresh, err := s.db.GetMessageForOwner(r.Context(), store.GetMessageForOwnerParams{
		ID:        message.ID,
		CreatedBy: user.ID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not reload poll")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"message": s.toMessageDTOs(r.Context(), user.ID, []store.Message{fresh})[0],
	})
}

func (s *Server) handlePinMessage(w http.ResponseWriter, r *http.Request) {
	var req pinMessageRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	message, ok := s.ownedMessage(w, r)
	if !ok {
		return
	}
	updated, err := s.db.SetMessagePinned(r.Context(), store.SetMessagePinnedParams{
		ID:     message.ID,
		Pinned: req.Pinned,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "not_found", "message not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", "could not pin message")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"message": s.toMessageDTOs(r.Context(), userFrom(r).ID, []store.Message{updated})[0],
	})
}
