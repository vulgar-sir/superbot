package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"runtime/debug"
	"strings"
	"time"

	"github.com/google/uuid"

	"superbot/internal/events"
	"superbot/internal/i18n"
	"superbot/internal/llm"
	"superbot/internal/store"
)

// permanentError marks a failure that retrying cannot fix (missing provider,
// exhausted budget, rejected credentials). Such cycles fail at once instead
// of burning three attempts and replaying side effects.
type permanentError struct{ err error }

func (p *permanentError) Error() string { return p.err.Error() }
func (p *permanentError) Unwrap() error { return p.err }

func permanent(err error) error {
	if err == nil {
		return nil
	}
	var already *permanentError
	if errors.As(err, &already) {
		return err
	}
	return &permanentError{err: err}
}

// isPermanent reports whether a cycle error should skip the retry queue.
func isPermanent(err error) bool {
	var marked *permanentError
	if errors.As(err, &marked) {
		return true
	}
	var apiErr *llm.APIError
	if errors.As(err, &apiErr) {
		return !llm.IsRetryable(err)
	}
	return false
}

type progressEnvelope struct {
	Pinned   int           `json:"pinned"`
	Messages []llm.Message `json:"messages"`
}

const resumeNotice = "【系统】上一次执行在这里被中断（服务重启）。上面的工具调用已经执行过，结果如上；" +
	"中断瞬间正在执行的操作可能已部分生效。请先核对已完成的工作，再从中断处继续；" +
	"不要重复已经成功且有副作用的操作（写文件、发消息、提交表单等）。"

// saveProgress records the transcript after a finished tool round. It is best
// effort: losing a checkpoint only means a restart replays more.
func (e *Engine) saveProgress(ctx context.Context, conv store.Conversation, cycle store.Cycle, run store.Run, bot store.Bot, messages []llm.Message, pinned, step int) {
	encoded, err := json.Marshal(progressEnvelope{Pinned: pinned, Messages: messages})
	if err != nil {
		return
	}
	if err := e.db.UpsertProgressCheckpoint(ctx, store.UpsertProgressCheckpointParams{
		OwnerID:        bot.OwnerID,
		RunID:          run.ID,
		CycleID:        cycle.ID,
		ConversationID: conv.ID,
		BotID:          bot.ID,
		Step:           int32(step),
		Messages:       encoded,
	}); err != nil && ctx.Err() == nil {
		e.logger.Warn("save progress checkpoint failed", "run_id", run.ID, "error", err)
	}
}

func (e *Engine) closeProgress(ctx context.Context, cycleID uuid.UUID) {
	writeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	_ = e.db.CloseProgressCheckpoints(writeCtx, cycleID)
}

func (e *Engine) loadProgressSeed(ctx context.Context, cycle store.Cycle, bot store.Bot) *runSeed {
	checkpoint, err := e.db.GetProgressCheckpointForCycle(ctx, cycle.ID)
	if err != nil || checkpoint.BotID != bot.ID {
		return nil
	}
	var envelope progressEnvelope
	if err := json.Unmarshal(checkpoint.Messages, &envelope); err != nil || len(envelope.Messages) == 0 {
		return nil
	}
	e.logger.Info("resuming bot turn from progress checkpoint",
		"cycle_id", cycle.ID, "step", checkpoint.Step, "attempt", cycle.Attempt)
	return &runSeed{
		Messages:     envelope.Messages,
		Pinned:       envelope.Pinned,
		Step:         int(checkpoint.Step),
		Continuation: []llm.Message{llm.TextMessage(llm.RoleUser, resumeNotice)},
	}
}

// injectArrivals appends messages that reached the conversation while the bot
// was working. It returns the new high-water ordinal and the ids delivered.
func (e *Engine) injectArrivals(ctx context.Context, conv store.Conversation, messages []llm.Message, after int64) ([]llm.Message, int64, []string) {
	rows, err := e.db.ListInjectableMessagesAfterOrdinal(ctx, store.ListInjectableMessagesAfterOrdinalParams{
		ConversationID: conv.ID,
		Ordinal:        after,
	})
	if err != nil || len(rows) == 0 {
		return messages, after, nil
	}
	delivered := make([]string, 0, len(rows))
	for _, row := range rows {
		after = row.Ordinal
		content := strings.TrimSpace(row.Content)
		if text := messageInjectText(row); text != "" {
			content = text
		}
		if content == "" {
			continue
		}
		messages = append(messages, llm.TextMessage(llm.RoleUser, arrivalText(row, content)))
		delivered = append(delivered, row.ID.String())
	}
	return messages, after, delivered
}

func arrivalText(row store.Message, content string) string {
	if row.SenderType == "user" {
		if row.Ordinal > 0 {
			return "【用户在你工作期间发来新消息，请据此调整】\n" + content
		}
		return content
	}
	return "【系统通知】\n" + content
}

// noteSeenOrdinal remembers how far into the conversation the running cycle
// has read, so messages that arrive after its last model call still get an
// answer once the cycle ends.
func (e *Engine) noteSeenOrdinal(cycleID uuid.UUID, ordinal int64) {
	e.seenMu.Lock()
	defer e.seenMu.Unlock()
	if ordinal > e.seenOrdinals[cycleID] {
		e.seenOrdinals[cycleID] = ordinal
	}
}

func (e *Engine) takeSeenOrdinal(cycleID uuid.UUID) int64 {
	e.seenMu.Lock()
	defer e.seenMu.Unlock()
	ordinal := e.seenOrdinals[cycleID]
	delete(e.seenOrdinals, cycleID)
	return ordinal
}

// scheduleFollowUp starts a new cycle when messages arrived too late to be
// injected into the one that just finished. Without it an interjection sent
// during the final model call would sit unanswered until the user wrote again.
func (e *Engine) scheduleFollowUp(ctx context.Context, conv store.Conversation, finished store.Cycle, seen int64) bool {
	if seen <= 0 {
		return false
	}
	writeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	rows, err := e.db.ListInjectableMessagesAfterOrdinal(writeCtx, store.ListInjectableMessagesAfterOrdinalParams{
		ConversationID: conv.ID,
		Ordinal:        seen,
	})
	if err != nil || len(rows) == 0 {
		return false
	}
	trigger := rows[len(rows)-1]
	started, err := e.wakeConversation(writeCtx, conv.ID, trigger, finished.MaxRounds)
	if err != nil {
		e.logger.Warn("schedule follow-up cycle failed", "conversation_id", conv.ID, "error", err)
	}
	return started
}

// wakeConversation queues a cycle triggered by an existing message. It
// reports false when the conversation is already busy: the running cycle
// will pick the message up through injectArrivals instead.
func (e *Engine) wakeConversation(ctx context.Context, conversationID uuid.UUID, trigger store.Message, maxRounds int32) (bool, error) {
	return e.chatSvc.Wake(ctx, conversationID, trigger, maxRounds, "follow_up")
}

// maxAutoWakes bounds how many times results from other bots may restart a
// conversation since the user last spoke. Past it the results stay visible
// but nothing runs until a human writes again.
const maxAutoWakes = 8

// wakeForResult restarts the conversation a result was delivered to, so the
// bot that asked continues on its own instead of waiting for the user.
func (e *Engine) wakeForResult(ctx context.Context, conversationID uuid.UUID, result store.Message) {
	writeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	count, err := e.db.CountInjectedSinceLastUser(writeCtx, conversationID)
	if err != nil {
		return
	}
	if count > maxAutoWakes {
		e.logger.Warn("auto-wake limit reached; waiting for the user", "conversation_id", conversationID, "count", count)
		return
	}
	conv, err := e.db.GetConversation(writeCtx, conversationID)
	if err != nil {
		return
	}
	if conv.Kind != "direct" && conv.Kind != "team" {
		return
	}
	maxRounds := int32(1)
	if conv.Kind == "team" {
		maxRounds = defaultTeamRounds
	}
	if _, err := e.wakeConversation(writeCtx, conversationID, result, maxRounds); err != nil {
		e.logger.Warn("wake conversation for result failed", "conversation_id", conversationID, "error", err)
	}
}

// resumeDeferredApproval restarts a run whose approval was decided while the
// conversation was busy with another cycle. It reports whether a resume
// cycle was queued.
func (e *Engine) resumeDeferredApproval(ctx context.Context, conv store.Conversation) bool {
	writeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	checkpoint, err := e.db.GetWaitingCheckpointForConversation(writeCtx, conv.ID)
	if err != nil || checkpoint.ApprovalID == nil {
		return false
	}
	approval, err := e.db.GetToolApproval(writeCtx, *checkpoint.ApprovalID)
	if err != nil || approval.Status == "pending" {
		return false
	}
	language := i18n.AccountLanguage(writeCtx, e.db, checkpoint.OwnerID)
	notice := i18n.Text(language, "工具审批已通过：", "Tool approval granted: ") + approval.ToolName
	if approval.Status != "allowed" {
		notice = i18n.Text(language, "工具审批未通过：", "Tool approval not granted: ") + approval.ToolName
	}
	meta, _ := json.Marshal(map[string]any{"approval_id": approval.ID.String(), "tool_name": approval.ToolName})
	started, err := e.chatSvc.ResumeAfterApproval(writeCtx, checkpoint, notice, meta)
	if err != nil {
		e.logger.Warn("deferred approval resume failed", "conversation_id", conv.ID, "error", err)
	}
	return started
}

// triggerHop reads how deep in a bot-to-bot chain the current turn is.
func (e *Engine) triggerHop(ctx context.Context, conv store.Conversation, cycle store.Cycle) int {
	if conv.Kind != "agent" {
		return 0
	}
	trigger, err := e.db.GetMessage(ctx, cycle.TriggerMessageID)
	if err != nil || len(trigger.Metadata) == 0 {
		return 0
	}
	var meta struct {
		Hop int `json:"hop"`
	}
	_ = json.Unmarshal(trigger.Metadata, &meta)
	return meta.Hop
}

// recoverCycle turns a panic inside a cycle into a failed attempt so one bad
// code path cannot take the whole worker process down.
func (e *Engine) recoverCycle(ctx context.Context, cycle store.Cycle, leaseID uuid.UUID, recovered any) error {
	err := fmt.Errorf("内部错误（panic）：%v", recovered)
	e.logger.Error("cycle panicked", "cycle_id", cycle.ID, "panic", recovered, "stack", string(debug.Stack()))
	e.emitError(ctx, cycle.ConversationID, &cycle.ID, nil, err)
	e.failCycle(ctx, cycle, leaseID, err)
	return err
}

const (
	deltaEventMaxAge  = 7 * 24 * time.Hour
	deltaEventKeepNew = 2000
	housekeepingEvery = time.Hour
)

// Housekeeping runs the slow maintenance a worker owns: failing cycles whose
// workers keep dying, and pruning streaming deltas that only mattered live.
func (e *Engine) Housekeeping(ctx context.Context) {
	if rows, err := e.db.FailExhaustedCycles(ctx); err == nil {
		for _, row := range rows {
			e.logger.Warn("cycle failed after repeated worker loss", "cycle_id", row.ID)
			cycleID := row.ID
			_ = e.appendEvent(ctx, row.ConversationID, &cycleID, nil, events.TypeCycleUpdated, map[string]any{
				"cycle_id": row.ID.String(),
				"status":   "failed",
				"error":    "worker 多次中断，已停止自动重试",
			})
		}
	}
	e.reviewMemories(ctx)
	e.seenMu.Lock()
	due := time.Since(e.lastPrune) >= housekeepingEvery
	if due {
		e.lastPrune = time.Now()
	}
	e.seenMu.Unlock()
	if !due {
		return
	}
	pruned, err := e.db.PruneDeltaEvents(ctx, store.PruneDeltaEventsParams{
		Types:    []string{events.TypeTextDelta, events.TypeReasoningDelta},
		Before:   time.Now().Add(-deltaEventMaxAge),
		KeepLast: deltaEventKeepNew,
	})
	if err != nil {
		e.logger.Warn("prune delta events failed", "error", err)
		return
	}
	if pruned > 0 {
		e.logger.Info("pruned streaming delta events", "rows", pruned)
	}
}

// EstimateToolDefsTokens sizes the tool schema block, which counts against
// the context window but is not part of the message list.
func EstimateToolDefsTokens(defs []llm.ToolDef) int {
	total := 0
	for _, def := range defs {
		total += 8 + EstimateTokens(def.Name) + EstimateTokens(def.Description) + EstimateTokens(string(def.Parameters))
	}
	return total
}
