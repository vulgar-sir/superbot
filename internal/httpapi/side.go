package httpapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"superbot/internal/goals"
	"superbot/internal/llm"
	"superbot/internal/store"
)

const (
	sideMaxQuestionRunes = 2000
	sideHistoryMessages  = 40
	sideHistoryRunes     = 24_000
	sideMessageRunes     = 3000
	sideMaxTokens        = 1500
	sideListLimit        = 50
)

type sideChatDTO struct {
	ID         uuid.UUID  `json:"id"`
	BotID      *uuid.UUID `json:"bot_id"`
	Question   string     `json:"question"`
	Answer     string     `json:"answer"`
	Status     string     `json:"status"`
	Error      string     `json:"error"`
	CreatedAt  time.Time  `json:"created_at"`
	AnsweredAt *time.Time `json:"answered_at"`
}

func toSideChatDTO(row store.SideChat) sideChatDTO {
	return sideChatDTO{
		ID: row.ID, BotID: row.BotID, Question: row.Question, Answer: row.Answer,
		Status: row.Status, Error: row.Error, CreatedAt: row.CreatedAt, AnsweredAt: row.AnsweredAt,
	}
}

func (s *Server) handleListSideChats(w http.ResponseWriter, r *http.Request) {
	conversationID, ok := parseUUIDParam(w, r, "conversationID")
	if !ok {
		return
	}
	if _, _, err := s.chat.GetConversation(r.Context(), accountFrom(r), userFrom(r).ID, conversationID); err != nil {
		writeChatError(w, err, "could not load conversation")
		return
	}
	_, _ = s.db.FailStaleSideChats(r.Context())
	rows, err := s.db.ListSideChats(r.Context(), store.ListSideChatsParams{
		ConversationID: conversationID, OwnerID: accountFrom(r), Limit: sideListLimit,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not list side questions")
		return
	}
	items := make([]sideChatDTO, 0, len(rows))
	for i := len(rows) - 1; i >= 0; i-- {
		items = append(items, toSideChatDTO(rows[i]))
	}
	writeJSON(w, http.StatusOK, map[string]any{"side_chats": items})
}

type askSideRequest struct {
	Question string `json:"question"`
}

// handleAskSide answers a quick question from a snapshot of the conversation
// without touching it: nothing is written to the transcript, no tools run and
// a bot that is in the middle of a task is not interrupted.
func (s *Server) handleAskSide(w http.ResponseWriter, r *http.Request) {
	conversationID, ok := parseUUIDParam(w, r, "conversationID")
	if !ok {
		return
	}
	conv, messages, err := s.chat.GetConversation(r.Context(), accountFrom(r), userFrom(r).ID, conversationID)
	if err != nil {
		writeChatError(w, err, "could not load conversation")
		return
	}
	var req askSideRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	question := strings.TrimSpace(req.Question)
	if question == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "question is required")
		return
	}
	if len([]rune(question)) > sideMaxQuestionRunes {
		writeError(w, http.StatusBadRequest, "invalid_request", "question is too long")
		return
	}
	bot, err := s.sideBot(r.Context(), conv)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	providerRow, err := s.db.GetProvider(r.Context(), store.GetProviderParams{ID: *bot.ProviderID, OwnerID: accountFrom(r)})
	if err != nil || !providerRow.Enabled {
		writeError(w, http.StatusBadRequest, "invalid_request", "the bot's provider is not available")
		return
	}
	provider, err := s.factory.Build(providerRow, s.cfg.LLMTimeout)
	if err != nil {
		writeError(w, http.StatusBadRequest, "provider_failed", err.Error())
		return
	}

	row, err := s.db.CreateSideChat(r.Context(), store.CreateSideChatParams{
		ConversationID: conv.ID, OwnerID: accountFrom(r), BotID: &bot.ID, Question: question,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not save side question")
		return
	}

	// The answer must be recorded even if the browser gives up waiting.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), s.cfg.LLMTimeout+30*time.Second)
	defer cancel()
	request := llm.Request{
		Model:     bot.Model,
		Messages:  s.sideMessages(ctx, conv, bot, messages, question),
		MaxTokens: sideMaxTokens,
	}
	answer, usage, callErr := collectText(ctx, provider, request)
	answer = llm.StripArtifacts(answer)
	status, errText := "answered", ""
	if callErr != nil {
		status, errText = "failed", callErr.Error()
	} else if strings.TrimSpace(answer) == "" {
		status, errText = "failed", "模型没有返回内容"
	}
	finished, err := s.db.FinishSideChat(ctx, store.FinishSideChatParams{
		ID: row.ID, Answer: strings.TrimSpace(answer), Status: status, Error: errText,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not save side answer")
		return
	}
	s.recordSideUsage(ctx, conv, bot, providerRow, request, answer, usage)
	if status == "failed" {
		writeJSON(w, http.StatusBadGateway, map[string]any{"side_chat": toSideChatDTO(finished)})
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"side_chat": toSideChatDTO(finished)})
}

func (s *Server) handleDeleteSideChat(w http.ResponseWriter, r *http.Request) {
	sideID, ok := parseUUIDParam(w, r, "sideID")
	if !ok {
		return
	}
	n, err := s.db.DeleteSideChat(r.Context(), store.DeleteSideChatParams{ID: sideID, OwnerID: accountFrom(r)})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not delete side question")
		return
	}
	if n == 0 {
		writeError(w, http.StatusNotFound, "not_found", "side question not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// sideBot picks who answers: the bot of a direct chat, or the lead of a team.
func (s *Server) sideBot(ctx context.Context, conv store.Conversation) (store.Bot, error) {
	var botID uuid.UUID
	switch {
	case conv.DirectBotID != nil:
		botID = *conv.DirectBotID
	case conv.Kind == "team" && conv.TeamID != nil:
		roster, err := goals.LoadRoster(ctx, s.db, conv)
		if err != nil {
			return store.Bot{}, err
		}
		botID = roster.LeadID
	default:
		return store.Bot{}, errors.New("side questions are not available in this conversation")
	}
	bot, err := s.db.GetBot(ctx, store.GetBotParams{ID: botID, OwnerID: conv.CreatedBy})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return store.Bot{}, errors.New("bot not found")
		}
		return store.Bot{}, err
	}
	if bot.ProviderID == nil {
		return store.Bot{}, fmt.Errorf("bot %q has no provider configured", bot.Name)
	}
	return bot, nil
}

// sideMessages builds the frozen snapshot: who the bot is, the latest
// checkpoint, the recent transcript, and the question. Tools are not offered.
func (s *Server) sideMessages(ctx context.Context, conv store.Conversation, bot store.Bot, history []store.Message, question string) []llm.Message {
	var system strings.Builder
	fmt.Fprintf(&system, "你是 %s。", bot.Name)
	if bot.Description != "" {
		fmt.Fprintf(&system, "你的职责：%s。", bot.Description)
	}
	if bot.SystemPrompt != "" {
		system.WriteString("\n\n## 你的专属设定\n")
		system.WriteString(bot.SystemPrompt)
	}
	system.WriteString("\n\n## 旁路提问\n")
	system.WriteString("用户在不打断主对话的情况下顺手问你一个问题。下面是主对话当前的快照，仅供参考。\n")
	system.WriteString("- 只根据快照和你已有的知识直接回答，简短、具体。\n")
	system.WriteString("- 你不能调用任何工具，也不要声称去执行了什么。\n")
	system.WriteString("- 这次问答不会进入主对话；如果问题需要真正动手才能回答，就说明需要在主对话里处理。\n")
	system.WriteString("- 使用用户提问时使用的语言。")
	messages := []llm.Message{llm.TextMessage(llm.RoleSystem, system.String())}

	afterSeq := int64(-1)
	if summary, err := s.db.GetLatestConversationSummary(ctx, conv.ID); err == nil && strings.TrimSpace(summary.Content) != "" {
		afterSeq = summary.UpToSeq
		messages = append(messages, llm.TextMessage(llm.RoleSystem, "## 早前对话检查点\n"+summary.Content))
	}

	names := map[uuid.UUID]string{bot.ID: bot.Name}
	if bots, err := s.db.ListBots(ctx, conv.CreatedBy); err == nil {
		for _, item := range bots {
			names[item.ID] = item.Name
		}
	}
	lines := make([]string, 0, sideHistoryMessages)
	total := 0
	for i := len(history) - 1; i >= 0 && len(lines) < sideHistoryMessages; i-- {
		msg := history[i]
		content := strings.TrimSpace(msg.Content)
		if content == "" || (afterSeq >= 0 && msg.Ordinal <= afterSeq) {
			continue
		}
		speaker := "系统"
		switch msg.SenderType {
		case "user":
			speaker = "用户"
		case "bot":
			speaker = "Bot"
			if msg.SenderBotID != nil {
				if name, ok := names[*msg.SenderBotID]; ok {
					speaker = name
				}
			}
		}
		if runes := []rune(content); len(runes) > sideMessageRunes {
			content = string(runes[:sideMessageRunes]) + "…"
		}
		line := speaker + ": " + content
		total += len([]rune(line))
		if total > sideHistoryRunes {
			break
		}
		lines = append(lines, line)
	}
	for i, j := 0, len(lines)-1; i < j; i, j = i+1, j-1 {
		lines[i], lines[j] = lines[j], lines[i]
	}
	var user strings.Builder
	if len(lines) > 0 {
		user.WriteString("主对话快照（由旧到新）：\n")
		user.WriteString(strings.Join(lines, "\n\n"))
		user.WriteString("\n\n———\n")
	}
	user.WriteString("旁路问题：")
	user.WriteString(question)
	return append(messages, llm.TextMessage(llm.RoleUser, user.String()))
}

func (s *Server) recordSideUsage(ctx context.Context, conv store.Conversation, bot store.Bot, providerRow store.Provider, request llm.Request, answer string, usage *llm.Usage) {
	estimated := usage == nil
	if estimated {
		prompt := 0
		for _, msg := range request.Messages {
			prompt += len([]rune(msg.Text()))/2 + 4
		}
		usage = &llm.Usage{PromptTokens: prompt, CompletionTokens: len([]rune(answer))/2 + 1}
	}
	cost := int64(0)
	if model, err := s.db.GetModelByName(ctx, store.GetModelByNameParams{ProviderID: providerRow.ID, Name: bot.Model}); err == nil {
		cached := int64(usage.CachedTokens)
		prompt := int64(usage.PromptTokens)
		if cached > prompt {
			cached = prompt
		}
		cost = (prompt-cached)*model.PriceInMicrosPerMtok/1_000_000 +
			cached*model.PriceCacheMicrosPerMtok/1_000_000 +
			int64(usage.CompletionTokens)*model.PriceOutMicrosPerMtok/1_000_000
	}
	if _, err := s.db.CreateUsageRecord(ctx, store.CreateUsageRecordParams{
		UserID:           conv.CreatedBy,
		BotID:            &bot.ID,
		ProviderID:       &providerRow.ID,
		Model:            bot.Model,
		PromptTokens:     int64(usage.PromptTokens),
		CachedTokens:     int64(usage.CachedTokens),
		CompletionTokens: int64(usage.CompletionTokens),
		CostMicros:       cost,
		Estimated:        estimated,
	}); err != nil {
		s.logger.Warn("record side question usage failed", "conversation_id", conv.ID, "error", err)
	}
}

// collectText runs one model call to completion and returns its text.
func collectText(ctx context.Context, provider llm.Provider, request llm.Request) (string, *llm.Usage, error) {
	stream, err := provider.Stream(ctx, request)
	if err != nil {
		return "", nil, err
	}
	defer stream.Close()
	var text strings.Builder
	var usage *llm.Usage
	finished := false
	for event := range stream.Events() {
		switch event.Type {
		case llm.EventTextDelta:
			text.WriteString(event.Text)
		case llm.EventUsage:
			usage = event.Usage
		case llm.EventError:
			return text.String(), usage, event.Err
		case llm.EventDone:
			finished = true
		}
	}
	if !finished {
		if err := ctx.Err(); err != nil {
			return text.String(), usage, err
		}
		return text.String(), usage, errors.New("model stream ended unexpectedly")
	}
	return text.String(), usage, nil
}
