package chat

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"superbot/internal/events"
	"superbot/internal/store"
)

var (
	ErrForbidden = errors.New("forbidden")
	ErrNotFound  = errors.New("not found")
	ErrBusy      = errors.New("conversation already has an active cycle")
	ErrInvalid   = errors.New("invalid request")
)

const (
	maxMessageBytes = 64 * 1024
	maxClientMsgID  = 128
)

type Service struct {
	db     *store.DB
	bus    *events.Bus
	logger *slog.Logger
}

func NewService(db *store.DB, bus *events.Bus, logger *slog.Logger) *Service {
	return &Service{db: db, bus: bus, logger: logger}
}

type SendResult struct {
	Message      store.Message
	Cycle        *store.Cycle
	Interjection bool
}

func (s *Service) CreateDirectConversation(ctx context.Context, accountID, userID, botID uuid.UUID, title string) (store.Conversation, error) {
	bot, err := s.db.GetBot(ctx, store.GetBotParams{ID: botID, OwnerID: accountID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return store.Conversation{}, ErrNotFound
		}
		return store.Conversation{}, fmt.Errorf("get bot: %w", err)
	}
	if existing, err := s.db.GetDirectConversationByBot(ctx, store.GetDirectConversationByBotParams{
		CreatedBy:    accountID,
		DirectBotID:  &botID,
		DirectUserID: &userID,
	}); err == nil {
		return existing, nil
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return store.Conversation{}, fmt.Errorf("find direct conversation: %w", err)
	}
	if strings.TrimSpace(title) == "" {
		title = "和 " + bot.Name + " 的对话"
	}
	conv, err := s.db.CreateConversation(ctx, store.CreateConversationParams{
		DirectBotID: &botID,
		Kind:        "direct",
		Title:       title,
		CreatedBy:   accountID,
	})
	if err != nil {
		return store.Conversation{}, fmt.Errorf("create conversation: %w", err)
	}
	if err := s.db.SetConversationDirectUser(ctx, store.SetConversationDirectUserParams{
		ID: conv.ID, DirectUserID: &userID,
	}); err != nil {
		return store.Conversation{}, fmt.Errorf("set direct user: %w", err)
	}
	conv.DirectUserID = &userID
	return conv, nil
}

func (s *Service) CreateTeamConversation(ctx context.Context, userID, teamID uuid.UUID, title string) (store.Conversation, error) {
	team, err := s.db.GetTeam(ctx, store.GetTeamParams{ID: teamID, OwnerID: userID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return store.Conversation{}, ErrNotFound
		}
		return store.Conversation{}, fmt.Errorf("get team: %w", err)
	}
	if team.Mode == "group" && team.RoomConversationID != nil {
		conv, err := s.db.GetConversation(ctx, *team.RoomConversationID)
		if err == nil {
			return conv, nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return store.Conversation{}, fmt.Errorf("load group room: %w", err)
		}
	}
	if strings.TrimSpace(title) == "" {
		if team.Mode == "group" {
			title = team.Name
		} else {
			title = team.Name + " 协作"
		}
	}
	conv, err := s.db.CreateConversation(ctx, store.CreateConversationParams{
		TeamID:    &teamID,
		Kind:      "team",
		Title:     title,
		CreatedBy: userID,
	})
	if err != nil {
		return store.Conversation{}, fmt.Errorf("create conversation: %w", err)
	}
	if team.Mode == "group" {
		if err := s.db.SetTeamRoomConversation(ctx, store.SetTeamRoomConversationParams{
			ID:                 team.ID,
			OwnerID:            userID,
			RoomConversationID: &conv.ID,
		}); err != nil {
			return store.Conversation{}, fmt.Errorf("attach group room: %w", err)
		}
	}
	return conv, nil
}

func (s *Service) ListConversations(ctx context.Context, accountID, userID uuid.UUID, limit, offset int32) ([]store.Conversation, error) {
	return s.db.ListConversations(ctx, store.ListConversationsParams{
		CreatedBy:    accountID,
		DirectUserID: &userID,
		Limit:        limit,
		Offset:       offset,
	})
}

func (s *Service) GetConversation(ctx context.Context, accountID, userID, conversationID uuid.UUID) (store.Conversation, []store.Message, error) {
	conv, err := s.getOwnedConversation(ctx, accountID, userID, conversationID)
	if err != nil {
		return store.Conversation{}, nil, err
	}
	messages, err := s.db.ListMessages(ctx, conversationID)
	if err != nil {
		return store.Conversation{}, nil, fmt.Errorf("list messages: %w", err)
	}
	return conv, messages, nil
}

// SendUserMessage appends a user message and, when the room is idle, creates a
// cycle for the worker to pick up. When a cycle is already running the message
// is an interjection: it is stored and the running worker will see it before
// its next bot turn.
func (s *Service) SendUserMessage(
	ctx context.Context,
	accountID, userID, conversationID uuid.UUID,
	content string,
	clientMsgID string,
	parentMessageID *uuid.UUID,
	attachmentIDs []uuid.UUID,
	metadata json.RawMessage,
) (SendResult, error) {
	content = strings.TrimSpace(content)
	if content == "" {
		return SendResult{}, fmt.Errorf("%w: message content is required", ErrInvalid)
	}
	if len(content) > maxMessageBytes {
		return SendResult{}, fmt.Errorf("%w: message is too long (max %d bytes)", ErrInvalid, maxMessageBytes)
	}
	if len(clientMsgID) > maxClientMsgID {
		return SendResult{}, fmt.Errorf("%w: client_msg_id is too long", ErrInvalid)
	}
	if len(metadata) == 0 {
		metadata = json.RawMessage(`{}`)
	}

	tx, err := s.db.Pool.Begin(ctx)
	if err != nil {
		return SendResult{}, fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback(ctx)
	q := s.db.WithTx(tx)

	conv, err := q.GetConversation(ctx, conversationID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return SendResult{}, ErrNotFound
		}
		return SendResult{}, fmt.Errorf("get conversation: %w", err)
	}
	if conv.CreatedBy != accountID {
		return SendResult{}, ErrForbidden
	}
	if conv.Kind == "direct" && conv.DirectUserID != nil && *conv.DirectUserID != userID {
		return SendResult{}, ErrForbidden
	}

	var clientPtr *string
	if clientMsgID != "" {
		clientPtr = &clientMsgID
		existing, err := q.GetMessageByClientID(ctx, store.GetMessageByClientIDParams{
			ConversationID: conversationID,
			ClientMsgID:    clientPtr,
		})
		if err == nil {
			if err := tx.Commit(ctx); err != nil {
				return SendResult{}, fmt.Errorf("commit idempotent message: %w", err)
			}
			return SendResult{Message: existing}, nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return SendResult{}, fmt.Errorf("look up client message: %w", err)
		}
	}

	if parentMessageID != nil {
		parent, err := q.GetMessage(ctx, *parentMessageID)
		if err != nil || parent.ConversationID != conversationID {
			return SendResult{}, fmt.Errorf("%w: parent message is invalid", ErrInvalid)
		}
	}
	for _, attachmentID := range attachmentIDs {
		attachment, err := q.GetAttachmentForOwner(ctx, store.GetAttachmentForOwnerParams{
			ID:      attachmentID,
			OwnerID: accountID,
		})
		if err != nil || attachment.MessageID != nil {
			return SendResult{}, fmt.Errorf("%w: attachment is invalid", ErrInvalid)
		}
	}

	msg, err := q.CreateMessage(ctx, store.CreateMessageParams{
		ConversationID:  conversationID,
		SenderType:      "user",
		Content:         content,
		Metadata:        metadata,
		ClientMsgID:     clientPtr,
		ParentMessageID: parentMessageID,
	})
	if err != nil {
		return SendResult{}, fmt.Errorf("create message: %w", err)
	}
	for _, attachmentID := range attachmentIDs {
		if err := q.AttachAttachmentToMessage(ctx, store.AttachAttachmentToMessageParams{
			ID:        attachmentID,
			OwnerID:   accountID,
			MessageID: &msg.ID,
		}); err != nil {
			return SendResult{}, fmt.Errorf("attach attachment: %w", err)
		}
	}

	result := SendResult{Message: msg}

	active, err := q.GetActiveCycle(ctx, conversationID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		maxRounds := int32(1)
		if conv.Kind == "team" && conv.TeamID != nil {
			team, teamErr := q.GetTeam(ctx, store.GetTeamParams{ID: *conv.TeamID, OwnerID: conv.CreatedBy})
			if teamErr != nil {
				return SendResult{}, fmt.Errorf("load team: %w", teamErr)
			}
			var teamCfg struct {
				MaxRounds int `json:"max_rounds"`
			}
			_ = json.Unmarshal(team.Config, &teamCfg)
			if teamCfg.MaxRounds <= 0 {
				teamCfg.MaxRounds = 6
			}
			if teamCfg.MaxRounds > 12 {
				teamCfg.MaxRounds = 12
			}
			maxRounds = int32(teamCfg.MaxRounds)
		}
		cycle, err := q.CreateCycle(ctx, store.CreateCycleParams{
			ConversationID:   conversationID,
			TriggerMessageID: msg.ID,
			MaxRounds:        maxRounds,
		})
		if err != nil {
			var pgErr *pgconn.PgError
			if errors.As(err, &pgErr) && pgErr.Code == "23505" {
				return SendResult{}, ErrBusy
			}
			return SendResult{}, fmt.Errorf("create cycle: %w", err)
		}
		if err := q.SetMessageCycle(ctx, store.SetMessageCycleParams{ID: msg.ID, CycleID: &cycle.ID}); err != nil {
			return SendResult{}, fmt.Errorf("attach cycle to message: %w", err)
		}
		msg.CycleID = &cycle.ID
		result.Message = msg
		result.Cycle = &cycle
		if _, err := events.Append(ctx, q, conversationID, &cycle.ID, nil, events.TypeMessageCreated, map[string]any{"message": msg}); err != nil {
			return SendResult{}, err
		}
		if _, err := events.Append(ctx, q, conversationID, &cycle.ID, nil, events.TypeCycleUpdated, map[string]any{
			"cycle_id": cycle.ID,
			"status":   cycle.Status,
			"round":    cycle.Round,
		}); err != nil {
			return SendResult{}, err
		}
	case err != nil:
		return SendResult{}, fmt.Errorf("get active cycle: %w", err)
	default:
		result.Interjection = true
		if _, err := events.Append(ctx, q, conversationID, &active.ID, nil, events.TypeMessageCreated, map[string]any{"message": msg}); err != nil {
			return SendResult{}, err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return SendResult{}, fmt.Errorf("commit: %w", err)
	}
	if err := s.bus.Notify(ctx, conversationID); err != nil {
		s.logger.Warn("notify failed", "conversation_id", conversationID, "error", err)
	}
	return result, nil
}

func (s *Service) RequestStop(ctx context.Context, accountID, userID, conversationID uuid.UUID) (int64, error) {
	if _, err := s.getOwnedConversation(ctx, accountID, userID, conversationID); err != nil {
		return 0, err
	}
	n, err := s.db.RequestCycleCancel(ctx, conversationID)
	if err != nil {
		return 0, fmt.Errorf("request cycle cancel: %w", err)
	}
	if n > 0 {
		if err := s.bus.Notify(ctx, conversationID); err != nil {
			s.logger.Warn("notify failed", "conversation_id", conversationID, "error", err)
		}
	}
	return n, nil
}

func (s *Service) getOwnedConversation(ctx context.Context, accountID, userID, conversationID uuid.UUID) (store.Conversation, error) {
	conv, err := s.db.GetConversation(ctx, conversationID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return store.Conversation{}, ErrNotFound
		}
		return store.Conversation{}, fmt.Errorf("get conversation: %w", err)
	}
	if conv.CreatedBy != accountID {
		return store.Conversation{}, ErrForbidden
	}
	if conv.Kind == "direct" && conv.DirectUserID != nil && *conv.DirectUserID != userID {
		return store.Conversation{}, ErrForbidden
	}
	return conv, nil
}

// ResumeAfterApproval queues the cycle that continues a run paused for a tool
// approval. It reports false when the conversation is busy with another
// cycle; the checkpoint then stays waiting and the engine resumes it once
// that cycle ends, so a resolved approval is never silently dropped.
func (s *Service) ResumeAfterApproval(ctx context.Context, checkpoint store.RunCheckpoint, notice string, metadata json.RawMessage) (bool, error) {
	if len(metadata) == 0 {
		metadata = json.RawMessage("{}")
	}
	tx, err := s.db.Pool.Begin(ctx)
	if err != nil {
		return false, fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback(ctx)
	q := s.db.WithTx(tx)

	if _, err := q.GetActiveCycle(ctx, checkpoint.ConversationID); err == nil {
		return false, nil
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return false, fmt.Errorf("get active cycle: %w", err)
	}
	msg, err := q.CreateMessage(ctx, store.CreateMessageParams{
		ConversationID: checkpoint.ConversationID,
		SenderType:     "system",
		Content:        notice,
		Metadata:       metadata,
	})
	if err != nil {
		return false, fmt.Errorf("create approval notice: %w", err)
	}
	cycle, err := q.CreateCycle(ctx, store.CreateCycleParams{
		ConversationID:   checkpoint.ConversationID,
		TriggerMessageID: msg.ID,
		MaxRounds:        1,
	})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return false, nil
		}
		return false, fmt.Errorf("create resume cycle: %w", err)
	}
	if err := q.SetCycleResumeCheckpoint(ctx, store.SetCycleResumeCheckpointParams{
		ID: cycle.ID, ResumeCheckpointID: &checkpoint.ID,
	}); err != nil {
		return false, fmt.Errorf("attach checkpoint: %w", err)
	}
	if err := q.SetMessageCycle(ctx, store.SetMessageCycleParams{ID: msg.ID, CycleID: &cycle.ID}); err != nil {
		return false, fmt.Errorf("attach cycle to notice: %w", err)
	}
	msg.CycleID = &cycle.ID
	if _, err := events.Append(ctx, q, checkpoint.ConversationID, &cycle.ID, nil, events.TypeMessageCreated, map[string]any{"message": msg}); err != nil {
		return false, err
	}
	if _, err := events.Append(ctx, q, checkpoint.ConversationID, &cycle.ID, nil, events.TypeCycleUpdated, map[string]any{
		"cycle_id": cycle.ID,
		"status":   cycle.Status,
		"round":    cycle.Round,
	}); err != nil {
		return false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("commit: %w", err)
	}
	if err := s.bus.Notify(ctx, checkpoint.ConversationID); err != nil {
		s.logger.Warn("notify failed", "conversation_id", checkpoint.ConversationID, "error", err)
	}
	return true, nil
}

// Wake queues a cycle triggered by a message that is already in the
// conversation: a late interjection, a result handed back by another bot, or
// a goal check. It reports false when the conversation is busy; the running
// cycle then picks the message up itself.
func (s *Service) Wake(ctx context.Context, conversationID uuid.UUID, trigger store.Message, maxRounds int32, reason string) (bool, error) {
	if maxRounds < 1 {
		maxRounds = 1
	}
	tx, err := s.db.Pool.Begin(ctx)
	if err != nil {
		return false, fmt.Errorf("begin wake: %w", err)
	}
	defer tx.Rollback(ctx)
	q := s.db.WithTx(tx)
	if _, err := q.GetActiveCycle(ctx, conversationID); err == nil {
		return false, nil
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return false, fmt.Errorf("get active cycle: %w", err)
	}
	cycle, err := q.CreateCycle(ctx, store.CreateCycleParams{
		ConversationID:   conversationID,
		TriggerMessageID: trigger.ID,
		MaxRounds:        maxRounds,
	})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return false, nil
		}
		return false, fmt.Errorf("create wake cycle: %w", err)
	}
	if _, err := events.Append(ctx, q, conversationID, &cycle.ID, nil, events.TypeCycleUpdated, map[string]any{
		"cycle_id": cycle.ID,
		"status":   cycle.Status,
		"round":    cycle.Round,
		"reason":   reason,
	}); err != nil {
		return false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("commit wake: %w", err)
	}
	if err := s.bus.Notify(ctx, conversationID); err != nil {
		s.logger.Warn("notify failed", "conversation_id", conversationID, "error", err)
	}
	return true, nil
}
