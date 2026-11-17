package agent

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"superbot/internal/chat"
	"superbot/internal/memory"
	"superbot/internal/store"
	"superbot/internal/tools"
)

// memoryContext is who the memory tools of a turn act for. Saves are checked
// against similar memories by the bot's own model, billed to the
// conversation like the automatic review.
func (e *Engine) memoryContext(ctx context.Context, bot store.Bot, conv store.Conversation, cycle store.Cycle, teamID *uuid.UUID) tools.MemoryContext {
	return tools.MemoryContext{
		OwnerID: bot.OwnerID, BotID: bot.ID, TeamID: teamID,
		UserID: e.turnUserID(ctx, conv, cycle),
		Judge: memory.NewExtractorJudge(&memoryExtractor{engine: e}, memory.ExtractRequest{
			BotID: bot.ID, OwnerID: bot.OwnerID, UserID: conv.CreatedBy, ConversationID: conv.ID,
		}),
	}
}

// turnUserID is the person a turn talks with: the user of a direct
// conversation, or in a group the sender of the message that started the
// cycle. Nil when no person is known (routines, messages from before senders
// were recorded).
func (e *Engine) turnUserID(ctx context.Context, conv store.Conversation, cycle store.Cycle) *uuid.UUID {
	if conv.Kind != "team" {
		return conv.DirectUserID
	}
	msg, err := e.db.GetMessage(ctx, cycle.TriggerMessageID)
	if err != nil || msg.SenderType != "user" {
		return nil
	}
	return chat.SenderUserID(msg.Metadata)
}

// recallUserID is whose personal memories a recall for msg may include: the
// conversation's user, or in a group the person who wrote msg.
func recallUserID(conv store.Conversation, msg store.Message) *uuid.UUID {
	if conv.Kind != "team" {
		return conv.DirectUserID
	}
	return chat.SenderUserID(msg.Metadata)
}

// memoryRecallBlock renders recalled memories for the prompt. The date is
// when a memory was last written or confirmed, so the model can prefer the
// newer of two memories that disagree.
func memoryRecallBlock(items []store.Memory) string {
	if len(items) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("## 相关记忆\n以下是从长期记忆中自动召回的相关事实，日期为最后确认时间；它们可能过时，以当前对话与工具结果为准，同一件事有多条时以较新的为准。\n")
	for _, item := range items {
		fmt.Fprintf(&sb, "- [%s，%s] %s\n", item.ScopeType, item.UpdatedAt.Format("2006-01-02"), truncateForCoordinator(item.Content, 400))
	}
	return sb.String()
}
