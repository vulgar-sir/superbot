package goals

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"superbot/internal/store"
)

// Service persists goals. Changes run inside a transaction that locks the
// goal row, so two members reporting at the same moment cannot overwrite
// each other.
type Service struct {
	db *store.DB
}

func NewService(db *store.DB) *Service {
	return &Service{db: db}
}

// Load returns the conversation's goal, or nil when none was ever drafted.
func (s *Service) Load(ctx context.Context, conversationID uuid.UUID) (*Goal, *store.GroupGoal, error) {
	row, err := s.db.GetGroupGoal(ctx, conversationID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, fmt.Errorf("load group goal: %w", err)
	}
	goal, err := decode(row.State)
	if err != nil {
		return nil, nil, err
	}
	return goal, &row, nil
}

// Apply runs one action and stores the result. A rejected action returns the
// rule that was broken and changes nothing.
func (s *Service) Apply(ctx context.Context, conversationID, ownerID uuid.UUID, action Action, actor Actor, members []Member) (*Goal, string, error) {
	tx, err := s.db.Pool.Begin(ctx)
	if err != nil {
		return nil, "", fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback(ctx)
	q := s.db.WithTx(tx)

	var current *Goal
	row, err := q.GetGroupGoalForUpdate(ctx, conversationID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
	case err != nil:
		return nil, "", fmt.Errorf("lock group goal: %w", err)
	default:
		if current, err = decode(row.State); err != nil {
			return nil, "", err
		}
	}
	next, message, err := Apply(current, action, actor, members, time.Now())
	if err != nil {
		return nil, "", err
	}
	if next == nil || next == current {
		return next, message, nil
	}
	state, err := json.Marshal(next)
	if err != nil {
		return nil, "", fmt.Errorf("encode group goal: %w", err)
	}
	if _, err := q.UpsertGroupGoal(ctx, store.UpsertGroupGoalParams{
		ConversationID: conversationID,
		OwnerID:        ownerID,
		Status:         next.Status,
		State:          state,
	}); err != nil {
		return nil, "", fmt.Errorf("save group goal: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, "", fmt.Errorf("commit: %w", err)
	}
	return next, message, nil
}

// RecordNudge notes that the lead was woken for this goal and at which point
// of the conversation, so the driver can tell whether a human has spoken
// since.
func (s *Service) RecordNudge(ctx context.Context, conversationID uuid.UUID, goal *Goal, ordinal int64) error {
	goal.Nudges++
	state, err := json.Marshal(goal)
	if err != nil {
		return err
	}
	return s.db.SetGroupGoalNudge(ctx, store.SetGroupGoalNudgeParams{
		ConversationID:   conversationID,
		State:            state,
		LastNudgeOrdinal: ordinal,
	})
}

// ResetNudges re-arms the idle driver after a human wrote in the group.
func (s *Service) ResetNudges(ctx context.Context, conversationID uuid.UUID, goal *Goal, ordinal int64) error {
	goal.Nudges = 0
	state, err := json.Marshal(goal)
	if err != nil {
		return err
	}
	return s.db.SetGroupGoalNudge(ctx, store.SetGroupGoalNudgeParams{
		ConversationID:   conversationID,
		State:            state,
		LastNudgeOrdinal: ordinal,
	})
}

func decode(raw json.RawMessage) (*Goal, error) {
	var goal Goal
	if err := json.Unmarshal(raw, &goal); err != nil {
		return nil, fmt.Errorf("decode group goal: %w", err)
	}
	if goal.Stages == nil {
		goal.Stages = []Stage{}
	}
	if goal.Constraints == nil {
		goal.Constraints = []string{}
	}
	if goal.Events == nil {
		goal.Events = []Event{}
	}
	for i := range goal.Stages {
		if goal.Stages[i].Tasks == nil {
			goal.Stages[i].Tasks = []Task{}
		}
	}
	return &goal, nil
}
