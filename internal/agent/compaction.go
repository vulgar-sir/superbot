package agent

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"

	"superbot/internal/events"
	"superbot/internal/llm"
	"superbot/internal/store"
)

const (
	messageKindCheckpoint = "context_checkpoint"
	messageKindAgentReply = "agent_reply"
	messageKindSubagent   = "subagent_result"
)

// maybeCompact folds older conversation history into a checkpoint summary
// when it has outgrown its share of the prompt budget. It is best effort: any
// failure leaves the conversation untouched and the turn continues with
// plain trimming.
func (e *Engine) maybeCompact(ctx context.Context, conv store.Conversation, cycle store.Cycle, bot store.Bot, runtime *coordinatorRuntime) {
	previous := ""
	afterSeq := int64(0)
	if summary, err := e.db.GetLatestConversationSummary(ctx, conv.ID); err == nil {
		previous = summary.Content
		afterSeq = summary.UpToSeq
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return
	}
	history, err := e.db.ListMessagesAfterOrdinal(ctx, store.ListMessagesAfterOrdinalParams{
		ConversationID: conv.ID,
		Ordinal:        afterSeq,
		Limit:          historyLoadLimit * 3,
	})
	if err != nil {
		return
	}
	filtered := history[:0:0]
	for _, msg := range history {
		if messageKind(msg) == messageKindCheckpoint || strings.TrimSpace(msg.Content) == "" {
			continue
		}
		filtered = append(filtered, msg)
	}
	plan := planCompaction(filtered, promptBudget(runtime.modelRow))
	if plan == nil {
		return
	}

	names := e.speakerNames(ctx, conv, bot)
	transcript := summaryTranscript(plan.Summarize, names, summaryInputMaxRunes)
	var user strings.Builder
	if body := strings.TrimSpace(stripVerbatim(previous)); body != "" {
		user.WriteString("上一份检查点：\n")
		user.WriteString(body)
		user.WriteString("\n\n")
	}
	user.WriteString("需要整理进检查点的较早对话（#数字 是消息序号）：\n")
	user.WriteString(transcript)

	source := "auto"
	body := ""
	for attempt := 0; attempt < 2 && body == ""; attempt++ {
		messages := []llm.Message{
			llm.TextMessage(llm.RoleSystem, summarySystemPrompt()),
			llm.TextMessage(llm.RoleUser, user.String()),
		}
		if attempt == 1 {
			messages = append(messages, llm.TextMessage(llm.RoleUser,
				"上一次输出缺少必需的标题或被截断。请重新输出，必须包含全部 8 个二级标题。"))
		}
		text, _, err := e.callCoordinator(ctx, conv, cycle, runtime, llm.Request{
			Model:     runtime.modelName,
			Messages:  messages,
			MaxTokens: summaryMaxTokens,
		})
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			e.logger.Warn("checkpoint summary call failed", "conversation_id", conv.ID, "error", err)
			break
		}
		if validSummary(text) {
			body = strings.TrimSpace(text)
		}
	}
	if body == "" {
		source = "fallback"
		body = fallbackSummary(previous, plan.Summarize, names)
	}

	count, _ := e.db.CountConversationSummaries(ctx, conv.ID)
	number := int(count) + 1
	last := plan.Summarize[len(plan.Summarize)-1]
	content := composeCheckpoint(number, body, verbatimLines(previous, plan.Summarize, verbatimBudgetTokens))
	created, err := e.db.CreateAutoConversationSummary(ctx, store.CreateAutoConversationSummaryParams{
		ConversationID: conv.ID,
		UpToMessageID:  &last.ID,
		UpToSeq:        last.Ordinal,
		Content:        content,
		Tokens:         int32(EstimateTokens(content)),
		Source:         source,
	})
	if err != nil {
		e.logger.Warn("save checkpoint summary failed", "conversation_id", conv.ID, "error", err)
		return
	}
	e.logger.Info("conversation checkpoint created",
		"conversation_id", conv.ID, "number", number, "source", source,
		"up_to_seq", last.Ordinal, "folded", len(plan.Summarize), "kept", len(plan.Keep))

	meta, _ := json.Marshal(map[string]any{
		"kind":       messageKindCheckpoint,
		"summary_id": created.ID.String(),
		"number":     number,
		"source":     source,
		"up_to_seq":  last.Ordinal,
		"folded":     len(plan.Summarize),
	})
	marker, err := e.db.CreateMessage(ctx, store.CreateMessageParams{
		ConversationID: conv.ID,
		CycleID:        &cycle.ID,
		SenderType:     "system",
		Content:        "已整理较早的对话",
		Metadata:       meta,
	})
	if err != nil {
		return
	}
	_ = e.appendEvent(ctx, conv.ID, &cycle.ID, nil, events.TypeMessageCreated, map[string]any{"message": marker})
}

func (e *Engine) speakerNames(ctx context.Context, conv store.Conversation, bot store.Bot) map[string]string {
	names := map[string]string{bot.ID.String(): bot.Name}
	if bots, err := e.db.ListBots(ctx, conv.CreatedBy); err == nil {
		for _, item := range bots {
			names[item.ID.String()] = item.Name
		}
	}
	return names
}

func messageKind(msg store.Message) string {
	if len(msg.Metadata) == 0 {
		return ""
	}
	var meta struct {
		Kind string `json:"kind"`
	}
	_ = json.Unmarshal(msg.Metadata, &meta)
	return meta.Kind
}

func messageToolsUsed(msg store.Message) []string {
	if len(msg.Metadata) == 0 {
		return nil
	}
	var meta struct {
		Tools []string `json:"tools_used"`
	}
	_ = json.Unmarshal(msg.Metadata, &meta)
	return meta.Tools
}

// messageInjectText returns the text a system card carries for the model, or
// "" when the card is display-only.
func messageInjectText(msg store.Message) string {
	if len(msg.Metadata) == 0 {
		return ""
	}
	var meta struct {
		Inject string `json:"inject"`
		Text   string `json:"inject_text"`
	}
	_ = json.Unmarshal(msg.Metadata, &meta)
	if meta.Inject != "true" {
		return ""
	}
	return strings.TrimSpace(meta.Text)
}

func messageForBot(msg store.Message) string {
	if len(msg.Metadata) == 0 {
		return ""
	}
	var meta struct {
		ForBotID string `json:"for_bot_id"`
	}
	_ = json.Unmarshal(msg.Metadata, &meta)
	return meta.ForBotID
}
