package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"superbot/internal/events"
	"superbot/internal/llm"
	"superbot/internal/store"
)

const (
	defaultTeamRounds = 6
	maxTeamRounds     = 12
	maxConsecutiveBot = 2
)

type teamConfig struct {
	CoordinatorProviderID string `json:"coordinator_provider_id"`
	CoordinatorModel      string `json:"coordinator_model"`
	PrimaryBotID          string `json:"primary_bot_id"`
	MaxRounds             int    `json:"max_rounds"`
}

type routeDecision struct {
	Action      string `json:"action"`
	NextBotID   string `json:"next_bot_id"`
	Instruction string `json:"instruction"`
	Reason      string `json:"reason"`
}

type coordinatorRuntime struct {
	provider    llm.Provider
	providerRow store.Provider
	modelRow    *store.Model
	modelName   string
}

// runTeam drives the M1 roundtable: a coordinator decides who speaks next,
// one bot runs a complete tool-using turn, then the coordinator sees the
// updated transcript and decides again.
func (e *Engine) runPipeline(ctx context.Context, conv store.Conversation, cycle store.Cycle, leaseID uuid.UUID, members []store.Bot) (*runOutcome, error) {
	if len(members) == 0 {
		return nil, fmt.Errorf("pipeline team has no bots")
	}
	return e.runPipelineFrom(ctx, conv, cycle, leaseID, members, 0, "")
}

// runPipelineFrom runs the stages starting at index `start`. An approval
// pause ends a cycle mid-pipeline; the resume cycle re-enters here so the
// remaining stages still run.
func (e *Engine) runPipelineFrom(ctx context.Context, conv store.Conversation, cycle store.Cycle, leaseID uuid.UUID, members []store.Bot, start int, previousOutput string) (*runOutcome, error) {
	outcome := &runOutcome{
		ConversationID: conv.ID,
		CycleID:        cycle.ID,
		RunsFinalized:  true,
		FinalStatus:    "done",
	}
	var lastSeen *uuid.UUID
	for index := start; index < len(members); index++ {
		member := members[index]
		instruction := ""
		if previousOutput != "" {
			instruction = "请基于上一位成员的产出继续完成任务。上一位的最终输出：\n" + truncateForCoordinator(previousOutput, 6000)
		}
		if err := e.appendEvent(ctx, conv.ID, &cycle.ID, nil, events.TypeRouteDecision, map[string]any{
			"round":         index + 1,
			"action":        "speak",
			"next_bot_id":   member.ID.String(),
			"next_bot_name": member.Name,
			"instruction":   instruction,
			"reason":        "流水线顺序执行",
		}); err != nil {
			return outcome, err
		}
		turnOutcome, err := e.runBotTurn(ctx, conv, cycle, leaseID, member, instruction, false)
		if err != nil {
			cause := context.Cause(ctx)
			switch {
			case errors.Is(err, errLeaseLost), errors.Is(cause, errLeaseLost):
				return outcome, errLeaseLost
			case errors.Is(cause, errCanceled):
				if turnOutcome != nil {
					e.finishRun(ctx, turnOutcome, "canceled", "用户停止了本轮协作", "")
				}
			case errors.Is(err, errApprovalPending):
				// The run is paused, not failed; it resumes from its checkpoint.
			default:
				if turnOutcome != nil {
					e.finishRun(ctx, turnOutcome, "failed", "", err.Error())
				}
			}
			return outcome, err
		}
		e.finishRun(ctx, turnOutcome, "succeeded", turnOutcome.Warning, "")
		outcome.RunID = turnOutcome.RunID
		outcome.FinalMessageID = turnOutcome.FinalMessageID
		outcome.FinalText = turnOutcome.FinalText
		outcome.UsageIn += turnOutcome.UsageIn
		outcome.UsageOut += turnOutcome.UsageOut
		outcome.CachedIn += turnOutcome.CachedIn
		outcome.CostMicros += turnOutcome.CostMicros
		outcome.Estimated = outcome.Estimated || turnOutcome.Estimated
		previousOutput = turnOutcome.FinalText
		if turnOutcome.FinalMessageID != nil {
			lastSeen = turnOutcome.FinalMessageID
		}
		state, _ := json.Marshal(map[string]any{
			"mode":              "pipeline",
			"stage":             index + 1,
			"last_bot_id":       member.ID.String(),
			"last_bot_name":     member.Name,
			"last_seen_message": lastSeen,
		})
		n, err := e.db.UpdateCycleProgress(ctx, store.UpdateCycleProgressParams{
			ID:                cycle.ID,
			LeaseID:           &leaseID,
			Round:             int32(index + 1),
			CoordinatorState:  state,
			LastSeenMessageID: lastSeen,
		})
		if err != nil {
			return outcome, fmt.Errorf("update cycle progress: %w", err)
		}
		if n == 0 {
			return outcome, errLeaseLost
		}
	}
	outcome.Warning = "流水线已完成"
	return outcome, nil
}

// runGroup implements Grok-style group chats: every member can read the room,
// but only @-mentioned bots speak. With no mention, the group's primary bot
// answers. A bot can pull in a teammate by mentioning it in its final message.
func (e *Engine) runGroup(
	ctx context.Context,
	conv store.Conversation,
	cycle store.Cycle,
	leaseID uuid.UUID,
	team store.Team,
	members []store.Bot,
) (*runOutcome, error) {
	if len(members) == 0 {
		return nil, fmt.Errorf("group has no bots")
	}
	cfg := parseTeamConfig(team.Config)
	maxTurns := cfg.MaxRounds
	if maxTurns <= 0 {
		maxTurns = defaultTeamRounds
	}
	if maxTurns > maxTeamRounds {
		maxTurns = maxTeamRounds
	}

	trigger, err := e.db.GetMessage(ctx, cycle.TriggerMessageID)
	if err != nil {
		return nil, fmt.Errorf("load trigger message: %w", err)
	}
	queue := mentionedBots(trigger.Content, members, uuid.Nil)
	if len(queue) == 0 {
		// A result delivered back to the group belongs to the member who
		// asked for it, not to whoever happens to be the lead.
		if target := findMember(members, messageForBot(trigger)); target != nil {
			queue = append(queue, *target)
		}
	}
	if len(queue) == 0 {
		lead := findMember(members, cfg.PrimaryBotID)
		if lead == nil {
			if owner, ownerErr := e.db.GetUserByID(ctx, conv.CreatedBy); ownerErr == nil && owner.PrimaryBotID != nil {
				lead = findMember(members, owner.PrimaryBotID.String())
			}
		}
		if lead == nil {
			lead = &members[0]
		}
		queue = append(queue, *lead)
	}

	return e.runGroupQueue(ctx, conv, cycle, leaseID, members, queue, map[uuid.UUID]int{}, 0, maxTurns)
}

// maxGroupSpeaks lets a member answer a teammate who called it back (A asks
// B, B replies, A concludes) while still ruling out an endless ping-pong.
const maxGroupSpeaks = 2

func (e *Engine) runGroupQueue(
	ctx context.Context,
	conv store.Conversation,
	cycle store.Cycle,
	leaseID uuid.UUID,
	members []store.Bot,
	queue []store.Bot,
	spoken map[uuid.UUID]int,
	turnsDone int,
	maxTurns int,
) (*runOutcome, error) {
	outcome := &runOutcome{
		ConversationID: conv.ID,
		CycleID:        cycle.ID,
		RunsFinalized:  true,
		FinalStatus:    "done",
	}
	var lastSeen *uuid.UUID

	for turn := turnsDone; turn < maxTurns && len(queue) > 0; turn++ {
		member := queue[0]
		queue = queue[1:]
		if spoken[member.ID] >= maxGroupSpeaks {
			continue
		}
		spoken[member.ID]++

		instruction := "这是团队群聊。只有被点名、或你有新的结论/进展时才发言；不要重复队友，不要发「收到」之类的空消息。需要队友继续时，在回复中用 @队友名字 点名。"
		if err := e.appendEvent(ctx, conv.ID, &cycle.ID, nil, events.TypeRouteDecision, map[string]any{
			"round":         turn + 1,
			"action":        "speak",
			"next_bot_id":   member.ID.String(),
			"next_bot_name": member.Name,
			"instruction":   instruction,
			"reason":        "群聊 @ 提及或主 Bot 回应",
		}); err != nil {
			return outcome, err
		}

		turnOutcome, err := e.runBotTurn(ctx, conv, cycle, leaseID, member, instruction, false)
		if err != nil {
			cause := context.Cause(ctx)
			switch {
			case errors.Is(err, errLeaseLost), errors.Is(cause, errLeaseLost):
				return outcome, errLeaseLost
			case errors.Is(cause, errCanceled):
				if turnOutcome != nil {
					e.finishRun(ctx, turnOutcome, "canceled", "用户停止了本轮协作", "")
				}
			case errors.Is(err, errApprovalPending):
				// The run is paused, not failed; it resumes from its checkpoint.
			default:
				if turnOutcome != nil {
					e.finishRun(ctx, turnOutcome, "failed", "", err.Error())
				}
			}
			return outcome, err
		}
		e.finishRun(ctx, turnOutcome, "succeeded", turnOutcome.Warning, "")
		outcome.RunID = turnOutcome.RunID
		outcome.FinalMessageID = turnOutcome.FinalMessageID
		outcome.FinalText = turnOutcome.FinalText
		outcome.UsageIn += turnOutcome.UsageIn
		outcome.UsageOut += turnOutcome.UsageOut
		outcome.CachedIn += turnOutcome.CachedIn
		outcome.CostMicros += turnOutcome.CostMicros
		outcome.Estimated = outcome.Estimated || turnOutcome.Estimated
		if turnOutcome.FinalMessageID != nil {
			lastSeen = turnOutcome.FinalMessageID
		}

		queue = enqueueMentioned(queue, mentionedBots(turnOutcome.FinalText, members, member.ID), spoken)

		queued := make([]string, 0, len(queue))
		for _, next := range queue {
			queued = append(queued, next.ID.String())
		}
		spokenByID := make(map[string]int, len(spoken))
		for id, count := range spoken {
			spokenByID[id.String()] = count
		}
		state, _ := json.Marshal(map[string]any{
			"mode":              "group",
			"turn":              turn + 1,
			"last_bot_id":       member.ID.String(),
			"last_bot_name":     member.Name,
			"last_seen_message": lastSeen,
			"queue":             queued,
			"spoken":            spokenByID,
		})
		n, err := e.db.UpdateCycleProgress(ctx, store.UpdateCycleProgressParams{
			ID:                cycle.ID,
			LeaseID:           &leaseID,
			Round:             int32(turn + 1),
			CoordinatorState:  state,
			LastSeenMessageID: lastSeen,
		})
		if err != nil {
			return outcome, fmt.Errorf("update cycle progress: %w", err)
		}
		if n == 0 {
			return outcome, errLeaseLost
		}
	}
	return outcome, nil
}

// runAgent executes the recipient side of a Bot-to-Bot private thread.
func (e *Engine) runAgent(ctx context.Context, conv store.Conversation, cycle store.Cycle, leaseID uuid.UUID) (*runOutcome, error) {
	thread, err := e.db.GetAgentThreadByConversation(ctx, conv.ID)
	if err != nil {
		return nil, fmt.Errorf("load agent thread: %w", err)
	}
	trigger, err := e.db.GetMessage(ctx, cycle.TriggerMessageID)
	if err != nil {
		return nil, fmt.Errorf("load agent trigger: %w", err)
	}
	if trigger.SenderBotID == nil {
		return nil, fmt.Errorf("agent trigger has no sender bot")
	}
	senderID := *trigger.SenderBotID
	var sender, recipient store.Bot
	switch senderID {
	case thread.BotAID:
		sender, err = e.db.GetBot(ctx, store.GetBotParams{ID: thread.BotAID, OwnerID: conv.CreatedBy})
		if err == nil {
			recipient, err = e.db.GetBot(ctx, store.GetBotParams{ID: thread.BotBID, OwnerID: conv.CreatedBy})
		}
	case thread.BotBID:
		sender, err = e.db.GetBot(ctx, store.GetBotParams{ID: thread.BotBID, OwnerID: conv.CreatedBy})
		if err == nil {
			recipient, err = e.db.GetBot(ctx, store.GetBotParams{ID: thread.BotAID, OwnerID: conv.CreatedBy})
		}
	default:
		return nil, fmt.Errorf("agent trigger sender is not part of the thread")
	}
	if err != nil {
		return nil, fmt.Errorf("load agent participants: %w", err)
	}

	outcome := &runOutcome{
		ConversationID: conv.ID,
		CycleID:        cycle.ID,
		RunsFinalized:  true,
		FinalStatus:    "done",
	}
	instruction := fmt.Sprintf(
		"这是你与 %s 的 Bot 私信线程，不是用户对话。请直接处理对方的消息；需要回复时，最终回复会作为私信回复。不要假装用户说过话。",
		sender.Name,
	)
	turnOutcome, err := e.runBotTurn(ctx, conv, cycle, leaseID, recipient, instruction, false)
	if err != nil {
		cause := context.Cause(ctx)
		switch {
		case errors.Is(err, errLeaseLost), errors.Is(cause, errLeaseLost):
			return outcome, errLeaseLost
		case errors.Is(cause, errCanceled):
			if turnOutcome != nil {
				e.finishRun(ctx, turnOutcome, "canceled", "用户停止了本轮协作", "")
			}
		case errors.Is(err, errApprovalPending):
			// The run is paused, not failed; it resumes from its checkpoint.
		default:
			if turnOutcome != nil {
				e.finishRun(ctx, turnOutcome, "failed", "", err.Error())
			}
		}
		return outcome, err
	}
	e.finishRun(ctx, turnOutcome, "succeeded", turnOutcome.Warning, "")
	outcome.RunID = turnOutcome.RunID
	outcome.FinalMessageID = turnOutcome.FinalMessageID
	outcome.FinalText = turnOutcome.FinalText
	outcome.UsageIn = turnOutcome.UsageIn
	outcome.UsageOut = turnOutcome.UsageOut
	outcome.CachedIn = turnOutcome.CachedIn
	outcome.CostMicros = turnOutcome.CostMicros
	outcome.Estimated = turnOutcome.Estimated

	if e.agentChat != nil && turnOutcome.FinalMessageID != nil {
		card, err := e.agentChat.RecordReply(ctx, thread, recipient, sender, turnOutcome.FinalText, *turnOutcome.FinalMessageID)
		if err != nil {
			e.logger.Warn("record agent reply failed", "thread_id", thread.ID, "error", err)
		} else if card != nil {
			e.wakeForResult(ctx, card.ConversationID, *card)
		}
	}
	state, _ := json.Marshal(map[string]any{
		"mode":         "agent",
		"recipient_id": recipient.ID.String(),
		"recipient":    recipient.Name,
		"last_message": turnOutcome.FinalMessageID,
	})
	if _, err := e.db.UpdateCycleProgress(ctx, store.UpdateCycleProgressParams{
		ID:                cycle.ID,
		LeaseID:           &leaseID,
		Round:             1,
		CoordinatorState:  state,
		LastSeenMessageID: turnOutcome.FinalMessageID,
	}); err != nil {
		return outcome, fmt.Errorf("update agent cycle progress: %w", err)
	}
	return outcome, nil
}

func mentionedBots(content string, members []store.Bot, exclude uuid.UUID) []store.Bot {
	out := make([]store.Bot, 0, 2)
	for _, member := range members {
		if member.ID == exclude {
			continue
		}
		if hasMention(content, member.Name) {
			out = append(out, member)
		}
	}
	return out
}

// hasMention matches @name without the @Bot -> @Bot2 prefix bug and without
// matching e-mail addresses. It intentionally accepts a Latin name followed
// by CJK text (e.g. @Alice检查) and a CJK name followed by CJK text.
func hasMention(content, name string) bool {
	name = strings.TrimSpace(name)
	if name == "" {
		return false
	}
	lower := strings.ToLower(content)
	needle := "@" + strings.ToLower(name)
	for offset := 0; offset < len(lower); {
		index := strings.Index(lower[offset:], needle)
		if index < 0 {
			return false
		}
		start := offset + index
		end := start + len(needle)
		if mentionBoundaryBefore(lower, start) && mentionBoundaryAfter(lower, end) {
			return true
		}
		offset = start + 1
	}
	return false
}

func mentionBoundaryBefore(content string, index int) bool {
	if index == 0 {
		return true
	}
	r, _ := utf8.DecodeLastRuneInString(content[:index])
	return !isASCIITokenRune(r)
}

func mentionBoundaryAfter(content string, index int) bool {
	if index >= len(content) {
		return true
	}
	r, _ := utf8.DecodeRuneInString(content[index:])
	return !isASCIITokenRune(r)
}

func isASCIITokenRune(r rune) bool {
	return r < 128 && (r == '_' || r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z')
}

func (e *Engine) runTeam(ctx context.Context, conv store.Conversation, cycle store.Cycle, leaseID uuid.UUID) (*runOutcome, error) {
	if conv.TeamID == nil {
		return nil, fmt.Errorf("team conversation has no team")
	}
	team, err := e.db.GetTeam(ctx, store.GetTeamParams{ID: *conv.TeamID, OwnerID: conv.CreatedBy})
	if err != nil {
		return nil, fmt.Errorf("load team: %w", err)
	}
	teamBots, err := e.db.ListTeamBots(ctx, team.ID)
	if err != nil {
		return nil, fmt.Errorf("load team bots: %w", err)
	}
	if len(teamBots) == 0 {
		return nil, fmt.Errorf("team %q has no bots", team.Name)
	}
	members := make([]store.Bot, 0, len(teamBots))
	for _, tb := range teamBots {
		members = append(members, tb.Bot)
	}

	// A cycle that already recorded member turns was interrupted and
	// reclaimed (a fresh cycle has no progress); pick up after them.
	if outcome, resumed, err := e.resumeTeam(ctx, conv, cycle, leaseID, team, members); resumed {
		return outcome, err
	}
	if team.Mode == "pipeline" {
		return e.runPipeline(ctx, conv, cycle, leaseID, members)
	}
	if team.Mode == "group" {
		return e.runGroup(ctx, conv, cycle, leaseID, team, members)
	}
	return e.runRoundtable(ctx, conv, cycle, leaseID, team, members, cycle.TriggerMessageID, 1)
}

// runRoundtable runs the coordinator loop from startRound. The trigger is
// passed in because a resume cycle is triggered by an approval notice, not by
// the user's request the coordinator should keep steering towards.
func (e *Engine) runRoundtable(ctx context.Context, conv store.Conversation, cycle store.Cycle, leaseID uuid.UUID, team store.Team, members []store.Bot, triggerID uuid.UUID, startRound int) (*runOutcome, error) {
	cfg := parseTeamConfig(team.Config)
	maxRounds := cfg.MaxRounds
	if maxRounds <= 0 {
		maxRounds = defaultTeamRounds
	}
	if maxRounds > maxTeamRounds {
		maxRounds = maxTeamRounds
	}

	coordinator, err := e.resolveCoordinator(ctx, conv, members, cfg)
	if err != nil {
		return nil, err
	}

	trigger, err := e.db.GetMessage(ctx, triggerID)
	if err != nil {
		return nil, fmt.Errorf("load trigger message: %w", err)
	}

	outcome := &runOutcome{
		ConversationID: conv.ID,
		CycleID:        cycle.ID,
		RunsFinalized:  true,
		FinalStatus:    "done",
	}
	var lastBotID uuid.UUID
	var lastSeenMessageID *uuid.UUID
	consecutive := 0

	for round := startRound; round <= maxRounds; round++ {
		recent, err := e.recentMessages(ctx, conv.ID, 20)
		if err != nil {
			return outcome, err
		}
		decision, coordCost, err := e.coordinate(ctx, conv, cycle, team, members, trigger, recent, round, maxRounds, coordinator)
		outcome.CostMicros += coordCost
		if err != nil {
			return outcome, err
		}
		decision = applyMentionOverride(decision, recent, members)

		nextBot := findMember(members, decision.NextBotID)
		if decision.Action == "speak" && nextBot == nil {
			nextBot = nextMember(members, lastBotID)
		}

		if decision.Action == "speak" && nextBot != nil {
			if nextBot.ID == lastBotID {
				consecutive++
			} else {
				consecutive = 0
			}
			if consecutive >= maxConsecutiveBot {
				alternative := nextMember(members, nextBot.ID)
				if alternative == nil || alternative.ID == nextBot.ID {
					outcome.FinalStatus = "done"
					outcome.Warning = "同一 Bot 连续发言达到上限"
					return outcome, nil
				}
				nextBot = alternative
				consecutive = 0
			}
		}

		nextBotID := ""
		nextBotName := ""
		if nextBot != nil {
			nextBotID = nextBot.ID.String()
			nextBotName = nextBot.Name
		}
		routePayload := map[string]any{
			"round":             round,
			"action":            decision.Action,
			"next_bot_id":       nextBotID,
			"next_bot_name":     nextBotName,
			"instruction":       decision.Instruction,
			"reason":            decision.Reason,
			"coordinator_model": coordinator.modelName,
			"cost_micros":       coordCost,
		}
		if err := e.appendEvent(ctx, conv.ID, &cycle.ID, nil, events.TypeRouteDecision, routePayload); err != nil {
			return outcome, err
		}

		switch decision.Action {
		case "finish":
			outcome.FinalStatus = "done"
			return outcome, nil
		case "await_user":
			outcome.FinalStatus = "waiting_user"
			return outcome, nil
		}
		if nextBot == nil {
			outcome.FinalStatus = "done"
			outcome.Warning = "协调者没有选出有效成员"
			return outcome, nil
		}

		turnOutcome, err := e.runBotTurn(ctx, conv, cycle, leaseID, *nextBot, decision.Instruction, team.Mode == "router")
		if err != nil {
			cause := context.Cause(ctx)
			switch {
			case errors.Is(err, errLeaseLost), errors.Is(cause, errLeaseLost):
				return outcome, errLeaseLost
			case errors.Is(cause, errCanceled):
				if turnOutcome != nil {
					e.finishRun(ctx, turnOutcome, "canceled", "用户停止了本轮协作", "")
				}
			case errors.Is(err, errApprovalPending):
				// The run is paused, not failed; it resumes from its checkpoint.
			default:
				if turnOutcome != nil {
					e.finishRun(ctx, turnOutcome, "failed", "", err.Error())
				}
			}
			return outcome, err
		}
		e.finishRun(ctx, turnOutcome, "succeeded", turnOutcome.Warning, "")
		outcome.RunID = turnOutcome.RunID
		outcome.FinalMessageID = turnOutcome.FinalMessageID
		outcome.UsageIn += turnOutcome.UsageIn
		outcome.UsageOut += turnOutcome.UsageOut
		outcome.CachedIn += turnOutcome.CachedIn
		outcome.CostMicros += turnOutcome.CostMicros
		outcome.Estimated = outcome.Estimated || turnOutcome.Estimated
		lastBotID = nextBot.ID
		if turnOutcome.FinalMessageID != nil {
			lastSeenMessageID = turnOutcome.FinalMessageID
		}

		state, _ := json.Marshal(map[string]any{
			"round":             round,
			"last_bot_id":       nextBot.ID.String(),
			"last_bot_name":     nextBot.Name,
			"last_instruction":  decision.Instruction,
			"last_seen_message": lastSeenMessageID,
		})
		n, err := e.db.UpdateCycleProgress(ctx, store.UpdateCycleProgressParams{
			ID:                cycle.ID,
			LeaseID:           &leaseID,
			Round:             int32(round),
			CoordinatorState:  state,
			LastSeenMessageID: lastSeenMessageID,
		})
		if err != nil {
			return outcome, fmt.Errorf("update cycle progress: %w", err)
		}
		if n == 0 {
			return outcome, errLeaseLost
		}
	}

	outcome.FinalStatus = "done"
	outcome.Warning = "达到团队最大协作轮数"
	return outcome, nil
}

func (e *Engine) resolveCoordinator(ctx context.Context, conv store.Conversation, members []store.Bot, cfg teamConfig) (*coordinatorRuntime, error) {
	providerID := strings.TrimSpace(cfg.CoordinatorProviderID)
	modelName := strings.TrimSpace(cfg.CoordinatorModel)
	if providerID == "" || modelName == "" {
		first := members[0]
		if first.ProviderID == nil {
			return nil, fmt.Errorf("team has no provider configured for coordinator")
		}
		providerID = first.ProviderID.String()
		modelName = first.Model
	}
	parsedProviderID, err := uuid.Parse(providerID)
	if err != nil {
		return nil, fmt.Errorf("invalid coordinator provider id: %w", err)
	}
	providerRow, err := e.db.GetProvider(ctx, store.GetProviderParams{ID: parsedProviderID, OwnerID: conv.CreatedBy})
	if err != nil {
		return nil, fmt.Errorf("load coordinator provider: %w", err)
	}
	if !providerRow.Enabled {
		return nil, fmt.Errorf("coordinator provider %q is disabled", providerRow.Name)
	}
	provider, err := e.providers.Build(providerRow, e.cfg.LLMTimeout)
	if err != nil {
		return nil, err
	}
	var modelRow *store.Model
	row, err := e.db.GetModelByName(ctx, store.GetModelByNameParams{ProviderID: providerRow.ID, Name: modelName})
	switch {
	case err == nil:
		modelRow = &row
	case errors.Is(err, pgx.ErrNoRows):
		modelRow = nil
	default:
		return nil, fmt.Errorf("load coordinator model: %w", err)
	}
	return &coordinatorRuntime{
		provider:    provider,
		providerRow: providerRow,
		modelRow:    modelRow,
		modelName:   modelName,
	}, nil
}

func (e *Engine) coordinate(
	ctx context.Context,
	conv store.Conversation,
	cycle store.Cycle,
	team store.Team,
	members []store.Bot,
	trigger store.Message,
	recent []store.Message,
	round, maxRounds int,
	coordinator *coordinatorRuntime,
) (routeDecision, int64, error) {
	messages := buildCoordinatorMessages(team, members, trigger, recent, round, maxRounds)
	totalCost := int64(0)
	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		req := llm.Request{
			Model:     coordinator.modelName,
			Messages:  messages,
			MaxTokens: 512,
			JSONMode:  true,
		}
		text, cost, err := e.callCoordinator(ctx, conv, cycle, coordinator, req)
		totalCost += cost
		if err != nil {
			return routeDecision{}, totalCost, err
		}
		decision, err := parseRouteDecision(text)
		if err == nil {
			return decision, totalCost, nil
		}
		lastErr = err
		messages = append(messages,
			llm.TextMessage(llm.RoleSystem, "你上一次的输出不是合法 JSON，内容如下：\n"+truncateForCoordinator(text, 1000)+"\n请只输出一个 JSON 对象，不要使用 Markdown。"),
		)
	}
	// Never fail a whole cycle because the coordinator produced bad JSON.
	fallback := nextMember(members, uuid.Nil)
	decision := routeDecision{
		Action:      "speak",
		Instruction: "请根据当前对话继续推进任务。",
		Reason:      "协调者输出无法解析，已回退到轮转",
	}
	if fallback != nil {
		decision.NextBotID = fallback.ID.String()
	}
	e.logger.Warn("coordinator output invalid after retries", "cycle_id", cycle.ID, "error", lastErr)
	return decision, totalCost, nil
}

func (e *Engine) callCoordinator(ctx context.Context, conv store.Conversation, cycle store.Cycle, coordinator *coordinatorRuntime, req llm.Request) (string, int64, error) {
	reserved, err := e.reserve(ctx, conv.ID, cycle.ID, coordinator.modelRow, req)
	if err != nil {
		return "", 0, err
	}
	stream, err := coordinator.provider.Stream(ctx, req)
	if err != nil {
		e.release(ctx, conv.ID, cycle.ID, reserved)
		return "", 0, err
	}
	defer stream.Close()

	var text strings.Builder
	var usage *llm.Usage
	var streamErr error
	for event := range stream.Events() {
		switch event.Type {
		case llm.EventTextDelta:
			text.WriteString(event.Text)
		case llm.EventUsage:
			usage = event.Usage
		case llm.EventError:
			streamErr = event.Err
		}
	}
	if streamErr != nil {
		e.release(ctx, conv.ID, cycle.ID, reserved)
		return "", 0, streamErr
	}
	estimated := usage == nil
	if estimated {
		usage = &llm.Usage{
			PromptTokens:     EstimateMessagesTokens(req.Messages),
			CompletionTokens: EstimateTokens(text.String()),
		}
	}
	cost := computeCost(coordinator.modelRow, usage)
	e.settle(ctx, conv.ID, cycle.ID, reserved, cost)
	if _, err := e.db.CreateUsageRecord(ctx, store.CreateUsageRecordParams{
		UserID:           conv.CreatedBy,
		ProviderID:       &coordinator.providerRow.ID,
		Model:            coordinator.modelName,
		CycleID:          &cycle.ID,
		PromptTokens:     int64(usage.PromptTokens),
		CachedTokens:     int64(usage.CachedTokens),
		CompletionTokens: int64(usage.CompletionTokens),
		CostMicros:       cost,
		Estimated:        estimated,
	}); err != nil {
		e.logger.Warn("record coordinator usage failed", "cycle_id", cycle.ID, "error", err)
	}
	return text.String(), cost, nil
}

func buildCoordinatorMessages(team store.Team, members []store.Bot, trigger store.Message, recent []store.Message, round, maxRounds int) []llm.Message {
	var roster strings.Builder
	for _, member := range members {
		fmt.Fprintf(&roster, "- %s (id=%s, role=%s): %s\n", member.Name, member.ID, member.Description, member.Model)
	}
	var transcript strings.Builder
	start := 0
	if len(recent) > 12 {
		start = len(recent) - 12
	}
	for _, msg := range recent[start:] {
		content := strings.TrimSpace(msg.Content)
		if content == "" {
			continue
		}
		switch msg.SenderType {
		case "user":
			fmt.Fprintf(&transcript, "[用户] %s\n", truncateForCoordinator(content, 1500))
		case "bot":
			name := "Bot"
			for _, member := range members {
				if msg.SenderBotID != nil && member.ID == *msg.SenderBotID {
					name = member.Name
					break
				}
			}
			fmt.Fprintf(&transcript, "[%s] %s\n", name, truncateForCoordinator(content, 1500))
		}
	}

	system := `你是多 Bot 团队的协调者，只负责决定下一位发言者，不直接完成任务。
你的输出必须是单个 JSON 对象，不要使用 Markdown，不要输出 JSON 以外的任何文字。
JSON 字段：
- action: "speak" | "finish" | "await_user"
- next_bot_id: 下一位 Bot 的 id（action=speak 时必填，必须是团队成员）
- instruction: 给下一位 Bot 的具体指令
- reason: 一句话理由

规则：
1. 优先让最合适、尚未重复劳动的人发言；如果上一条消息里 @ 了某位成员，优先选择该成员。
2. 任务已经完成、或继续讨论没有新信息时返回 finish。
3. 必须让用户补充信息/确认时返回 await_user。
4. 不要连续让同一个 Bot 发言超过两次。
5. instruction 要具体，包含需要交付的结果。`

	var user strings.Builder
	fmt.Fprintf(&user, "团队：%s\n%s\n", team.Name, team.Description)
	user.WriteString("团队成员：\n")
	user.WriteString(roster.String())
	fmt.Fprintf(&user, "\n当前轮次：%d / %d\n", round, maxRounds)
	fmt.Fprintf(&user, "用户最初的任务：%s\n", truncateForCoordinator(trigger.Content, 2000))
	user.WriteString("\n最近对话：\n")
	user.WriteString(transcript.String())
	user.WriteString("\n请给出本轮决策 JSON。")

	return []llm.Message{
		llm.TextMessage(llm.RoleSystem, system),
		llm.TextMessage(llm.RoleUser, user.String()),
	}
}

func parseRouteDecision(raw string) (routeDecision, error) {
	raw = strings.TrimSpace(raw)
	raw = strings.TrimPrefix(raw, "```json")
	raw = strings.TrimPrefix(raw, "```")
	raw = strings.TrimSuffix(raw, "```")
	start := strings.Index(raw, "{")
	end := strings.LastIndex(raw, "}")
	if start < 0 || end <= start {
		return routeDecision{}, errors.New("no JSON object found")
	}
	var decision routeDecision
	if err := json.Unmarshal([]byte(raw[start:end+1]), &decision); err != nil {
		return routeDecision{}, err
	}
	decision.Action = strings.ToLower(strings.TrimSpace(decision.Action))
	decision.NextBotID = strings.TrimSpace(decision.NextBotID)
	decision.Instruction = strings.TrimSpace(decision.Instruction)
	decision.Reason = strings.TrimSpace(decision.Reason)
	switch decision.Action {
	case "speak", "finish", "await_user":
	case "":
		if decision.NextBotID != "" {
			decision.Action = "speak"
		} else {
			decision.Action = "finish"
		}
	default:
		return routeDecision{}, fmt.Errorf("unknown coordinator action %q", decision.Action)
	}
	return decision, nil
}

func applyMentionOverride(decision routeDecision, recent []store.Message, members []store.Bot) routeDecision {
	if decision.Action != "speak" || len(recent) == 0 {
		return decision
	}
	last := recent[len(recent)-1]
	var matched *store.Bot
	for i := range members {
		if hasMention(last.Content, members[i].Name) {
			if matched != nil {
				return decision // ambiguous mention, keep coordinator choice
			}
			matched = &members[i]
		}
	}
	if matched != nil {
		decision.NextBotID = matched.ID.String()
		if decision.Instruction == "" {
			decision.Instruction = "请回应上一条消息中的 @ 提及。"
		}
	}
	return decision
}

func findMember(members []store.Bot, rawID string) *store.Bot {
	id, err := uuid.Parse(strings.TrimSpace(rawID))
	if err != nil {
		return nil
	}
	for i := range members {
		if members[i].ID == id {
			return &members[i]
		}
	}
	return nil
}

func nextMember(members []store.Bot, after uuid.UUID) *store.Bot {
	if len(members) == 0 {
		return nil
	}
	if after == uuid.Nil {
		return &members[0]
	}
	for i := range members {
		if members[i].ID == after {
			return &members[(i+1)%len(members)]
		}
	}
	return &members[0]
}

func parseTeamConfig(raw json.RawMessage) teamConfig {
	var cfg teamConfig
	_ = json.Unmarshal(raw, &cfg)
	return cfg
}

func (e *Engine) recentMessages(ctx context.Context, conversationID uuid.UUID, limit int32) ([]store.Message, error) {
	items, err := e.db.ListRecentMessages(ctx, store.ListRecentMessagesParams{
		ConversationID: conversationID,
		Limit:          limit,
	})
	if err != nil {
		return nil, fmt.Errorf("load recent messages: %w", err)
	}
	for i, j := 0, len(items)-1; i < j; i, j = i+1, j-1 {
		items[i], items[j] = items[j], items[i]
	}
	return items, nil
}

func truncateForCoordinator(s string, limit int) string {
	runes := []rune(s)
	if len(runes) <= limit {
		return s
	}
	return string(runes[:limit]) + "…"
}

// enqueueMentioned appends newly mentioned members that may still speak and
// are not already waiting.
func enqueueMentioned(queue, mentioned []store.Bot, spoken map[uuid.UUID]int) []store.Bot {
	for _, next := range mentioned {
		if spoken[next.ID] >= maxGroupSpeaks {
			continue
		}
		waiting := false
		for _, queued := range queue {
			if queued.ID == next.ID {
				waiting = true
				break
			}
		}
		if !waiting {
			queue = append(queue, next)
		}
	}
	return queue
}

// continueTeam picks a team cycle back up after one member's turn was
// resumed from an approval checkpoint, so the stages or rounds that were
// still owed are not silently dropped.
func (e *Engine) continueTeam(ctx context.Context, conv store.Conversation, cycle store.Cycle, leaseID uuid.UUID, bot store.Bot, resumed *runOutcome) (*runOutcome, error) {
	resumed.RunsFinalized = true
	if conv.TeamID == nil {
		return resumed, nil
	}
	team, err := e.db.GetTeam(ctx, store.GetTeamParams{ID: *conv.TeamID, OwnerID: conv.CreatedBy})
	if err != nil {
		return resumed, nil
	}
	teamBots, err := e.db.ListTeamBots(ctx, team.ID)
	if err != nil || len(teamBots) == 0 {
		return resumed, nil
	}
	members := make([]store.Bot, 0, len(teamBots))
	position := -1
	for index, tb := range teamBots {
		members = append(members, tb.Bot)
		if tb.Bot.ID == bot.ID {
			position = index
		}
	}
	cfg := parseTeamConfig(team.Config)
	maxRounds := cfg.MaxRounds
	if maxRounds <= 0 {
		maxRounds = defaultTeamRounds
	}
	if maxRounds > maxTeamRounds {
		maxRounds = maxTeamRounds
	}

	var rest *runOutcome
	switch team.Mode {
	case "pipeline":
		if position < 0 || position+1 >= len(members) {
			return resumed, nil
		}
		rest, err = e.runPipelineFrom(ctx, conv, cycle, leaseID, members, position+1, resumed.FinalText)
	case "group":
		queue := enqueueMentioned(nil, mentionedBots(resumed.FinalText, members, bot.ID), map[uuid.UUID]int{bot.ID: 1})
		if len(queue) == 0 {
			return resumed, nil
		}
		rest, err = e.runGroupQueue(ctx, conv, cycle, leaseID, members, queue, map[uuid.UUID]int{bot.ID: 1}, 1, maxRounds)
	default:
		// The coordinator re-reads the transcript, sees the resumed reply and
		// decides whether anyone else still has to speak.
		triggerID := cycle.TriggerMessageID
		if rows, listErr := e.db.ListRecentMessages(ctx, store.ListRecentMessagesParams{ConversationID: conv.ID, Limit: 60}); listErr == nil {
			for _, row := range rows {
				if row.SenderType == "user" {
					triggerID = row.ID
					break
				}
			}
		}
		rest, err = e.runRoundtable(ctx, conv, cycle, leaseID, team, members, triggerID, 2)
	}
	if rest == nil {
		return resumed, err
	}
	rest.UsageIn += resumed.UsageIn
	rest.UsageOut += resumed.UsageOut
	rest.CachedIn += resumed.CachedIn
	rest.CostMicros += resumed.CostMicros
	rest.Estimated = rest.Estimated || resumed.Estimated
	if rest.FinalMessageID == nil {
		rest.RunID = resumed.RunID
		rest.FinalMessageID = resumed.FinalMessageID
		rest.FinalText = resumed.FinalText
	}
	return rest, err
}

// teamProgress is what a team cycle records after each finished member turn.
type teamProgress struct {
	Mode     string         `json:"mode"`
	Stage    int            `json:"stage"`
	Round    int            `json:"round"`
	Turn     int            `json:"turn"`
	LastSeen *uuid.UUID     `json:"last_seen_message"`
	Queue    []string       `json:"queue"`
	Spoken   map[string]int `json:"spoken"`
}

// resumeTeam continues a reclaimed team cycle after the last member turn it
// finished, so members who already answered are not run again. It reports
// false when the cycle recorded no progress, in which case the caller starts
// from the beginning.
func (e *Engine) resumeTeam(ctx context.Context, conv store.Conversation, cycle store.Cycle, leaseID uuid.UUID, team store.Team, members []store.Bot) (*runOutcome, bool, error) {
	if len(cycle.CoordinatorState) == 0 {
		return nil, false, nil
	}
	var progress teamProgress
	if err := json.Unmarshal(cycle.CoordinatorState, &progress); err != nil {
		return nil, false, nil
	}
	finished := &runOutcome{ConversationID: conv.ID, CycleID: cycle.ID, RunsFinalized: true, FinalStatus: "done"}
	cfg := parseTeamConfig(team.Config)
	maxRounds := cfg.MaxRounds
	if maxRounds <= 0 {
		maxRounds = defaultTeamRounds
	}
	if maxRounds > maxTeamRounds {
		maxRounds = maxTeamRounds
	}
	e.logger.Info("resuming team cycle", "cycle_id", cycle.ID, "mode", team.Mode, "attempt", cycle.Attempt,
		"stage", progress.Stage, "round", progress.Round, "turn", progress.Turn)

	switch team.Mode {
	case "pipeline":
		if progress.Stage <= 0 {
			return nil, false, nil
		}
		if progress.Stage >= len(members) {
			return finished, true, nil
		}
		previous := ""
		if progress.LastSeen != nil {
			if msg, err := e.db.GetMessage(ctx, *progress.LastSeen); err == nil {
				previous = msg.Content
			}
		}
		outcome, err := e.runPipelineFrom(ctx, conv, cycle, leaseID, members, progress.Stage, previous)
		return outcome, true, err
	case "group":
		if progress.Turn <= 0 {
			return nil, false, nil
		}
		byID := make(map[string]store.Bot, len(members))
		for _, member := range members {
			byID[member.ID.String()] = member
		}
		queue := make([]store.Bot, 0, len(progress.Queue))
		for _, id := range progress.Queue {
			if member, ok := byID[id]; ok {
				queue = append(queue, member)
			}
		}
		if len(queue) == 0 {
			return finished, true, nil
		}
		spoken := make(map[uuid.UUID]int, len(progress.Spoken))
		for id, count := range progress.Spoken {
			if parsed, err := uuid.Parse(id); err == nil {
				spoken[parsed] = count
			}
		}
		outcome, err := e.runGroupQueue(ctx, conv, cycle, leaseID, members, queue, spoken, progress.Turn, maxRounds)
		return outcome, true, err
	default:
		if progress.Round <= 0 {
			return nil, false, nil
		}
		if progress.Round >= maxRounds {
			return finished, true, nil
		}
		outcome, err := e.runRoundtable(ctx, conv, cycle, leaseID, team, members, cycle.TriggerMessageID, progress.Round+1)
		return outcome, true, err
	}
}
