package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"superbot/internal/events"
	"superbot/internal/store"
)

type pollTool struct {
	db      *store.DB
	bus     *events.Bus
	convID  uuid.UUID
	botID   uuid.UUID
	cycleID uuid.UUID
}

func NewPollTool(db *store.DB, bus *events.Bus, convID, botID, cycleID uuid.UUID) Tool {
	return &pollTool{db: db, bus: bus, convID: convID, botID: botID, cycleID: cycleID}
}

func (t *pollTool) Name() string { return "send_poll" }
func (t *pollTool) Description() string {
	return "在当前对话里向用户发送一个投票。选项 2–10 个，可设置多选。"
}
func (t *pollTool) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"question":{"type":"string"},"options":{"type":"array","items":{"type":"string"}},"multiple":{"type":"boolean"}},"required":["question","options"]}`)
}
func (t *pollTool) Execute(ctx context.Context, raw json.RawMessage) (Result, error) {
	var input struct {
		Question string   `json:"question"`
		Options  []string `json:"options"`
		Multiple bool     `json:"multiple"`
	}
	if err := json.Unmarshal(raw, &input); err != nil {
		return Result{Content: "invalid arguments", IsError: true}, nil
	}
	question := strings.TrimSpace(input.Question)
	if question == "" {
		return Result{Content: "question 不能为空", IsError: true}, nil
	}
	options := make([]map[string]string, 0, len(input.Options))
	seen := make(map[string]bool)
	for _, rawOption := range input.Options {
		text := strings.TrimSpace(rawOption)
		if text == "" || seen[text] {
			continue
		}
		seen[text] = true
		options = append(options, map[string]string{
			"id":   fmt.Sprintf("opt-%d", len(options)+1),
			"text": text,
		})
	}
	if len(options) < 2 || len(options) > 10 {
		return Result{Content: "options 需要 2–10 个非空且不重复的选项", IsError: true}, nil
	}
	metadata, _ := json.Marshal(map[string]any{
		"kind": "poll",
		"poll": map[string]any{
			"question": question,
			"multiple": input.Multiple,
			"options":  options,
			"votes":    map[string]int{},
			"my_votes": []string{},
		},
	})

	tx, err := t.db.Pool.Begin(ctx)
	if err != nil {
		return Result{Content: "创建投票失败: " + err.Error(), IsError: true}, nil
	}
	defer tx.Rollback(ctx)
	q := t.db.WithTx(tx)
	msg, err := q.CreateMessage(ctx, store.CreateMessageParams{
		ConversationID: t.convID,
		CycleID:        &t.cycleID,
		SenderType:     "bot",
		SenderBotID:    &t.botID,
		Content:        question,
		Metadata:       metadata,
	})
	if err != nil {
		return Result{Content: "创建投票失败: " + err.Error(), IsError: true}, nil
	}
	if _, err := events.Append(ctx, q, t.convID, &t.cycleID, nil, events.TypeMessageCreated, map[string]any{"message": msg}); err != nil {
		return Result{Content: "创建投票事件失败: " + err.Error(), IsError: true}, nil
	}
	if err := tx.Commit(ctx); err != nil {
		return Result{Content: "提交投票失败: " + err.Error(), IsError: true}, nil
	}
	if err := t.bus.Notify(ctx, t.convID); err != nil {
		// The poll is persisted; a missed notification is recovered by the
		// next SSE poll tick.
		_ = err
	}
	encoded, _ := json.Marshal(map[string]any{"message_id": msg.ID.String(), "question": question})
	return Result{Content: string(encoded)}, nil
}
