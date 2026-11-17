package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"superbot/internal/chat"
	"superbot/internal/events"
	"superbot/internal/store"
)

func (s *Server) conversationRoutes(r chi.Router) {
	r.Get("/", s.handleListConversations)
	r.Post("/", s.handleCreateConversation)
	r.Route("/{conversationID}", func(r chi.Router) {
		r.Get("/", s.handleGetConversation)
		r.Post("/messages", s.handleSendMessage)
		r.Post("/stop", s.handleStopConversation)
		r.Get("/stream", s.handleConversationStream)
		r.Get("/summary", s.handleGetConversationSummary)
		r.Post("/summary", s.handleSummarizeConversation)
		r.Get("/summaries", s.handleListConversationSummaries)
		r.Get("/goal", s.handleGetGoal)
		r.Post("/goal/actions", s.handleGoalAction)
		r.Get("/side", s.handleListSideChats)
		r.Post("/side", s.handleAskSide)
		r.Delete("/side/{sideID}", s.handleDeleteSideChat)
	})
}

func (s *Server) runRoutes(r chi.Router) {
	r.Get("/{runID}", s.handleGetRun)
	r.Get("/{runID}/events", s.handleRunEvents)
}

func (s *Server) handleListConversations(w http.ResponseWriter, r *http.Request) {
	limit, offset := parsePagination(r)
	conversations, err := s.chat.ListConversations(r.Context(), accountFrom(r), userFrom(r).ID, limit, offset)
	if err != nil {
		s.logger.Error("list conversations failed", "error", err)
		writeError(w, http.StatusInternalServerError, "internal", "could not list conversations")
		return
	}
	out := make([]conversationDTO, 0, len(conversations))
	for _, c := range conversations {
		out = append(out, toConversationDTO(c))
	}
	writeJSON(w, http.StatusOK, map[string]any{"conversations": out})
}

type createConversationRequest struct {
	Kind   string     `json:"kind"`
	BotID  *uuid.UUID `json:"bot_id"`
	TeamID *uuid.UUID `json:"team_id"`
	Title  string     `json:"title"`
}

func (s *Server) handleCreateConversation(w http.ResponseWriter, r *http.Request) {
	var req createConversationRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	if req.Kind == "" {
		req.Kind = "direct"
	}
	switch req.Kind {
	case "direct":
		if req.BotID == nil {
			writeError(w, http.StatusBadRequest, "invalid_request", "bot_id is required for direct conversations")
			return
		}
		conv, err := s.chat.CreateDirectConversation(r.Context(), accountFrom(r), userFrom(r).ID, *req.BotID, req.Title)
		if err != nil {
			writeChatError(w, err, "could not create conversation")
			return
		}
		writeJSON(w, http.StatusCreated, map[string]any{"conversation": toConversationDTO(conv)})
	case "team":
		if req.TeamID == nil {
			writeError(w, http.StatusBadRequest, "invalid_request", "team_id is required for team conversations")
			return
		}
		conv, err := s.chat.CreateTeamConversation(r.Context(), accountFrom(r), *req.TeamID, req.Title)
		if err != nil {
			writeChatError(w, err, "could not create team conversation")
			return
		}
		writeJSON(w, http.StatusCreated, map[string]any{"conversation": toConversationDTO(conv)})
	default:
		writeError(w, http.StatusBadRequest, "invalid_request", "kind must be direct or team")
	}
}

func (s *Server) toMessageDTOs(ctx context.Context, userID uuid.UUID, messages []store.Message) []messageDTO {
	out := make([]messageDTO, 0, len(messages))
	if len(messages) == 0 {
		return out
	}
	ids := make([]uuid.UUID, 0, len(messages))
	for _, message := range messages {
		ids = append(ids, message.ID)
	}
	rows, err := s.db.ListMessageReactions(ctx, store.ListMessageReactionsParams{
		Column1: ids,
		UserID:  userID,
	})
	if err != nil {
		s.logger.Warn("list message reactions failed", "error", err)
		rows = nil
	}
	byMessage := make(map[uuid.UUID][]messageReactionDTO)
	for _, row := range rows {
		byMessage[row.MessageID] = append(byMessage[row.MessageID], messageReactionDTO{
			Emoji:   row.Emoji,
			Count:   row.ReactionCount,
			Reacted: row.Reacted,
		})
	}
	// Only the metadata: files a bot sends can be large, and the list never
	// needs their bytes.
	attachments, err := s.db.ListAttachmentMetaForMessages(ctx, ids)
	if err != nil {
		s.logger.Warn("list message attachments failed", "error", err)
		attachments = nil
	}
	attachmentsByMessage := make(map[uuid.UUID][]attachmentDTO)
	for _, attachment := range attachments {
		if attachment.MessageID == nil {
			continue
		}
		attachmentsByMessage[*attachment.MessageID] = append(
			attachmentsByMessage[*attachment.MessageID],
			attachmentDTO{
				ID:        attachment.ID,
				Filename:  attachment.Filename,
				MimeType:  attachment.MimeType,
				SizeBytes: attachment.SizeBytes,
				URL:       "/api/attachments/" + attachment.ID.String(),
			},
		)
	}
	pollRows, err := s.db.ListPollVotes(ctx, store.ListPollVotesParams{
		Column1: ids,
		UserID:  userID,
	})
	if err != nil {
		s.logger.Warn("list poll votes failed", "error", err)
		pollRows = nil
	}
	votesByMessage := make(map[uuid.UUID]map[string]store.ListPollVotesRow)
	for _, row := range pollRows {
		if votesByMessage[row.MessageID] == nil {
			votesByMessage[row.MessageID] = make(map[string]store.ListPollVotesRow)
		}
		votesByMessage[row.MessageID][row.OptionID] = row
	}
	for _, message := range messages {
		dto := toMessageDTO(message)
		if reactions := byMessage[message.ID]; len(reactions) > 0 {
			dto.Reactions = reactions
		}
		if items := attachmentsByMessage[message.ID]; len(items) > 0 {
			dto.Attachments = items
		}
		if poll := buildPollDTO(message.Metadata, votesByMessage[message.ID]); poll != nil {
			dto.Poll = poll
		}
		out = append(out, dto)
	}
	return out
}

func buildPollDTO(metadata json.RawMessage, votes map[string]store.ListPollVotesRow) *pollDTO {
	var meta struct {
		Kind string `json:"kind"`
		Poll struct {
			Question string `json:"question"`
			Multiple bool   `json:"multiple"`
			Options  []struct {
				ID   string `json:"id"`
				Text string `json:"text"`
			} `json:"options"`
		} `json:"poll"`
	}
	if len(metadata) == 0 || json.Unmarshal(metadata, &meta) != nil {
		return nil
	}
	if meta.Kind != "poll" || len(meta.Poll.Options) == 0 {
		return nil
	}
	poll := &pollDTO{
		Question: meta.Poll.Question,
		Multiple: meta.Poll.Multiple,
		Options:  make([]pollOptionDTO, 0, len(meta.Poll.Options)),
	}
	for _, option := range meta.Poll.Options {
		item := pollOptionDTO{ID: option.ID, Text: option.Text}
		if row, ok := votes[option.ID]; ok {
			item.Votes = int(row.VoteCount)
			item.Voted = row.Voted
		}
		poll.TotalVotes += item.Votes
		poll.Options = append(poll.Options, item)
	}
	return poll
}

func (s *Server) handleGetConversation(w http.ResponseWriter, r *http.Request) {
	conversationID, ok := parseUUIDParam(w, r, "conversationID")
	if !ok {
		return
	}
	conv, messages, err := s.chat.GetConversation(r.Context(), accountFrom(r), userFrom(r).ID, conversationID)
	if err != nil {
		writeChatError(w, err, "could not load conversation")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"conversation": toConversationDTO(conv),
		"messages":     s.toMessageDTOs(r.Context(), userFrom(r).ID, messages),
	})
}

type sendMessageRequest struct {
	Content         string      `json:"content"`
	ClientMsgID     string      `json:"client_msg_id"`
	ParentMessageID *uuid.UUID  `json:"parent_message_id"`
	AttachmentIDs   []uuid.UUID `json:"attachment_ids"`
}

func (s *Server) handleSendMessage(w http.ResponseWriter, r *http.Request) {
	conversationID, ok := parseUUIDParam(w, r, "conversationID")
	if !ok {
		return
	}
	var req sendMessageRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	result, err := s.chat.SendUserMessage(
		r.Context(),
		accountFrom(r),
		userFrom(r).ID,
		conversationID,
		req.Content,
		strings.TrimSpace(req.ClientMsgID),
		req.ParentMessageID,
		req.AttachmentIDs,
		json.RawMessage(`{}`),
	)
	if err != nil {
		writeChatError(w, err, "could not send message")
		return
	}
	// A new user message is bot activity: keep its persistent computer from
	// being auto-slept while the cycle starts and runs.
	if conv, err := s.db.GetConversation(r.Context(), conversationID); err == nil && conv.DirectBotID != nil {
		_ = s.db.TouchBotActivity(r.Context(), store.TouchBotActivityParams{
			ID: *conv.DirectBotID, OwnerID: accountFrom(r),
		})
		// Uploaded images/media/documents should be discoverable in the bot
		// Library without a separate save step.
		if attachments, listErr := s.db.ListAttachmentsForMessages(r.Context(), []uuid.UUID{result.Message.ID}); listErr == nil {
			for _, attachment := range attachments {
				if _, indexErr := s.library.IndexAttachment(r.Context(), accountFrom(r), *conv.DirectBotID, attachment, &result.Message.ID); indexErr != nil {
					s.logger.Warn("index attachment into library failed", "attachment_id", attachment.ID, "error", indexErr)
				}
			}
		}
	}
	status := http.StatusCreated
	payload := map[string]any{
		"message":      s.toMessageDTOs(r.Context(), userFrom(r).ID, []store.Message{result.Message})[0],
		"interjection": result.Interjection,
	}
	if result.Cycle != nil {
		payload["cycle_id"] = result.Cycle.ID
		payload["cycle_status"] = result.Cycle.Status
	}
	writeJSON(w, status, payload)
}

func (s *Server) handleStopConversation(w http.ResponseWriter, r *http.Request) {
	conversationID, ok := parseUUIDParam(w, r, "conversationID")
	if !ok {
		return
	}
	n, err := s.chat.RequestStop(r.Context(), accountFrom(r), userFrom(r).ID, conversationID)
	if err != nil {
		writeChatError(w, err, "could not stop conversation")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"canceled_cycles": n})
}

func (s *Server) handleConversationStream(w http.ResponseWriter, r *http.Request) {
	conversationID, ok := parseUUIDParam(w, r, "conversationID")
	if !ok {
		return
	}
	conv, err := s.db.GetConversation(r.Context(), conversationID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "not_found", "conversation not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", "could not load conversation")
		return
	}
	if conv.CreatedBy != accountFrom(r) {
		writeError(w, http.StatusForbidden, "forbidden", "conversation belongs to another account")
		return
	}
	if conv.Kind == "direct" && conv.DirectUserID != nil && *conv.DirectUserID != userFrom(r).ID {
		writeError(w, http.StatusForbidden, "forbidden", "direct conversation belongs to another user")
		return
	}

	afterSeq := int64(0)
	if raw := strings.TrimSpace(r.URL.Query().Get("after")); raw != "" {
		parsed, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || parsed < 0 {
			writeError(w, http.StatusBadRequest, "invalid_request", "after must be a non-negative integer")
			return
		}
		afterSeq = parsed
	}
	// EventSource reconnects to the original URL; the id of the last event it
	// saw comes back in this header and is the real resume point.
	if raw := strings.TrimSpace(r.Header.Get("Last-Event-ID")); raw != "" {
		if parsed, err := strconv.ParseInt(raw, 10, 64); err == nil && parsed > afterSeq {
			afterSeq = parsed
		}
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "internal", "streaming is not supported")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache, no-transform")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, "retry: 3000\n\n")
	flusher.Flush()

	// Subscribe before replaying so events written during replay are not lost.
	signals, unsubscribe := s.bus.Subscribe(conversationID)
	defer unsubscribe()

	if minSeq, err := s.db.MinEventSeq(r.Context(), conversationID); err == nil && afterSeq > 0 && minSeq > 0 && afterSeq < minSeq-1 {
		env := events.Envelope{
			Seq:     minSeq,
			Type:    events.TypeResync,
			Payload: mustJSON(map[string]any{"min_seq": minSeq, "after": afterSeq}),
		}
		writeSSE(w, env)
		flusher.Flush()
		afterSeq = minSeq - 1
	}

	lastSeq := afterSeq
	pollTicker := time.NewTicker(2 * time.Second)
	defer pollTicker.Stop()
	pingTicker := time.NewTicker(15 * time.Second)
	defer pingTicker.Stop()

	for {
		items, err := s.db.ListEventsAfter(r.Context(), store.ListEventsAfterParams{
			ConversationID: conversationID,
			Seq:            lastSeq,
			Limit:          500,
		})
		if err != nil {
			s.logger.Warn("stream events query failed", "conversation_id", conversationID, "error", err)
			writeSSEError(w, "event query failed")
			flusher.Flush()
			return
		}
		for _, item := range items {
			writeSSE(w, events.Envelope{
				Seq:       item.Seq,
				Type:      item.Type,
				Payload:   item.Payload,
				CreatedAt: item.CreatedAt,
			})
			lastSeq = item.Seq
		}
		if len(items) > 0 {
			flusher.Flush()
			continue
		}

		select {
		case <-r.Context().Done():
			return
		case _, open := <-signals:
			if !open {
				signals = nil
			}
		case <-pollTicker.C:
		case <-pingTicker.C:
			fmt.Fprint(w, ": ping\n\n")
			flusher.Flush()
		}
	}
}

func writeSSE(w http.ResponseWriter, env events.Envelope) {
	payload, err := json.Marshal(env)
	if err != nil {
		return
	}
	fmt.Fprintf(w, "id: %d\nevent: %s\ndata: %s\n\n", env.Seq, env.Type, payload)
}

func writeSSEError(w http.ResponseWriter, message string) {
	env := events.Envelope{
		Type:    events.TypeError,
		Payload: mustJSON(map[string]any{"error": message}),
	}
	writeSSE(w, env)
}

func mustJSON(value any) json.RawMessage {
	body, err := json.Marshal(value)
	if err != nil {
		return json.RawMessage(`{}`)
	}
	return body
}

func (s *Server) handleGetRun(w http.ResponseWriter, r *http.Request) {
	runID, ok := parseUUIDParam(w, r, "runID")
	if !ok {
		return
	}
	run, err := s.getOwnedRun(r.Context(), accountFrom(r), runID)
	if err != nil {
		writeChatError(w, err, "could not load run")
		return
	}
	invocations, err := s.db.ListToolInvocations(r.Context(), run.ID)
	if err != nil {
		s.logger.Warn("list tool invocations failed", "run_id", run.ID, "error", err)
	}
	eventsList, err := s.db.ListRunEvents(r.Context(), &run.ID)
	if err != nil {
		s.logger.Warn("list run events failed", "run_id", run.ID, "error", err)
	}
	toolsOut := make([]toolInvocationDTO, 0, len(invocations))
	for _, inv := range invocations {
		toolsOut = append(toolsOut, toToolInvocationDTO(inv))
	}
	eventsOut := make([]eventDTO, 0, len(eventsList))
	for _, ev := range eventsList {
		eventsOut = append(eventsOut, toEventDTO(ev))
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"run":              toRunDTO(run),
		"tool_invocations": toolsOut,
		"events":           eventsOut,
	})
}

func (s *Server) handleRunEvents(w http.ResponseWriter, r *http.Request) {
	runID, ok := parseUUIDParam(w, r, "runID")
	if !ok {
		return
	}
	run, err := s.getOwnedRun(r.Context(), accountFrom(r), runID)
	if err != nil {
		writeChatError(w, err, "could not load run")
		return
	}
	items, err := s.db.ListRunEvents(r.Context(), &run.ID)
	if err != nil {
		s.logger.Error("list run events failed", "run_id", run.ID, "error", err)
		writeError(w, http.StatusInternalServerError, "internal", "could not list run events")
		return
	}
	out := make([]eventDTO, 0, len(items))
	for _, ev := range items {
		out = append(out, toEventDTO(ev))
	}
	writeJSON(w, http.StatusOK, map[string]any{"events": out})
}

func (s *Server) getOwnedRun(ctx context.Context, userID, runID uuid.UUID) (store.Run, error) {
	run, err := s.db.GetRun(ctx, runID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return store.Run{}, chat.ErrNotFound
		}
		return store.Run{}, err
	}
	conv, err := s.db.GetConversation(ctx, run.ConversationID)
	if err != nil {
		return store.Run{}, err
	}
	if conv.CreatedBy != userID {
		return store.Run{}, chat.ErrForbidden
	}
	return run, nil
}

func writeChatError(w http.ResponseWriter, err error, fallback string) {
	switch {
	case errors.Is(err, chat.ErrInvalid):
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
	case errors.Is(err, chat.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "not found")
	case errors.Is(err, chat.ErrForbidden):
		writeError(w, http.StatusForbidden, "forbidden", "access denied")
	case errors.Is(err, chat.ErrBusy):
		writeError(w, http.StatusConflict, "busy", "conversation already has an active cycle")
	default:
		writeError(w, http.StatusInternalServerError, "internal", fallback)
	}
}

func parsePagination(r *http.Request) (int32, int32) {
	limit := int32(50)
	offset := int32(0)
	if raw := r.URL.Query().Get("limit"); raw != "" {
		if v, err := strconv.Atoi(raw); err == nil && v > 0 && v <= 100 {
			limit = int32(v)
		}
	}
	if raw := r.URL.Query().Get("offset"); raw != "" {
		if v, err := strconv.Atoi(raw); err == nil && v >= 0 {
			offset = int32(v)
		}
	}
	return limit, offset
}
