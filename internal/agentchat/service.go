package agentchat

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"superbot/internal/events"
	"superbot/internal/store"
)

var (
	ErrNotFound = errors.New("agent thread not found")
	ErrInvalid  = errors.New("invalid agent message")
	ErrSameBot  = errors.New("a bot cannot message itself")
	ErrNotOwned = errors.New("bot does not belong to this account")
)

const (
	maxMessageBytes = 16 * 1024
	maxPreviewRunes = 160
	// maxReplyRunes bounds how much of a teammate's reply is handed back to
	// the sender's model.
	maxReplyRunes = 6000
	// MaxHops bounds a chain of bots messaging bots (A -> B -> C -> ...).
	MaxHops = 6
)

// ErrTooManyHops is returned when a bot-to-bot chain grows past MaxHops
// without a human in between.
var ErrTooManyHops = errors.New("bot-to-bot chain is too long")

// Service owns bot-to-bot private threads, the user-visible agent activity
// feed and the summary cards that keep the main chat readable.
type Service struct {
	db     *store.DB
	bus    *events.Bus
	logger *slog.Logger
}

func NewService(db *store.DB, bus *events.Bus, logger *slog.Logger) *Service {
	return &Service{db: db, bus: bus, logger: logger}
}

// FindBot resolves a recipient by UUID or exact name for the model-facing
// send_to_agent tool. Only bots owned by the same account are eligible.
func (s *Service) FindBot(ctx context.Context, ownerID uuid.UUID, ref string) (store.Bot, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return store.Bot{}, ErrNotFound
	}
	if id, err := uuid.Parse(ref); err == nil {
		bot, err := s.db.GetBot(ctx, store.GetBotParams{ID: id, OwnerID: ownerID})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return store.Bot{}, ErrNotFound
			}
			return store.Bot{}, err
		}
		return bot, nil
	}
	bots, err := s.db.ListBots(ctx, ownerID)
	if err != nil {
		return store.Bot{}, err
	}
	for _, bot := range bots {
		if strings.EqualFold(bot.Name, ref) {
			return bot, nil
		}
	}
	return store.Bot{}, ErrNotFound
}

type SendResult struct {
	Thread       store.AgentThread
	Conversation store.Conversation
	Message      store.Message
	Recipient    store.Bot
}

// Send appends a bot message to the pair's private thread, schedules a cycle
// for the recipient, writes an activity entry and drops a folded summary card
// into the origin conversation (if there is one).
func (s *Service) Send(
	ctx context.Context,
	ownerID, senderBotID, recipientBotID uuid.UUID,
	originConversationID *uuid.UUID,
	content string,
	hop int,
) (SendResult, error) {
	if hop >= MaxHops {
		return SendResult{}, ErrTooManyHops
	}
	content = strings.TrimSpace(content)
	if content == "" {
		return SendResult{}, fmt.Errorf("%w: message is required", ErrInvalid)
	}
	if len(content) > maxMessageBytes {
		return SendResult{}, fmt.Errorf("%w: message is too long", ErrInvalid)
	}
	if senderBotID == recipientBotID {
		return SendResult{}, ErrSameBot
	}
	sender, err := s.db.GetBot(ctx, store.GetBotParams{ID: senderBotID, OwnerID: ownerID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return SendResult{}, ErrNotOwned
		}
		return SendResult{}, fmt.Errorf("load sender: %w", err)
	}
	recipient, err := s.db.GetBot(ctx, store.GetBotParams{ID: recipientBotID, OwnerID: ownerID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return SendResult{}, ErrNotOwned
		}
		return SendResult{}, fmt.Errorf("load recipient: %w", err)
	}

	tx, err := s.db.Pool.Begin(ctx)
	if err != nil {
		return SendResult{}, fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback(ctx)
	q := s.db.WithTx(tx)

	origin := s.resolveOrigin(ctx, q, originConversationID)

	thread, err := q.GetAgentThreadByPair(ctx, store.GetAgentThreadByPairParams{
		OwnerID: ownerID,
		BotAID:  senderBotID,
		BotBID:  recipientBotID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		conv, convErr := q.CreateConversation(ctx, store.CreateConversationParams{
			Kind:      "agent",
			Title:     sender.Name + " ↔ " + recipient.Name,
			CreatedBy: ownerID,
		})
		if convErr != nil {
			return SendResult{}, fmt.Errorf("create agent conversation: %w", convErr)
		}
		thread, err = q.CreateAgentThread(ctx, store.CreateAgentThreadParams{
			OwnerID:              ownerID,
			ConversationID:       conv.ID,
			BotAID:               senderBotID,
			BotBID:               recipientBotID,
			OriginConversationID: origin,
		})
		if err != nil {
			return SendResult{}, fmt.Errorf("create agent thread: %w", err)
		}
	} else if err != nil {
		return SendResult{}, fmt.Errorf("find agent thread: %w", err)
	}
	conv, err := q.GetConversation(ctx, thread.ConversationID)
	if err != nil {
		return SendResult{}, fmt.Errorf("load agent conversation: %w", err)
	}

	msg, err := q.CreateMessage(ctx, store.CreateMessageParams{
		ConversationID: conv.ID,
		SenderType:     "bot",
		SenderBotID:    &sender.ID,
		Content:        content,
		Metadata:       json.RawMessage(fmt.Sprintf(`{"hop":%d}`, hop+1)),
	})
	if err != nil {
		return SendResult{}, fmt.Errorf("create agent message: %w", err)
	}
	cycle, err := q.CreateCycle(ctx, store.CreateCycleParams{
		ConversationID:   conv.ID,
		TriggerMessageID: msg.ID,
		MaxRounds:        4,
	})
	if err != nil {
		return SendResult{}, fmt.Errorf("create agent cycle: %w", err)
	}
	if err := q.SetMessageCycle(ctx, store.SetMessageCycleParams{ID: msg.ID, CycleID: &cycle.ID}); err != nil {
		return SendResult{}, fmt.Errorf("attach agent cycle: %w", err)
	}
	if _, err := events.Append(ctx, q, conv.ID, &cycle.ID, nil, events.TypeMessageCreated, map[string]any{"message": msg}); err != nil {
		return SendResult{}, err
	}
	if _, err := events.Append(ctx, q, conv.ID, &cycle.ID, nil, events.TypeCycleUpdated, map[string]any{
		"cycle_id": cycle.ID,
		"status":   cycle.Status,
		"round":    cycle.Round,
	}); err != nil {
		return SendResult{}, err
	}

	if origin != nil {
		if _, err := s.appendCard(ctx, q, *origin, cardPayload{
			ThreadID:       thread.ID,
			ConversationID: conv.ID,
			PeerBotID:      recipient.ID,
			PeerBotName:    recipient.Name,
			Direction:      "sent",
			Preview:        preview(content),
		}); err != nil {
			return SendResult{}, err
		}
	}

	if _, err := q.CreateActivity(ctx, store.CreateActivityParams{
		OwnerID:        ownerID,
		BotID:          recipient.ID,
		Kind:           "agent_message",
		Summary:        sender.Name + " 发来私信：" + preview(content),
		ConversationID: &conv.ID,
		MessageID:      &msg.ID,
		AgentThreadID:  &thread.ID,
	}); err != nil {
		return SendResult{}, fmt.Errorf("create agent activity: %w", err)
	}
	meta, _ := json.Marshal(map[string]any{
		"conversation_id": conv.ID.String(),
		"message_id":      msg.ID.String(),
		"agent_thread_id": thread.ID.String(),
		"bot_id":          recipient.ID.String(),
	})
	_, _ = q.CreateNotification(ctx, store.CreateNotificationParams{
		OwnerID:      ownerID,
		Kind:         "agent_message",
		Title:        sender.Name + " 发来私信",
		Body:         preview(content),
		ResourceType: "conversation",
		ResourceID:   conv.ID.String(),
		Metadata:     meta,
	})
	if err := q.TouchAgentThread(ctx, thread.ID); err != nil {
		return SendResult{}, fmt.Errorf("touch agent thread: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return SendResult{}, fmt.Errorf("commit: %w", err)
	}
	s.notify(ctx, conv.ID)
	if origin != nil {
		s.notify(ctx, *origin)
	}
	msg.CycleID = &cycle.ID
	return SendResult{Thread: thread, Conversation: conv, Message: msg, Recipient: recipient}, nil
}

func (s *Service) resolveOrigin(ctx context.Context, q *store.Queries, origin *uuid.UUID) *uuid.UUID {
	if origin == nil {
		return nil
	}
	conv, err := q.GetConversation(ctx, *origin)
	if err != nil {
		return nil
	}
	if conv.Kind != "agent" {
		return origin
	}
	thread, err := q.GetAgentThreadByConversation(ctx, conv.ID)
	if err != nil {
		return nil
	}
	return thread.OriginConversationID
}

type cardPayload struct {
	ThreadID       uuid.UUID
	ConversationID uuid.UUID
	PeerBotID      uuid.UUID
	PeerBotName    string
	Direction      string
	Preview        string
	// Reply is the full text handed back to the bot that asked. The card the
	// user sees stays folded; the model gets the substance.
	Reply    string
	ForBotID *uuid.UUID
}

func (s *Service) appendCard(ctx context.Context, q *store.Queries, origin uuid.UUID, payload cardPayload) (store.Message, error) {
	fields := map[string]any{
		"kind":            "agent_thread",
		"thread_id":       payload.ThreadID.String(),
		"conversation_id": payload.ConversationID.String(),
		"peer_bot_id":     payload.PeerBotID.String(),
		"peer_bot_name":   payload.PeerBotName,
		"direction":       payload.Direction,
		"preview":         payload.Preview,
	}
	if payload.Reply != "" {
		fields["inject"] = "true"
		fields["inject_text"] = payload.PeerBotName + " 回复了你的私信：\n" + truncate(payload.Reply, maxReplyRunes)
	}
	if payload.ForBotID != nil {
		fields["for_bot_id"] = payload.ForBotID.String()
	}
	meta, _ := json.Marshal(fields)
	content := "已发消息给 " + payload.PeerBotName
	if payload.Direction == "received" {
		content = "来自 " + payload.PeerBotName + " 的消息"
	}
	msg, err := q.CreateMessage(ctx, store.CreateMessageParams{
		ConversationID: origin,
		SenderType:     "system",
		Content:        content,
		Metadata:       meta,
	})
	if err != nil {
		return store.Message{}, fmt.Errorf("create agent summary card: %w", err)
	}
	if _, err := events.Append(ctx, q, origin, nil, nil, events.TypeMessageCreated, map[string]any{"message": msg}); err != nil {
		return store.Message{}, err
	}
	return msg, nil
}

// RecordUpdate turns a normal Bot reply into a sidebar activity entry.
func (s *Service) RecordUpdate(ctx context.Context, ownerID, botID, conversationID, messageID uuid.UUID, content string) error {
	if _, err := s.db.CreateActivity(ctx, store.CreateActivityParams{
		OwnerID:        ownerID,
		BotID:          botID,
		Kind:           "message",
		Summary:        preview(content),
		ConversationID: &conversationID,
		MessageID:      &messageID,
	}); err != nil {
		return fmt.Errorf("create message activity: %w", err)
	}
	meta, _ := json.Marshal(map[string]any{
		"conversation_id": conversationID.String(),
		"message_id":      messageID.String(),
		"bot_id":          botID.String(),
	})
	_, _ = s.db.CreateNotification(ctx, store.CreateNotificationParams{
		OwnerID:      ownerID,
		Kind:         "activity",
		Title:        "Bot 有新动态",
		Body:         preview(content),
		ResourceType: "message",
		ResourceID:   messageID.String(),
		Metadata:     meta,
	})
	if err := s.db.TouchBotActivity(ctx, store.TouchBotActivityParams{ID: botID, OwnerID: ownerID}); err != nil {
		s.logger.Warn("touch bot activity failed", "bot_id", botID, "error", err)
	}
	return nil
}

// RecordReply is called after the recipient finishes an agent-thread run. It
// returns the card dropped into the origin conversation (nil when the thread
// has no origin) so the caller can wake the bot that asked.
func (s *Service) RecordReply(ctx context.Context, thread store.AgentThread, responder store.Bot, peer store.Bot, content string, messageID uuid.UUID) (*store.Message, error) {
	if _, err := s.db.CreateActivity(ctx, store.CreateActivityParams{
		OwnerID:        thread.OwnerID,
		BotID:          responder.ID,
		Kind:           "agent_reply",
		Summary:        "回复了 " + peer.Name + "：" + preview(content),
		ConversationID: &thread.ConversationID,
		MessageID:      &messageID,
		AgentThreadID:  &thread.ID,
	}); err != nil {
		return nil, fmt.Errorf("create agent reply activity: %w", err)
	}
	meta, _ := json.Marshal(map[string]any{
		"conversation_id": thread.ConversationID.String(),
		"message_id":      messageID.String(),
		"agent_thread_id": thread.ID.String(),
		"bot_id":          responder.ID.String(),
	})
	_, _ = s.db.CreateNotification(ctx, store.CreateNotificationParams{
		OwnerID:      thread.OwnerID,
		Kind:         "agent_reply",
		Title:        responder.Name + " 回复了 " + peer.Name,
		Body:         preview(content),
		ResourceType: "agent_thread",
		ResourceID:   thread.ID.String(),
		Metadata:     meta,
	})
	if thread.OriginConversationID != nil {
		tx, err := s.db.Pool.Begin(ctx)
		if err != nil {
			return nil, err
		}
		defer tx.Rollback(ctx)
		q := s.db.WithTx(tx)
		card, err := s.appendCard(ctx, q, *thread.OriginConversationID, cardPayload{
			ThreadID:       thread.ID,
			ConversationID: thread.ConversationID,
			PeerBotID:      responder.ID,
			PeerBotName:    responder.Name,
			Direction:      "received",
			Preview:        preview(content),
			Reply:          content,
			ForBotID:       &peer.ID,
		})
		if err != nil {
			return nil, err
		}
		if err := tx.Commit(ctx); err != nil {
			return nil, err
		}
		s.notify(ctx, *thread.OriginConversationID)
		return &card, nil
	}
	return nil, nil
}

func truncate(content string, limit int) string {
	runes := []rune(strings.TrimSpace(content))
	if len(runes) <= limit {
		return string(runes)
	}
	return string(runes[:limit]) + "…"
}

func (s *Service) notify(ctx context.Context, conversationID uuid.UUID) {
	if s.bus == nil {
		return
	}
	if err := s.bus.Notify(ctx, conversationID); err != nil {
		s.logger.Warn("notify agent conversation failed", "conversation_id", conversationID, "error", err)
	}
}

func preview(content string) string {
	runes := []rune(strings.TrimSpace(content))
	if len(runes) <= maxPreviewRunes {
		return string(runes)
	}
	return string(runes[:maxPreviewRunes]) + "…"
}
