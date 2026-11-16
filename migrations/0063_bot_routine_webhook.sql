-- +goose Up
-- +goose StatementBegin
ALTER TABLE bot_routines ADD COLUMN IF NOT EXISTS trigger_token text;
UPDATE bot_routines
SET trigger_token = md5(random()::text || clock_timestamp()::text || id::text)
WHERE trigger_token IS NULL;
ALTER TABLE bot_routines ALTER COLUMN trigger_token SET NOT NULL;
CREATE UNIQUE INDEX IF NOT EXISTS bot_routines_trigger_token_idx
    ON bot_routines (trigger_token);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS bot_routines_trigger_token_idx;
ALTER TABLE bot_routines DROP COLUMN IF EXISTS trigger_token;
-- +goose StatementEnd
