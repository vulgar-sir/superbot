// Package emailtrigger runs Bot routines whose schedule starts with "email:".
// It polls the account's IMAP inbox and de-duplicates by Message-ID so each
// message triggers at most once.
package emailtrigger

import (
	"context"
	"log/slog"
	"strings"

	"github.com/google/uuid"

	"superbot/internal/connectors"
	"superbot/internal/mailbox"
	"superbot/internal/routines"
	"superbot/internal/store"
)

type Service struct {
	db         *store.DB
	connectors *connectors.Service
	routines   *routines.Service
	mailbox    *mailbox.Service
	logger     *slog.Logger
}

func NewService(db *store.DB, connectorService *connectors.Service, routineService *routines.Service, logger *slog.Logger) *Service {
	return &Service{
		db: db, connectors: connectorService, routines: routineService,
		mailbox: mailbox.NewService(), logger: logger,
	}
}

// CheckOnce polls every account that has enabled email-trigger routines.
func (s *Service) CheckOnce(ctx context.Context) {
	routineRows, err := s.db.ListEmailTriggerRoutines(ctx)
	if err != nil {
		s.logger.Warn("list email trigger routines failed", "error", err)
		return
	}
	byAccount := map[uuid.UUID][]store.BotRoutine{}
	for _, routine := range routineRows {
		byAccount[routine.OwnerID] = append(byAccount[routine.OwnerID], routine)
	}
	for accountID, accountRoutines := range byAccount {
		connector, err := s.connectors.ByProvider(ctx, accountID, "email")
		if err != nil {
			continue
		}
		password, err := s.connectors.AccessToken(connector)
		if err != nil || strings.TrimSpace(password) == "" {
			continue
		}
		config, err := mailbox.ParseConfig(connector.Config)
		if err != nil {
			continue
		}
		messages, err := s.mailbox.Recent(ctx, config, password, 20)
		if err != nil {
			s.logger.Warn("email trigger inbox poll failed", "account_id", accountID, "error", err)
			continue
		}
		for _, message := range messages {
			messageKey := strings.TrimSpace(message.MessageID)
			if messageKey == "" {
				messageKey = message.From + "|" + message.Subject + "|" + message.Date.UTC().Format("2006-01-02T15:04:05Z")
			}
			inserted, err := s.db.InsertEmailTriggerSeen(ctx, store.InsertEmailTriggerSeenParams{
				MessageID: messageKey, AccountID: accountID,
			})
			if err != nil || inserted == 0 {
				continue
			}
			for _, routine := range accountRoutines {
				if !matchesEmailFilter(routine.Schedule, message) {
					continue
				}
				if err := s.routines.Trigger(ctx, routine); err != nil {
					s.logger.Warn("email trigger routine failed", "routine_id", routine.ID, "error", err)
				}
			}
		}
	}
}

// matchesEmailFilter understands "email:" (any message) and
// "email:<substring>" (case-insensitive match on from or subject).
func matchesEmailFilter(schedule string, message mailbox.Message) bool {
	filter := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(schedule), "email:"))
	if filter == "" {
		return true
	}
	filter = strings.ToLower(filter)
	haystack := strings.ToLower(message.From + " " + message.Subject)
	return strings.Contains(haystack, filter)
}
