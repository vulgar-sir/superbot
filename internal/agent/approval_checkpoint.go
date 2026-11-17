package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"superbot/internal/events"
	"superbot/internal/llm"
	"superbot/internal/store"
)

func (e *Engine) pauseForApproval(
	ctx context.Context,
	conv store.Conversation,
	cycle store.Cycle,
	run store.Run,
	bot store.Bot,
	calls []llm.ToolCall,
	messages []llm.Message,
	step int,
) error {
	if len(calls) == 0 {
		return fmt.Errorf("no pending tool calls")
	}
	call := calls[0]
	maskedArgs := maskApprovalArgs(call.Arguments)
	approval, err := e.db.CreateToolApproval(ctx, store.CreateToolApprovalParams{
		OwnerID:        bot.OwnerID,
		ConversationID: conv.ID,
		RunID:          run.ID,
		CallID:         call.ID,
		ToolName:       call.Name,
		Args:           maskedArgs,
	})
	if err != nil {
		return err
	}
	encodedMessages, err := json.Marshal(messages)
	if err != nil {
		return err
	}
	encodedCall, err := json.Marshal(calls)
	if err != nil {
		return err
	}
	checkpoint, err := e.db.CreateRunCheckpoint(ctx, store.CreateRunCheckpointParams{
		OwnerID:        bot.OwnerID,
		RunID:          run.ID,
		CycleID:        cycle.ID,
		ConversationID: conv.ID,
		BotID:          bot.ID,
		Step:           int32(step),
		Messages:       encodedMessages,
		PendingCall:    encodedCall,
		ApprovalID:     &approval.ID,
	})
	if err != nil {
		return err
	}
	_ = e.db.SetApprovalCheckpoint(ctx, store.SetApprovalCheckpointParams{
		ID: approval.ID, CheckpointID: &checkpoint.ID,
	})
	meta, _ := json.Marshal(map[string]any{
		"approval_id":     approval.ID.String(),
		"conversation_id": conv.ID.String(),
		"run_id":          run.ID.String(),
		"tool_name":       call.Name,
		"bot_id":          bot.ID.String(),
	})
	_, _ = e.db.CreateNotification(ctx, store.CreateNotificationParams{
		OwnerID:      bot.OwnerID,
		Kind:         "tool_approval",
		Title:        "需要批准：" + call.Name,
		Body:         bot.Name + " 请求执行工具",
		ResourceType: "tool_approval",
		ResourceID:   approval.ID.String(),
		Metadata:     meta,
	})
	_ = e.appendEvent(ctx, conv.ID, &cycle.ID, &run.ID, events.TypeToolApproval, map[string]any{
		"approval_id":     approval.ID.String(),
		"conversation_id": conv.ID.String(),
		"run_id":          run.ID.String(),
		"call_id":         call.ID,
		"name":            call.Name,
		"tool_name":       call.Name,
		"arguments":       maskedArgs,
	})
	return nil
}

func (e *Engine) resumeFromCheckpoint(ctx context.Context, conv store.Conversation, cycle store.Cycle, leaseID uuid.UUID) (*runOutcome, error) {
	if cycle.ResumeCheckpointID == nil {
		return nil, fmt.Errorf("cycle has no resume checkpoint")
	}
	checkpoint, err := e.db.GetRunCheckpointByID(ctx, *cycle.ResumeCheckpointID)
	if err != nil {
		return nil, fmt.Errorf("load checkpoint: %w", err)
	}
	if checkpoint.Status != "waiting" {
		// A team resume consumes its checkpoint before running the stages
		// that were still owed; if that cycle is reclaimed, the recorded
		// team progress takes over from there.
		if conv.Kind == "team" && checkpoint.Status == "consumed" {
			return e.runTeam(ctx, conv, cycle, leaseID)
		}
		return nil, fmt.Errorf("checkpoint is already %s", checkpoint.Status)
	}
	decision := "denied"
	if checkpoint.ApprovalID != nil {
		if approval, err := e.db.GetToolApproval(ctx, *checkpoint.ApprovalID); err == nil {
			decision = approval.Status
		}
	}
	var messages []llm.Message
	if err := json.Unmarshal(checkpoint.Messages, &messages); err != nil {
		return nil, fmt.Errorf("decode checkpoint messages: %w", err)
	}
	var calls []llm.ToolCall
	if err := json.Unmarshal(checkpoint.PendingCall, &calls); err != nil || len(calls) == 0 {
		return nil, fmt.Errorf("decode checkpoint calls: %w", err)
	}
	bot, err := e.db.GetBot(ctx, store.GetBotParams{ID: checkpoint.BotID, OwnerID: checkpoint.OwnerID})
	if err != nil {
		return nil, fmt.Errorf("load checkpoint bot: %w", err)
	}
	var continuation []llm.Message
	if rows, err := e.db.ListUserMessagesAfterTime(ctx, store.ListUserMessagesAfterTimeParams{
		ConversationID: conv.ID, CreatedAt: checkpoint.CreatedAt,
	}); err == nil {
		for _, row := range rows {
			continuation = append(continuation, llm.TextMessage(llm.RoleUser, row.Content))
		}
	}
	outcome, err := e.runBotTurnSeeded(ctx, conv, cycle, leaseID, bot, "", false, &runSeed{
		Messages:         messages,
		Step:             int(checkpoint.Step),
		PendingCalls:     calls,
		ApprovalDecision: decision,
		Continuation:     continuation,
		PriorRunID:       &checkpoint.RunID,
	})
	if err != nil {
		if errors.Is(err, errApprovalPending) {
			// The run paused again under a new checkpoint; this one must
			// not be resumed a second time.
			_ = e.db.MarkRunCheckpointConsumed(context.WithoutCancel(ctx), checkpoint.ID)
		}
		if conv.Kind == "subagent" && !errors.Is(err, errApprovalPending) && !errors.Is(err, errLeaseLost) {
			e.failSubagentResume(ctx, conv, err.Error())
		}
		return outcome, err
	}
	if conv.Kind == "subagent" {
		e.completeSubagentResume(ctx, conv, outcome)
	}
	_ = e.db.MarkRunCheckpointConsumed(ctx, checkpoint.ID)
	if conv.Kind == "team" {
		e.finishRun(ctx, outcome, "succeeded", outcome.Warning, "")
		return e.continueTeam(ctx, conv, cycle, leaseID, bot, outcome)
	}
	return outcome, nil
}

// pausedRunCarry returns what a run that paused for approval had already
// written, and the chain of paused runs the reply has passed through so far
// (that run last). A resumed run streams its predecessor's text first, so
// the latest run's text already holds every earlier one's.
func (e *Engine) pausedRunCarry(ctx context.Context, runID uuid.UUID) (string, []string) {
	var chain []string
	rows, err := e.db.ListRunEvents(ctx, &runID)
	if err != nil {
		e.logger.Warn("load paused run events failed", "run_id", runID, "error", err)
		return "", []string{runID.String()}
	}
	var text strings.Builder
	for _, row := range rows {
		switch row.Type {
		case events.TypeTextDelta:
			var payload struct {
				Text string `json:"text"`
			}
			if json.Unmarshal(row.Payload, &payload) == nil {
				text.WriteString(payload.Text)
			}
		case events.TypeStatus:
			var payload struct {
				ResumedFrom []string `json:"resumed_from"`
			}
			if json.Unmarshal(row.Payload, &payload) == nil && len(payload.ResumedFrom) > 0 {
				chain = payload.ResumedFrom
			}
		}
	}
	chain = append(append([]string(nil), chain...), runID.String())
	return llm.StripArtifacts(strings.TrimSpace(text.String())), chain
}
