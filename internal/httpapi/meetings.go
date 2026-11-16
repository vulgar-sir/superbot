package httpapi

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"superbot/internal/llm"
	"superbot/internal/store"
)

type meetingDTO struct {
	ID         uuid.UUID `json:"id"`
	Title      string    `json:"title"`
	Transcript string    `json:"transcript,omitempty"`
	Summary    string    `json:"summary"`
	StartsAt   time.Time `json:"starts_at"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

func toMeetingDTO(meeting store.Meeting, includeTranscript bool) meetingDTO {
	dto := meetingDTO{
		ID: meeting.ID, Title: meeting.Title, Summary: meeting.Summary,
		StartsAt: meeting.StartsAt, CreatedAt: meeting.CreatedAt, UpdatedAt: meeting.UpdatedAt,
	}
	if includeTranscript {
		dto.Transcript = meeting.Transcript
	}
	return dto
}

func (s *Server) meetingRoutes(r chi.Router) {
	r.Get("/", s.handleListMeetings)
	r.Post("/", s.handleCreateMeeting)
	r.Get("/{meetingID}", s.handleGetMeeting)
	r.Post("/{meetingID}/summarize", s.handleSummarizeMeeting)
	r.Delete("/{meetingID}", s.handleDeleteMeeting)
}

func (s *Server) handleListMeetings(w http.ResponseWriter, r *http.Request) {
	meetings, err := s.db.ListMeetings(r.Context(), accountFrom(r))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not list meetings")
		return
	}
	out := make([]meetingDTO, 0, len(meetings))
	for _, meeting := range meetings {
		out = append(out, meetingDTO{
			ID: meeting.ID, Title: meeting.Title, Summary: meeting.Summary,
			StartsAt: meeting.StartsAt, CreatedAt: meeting.CreatedAt, UpdatedAt: meeting.UpdatedAt,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"meetings": out})
}

func (s *Server) handleCreateMeeting(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Title      string `json:"title"`
		Transcript string `json:"transcript"`
		StartsAt   string `json:"starts_at"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	title := strings.TrimSpace(req.Title)
	transcript := strings.TrimSpace(req.Transcript)
	if title == "" || transcript == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "title and transcript are required")
		return
	}
	start := time.Now()
	if strings.TrimSpace(req.StartsAt) != "" {
		parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(req.StartsAt))
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_request", "starts_at must be RFC3339")
			return
		}
		start = parsed
	}
	userID := userFrom(r).ID
	meeting, err := s.db.CreateMeeting(r.Context(), store.CreateMeetingParams{
		OwnerID: accountFrom(r), Title: title, Transcript: transcript, StartsAt: start, CreatedByUserID: &userID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not create meeting")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"meeting": toMeetingDTO(meeting, true)})
}

func (s *Server) handleGetMeeting(w http.ResponseWriter, r *http.Request) {
	meetingID, ok := parseUUIDParam(w, r, "meetingID")
	if !ok {
		return
	}
	meeting, err := s.db.GetMeeting(r.Context(), store.GetMeetingParams{ID: meetingID, OwnerID: accountFrom(r)})
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "meeting not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"meeting": toMeetingDTO(meeting, true)})
}

func (s *Server) handleSummarizeMeeting(w http.ResponseWriter, r *http.Request) {
	meetingID, ok := parseUUIDParam(w, r, "meetingID")
	if !ok {
		return
	}
	meeting, err := s.db.GetMeeting(r.Context(), store.GetMeetingParams{ID: meetingID, OwnerID: accountFrom(r)})
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "meeting not found")
		return
	}
	account, err := s.db.GetAccountByID(r.Context(), accountFrom(r))
	if err != nil || account.PrimaryBotID == nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "account has no primary bot")
		return
	}
	bot, err := s.db.GetBot(r.Context(), store.GetBotParams{ID: *account.PrimaryBotID, OwnerID: accountFrom(r)})
	if err != nil || bot.ProviderID == nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "primary bot has no provider")
		return
	}
	providerRow, err := s.db.GetProvider(r.Context(), store.GetProviderParams{ID: *bot.ProviderID, OwnerID: accountFrom(r)})
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "provider not found")
		return
	}
	provider, err := s.factory.Build(providerRow, s.cfg.LLMTimeout)
	if err != nil {
		writeError(w, http.StatusBadRequest, "provider_failed", err.Error())
		return
	}
	transcript := meeting.Transcript
	if len([]rune(transcript)) > 60_000 {
		transcript = string([]rune(transcript)[:60_000]) + "\n…[truncated]"
	}
	ctx, cancel := context.WithTimeout(r.Context(), s.cfg.LLMTimeout)
	defer cancel()
	stream, err := provider.Stream(ctx, llm.Request{
		Model: bot.Model,
		Messages: []llm.Message{
			llm.TextMessage(llm.RoleSystem, "你是会议记录助手。根据 transcript 输出中文摘要，包含：结论、关键决定、行动项（负责人/期限，如未提及则标注未指定）。不要编造。"),
			llm.TextMessage(llm.RoleUser, "会议标题："+meeting.Title+"\n\nTranscript:\n"+transcript),
		},
		MaxTokens: 1600,
	})
	if err != nil {
		writeError(w, http.StatusBadGateway, "summarize_failed", err.Error())
		return
	}
	defer stream.Close()
	var summary strings.Builder
	for event := range stream.Events() {
		switch event.Type {
		case llm.EventTextDelta:
			summary.WriteString(event.Text)
		case llm.EventError:
			writeError(w, http.StatusBadGateway, "summarize_failed", event.Err.Error())
			return
		}
	}
	text := strings.TrimSpace(summary.String())
	if text == "" {
		writeError(w, http.StatusBadGateway, "summarize_failed", "model returned no summary")
		return
	}
	updated, err := s.db.SetMeetingSummary(r.Context(), store.SetMeetingSummaryParams{
		ID: meeting.ID, OwnerID: accountFrom(r), Summary: text,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not save summary")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"meeting": toMeetingDTO(updated, true)})
}

func (s *Server) handleDeleteMeeting(w http.ResponseWriter, r *http.Request) {
	meetingID, ok := parseUUIDParam(w, r, "meetingID")
	if !ok {
		return
	}
	n, err := s.db.DeleteMeeting(r.Context(), store.DeleteMeetingParams{ID: meetingID, OwnerID: accountFrom(r)})
	if err != nil || n == 0 {
		writeError(w, http.StatusNotFound, "not_found", "meeting not found")
		return
	}
	writeNoContent(w)
}
