package agent

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"superbot/internal/store"
)

// botModelStale reports whether the bot's model can no longer be used: no
// provider, a deleted or disabled one, or a model its provider stopped
// listing. botmodel.Repair moves such bots; see RepointStaleBots.
func (e *Engine) botModelStale(ctx context.Context, bot store.Bot) bool {
	return e.modelStale(ctx, bot.OwnerID, bot.ProviderID, bot.Model)
}

func (e *Engine) modelStale(ctx context.Context, ownerID uuid.UUID, providerID *uuid.UUID, model string) bool {
	if providerID == nil {
		return true
	}
	provider, err := e.db.GetProvider(ctx, store.GetProviderParams{ID: *providerID, OwnerID: ownerID})
	if err != nil {
		return errors.Is(err, pgx.ErrNoRows)
	}
	if !provider.Enabled || provider.Type != "llm" {
		return true
	}
	if _, err := e.db.GetModelByName(ctx, store.GetModelByNameParams{ProviderID: provider.ID, Name: model}); errors.Is(err, pgx.ErrNoRows) {
		// A provider without a model list is taken at its word.
		models, err := e.db.ListModelsByProvider(ctx, provider.ID)
		return err == nil && len(models) > 0
	}
	return false
}
