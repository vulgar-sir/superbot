package httpapi

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"superbot/internal/store"
)

func (s *Server) agentThreadRoutes(r chi.Router) {
	r.Get("/{threadID}", s.handleGetAgentThread)
}

type agentThreadDTO struct {
	ID                   uuid.UUID  `json:"id"`
	ConversationID       uuid.UUID  `json:"conversation_id"`
	BotAID               uuid.UUID  `json:"bot_a_id"`
	BotBID               uuid.UUID  `json:"bot_b_id"`
	OriginConversationID *uuid.UUID `json:"origin_conversation_id"`
	CreatedAt            time.Time  `json:"created_at"`
	UpdatedAt            time.Time  `json:"updated_at"`
}

func toAgentThreadDTO(thread store.AgentThread) agentThreadDTO {
	return agentThreadDTO{
		ID:                   thread.ID,
		ConversationID:       thread.ConversationID,
		BotAID:               thread.BotAID,
		BotBID:               thread.BotBID,
		OriginConversationID: thread.OriginConversationID,
		CreatedAt:            thread.CreatedAt,
		UpdatedAt:            thread.UpdatedAt,
	}
}

func (s *Server) handleGetAgentThread(w http.ResponseWriter, r *http.Request) {
	threadID, ok := parseUUIDParam(w, r, "threadID")
	if !ok {
		return
	}
	accountID := accountFrom(r)
	thread, err := s.db.GetAgentThread(r.Context(), store.GetAgentThreadParams{ID: threadID, OwnerID: accountID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "not_found", "agent thread not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", "could not load agent thread")
		return
	}
	conv, err := s.db.GetConversation(r.Context(), thread.ConversationID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not load agent conversation")
		return
	}
	messages, err := s.db.ListMessages(r.Context(), thread.ConversationID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not load agent messages")
		return
	}
	botA, errA := s.db.GetBot(r.Context(), store.GetBotParams{ID: thread.BotAID, OwnerID: accountID})
	botB, errB := s.db.GetBot(r.Context(), store.GetBotParams{ID: thread.BotBID, OwnerID: accountID})
	if errA != nil || errB != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not load agent bots")
		return
	}
	outMessages := make([]messageDTO, 0, len(messages))
	for _, message := range messages {
		outMessages = append(outMessages, toMessageDTO(message))
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"thread":       toAgentThreadDTO(thread),
		"conversation": toConversationDTO(conv),
		"messages":     outMessages,
		"bot_a":        toBotDTO(botA),
		"bot_b":        toBotDTO(botB),
	})
}

type activityDTO struct {
	ID             uuid.UUID  `json:"id"`
	BotID          uuid.UUID  `json:"bot_id"`
	BotName        string     `json:"bot_name"`
	Kind           string     `json:"kind"`
	Summary        string     `json:"summary"`
	ConversationID *uuid.UUID `json:"conversation_id"`
	MessageID      *uuid.UUID `json:"message_id"`
	AgentThreadID  *uuid.UUID `json:"agent_thread_id"`
	Read           bool       `json:"read"`
	CreatedAt      time.Time  `json:"created_at"`
}

func toActivityDTO(activity store.Activity, botName string) activityDTO {
	return activityDTO{
		ID:             activity.ID,
		BotID:          activity.BotID,
		BotName:        botName,
		Kind:           activity.Kind,
		Summary:        activity.Summary,
		ConversationID: activity.ConversationID,
		MessageID:      activity.MessageID,
		AgentThreadID:  activity.AgentThreadID,
		Read:           activity.ReadAt != nil,
		CreatedAt:      activity.CreatedAt,
	}
}

func (s *Server) activityRoutes(r chi.Router) {
	r.Get("/", s.handleListActivities)
	r.Post("/read", s.handleMarkActivitiesRead)
	r.Post("/unread", s.handleMarkActivitiesUnread)
}

func (s *Server) handleListActivities(w http.ResponseWriter, r *http.Request) {
	accountID := accountFrom(r)
	limit := parseLimit(r.URL.Query().Get("limit"), 50, 200)
	var botID *uuid.UUID
	if raw := strings.TrimSpace(r.URL.Query().Get("bot_id")); raw != "" {
		parsed, err := uuid.Parse(raw)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_request", "invalid bot_id")
			return
		}
		botID = &parsed
	}
	if botID != nil {
		rows, err := s.db.ListActivitiesForBot(r.Context(), store.ListActivitiesForBotParams{
			OwnerID: accountID,
			BotID:   *botID,
			Limit:   limit,
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal", "could not load activities")
			return
		}
		out := make([]activityDTO, 0, len(rows))
		for _, row := range rows {
			out = append(out, toActivityDTO(row.Activity, row.BotName))
		}
		writeJSON(w, http.StatusOK, map[string]any{"activities": out})
		return
	}
	rows, err := s.db.ListActivitiesForOwner(r.Context(), store.ListActivitiesForOwnerParams{
		OwnerID: accountID,
		Limit:   limit,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not load activities")
		return
	}
	out := make([]activityDTO, 0, len(rows))
	for _, row := range rows {
		out = append(out, toActivityDTO(row.Activity, row.BotName))
	}
	writeJSON(w, http.StatusOK, map[string]any{"activities": out})
}

type markActivitiesReadRequest struct {
	BotID *uuid.UUID `json:"bot_id"`
}

func (s *Server) handleMarkActivitiesRead(w http.ResponseWriter, r *http.Request) {
	var req markActivitiesReadRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	accountID := accountFrom(r)
	var err error
	if req.BotID == nil {
		err = s.db.MarkAllActivitiesRead(r.Context(), accountID)
	} else {
		if _, lookupErr := s.db.GetBot(r.Context(), store.GetBotParams{ID: *req.BotID, OwnerID: accountID}); lookupErr != nil {
			writeError(w, http.StatusNotFound, "not_found", "bot not found")
			return
		}
		err = s.db.MarkActivitiesRead(r.Context(), store.MarkActivitiesReadParams{
			OwnerID: accountID,
			BotID:   *req.BotID,
		})
		if err == nil {
			err = s.db.SetBotMarkedUnread(r.Context(), store.SetBotMarkedUnreadParams{
				ID:           *req.BotID,
				OwnerID:      accountID,
				MarkedUnread: false,
			})
		}
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not mark activities read")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

type markActivitiesUnreadRequest struct {
	BotID uuid.UUID `json:"bot_id"`
}

func (s *Server) handleMarkActivitiesUnread(w http.ResponseWriter, r *http.Request) {
	var req markActivitiesUnreadRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	accountID := accountFrom(r)
	if _, err := s.db.GetBot(r.Context(), store.GetBotParams{ID: req.BotID, OwnerID: accountID}); err != nil {
		writeError(w, http.StatusNotFound, "not_found", "bot not found")
		return
	}
	if err := s.db.MarkBotActivitiesUnread(r.Context(), store.MarkBotActivitiesUnreadParams{
		OwnerID: accountID,
		BotID:   req.BotID,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not mark activities unread")
		return
	}
	if err := s.db.SetBotMarkedUnread(r.Context(), store.SetBotMarkedUnreadParams{
		ID:           req.BotID,
		OwnerID:      accountID,
		MarkedUnread: true,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not mark bot unread")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

type messageSearchDTO struct {
	ID                uuid.UUID `json:"id"`
	ConversationID    uuid.UUID `json:"conversation_id"`
	ConversationTitle string    `json:"conversation_title"`
	Content           string    `json:"content"`
	SenderType        string    `json:"sender_type"`
	BotName           string    `json:"bot_name"`
	CreatedAt         time.Time `json:"created_at"`
}

func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	if query == "" {
		writeJSON(w, http.StatusOK, map[string]any{"activities": []activityDTO{}, "bots": []botDTO{}, "messages": []messageSearchDTO{}})
		return
	}
	accountID := accountFrom(r)
	limit := parseLimit(r.URL.Query().Get("limit"), 30, 100)
	rows, err := s.db.SearchActivities(r.Context(), store.SearchActivitiesParams{
		OwnerID:  accountID,
		Query:    &query,
		RowLimit: limit,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "search failed")
		return
	}
	activities := make([]activityDTO, 0, len(rows))
	for _, row := range rows {
		activities = append(activities, toActivityDTO(row.Activity, row.BotName))
	}
	bots, err := s.db.ListBots(r.Context(), accountID)
	if err != nil {
		bots = nil
	}
	needle := strings.ToLower(query)
	matchedBots := make([]botDTO, 0, 5)
	for _, bot := range bots {
		if strings.Contains(strings.ToLower(bot.Name+" "+bot.Title+" "+bot.Description), needle) {
			matchedBots = append(matchedBots, toBotDTO(bot))
			if len(matchedBots) >= 10 {
				break
			}
		}
	}
	messageRows, messageErr := s.db.SearchMessages(r.Context(), store.SearchMessagesParams{
		OwnerID:  accountID,
		Query:    &query,
		RowLimit: limit,
	})
	messages := make([]messageSearchDTO, 0)
	if messageErr == nil {
		for _, row := range messageRows {
			content := strings.TrimSpace(row.Content)
			if len([]rune(content)) > 500 {
				content = string([]rune(content)[:500]) + "…"
			}
			messages = append(messages, messageSearchDTO{
				ID:                row.ID,
				ConversationID:    row.ConversationID,
				ConversationTitle: row.ConversationTitle,
				Content:           content,
				SenderType:        row.SenderType,
				BotName:           row.BotName,
				CreatedAt:         row.CreatedAt,
			})
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"activities": activities,
		"bots":       matchedBots,
		"messages":   messages,
	})
}

func parseLimit(raw string, fallback, max int32) int32 {
	if raw == "" {
		return fallback
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value <= 0 {
		return fallback
	}
	if int32(value) > max {
		return max
	}
	return int32(value)
}
