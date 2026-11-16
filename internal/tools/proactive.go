package tools

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"superbot/internal/events"
	"superbot/internal/store"
)

type proactiveMessageTool struct {
	db             *store.DB
	bus            *events.Bus
	ownerID        uuid.UUID
	botID          uuid.UUID
	botName        string
	conversationID uuid.UUID
}

// NewProactiveMessageTool posts into the DM the bot is currently talking in
// (conversationID) so proactive messages never land in a parallel thread.
func NewProactiveMessageTool(db *store.DB, bus *events.Bus, ownerID, botID uuid.UUID, botName string, conversationID uuid.UUID) Tool {
	return &proactiveMessageTool{
		db: db, bus: bus, ownerID: ownerID, botID: botID, botName: botName,
		conversationID: conversationID,
	}
}

func (t *proactiveMessageTool) Name() string { return "send_to_user" }
func (t *proactiveMessageTool) Description() string {
	return "主动给用户发一条私信，用于长任务完成后汇报或异步提醒。会进入 Bot 与用户的私聊。"
}
func (t *proactiveMessageTool) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"content":{"type":"string"}},"required":["content"]}`)
}
func (t *proactiveMessageTool) Execute(ctx context.Context, raw json.RawMessage) (Result, error) {
	var input struct {
		Content string `json:"content"`
	}
	if err := json.Unmarshal(raw, &input); err != nil {
		return Result{Content: "invalid arguments", IsError: true}, nil
	}
	content := strings.TrimSpace(input.Content)
	if content == "" {
		return Result{Content: "content 不能为空", IsError: true}, nil
	}
	bot, err := t.db.GetBot(ctx, store.GetBotParams{ID: t.botID, OwnerID: t.ownerID})
	if err != nil {
		return Result{Content: "读取 Bot 失败: " + err.Error(), IsError: true}, nil
	}
	if proactivityDisabled(bot.Proactivity) {
		return Result{Content: "主动消息已在 Bot 设置中关闭", IsError: true}, nil
	}
	var dm store.Conversation
	if t.conversationID != uuid.Nil {
		current, err := t.db.GetConversation(ctx, t.conversationID)
		if err != nil || current.CreatedBy != t.ownerID ||
			current.DirectBotID == nil || *current.DirectBotID != t.botID {
			return Result{Content: "当前会话不可用于主动消息", IsError: true}, nil
		}
		dm = current
	} else {
		found, err := t.db.GetDirectConversationByBot(ctx, store.GetDirectConversationByBotParams{
			CreatedBy:   t.ownerID,
			DirectBotID: &t.botID,
		})
		if err != nil {
			if !errors.Is(err, pgx.ErrNoRows) {
				return Result{Content: "查找私聊失败: " + err.Error(), IsError: true}, nil
			}
			created, createErr := t.db.CreateConversation(ctx, store.CreateConversationParams{
				DirectBotID: &t.botID,
				Kind:        "direct",
				Title:       t.botName,
				CreatedBy:   t.ownerID,
			})
			if createErr != nil {
				return Result{Content: "创建私聊失败: " + createErr.Error(), IsError: true}, nil
			}
			found = created
		}
		dm = found
	}
	metadata, _ := json.Marshal(map[string]any{"kind": "proactive"})
	tx, err := t.db.Pool.Begin(ctx)
	if err != nil {
		return Result{Content: "发送主动消息失败: " + err.Error(), IsError: true}, nil
	}
	defer tx.Rollback(ctx)
	q := t.db.WithTx(tx)
	msg, err := q.CreateMessage(ctx, store.CreateMessageParams{
		ConversationID: dm.ID,
		SenderType:     "bot",
		SenderBotID:    &t.botID,
		Content:        content,
		Metadata:       metadata,
	})
	if err != nil {
		return Result{Content: "发送主动消息失败: " + err.Error(), IsError: true}, nil
	}
	if _, err := events.Append(ctx, q, dm.ID, nil, nil, events.TypeMessageCreated, map[string]any{"message": msg}); err != nil {
		return Result{Content: "写入主动消息事件失败: " + err.Error(), IsError: true}, nil
	}
	if err := tx.Commit(ctx); err != nil {
		return Result{Content: "提交主动消息失败: " + err.Error(), IsError: true}, nil
	}
	_ = t.bus.Notify(ctx, dm.ID)
	encoded, _ := json.Marshal(map[string]any{
		"conversation_id": dm.ID.String(),
		"message_id":      msg.ID.String(),
	})
	return Result{Content: string(encoded)}, nil
}

type proactiveSettingTool struct {
	db      *store.DB
	ownerID uuid.UUID
	botID   uuid.UUID
}

func NewProactiveSettingTool(db *store.DB, ownerID, botID uuid.UUID) Tool {
	return &proactiveSettingTool{db: db, ownerID: ownerID, botID: botID}
}

func (t *proactiveSettingTool) Name() string { return "set_proactive_messaging" }
func (t *proactiveSettingTool) Description() string {
	return "开启或关闭自己的主动消息。关闭后 send_to_user 会被拒绝。"
}
func (t *proactiveSettingTool) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"enabled":{"type":"boolean"}},"required":["enabled"]}`)
}
func (t *proactiveSettingTool) Execute(ctx context.Context, raw json.RawMessage) (Result, error) {
	var input struct {
		Enabled *bool `json:"enabled"`
	}
	if err := json.Unmarshal(raw, &input); err != nil || input.Enabled == nil {
		return Result{Content: "enabled 必须是布尔值", IsError: true}, nil
	}
	value, _ := json.Marshal(map[string]any{
		"enabled":    *input.Enabled,
		"updated_at": time.Now().UTC().Format(time.RFC3339),
	})
	bot, err := t.db.SetBotProactivity(ctx, store.SetBotProactivityParams{
		ID: t.botID, OwnerID: t.ownerID, Proactivity: value,
	})
	if err != nil {
		return Result{Content: "更新主动消息设置失败: " + err.Error(), IsError: true}, nil
	}
	encoded, _ := json.Marshal(map[string]any{"enabled": *input.Enabled, "bot_id": bot.ID.String()})
	return Result{Content: string(encoded)}, nil
}

func proactivityDisabled(raw json.RawMessage) bool {
	var value struct {
		Enabled *bool `json:"enabled"`
	}
	if len(raw) == 0 || json.Unmarshal(raw, &value) != nil || value.Enabled == nil {
		return false
	}
	return !*value.Enabled
}
