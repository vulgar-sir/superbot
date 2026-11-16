package seed

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	"superbot/internal/secrets"
	"superbot/internal/store"
)

// EnsureDeepSeek creates the default DeepSeek provider and model on first
// boot when SUPERBOT_DEEPSEEK_API_KEY is set. It never overwrites existing
// providers, so UI edits always win after the initial seed.
func EnsureDeepSeek(ctx context.Context, db *store.DB, box *secrets.Box, ownerID uuid.UUID, apiKey string) error {
	if apiKey == "" {
		return nil
	}
	existing, err := db.ListProvidersByType(ctx, store.ListProvidersByTypeParams{
		OwnerID: ownerID,
		Type:    "llm",
	})
	if err != nil {
		return fmt.Errorf("list llm providers: %w", err)
	}
	if len(existing) > 0 {
		return nil
	}
	enc, err := box.EncryptString(apiKey)
	if err != nil {
		return fmt.Errorf("encrypt deepseek key: %w", err)
	}
	cfg := json.RawMessage(`{"preset":"deepseek"}`)
	provider, err := db.CreateProvider(ctx, store.CreateProviderParams{
		OwnerID:   ownerID,
		Type:      "llm",
		Kind:      "openai_compat",
		Name:      "DeepSeek",
		BaseUrl:   "https://api.deepseek.com",
		ApiKeyEnc: enc,
		Config:    cfg,
		Enabled:   true,
	})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return nil
		}
		return fmt.Errorf("create deepseek provider: %w", err)
	}

	// Prices are peak-hour CNY per 1M tokens, stored as micro-CNY.
	_, err = db.CreateModel(ctx, store.CreateModelParams{
		ProviderID:              provider.ID,
		Name:                    "deepseek-flash",
		ContextWindow:           1_048_576,
		MaxOutput:               393_216,
		SupportsTools:           true,
		SupportsVision:          true,
		SupportsReasoning:       true,
		PriceInMicrosPerMtok:    2_000_000,
		PriceCacheMicrosPerMtok: 40_000,
		PriceOutMicrosPerMtok:   8_000_000,
	})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return nil
		}
		return fmt.Errorf("create deepseek model: %w", err)
	}
	return nil
}
