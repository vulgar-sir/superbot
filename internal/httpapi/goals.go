package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"superbot/internal/events"
	"superbot/internal/goals"
	"superbot/internal/store"
)

type goalMemberDTO struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type goalResponse struct {
	Goal      *goals.Goal     `json:"goal"`
	LeadBotID string          `json:"lead_bot_id"`
	Members   []goalMemberDTO `json:"members"`
	UpdatedAt *time.Time      `json:"updated_at"`
	Message   string          `json:"message,omitempty"`
}

func (s *Server) goalResponseFor(r *http.Request, conv store.Conversation, message string) (goalResponse, error) {
	response := goalResponse{Members: []goalMemberDTO{}, Message: message}
	roster, err := goals.LoadRoster(r.Context(), s.db, conv)
	if err != nil {
		return response, err
	}
	response.LeadBotID = roster.LeadID.String()
	for _, member := range roster.Members {
		response.Members = append(response.Members, goalMemberDTO{ID: member.ID, Name: member.Name})
	}
	goal, row, err := s.goals.Load(r.Context(), conv.ID)
	if err != nil {
		return response, err
	}
	response.Goal = goal
	if row != nil {
		response.UpdatedAt = &row.UpdatedAt
	}
	return response, nil
}

func (s *Server) handleGetGoal(w http.ResponseWriter, r *http.Request) {
	conversationID, ok := parseUUIDParam(w, r, "conversationID")
	if !ok {
		return
	}
	conv, _, err := s.chat.GetConversation(r.Context(), accountFrom(r), userFrom(r).ID, conversationID)
	if err != nil {
		writeChatError(w, err, "could not load conversation")
		return
	}
	if conv.Kind != "team" || conv.TeamID == nil {
		writeJSON(w, http.StatusOK, goalResponse{Members: []goalMemberDTO{}})
		return
	}
	response, err := s.goalResponseFor(r, conv, "")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not load goal")
		return
	}
	writeJSON(w, http.StatusOK, response)
}

// handleGoalAction lets the user steer the goal directly: the same rules as
// the bots' group_goal tool, with the user's authority.
func (s *Server) handleGoalAction(w http.ResponseWriter, r *http.Request) {
	conversationID, ok := parseUUIDParam(w, r, "conversationID")
	if !ok {
		return
	}
	conv, _, err := s.chat.GetConversation(r.Context(), accountFrom(r), userFrom(r).ID, conversationID)
	if err != nil {
		writeChatError(w, err, "could not load conversation")
		return
	}
	if conv.Kind != "team" || conv.TeamID == nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "goals are only available in group conversations")
		return
	}
	var action goals.Action
	if err := decodeJSON(w, r, &action); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	roster, err := goals.LoadRoster(r.Context(), s.db, conv)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	goal, message, err := s.goals.Apply(r.Context(), conv.ID, conv.CreatedBy, action, goals.Actor{IsUser: true, Name: "用户"}, roster.Members)
	if err != nil {
		if errors.Is(err, goals.ErrForbidden) {
			writeError(w, http.StatusForbidden, "forbidden", err.Error())
			return
		}
		// Rule violations are the user's to read, not internal failures.
		writeError(w, http.StatusUnprocessableEntity, "goal_rule", err.Error())
		return
	}
	if goal != nil && action.Action != "status" {
		if _, err := events.Append(r.Context(), s.db.Queries, conv.ID, nil, nil, events.TypeGoalUpdated, map[string]any{"goal": goal}); err == nil {
			_ = s.bus.Notify(r.Context(), conv.ID)
		}
		s.audit(r.Context(), accountFrom(r), userFrom(r).ID, "goal."+action.Action, "conversation", &conv.ID, nil)
		if action.Action == "start" || action.Action == "resume" {
			s.kickGoalLead(r, conv, goal, roster)
		}
	}
	response, err := s.goalResponseFor(r, conv, message)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not load goal")
		return
	}
	writeJSON(w, http.StatusOK, response)
}

// kickGoalLead wakes the lead right after the user starts or resumes a goal,
// so work begins without the user having to send another message.
func (s *Server) kickGoalLead(r *http.Request, conv store.Conversation, goal *goals.Goal, roster goals.Roster) {
	text := goals.NudgeText(goal)
	meta, _ := json.Marshal(map[string]any{
		"kind":        "goal_nudge",
		"inject":      "true",
		"inject_text": text,
		"for_bot_id":  roster.LeadID.String(),
	})
	msg, err := s.db.CreateMessage(r.Context(), store.CreateMessageParams{
		ConversationID: conv.ID,
		SenderType:     "system",
		Content:        "目标检查",
		Metadata:       meta,
	})
	if err != nil {
		s.logger.Warn("create goal nudge failed", "conversation_id", conv.ID, "error", err)
		return
	}
	if _, err := events.Append(r.Context(), s.db.Queries, conv.ID, nil, nil, events.TypeMessageCreated, map[string]any{"message": msg}); err != nil {
		s.logger.Warn("append goal nudge event failed", "conversation_id", conv.ID, "error", err)
	}
	if err := s.goals.RecordNudge(r.Context(), conv.ID, goal, msg.Ordinal); err != nil {
		s.logger.Warn("record goal nudge failed", "conversation_id", conv.ID, "error", err)
	}
	if _, err := s.chat.Wake(r.Context(), conv.ID, msg, 6, "goal"); err != nil {
		s.logger.Warn("wake goal lead failed", "conversation_id", conv.ID, "error", err)
	}
}
