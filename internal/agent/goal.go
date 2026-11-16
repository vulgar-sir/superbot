package agent

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"superbot/internal/events"
	"superbot/internal/goals"
	"superbot/internal/store"
	"superbot/internal/tools"
)

const messageKindGoalNudge = "goal_nudge"

// goalTool returns the group_goal tool for a member of a team conversation,
// or nil when the conversation has no team.
func (e *Engine) goalTool(ctx context.Context, conv store.Conversation, cycle store.Cycle, bot store.Bot) tools.Tool {
	if conv.Kind != "team" || conv.TeamID == nil {
		return nil
	}
	roster, err := goals.LoadRoster(ctx, e.db, conv)
	if err != nil {
		return nil
	}
	actor := goals.Actor{BotID: bot.ID.String(), Name: bot.Name, IsLead: roster.IsLead(bot.ID)}
	cycleID := cycle.ID
	return tools.NewGroupGoalTool(e.goals, conv.ID, conv.CreatedBy, actor, roster.Members,
		func(ctx context.Context, goal *goals.Goal) {
			_ = e.appendEvent(ctx, conv.ID, &cycleID, nil, events.TypeGoalUpdated, map[string]any{"goal": goal})
		})
}

// goalBlock renders the goal for a member's prompt; "" when there is none.
func (e *Engine) goalBlock(ctx context.Context, conv store.Conversation, bot store.Bot) string {
	if conv.Kind != "team" || conv.TeamID == nil {
		return ""
	}
	goal, _, err := e.goals.Load(ctx, conv.ID)
	if err != nil || goal == nil {
		return ""
	}
	roster, err := goals.LoadRoster(ctx, e.db, conv)
	if err != nil {
		return ""
	}
	return goals.Render(goal, bot.ID.String(), roster.IsLead(bot.ID))
}

// driveGoal wakes the lead when a round ended with the goal still open and
// nobody working on it. It gives up after a couple of fruitless wake-ups and
// stays quiet until a human writes again, so an unattended group cannot spend
// money in a loop. It reports whether a cycle was queued.
func (e *Engine) driveGoal(ctx context.Context, conv store.Conversation) bool {
	if conv.Kind != "team" || conv.TeamID == nil {
		return false
	}
	writeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	goal, row, err := e.goals.Load(writeCtx, conv.ID)
	if err != nil || goal == nil || goal.Status != goals.StatusActive {
		return false
	}
	if spoken, err := e.db.ListUserMessagesAfterOrdinal(writeCtx, store.ListUserMessagesAfterOrdinalParams{
		ConversationID: conv.ID, Ordinal: row.LastNudgeOrdinal,
	}); err == nil && len(spoken) > 0 && goal.Nudges > 0 {
		goal.Nudges = 0
	}
	if !goals.ShouldNudge(goal) {
		return false
	}
	// The lead just asked the user something: waking it again would only
	// produce "still waiting" noise.
	if last, err := e.db.GetLatestBotMessage(writeCtx, conv.ID); err == nil && endsWithQuestion(last.Content) {
		return false
	}
	roster, err := goals.LoadRoster(writeCtx, e.db, conv)
	if err != nil {
		return false
	}
	text := goals.NudgeText(goal)
	meta, _ := json.Marshal(map[string]any{
		"kind":        messageKindGoalNudge,
		"inject":      "true",
		"inject_text": text,
		"for_bot_id":  roster.LeadID.String(),
	})
	msg, err := e.db.CreateMessage(writeCtx, store.CreateMessageParams{
		ConversationID: conv.ID,
		SenderType:     "system",
		Content:        "目标检查",
		Metadata:       meta,
	})
	if err != nil {
		return false
	}
	_ = e.appendEvent(writeCtx, conv.ID, nil, nil, events.TypeMessageCreated, map[string]any{"message": msg})
	if err := e.goals.RecordNudge(writeCtx, conv.ID, goal, msg.Ordinal); err != nil {
		e.logger.Warn("record goal nudge failed", "conversation_id", conv.ID, "error", err)
		return false
	}
	started, err := e.wakeConversation(writeCtx, conv.ID, msg, defaultTeamRounds)
	if err != nil {
		e.logger.Warn("wake lead for goal failed", "conversation_id", conv.ID, "error", err)
	}
	return started
}

func endsWithQuestion(content string) bool {
	trimmed := strings.TrimRight(strings.TrimSpace(content), "”\"'）)】] 　")
	return strings.HasSuffix(trimmed, "？") || strings.HasSuffix(trimmed, "?")
}
