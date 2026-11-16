package routines

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/robfig/cron/v3"

	"superbot/internal/events"
	"superbot/internal/store"
)

type Service struct {
	db     *store.DB
	bus    *events.Bus
	logger *slog.Logger
	parser cron.Parser
}

func NewService(db *store.DB, bus *events.Bus, logger *slog.Logger) *Service {
	return &Service{
		db:     db,
		bus:    bus,
		logger: logger,
		parser: cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor),
	}
}

func (s *Service) nextRun(expr string, from time.Time) (*time.Time, error) {
	schedule, err := s.parser.Parse(strings.TrimSpace(expr))
	if err != nil {
		return nil, fmt.Errorf("invalid cron expression: %w", err)
	}
	next := schedule.Next(from)
	if next.IsZero() {
		return nil, fmt.Errorf("cron expression never runs")
	}
	return &next, nil
}

// accountLocation is the time zone an account's schedules are read in: the
// account's own setting (Settings → time zone), else the platform default.
// "每天 9:00" then means 9:00 for the person who set it up, not for the server.
func (s *Service) accountLocation(ctx context.Context, accountID uuid.UUID) *time.Location {
	if s.db != nil {
		if raw, err := s.db.GetSetting(ctx, "timezone:"+accountID.String()); err == nil {
			var prefs struct {
				Timezone string `json:"timezone"`
			}
			if json.Unmarshal(raw, &prefs) == nil && strings.TrimSpace(prefs.Timezone) != "" {
				if loc, err := time.LoadLocation(strings.TrimSpace(prefs.Timezone)); err == nil {
					return loc
				}
			}
		}
	}
	if loc, err := time.LoadLocation(defaultBotTimezone); err == nil {
		return loc
	}
	return time.Local
}

func (s *Service) teamLocation(ctx context.Context, teamID uuid.UUID) *time.Location {
	if team, err := s.db.GetTeamByID(ctx, teamID); err == nil {
		return s.accountLocation(ctx, team.OwnerID)
	}
	return s.accountLocation(ctx, uuid.Nil)
}

func (s *Service) Create(ctx context.Context, ownerID, teamID uuid.UUID, name, cronExpr, prompt string, enabled bool) (store.Routine, error) {
	if _, err := s.db.GetTeam(ctx, store.GetTeamParams{ID: teamID, OwnerID: ownerID}); err != nil {
		return store.Routine{}, fmt.Errorf("team not found")
	}
	name = strings.TrimSpace(name)
	prompt = strings.TrimSpace(prompt)
	if name == "" || prompt == "" {
		return store.Routine{}, fmt.Errorf("name and prompt are required")
	}
	var next *time.Time
	if enabled {
		var err error
		next, err = s.nextRun(cronExpr, time.Now().In(s.accountLocation(ctx, ownerID)))
		if err != nil {
			return store.Routine{}, err
		}
	}
	return s.db.CreateRoutine(ctx, store.CreateRoutineParams{
		TeamID:    teamID,
		Name:      name,
		Cron:      strings.TrimSpace(cronExpr),
		Prompt:    prompt,
		Enabled:   enabled,
		NextRunAt: next,
	})
}

func (s *Service) Update(ctx context.Context, ownerID, routineID uuid.UUID, name, cronExpr, prompt string, enabled bool) (store.Routine, error) {
	existing, err := s.db.GetRoutineForOwner(ctx, store.GetRoutineForOwnerParams{ID: routineID, OwnerID: ownerID})
	if err != nil {
		return store.Routine{}, fmt.Errorf("routine not found")
	}
	name = strings.TrimSpace(name)
	prompt = strings.TrimSpace(prompt)
	if name == "" || prompt == "" {
		return store.Routine{}, fmt.Errorf("name and prompt are required")
	}
	var next *time.Time
	if enabled {
		next, err = s.nextRun(cronExpr, time.Now().In(s.accountLocation(ctx, ownerID)))
		if err != nil {
			return store.Routine{}, err
		}
	}
	return s.db.UpdateRoutine(ctx, store.UpdateRoutineParams{
		ID:        existing.ID,
		TeamID:    existing.TeamID,
		Name:      name,
		Cron:      strings.TrimSpace(cronExpr),
		Prompt:    prompt,
		Enabled:   enabled,
		NextRunAt: next,
	})
}

func (s *Service) List(ctx context.Context, ownerID uuid.UUID) ([]store.Routine, error) {
	return s.db.ListRoutinesByOwner(ctx, ownerID)
}

func (s *Service) Delete(ctx context.Context, ownerID, routineID uuid.UUID) error {
	existing, err := s.db.GetRoutineForOwner(ctx, store.GetRoutineForOwnerParams{ID: routineID, OwnerID: ownerID})
	if err != nil {
		return fmt.Errorf("routine not found")
	}
	n, err := s.db.DeleteRoutine(ctx, store.DeleteRoutineParams{ID: routineID, TeamID: existing.TeamID})
	if err != nil {
		return err
	}
	if n == 0 {
		return fmt.Errorf("routine not found")
	}
	return nil
}

// ReconcileDue creates a fresh team conversation for every due routine. The
// normal worker cycle engine then runs the roundtable for that conversation.
func (s *Service) ExpireStaleApprovals(ctx context.Context) {
	approvals, err := s.db.ListExpiredPendingApprovals(ctx)
	if err != nil {
		s.logger.Warn("list expired approvals failed", "error", err)
		return
	}
	for _, approval := range approvals {
		if err := s.db.ExpireToolApproval(ctx, approval.ID); err != nil {
			s.logger.Warn("expire approval failed", "approval_id", approval.ID, "error", err)
			continue
		}
		if approval.CheckpointID != nil {
			_ = s.db.DiscardCheckpoint(ctx, *approval.CheckpointID)
			_ = s.db.CancelCycleForCheckpoint(ctx, *approval.CheckpointID)
		}
		s.logger.Info("approval expired", "approval_id", approval.ID, "tool", approval.ToolName)
	}
}

func (s *Service) ReconcileStaleSubagents(ctx context.Context) {
	tasks, err := s.db.ListRunningSubagentTasks(ctx)
	if err != nil {
		s.logger.Warn("list running subagents failed", "error", err)
		return
	}
	for _, task := range tasks {
		if task.ChildCycleID == nil {
			_, _ = s.db.FinishSubagentTask(ctx, store.FinishSubagentTaskParams{
				ID: task.ID, Status: "failed", Error: "子代理没有执行周期",
			})
			continue
		}
		cycle, err := s.db.GetCycle(ctx, *task.ChildCycleID)
		if err != nil || time.Since(cycle.UpdatedAt) < 5*time.Second {
			continue
		}
		status := ""
		summary := ""
		errMsg := ""
		switch cycle.Status {
		case "done":
			status = "succeeded"
			if msg, err := s.db.GetLatestBotMessage(ctx, cycle.ConversationID); err == nil {
				summary = truncateText(msg.Content, 4000)
			}
		case "canceled":
			status = "canceled"
			errMsg = "子代理执行周期已取消"
		case "failed":
			status = "failed"
			errMsg = cycle.Error
			if errMsg == "" {
				errMsg = "子代理执行周期失败"
			}
		default:
			continue
		}
		if _, err := s.db.FinishSubagentTask(ctx, store.FinishSubagentTaskParams{
			ID: task.ID, Status: status, ResultSummary: summary, Error: errMsg,
		}); err != nil {
			s.logger.Warn("reconcile subagent failed", "task_id", task.ID, "error", err)
			continue
		}
		if status == "succeeded" && summary != "" {
			meta, _ := json.Marshal(map[string]any{"subagent_task_id": task.ID.String()})
			msg, err := s.db.CreateMessage(ctx, store.CreateMessageParams{
				ConversationID: task.ParentConversationID,
				SenderType:     "system",
				Content:        "子代理已完成（恢复）：" + truncateText(summary, 1200),
				Metadata:       meta,
			})
			if err == nil {
				_, _ = events.Append(ctx, s.db.Queries, task.ParentConversationID, nil, nil, events.TypeMessageCreated, map[string]any{"message": msg})
				_ = s.bus.Notify(ctx, task.ParentConversationID)
			}
		}
		s.logger.Info("reconciled stale subagent", "task_id", task.ID, "status", status)
	}
}

func truncateText(value string, max int) string {
	runes := []rune(value)
	if len(runes) <= max {
		return value
	}
	return string(runes[:max]) + "…"
}

func (s *Service) ReconcileDue(ctx context.Context) {
	s.ExpireStaleApprovals(ctx)
	s.ReconcileStaleSubagents(ctx)
	due, err := s.db.ListDueRoutines(ctx)
	if err != nil {
		s.logger.Warn("list due routines failed", "error", err)
		return
	}
	for _, routine := range due {
		if err := s.createRoutineRun(ctx, routine); err != nil {
			s.logger.Warn("routine run failed", "routine_id", routine.ID, "error", err)
		}
		next, nextErr := s.nextRun(routine.Cron, time.Now().In(s.teamLocation(ctx, routine.TeamID)))
		if nextErr != nil {
			s.logger.Warn("routine cron is invalid; disabling", "routine_id", routine.ID, "error", nextErr)
			_, _ = s.db.DisableRoutine(ctx, routine.ID)
			continue
		}
		if _, err := s.db.MarkRoutineRun(ctx, store.MarkRoutineRunParams{ID: routine.ID, NextRunAt: next}); err != nil {
			s.logger.Warn("mark routine run failed", "routine_id", routine.ID, "error", err)
		}
	}
	s.reconcileBotRoutines(ctx)
}

func (s *Service) createRoutineRun(ctx context.Context, routine store.Routine) error {
	team, err := s.db.GetTeamByID(ctx, routine.TeamID)
	if err != nil {
		return fmt.Errorf("load team: %w", err)
	}
	maxRounds := int32(6)
	var teamCfg struct {
		MaxRounds int `json:"max_rounds"`
	}
	_ = json.Unmarshal(team.Config, &teamCfg)
	if teamCfg.MaxRounds > 0 {
		maxRounds = int32(teamCfg.MaxRounds)
	}
	if maxRounds > 12 {
		maxRounds = 12
	}
	convID, err := s.routineConversation(ctx, team)
	if err != nil {
		return err
	}

	tx, err := s.db.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	q := s.db.WithTx(tx)

	meta, _ := json.Marshal(map[string]string{
		"routine_id":   routine.ID.String(),
		"kind":         "routine",
		"routine_name": routine.Name,
	})
	msg, err := q.CreateMessage(ctx, store.CreateMessageParams{
		ConversationID: convID,
		SenderType:     "user",
		Content:        "【例程：" + routine.Name + "】\n" + routine.Prompt,
		Metadata:       meta,
	})
	if err != nil {
		return fmt.Errorf("create message: %w", err)
	}
	// A group that is already working receives the routine like any other
	// message sent mid-turn: the running cycle picks it up before its next
	// model call, or answers it in a follow-up turn.
	var cycleID *uuid.UUID
	if _, activeErr := q.GetActiveCycle(ctx, convID); errors.Is(activeErr, pgx.ErrNoRows) {
		cycle, err := q.CreateCycle(ctx, store.CreateCycleParams{
			ConversationID:   convID,
			TriggerMessageID: msg.ID,
			MaxRounds:        maxRounds,
		})
		if err != nil {
			return fmt.Errorf("create cycle: %w", err)
		}
		if err := q.SetMessageCycle(ctx, store.SetMessageCycleParams{ID: msg.ID, CycleID: &cycle.ID}); err != nil {
			return fmt.Errorf("attach cycle: %w", err)
		}
		msg.CycleID = &cycle.ID
		cycleID = &cycle.ID
	} else if activeErr != nil {
		return fmt.Errorf("get active cycle: %w", activeErr)
	}
	if _, err := events.Append(ctx, q, convID, cycleID, nil, events.TypeMessageCreated, map[string]any{"message": msg}); err != nil {
		return err
	}
	if cycleID != nil {
		if _, err := events.Append(ctx, q, convID, cycleID, nil, events.TypeCycleUpdated, map[string]any{
			"cycle_id": *cycleID,
			"status":   "queued",
			"routine":  routine.ID,
		}); err != nil {
			return err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	if err := s.bus.Notify(ctx, convID); err != nil {
		s.logger.Warn("notify routine conversation failed", "conversation_id", convID, "error", err)
	}
	return nil
}

// routineConversation is where a team routine posts: the team's own room,
// so results appear in the group chat the user already has open instead of
// in a new conversation per run. A team without a room gets its most recent
// conversation, or a new one that becomes its room.
func (s *Service) routineConversation(ctx context.Context, team store.Team) (uuid.UUID, error) {
	if team.RoomConversationID != nil {
		if conv, err := s.db.GetConversation(ctx, *team.RoomConversationID); err == nil {
			return conv.ID, nil
		}
	}
	if recent, err := s.db.ListTeamConversations(ctx, store.ListTeamConversationsParams{
		TeamID: &team.ID, Limit: 1,
	}); err == nil && len(recent) > 0 {
		return recent[0].ID, nil
	}
	conv, err := s.db.CreateConversation(ctx, store.CreateConversationParams{
		TeamID:    &team.ID,
		Kind:      "team",
		Title:     team.Name,
		CreatedBy: team.OwnerID,
	})
	if err != nil {
		return uuid.Nil, fmt.Errorf("create team conversation: %w", err)
	}
	if err := s.db.SetTeamRoomConversation(ctx, store.SetTeamRoomConversationParams{
		ID: team.ID, OwnerID: team.OwnerID, RoomConversationID: &conv.ID,
	}); err != nil {
		s.logger.Warn("attach room conversation failed", "team_id", team.ID, "error", err)
	}
	return conv.ID, nil
}
