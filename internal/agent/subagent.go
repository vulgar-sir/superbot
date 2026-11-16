package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"superbot/internal/events"
	"superbot/internal/i18n"
	"superbot/internal/store"
)

func (e *Engine) runSubagent(ctx context.Context, conv store.Conversation, cycle store.Cycle, leaseID uuid.UUID) (*runOutcome, error) {
	task, err := e.db.GetSubagentTaskByChildCycle(ctx, &cycle.ID)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, fmt.Errorf("subagent task not found for cycle %s", cycle.ID)
		}
		return nil, fmt.Errorf("load subagent task: %w", err)
	}
	_, _ = e.db.MarkSubagentRunning(ctx, task.ID)
	outcome := &runOutcome{
		ConversationID: conv.ID,
		CycleID:        cycle.ID,
		RunsFinalized:  true,
		FinalStatus:    "done",
	}
	if conv.DirectBotID == nil {
		err := fmt.Errorf("subagent conversation has no bot")
		e.finishSubagentTask(ctx, task, "failed", "", err.Error())
		return outcome, err
	}
	bot, err := e.db.GetBot(ctx, store.GetBotParams{ID: *conv.DirectBotID, OwnerID: conv.CreatedBy})
	if err != nil {
		e.finishSubagentTask(ctx, task, "failed", "", err.Error())
		return outcome, err
	}
	instruction := "你是被父 Bot 派生的隔离子代理。独立完成任务，只返回可复用的结果摘要和工作产物路径；不要假装与用户对话。任务目标：\n" + task.Objective
	turnOutcome, err := e.runBotTurn(ctx, conv, cycle, leaseID, bot, instruction, true)
	if err != nil {
		cause := context.Cause(ctx)
		switch {
		case err == errLeaseLost || cause == errLeaseLost:
			return outcome, err
		case cause == errCanceled:
			e.finishSubagentTask(ctx, task, "canceled", "", "父任务已取消")
			return outcome, err
		case errors.Is(err, errApprovalPending):
			// ProcessCycle marks the child cycle waiting_approval; the task
			// remains running until the resume cycle finishes.
			return outcome, err
		default:
			e.finishSubagentTask(ctx, task, "failed", "", err.Error())
			return outcome, err
		}
	}
	e.finishRun(ctx, turnOutcome, "succeeded", turnOutcome.Warning, "")
	outcome.RunID = turnOutcome.RunID
	outcome.FinalMessageID = turnOutcome.FinalMessageID
	outcome.FinalText = turnOutcome.FinalText
	outcome.UsageIn = turnOutcome.UsageIn
	outcome.UsageOut = turnOutcome.UsageOut
	outcome.CachedIn = turnOutcome.CachedIn
	outcome.Estimated = turnOutcome.Estimated
	outcome.CostMicros = turnOutcome.CostMicros
	summary := truncateForCoordinator(turnOutcome.FinalText, 4000)
	e.finishSubagentTask(ctx, task, "succeeded", summary, "")
	e.announceSubagent(ctx, task, summary)
	return outcome, nil
}

func (e *Engine) finishSubagentTask(ctx context.Context, task store.SubagentTask, status, summary, errMsg string) {
	_, _ = e.db.FinishSubagentTask(ctx, store.FinishSubagentTaskParams{
		ID:            task.ID,
		Status:        status,
		ResultSummary: summary,
		Error:         errMsg,
	})
	meta, _ := json.Marshal(map[string]any{
		"subagent_task_id":    task.ID.String(),
		"parent_conversation": task.ParentConversationID.String(),
		"child_conversation":  task.ChildConversationID.String(),
	})
	language := i18n.AccountLanguage(ctx, e.db, task.OwnerID)
	title := i18n.Text(language, "子代理已完成", "Subagent finished")
	if status == "failed" {
		title = i18n.Text(language, "子代理执行失败", "Subagent failed")
	} else if status == "canceled" {
		title = i18n.Text(language, "子代理已取消", "Subagent canceled")
	}
	body := summary
	if body == "" {
		body = errMsg
	}
	_, _ = e.db.CreateNotification(ctx, store.CreateNotificationParams{
		OwnerID:      task.OwnerID,
		Kind:         "subagent",
		Title:        title,
		Body:         truncateForCoordinator(body, 500),
		ResourceType: "subagent",
		ResourceID:   task.ID.String(),
		Metadata:     meta,
	})
}

func (e *Engine) announceSubagent(ctx context.Context, task store.SubagentTask, summary string) {
	language := i18n.AccountLanguage(ctx, e.db, task.OwnerID)
	content := i18n.Text(language, "子代理已完成：", "Subagent finished: ") + truncateForCoordinator(summary, 1200)
	meta, _ := json.Marshal(map[string]any{
		"subagent_task_id": task.ID.String(),
		"kind":             messageKindSubagent,
		"inject":           "true",
		"inject_text": "你派出的子代理已完成任务「" + truncateForCoordinator(task.Objective, 200) +
			"」，结果如下：\n" + truncateForCoordinator(summary, 4000),
		"for_bot_id": task.ParentBotID.String(),
	})
	msg, err := e.db.CreateMessage(ctx, store.CreateMessageParams{
		ConversationID: task.ParentConversationID,
		SenderType:     "system",
		Content:        content,
		Metadata:       meta,
	})
	if err != nil {
		e.logger.Warn("announce subagent failed", "task_id", task.ID, "error", err)
		return
	}
	if _, err := events.Append(ctx, e.db.Queries, task.ParentConversationID, nil, nil, events.TypeMessageCreated, map[string]any{"message": msg}); err == nil {
		_ = e.bus.Notify(ctx, task.ParentConversationID)
	}
	e.wakeForResult(ctx, task.ParentConversationID, msg)
}

func (e *Engine) completeSubagentResume(ctx context.Context, conv store.Conversation, outcome *runOutcome) {
	task, err := e.db.GetSubagentTaskByChildConversation(ctx, conv.ID)
	if err != nil {
		e.logger.Warn("load subagent task for resume failed", "conversation_id", conv.ID, "error", err)
		return
	}
	summary := truncateForCoordinator(outcome.FinalText, 4000)
	e.finishSubagentTask(ctx, task, "succeeded", summary, "")
	e.announceSubagent(ctx, task, summary)
}

func (e *Engine) failSubagentResume(ctx context.Context, conv store.Conversation, errMsg string) {
	task, err := e.db.GetSubagentTaskByChildConversation(ctx, conv.ID)
	if err != nil {
		return
	}
	e.finishSubagentTask(ctx, task, "failed", "", errMsg)
}
