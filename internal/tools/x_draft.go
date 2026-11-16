package tools

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/google/uuid"

	"superbot/internal/events"
	"superbot/internal/store"
)

type xDraftTool struct {
	db      *store.DB
	bus     *events.Bus
	convID  uuid.UUID
	botID   uuid.UUID
	cycleID uuid.UUID
}

func NewXDraftTool(db *store.DB, bus *events.Bus, convID, botID, cycleID uuid.UUID) Tool {
	return &xDraftTool{db: db, bus: bus, convID: convID, botID: botID, cycleID: cycleID}
}

func (t *xDraftTool) Name() string { return "draft_x_post" }
func (t *xDraftTool) Description() string {
	return "在当前对话里生成一条 X（Twitter）帖子草稿，等待用户确认后再发布。不会自动发到 X。"
}
func (t *xDraftTool) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"text":{"type":"string"},"reply_to":{"type":"string"}},"required":["text"]}`)
}
func (t *xDraftTool) Execute(ctx context.Context, raw json.RawMessage) (Result, error) {
	var input struct {
		Text    string `json:"text"`
		ReplyTo string `json:"reply_to"`
	}
	if err := json.Unmarshal(raw, &input); err != nil {
		return Result{Content: "invalid arguments", IsError: true}, nil
	}
	text := strings.TrimSpace(input.Text)
	if text == "" {
		return Result{Content: "text 不能为空", IsError: true}, nil
	}
	if len([]rune(text)) > 280 {
		return Result{Content: "X 帖子不能超过 280 个字符", IsError: true}, nil
	}
	metadata, _ := json.Marshal(map[string]any{
		"kind":     "x_draft",
		"text":     text,
		"reply_to": strings.TrimSpace(input.ReplyTo),
		"posted":   false,
	})
	tx, err := t.db.Pool.Begin(ctx)
	if err != nil {
		return Result{Content: "创建草稿失败: " + err.Error(), IsError: true}, nil
	}
	defer tx.Rollback(ctx)
	q := t.db.WithTx(tx)
	msg, err := q.CreateMessage(ctx, store.CreateMessageParams{
		ConversationID: t.convID,
		CycleID:        &t.cycleID,
		SenderType:     "bot",
		SenderBotID:    &t.botID,
		Content:        text,
		Metadata:       metadata,
	})
	if err != nil {
		return Result{Content: "创建草稿失败: " + err.Error(), IsError: true}, nil
	}
	if _, err := events.Append(ctx, q, t.convID, &t.cycleID, nil, events.TypeMessageCreated, map[string]any{"message": msg}); err != nil {
		return Result{Content: "创建草稿事件失败: " + err.Error(), IsError: true}, nil
	}
	if err := tx.Commit(ctx); err != nil {
		return Result{Content: "提交草稿失败: " + err.Error(), IsError: true}, nil
	}
	_ = t.bus.Notify(ctx, t.convID)
	encoded, _ := json.Marshal(map[string]any{"message_id": msg.ID.String(), "draft": true})
	return Result{Content: string(encoded)}, nil
}
