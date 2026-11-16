package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"superbot/internal/llm"
	"superbot/internal/store"
)

type conversationSummaryDTO struct {
	ID             uuid.UUID  `json:"id"`
	ConversationID uuid.UUID  `json:"conversation_id"`
	UpToMessageID  *uuid.UUID `json:"up_to_message_id"`
	UpToSeq        int64      `json:"up_to_seq"`
	Content        string     `json:"content"`
	Tokens         int32      `json:"tokens"`
	Source         string     `json:"source"`
	CreatedAt      time.Time  `json:"created_at"`
}

func toConversationSummaryDTO(summary store.ConversationSummary) conversationSummaryDTO {
	return conversationSummaryDTO{
		ID: summary.ID, ConversationID: summary.ConversationID,
		UpToMessageID: summary.UpToMessageID, UpToSeq: summary.UpToSeq,
		Content: summary.Content, Tokens: summary.Tokens, Source: summary.Source, CreatedAt: summary.CreatedAt,
	}
}

// handleListConversationSummaries returns the conversation's checkpoints,
// newest first, so the UI can open the one a marker refers to.
func (s *Server) handleListConversationSummaries(w http.ResponseWriter, r *http.Request) {
	conversationID, ok := parseUUIDParam(w, r, "conversationID")
	if !ok {
		return
	}
	if _, _, err := s.chat.GetConversation(r.Context(), accountFrom(r), userFrom(r).ID, conversationID); err != nil {
		writeChatError(w, err, "could not load conversation")
		return
	}
	rows, err := s.db.ListConversationSummaries(r.Context(), store.ListConversationSummariesParams{
		ConversationID: conversationID, Limit: 50,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not load summaries")
		return
	}
	items := make([]conversationSummaryDTO, 0, len(rows))
	for _, row := range rows {
		items = append(items, toConversationSummaryDTO(row))
	}
	writeJSON(w, http.StatusOK, map[string]any{"summaries": items})
}

func (s *Server) handleGetConversationSummary(w http.ResponseWriter, r *http.Request) {
	conversationID, ok := parseUUIDParam(w, r, "conversationID")
	if !ok {
		return
	}
	if _, _, err := s.chat.GetConversation(r.Context(), accountFrom(r), userFrom(r).ID, conversationID); err != nil {
		writeChatError(w, err, "could not load conversation")
		return
	}
	summary, err := s.db.GetLatestConversationSummary(r.Context(), conversationID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeJSON(w, http.StatusOK, map[string]any{"summary": nil})
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", "could not load summary")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"summary": toConversationSummaryDTO(summary)})
}

func (s *Server) handleSummarizeConversation(w http.ResponseWriter, r *http.Request) {
	conversationID, ok := parseUUIDParam(w, r, "conversationID")
	if !ok {
		return
	}
	conv, messages, err := s.chat.GetConversation(r.Context(), accountFrom(r), userFrom(r).ID, conversationID)
	if err != nil {
		writeChatError(w, err, "could not load conversation")
		return
	}
	if len(messages) < 2 {
		writeError(w, http.StatusBadRequest, "invalid_request", "conversation needs at least two messages")
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

	bots, _ := s.db.ListBots(r.Context(), accountFrom(r))
	names := make(map[uuid.UUID]string, len(bots))
	for _, item := range bots {
		names[item.ID] = item.Name
	}
	if len(messages) > 120 {
		messages = messages[len(messages)-120:]
	}
	var transcript strings.Builder
	for _, message := range messages {
		content := strings.TrimSpace(message.Content)
		if content == "" {
			continue
		}
		switch message.SenderType {
		case "user":
			transcript.WriteString("用户: ")
		case "bot":
			name := "Bot"
			if message.SenderBotID != nil {
				if value, ok := names[*message.SenderBotID]; ok {
					name = value
				}
			}
			transcript.WriteString("[" + name + "]: ")
		default:
			transcript.WriteString("系统: ")
		}
		transcript.WriteString(content)
		transcript.WriteString("\n\n")
	}
	text := transcript.String()
	if len([]rune(text)) > 60_000 {
		text = string([]rune(text)[:60_000]) + "\n…[truncated]"
	}
	ctx, cancel := context.WithTimeout(r.Context(), s.cfg.LLMTimeout)
	defer cancel()
	stream, err := provider.Stream(ctx, llm.Request{
		Model: bot.Model,
		Messages: []llm.Message{
			llm.TextMessage(llm.RoleSystem, "你是对话摘要助手。用中文输出事实性摘要，保留目标、关键决定、未完成事项和重要数据；不要编造，不要添加寒暄。"),
			llm.TextMessage(llm.RoleUser, "对话标题："+conv.Title+"\n\n记录：\n"+text),
		},
		MaxTokens: 1200,
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
	content := strings.TrimSpace(summary.String())
	if content == "" {
		writeError(w, http.StatusBadGateway, "summarize_failed", "model returned no summary")
		return
	}
	last := messages[len(messages)-1]
	created, err := s.db.CreateConversationSummary(r.Context(), store.CreateConversationSummaryParams{
		ConversationID: conv.ID,
		UpToMessageID:  &last.ID,
		UpToSeq:        last.Ordinal,
		Content:        content,
		Tokens:         int32(len([]rune(content)) / 4),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not save summary")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"summary": toConversationSummaryDTO(created)})
}
