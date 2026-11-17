// Package botmodel keeps every bot on a model that can actually run.
//
// A bot names its provider and model. When the provider is deleted (its id
// is cleared), disabled, or stops listing the model, the bot would fail on
// every turn until someone edited it by hand. Repair moves such bots to a
// usable model and tells the account which ones moved.
package botmodel

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/google/uuid"

	"superbot/internal/i18n"
	"superbot/internal/store"
)

// Repair moves the account's bots whose model can no longer be used (see
// RepointStaleBots for which model they get) and notifies the account. It
// returns the bots that moved; an error only means nothing changed.
func Repair(ctx context.Context, db *store.DB, ownerID uuid.UUID, logger *slog.Logger) ([]store.RepointStaleBotsRow, error) {
	moved, err := db.RepointStaleBots(ctx, ownerID)
	if err != nil {
		return nil, fmt.Errorf("repoint stale bots: %w", err)
	}
	if len(moved) == 0 {
		return nil, nil
	}
	providerNames := map[uuid.UUID]string{}
	var lines []string
	ids := make([]string, 0, len(moved))
	for _, bot := range moved {
		target := bot.Model
		if bot.ProviderID != nil {
			name, ok := providerNames[*bot.ProviderID]
			if !ok {
				if provider, err := db.GetProvider(ctx, store.GetProviderParams{ID: *bot.ProviderID, OwnerID: ownerID}); err == nil {
					name = provider.Name
				}
				providerNames[*bot.ProviderID] = name
			}
			if name != "" {
				target = name + " / " + bot.Model
			}
		}
		previous := bot.PreviousModel
		if previous == "" {
			previous = "-"
		}
		lines = append(lines, fmt.Sprintf("%s：%s → %s", bot.Name, previous, target))
		ids = append(ids, bot.ID.String())
		if logger != nil {
			logger.Info("bot moved to a usable model", "bot_id", bot.ID, "from", bot.PreviousModel, "to", target)
		}
	}
	language := i18n.AccountLanguage(ctx, db, ownerID)
	title := fmt.Sprintf(i18n.Text(language, "%d 个 Bot 已自动切换模型", "%d bot(s) switched to a usable model"), len(moved))
	body := i18n.Text(language, "原来的模型已不可用（服务商被删除、停用或不再提供该模型）：\n",
		"Their model is no longer available (provider deleted, disabled or no longer offering it):\n") +
		strings.Join(lines, "\n")
	meta, _ := json.Marshal(map[string]any{"bot_ids": ids})
	if _, err := db.CreateNotification(ctx, store.CreateNotificationParams{
		OwnerID:      ownerID,
		Kind:         "bot_model",
		Title:        title,
		Body:         body,
		ResourceType: "bot",
		ResourceID:   ids[0],
		Metadata:     meta,
	}); err != nil && logger != nil {
		logger.Warn("notify bot model switch failed", "error", err)
	}
	return moved, nil
}
