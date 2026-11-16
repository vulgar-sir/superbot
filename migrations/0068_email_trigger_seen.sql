-- +goose Up
-- +goose StatementBegin
CREATE TABLE email_trigger_seen (
    message_id text PRIMARY KEY,
    account_id uuid NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    seen_at    timestamptz NOT NULL DEFAULT now()
);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS email_trigger_seen;
-- +goose StatementEnd
