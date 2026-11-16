-- +goose Up
ALTER TABLE bot_templates DROP CONSTRAINT IF EXISTS bot_templates_visibility_check;
ALTER TABLE bot_templates ADD CONSTRAINT bot_templates_visibility_check
    CHECK (visibility IN ('owner', 'account', 'public'));
ALTER TABLE bot_templates ADD COLUMN install_count integer NOT NULL DEFAULT 0;
ALTER TABLE bot_templates ADD COLUMN published_at timestamptz;
CREATE INDEX bot_templates_market_idx
    ON bot_templates(install_count DESC, updated_at DESC)
    WHERE visibility = 'public' AND status = 'published';

-- +goose Down
DROP INDEX IF EXISTS bot_templates_market_idx;
ALTER TABLE bot_templates DROP COLUMN IF EXISTS published_at;
ALTER TABLE bot_templates DROP COLUMN IF EXISTS install_count;
ALTER TABLE bot_templates DROP CONSTRAINT IF EXISTS bot_templates_visibility_check;
ALTER TABLE bot_templates ADD CONSTRAINT bot_templates_visibility_check
    CHECK (visibility IN ('owner', 'account'));
