-- +goose Up
-- +goose StatementBegin
CREATE TABLE bot_service_tokens (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    account_id   uuid NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    bot_id       uuid NOT NULL REFERENCES bots(id) ON DELETE CASCADE,
    name         text NOT NULL DEFAULT '',
    token_hash   text NOT NULL UNIQUE,
    token_prefix text NOT NULL DEFAULT '',
    created_by   uuid REFERENCES users(id) ON DELETE SET NULL,
    last_used_at timestamptz,
    revoked_at   timestamptz,
    created_at   timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX bot_service_tokens_bot_idx ON bot_service_tokens (bot_id, created_at DESC);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS bot_service_tokens;
-- +goose StatementEnd
