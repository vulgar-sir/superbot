package routines

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/robfig/cron/v3"

	"superbot/internal/events"
	"superbot/internal/store"
)

const defaultBotTimezone = "Asia/Shanghai"

// flexibleGrace is how late a "flexible" routine may start before the worker
// skips that occurrence instead of doing stale work. Exact routines always
// catch up as soon as the worker sees them.
const flexibleGrace = 15 * time.Minute

func (s *Service) nextBotRun(schedule, timezone string, from time.Time) (*time.Time, error) {
	schedule = strings.TrimSpace(schedule)
	if schedule == "" {
		return nil, errors.New("schedule is required")
	}
	if strings.HasPrefix(schedule, "email:") {
		// Email-trigger routines have no clock schedule; the worker's inbox
		// watcher starts them.
		return nil, nil
	}
	if strings.HasPrefix(schedule, "@every ") {
		duration, err := time.ParseDuration(strings.TrimSpace(strings.TrimPrefix(schedule, "@every ")))
		if err != nil || duration <= 0 {
			return nil, errors.New("invalid @every duration")
		}
		next := from.Add(duration)
		return &next, nil
	}
	if strings.HasPrefix(schedule, "once:") {
		parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(strings.TrimPrefix(schedule, "once:")))
		if err != nil {
			return nil, errors.New("invalid once schedule; use once:<RFC3339>")
		}
		return &parsed, nil
	}
	loc, err := time.LoadLocation(strings.TrimSpace(timezone))
	if err != nil {
		return nil, errors.New("invalid timezone")
	}
	parser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor)
	parsed, err := parser.Parse(schedule)
	if err != nil {
		return nil, fmt.Errorf("invalid cron expression: %w", err)
	}
	next := parsed.Next(from.In(loc))
	if next.IsZero() {
		return nil, errors.New("schedule never runs")
	}
	return &next, nil
}

func (s *Service) CreateBotRoutine(
	ctx context.Context,
	ownerID, botID uuid.UUID,
	name, schedule, timezone, timing, prompt string,
	enabled bool,
) (store.BotRoutine, error) {
	if _, err := s.db.GetBot(ctx, store.GetBotParams{ID: botID, OwnerID: ownerID}); err != nil {
		return store.BotRoutine{}, errors.New("bot not found")
	}
	name = strings.TrimSpace(name)
	prompt = strings.TrimSpace(prompt)
	if name == "" || prompt == "" {
		return store.BotRoutine{}, errors.New("name and prompt are required")
	}
	timezone = strings.TrimSpace(timezone)
	if timezone == "" {
		timezone = defaultBotTimezone
	}
	if timing != "flexible" {
		timing = "exact"
	}
	var next *time.Time
	if enabled {
		var err error
		next, err = s.nextBotRun(schedule, timezone, time.Now())
		if err != nil {
			return store.BotRoutine{}, err
		}
	}
	token, err := newTriggerToken()
	if err != nil {
		return store.BotRoutine{}, err
	}
	return s.db.CreateBotRoutine(ctx, store.CreateBotRoutineParams{
		OwnerID:      ownerID,
		BotID:        botID,
		Name:         name,
		Schedule:     strings.TrimSpace(schedule),
		Timezone:     timezone,
		Timing:       timing,
		Enabled:      enabled,
		Paused:       false,
		Prompt:       prompt,
		NextRunAt:    next,
		TriggerToken: token,
	})
}

func (s *Service) UpdateBotRoutine(
	ctx context.Context,
	ownerID, routineID uuid.UUID,
	name, schedule, timezone, timing, prompt string,
	enabled, paused bool,
) (store.BotRoutine, error) {
	existing, err := s.db.GetBotRoutineForOwner(ctx, store.GetBotRoutineForOwnerParams{
		ID: routineID, OwnerID: ownerID,
	})
	if err != nil {
		return store.BotRoutine{}, errors.New("routine not found")
	}
	name = strings.TrimSpace(name)
	prompt = strings.TrimSpace(prompt)
	if name == "" || prompt == "" {
		return store.BotRoutine{}, errors.New("name and prompt are required")
	}
	timezone = strings.TrimSpace(timezone)
	if timezone == "" {
		timezone = defaultBotTimezone
	}
	if timing != "flexible" {
		timing = "exact"
	}
	var next *time.Time
	if enabled && !paused {
		next, err = s.nextBotRun(schedule, timezone, time.Now())
		if err != nil {
			return store.BotRoutine{}, err
		}
	}
	return s.db.UpdateBotRoutine(ctx, store.UpdateBotRoutineParams{
		ID:        existing.ID,
		OwnerID:   ownerID,
		Name:      name,
		Schedule:  strings.TrimSpace(schedule),
		Timezone:  timezone,
		Timing:    timing,
		Enabled:   enabled,
		Paused:    paused,
		Prompt:    prompt,
		NextRunAt: next,
	})
}

// shouldSkipFlexible reports whether a flexible routine is too far past its
// scheduled time to still be worth running. One-off routines always run.
func shouldSkipFlexible(routine store.BotRoutine, now time.Time) bool {
	if routine.Timing != "flexible" || routine.NextRunAt == nil {
		return false
	}
	if strings.HasPrefix(strings.TrimSpace(routine.Schedule), "once:") {
		return false
	}
	return now.Sub(*routine.NextRunAt) > flexibleGrace
}

func newTriggerToken() (string, error) {
	buffer := make([]byte, 16)
	if _, err := rand.Read(buffer); err != nil {
		return "", err
	}
	return hex.EncodeToString(buffer), nil
}

// Trigger creates a run for an already-authorized routine. Callers are
// responsible for resolving the routine and checking that it is enabled.
func (s *Service) Trigger(ctx context.Context, routine store.BotRoutine) error {
	return s.createBotRoutineRun(ctx, routine)
}

func (s *Service) ListBotRoutines(ctx context.Context, ownerID uuid.UUID, botID *uuid.UUID) ([]store.BotRoutine, error) {
	if botID != nil {
		if _, err := s.db.GetBot(ctx, store.GetBotParams{ID: *botID, OwnerID: ownerID}); err != nil {
			return nil, errors.New("bot not found")
		}
		return s.db.ListBotRoutinesByBot(ctx, store.ListBotRoutinesByBotParams{
			OwnerID: ownerID, BotID: *botID,
		})
	}
	return s.db.ListBotRoutinesByOwner(ctx, ownerID)
}

func (s *Service) TestBotRoutine(ctx context.Context, ownerID, routineID uuid.UUID) error {
	routine, err := s.db.GetBotRoutineForOwner(ctx, store.GetBotRoutineForOwnerParams{
		ID:      routineID,
		OwnerID: ownerID,
	})
	if err != nil {
		return errors.New("routine not found")
	}
	return s.createBotRoutineRun(ctx, routine)
}

func (s *Service) DeleteBotRoutine(ctx context.Context, ownerID, routineID uuid.UUID) error {
	n, err := s.db.DeleteBotRoutine(ctx, store.DeleteBotRoutineParams{ID: routineID, OwnerID: ownerID})
	if err != nil {
		return err
	}
	if n == 0 {
		return errors.New("routine not found")
	}
	return nil
}

func (s *Service) reconcileBotRoutines(ctx context.Context) {
	due, err := s.db.ListDueBotRoutines(ctx)
	if err != nil {
		s.logger.Warn("list due bot routines failed", "error", err)
		return
	}
	for _, routine := range due {
		once := strings.HasPrefix(strings.TrimSpace(routine.Schedule), "once:")
		if shouldSkipFlexible(routine, time.Now()) {
			s.logger.Info("skipping stale flexible routine", "routine_id", routine.ID,
				"next_run_at", routine.NextRunAt)
		} else if err := s.createBotRoutineRun(ctx, routine); err != nil {
			s.logger.Warn("bot routine run failed", "routine_id", routine.ID, "error", err)
		}
		if once {
			_, _ = s.db.DisableBotRoutine(ctx, routine.ID)
			continue
		}
		next, nextErr := s.nextBotRun(routine.Schedule, routine.Timezone, time.Now())
		if nextErr != nil {
			s.logger.Warn("bot routine schedule invalid; disabling", "routine_id", routine.ID, "error", nextErr)
			_, _ = s.db.DisableBotRoutine(ctx, routine.ID)
			continue
		}
		if _, err := s.db.MarkBotRoutineRun(ctx, store.MarkBotRoutineRunParams{
			ID: routine.ID, NextRunAt: next,
		}); err != nil {
			s.logger.Warn("mark bot routine run failed", "routine_id", routine.ID, "error", err)
		}
	}
}

func (s *Service) createBotRoutineRun(ctx context.Context, routine store.BotRoutine) error {
	bot, err := s.db.GetBot(ctx, store.GetBotParams{ID: routine.BotID, OwnerID: routine.OwnerID})
	if err != nil {
		return fmt.Errorf("load bot: %w", err)
	}
	tx, err := s.db.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	q := s.db.WithTx(tx)

	// Prefer the creator's DM so routine runs land in the same chat the user
	// sees, not a parallel DM without direct_user_id.
	conv, err := q.GetDirectConversationByBot(ctx, store.GetDirectConversationByBotParams{
		CreatedBy: routine.OwnerID, DirectBotID: &bot.ID, DirectUserID: bot.CreatedByUserID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		if bot.CreatedByUserID != nil {
			conv, err = q.CreateDirectUserConversation(ctx, store.CreateDirectUserConversationParams{
				DirectBotID: &bot.ID, DirectUserID: bot.CreatedByUserID,
				Title: "和 " + bot.Name + " 的对话", CreatedBy: routine.OwnerID,
			})
		} else {
			conv, err = q.CreateConversation(ctx, store.CreateConversationParams{
				DirectBotID: &bot.ID, Kind: "direct",
				Title: "和 " + bot.Name + " 的对话", CreatedBy: routine.OwnerID,
			})
		}
	}
	if err != nil {
		return fmt.Errorf("load bot dm: %w", err)
	}
	meta, _ := json.Marshal(map[string]any{
		"bot_routine_id": routine.ID.String(),
		"routine_name":   routine.Name,
	})
	msg, err := q.CreateMessage(ctx, store.CreateMessageParams{
		ConversationID: conv.ID,
		SenderType:     "user",
		Content:        routine.Prompt,
		Metadata:       meta,
	})
	if err != nil {
		return fmt.Errorf("create routine message: %w", err)
	}
	cycle, err := q.CreateCycle(ctx, store.CreateCycleParams{
		ConversationID:   conv.ID,
		TriggerMessageID: msg.ID,
		MaxRounds:        1,
	})
	if err != nil {
		// The DM already has a running cycle (for example the user is chatting).
		// Skip this run instead of retrying forever; the next trigger will fire
		// once the conversation is idle.
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return nil
		}
		return fmt.Errorf("create routine cycle: %w", err)
	}
	if err := q.SetMessageCycle(ctx, store.SetMessageCycleParams{ID: msg.ID, CycleID: &cycle.ID}); err != nil {
		return fmt.Errorf("attach routine cycle: %w", err)
	}
	msg.CycleID = &cycle.ID
	if _, err := events.Append(ctx, q, conv.ID, &cycle.ID, nil, events.TypeMessageCreated, map[string]any{"message": msg}); err != nil {
		return err
	}
	if _, err := events.Append(ctx, q, conv.ID, &cycle.ID, nil, events.TypeCycleUpdated, map[string]any{
		"cycle_id": cycle.ID,
		"status":   cycle.Status,
		"routine":  routine.ID,
	}); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	if err := s.bus.Notify(ctx, conv.ID); err != nil {
		s.logger.Warn("notify bot routine conversation failed", "conversation_id", conv.ID, "error", err)
	}
	return nil
}
