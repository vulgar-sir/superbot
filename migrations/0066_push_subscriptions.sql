-- +goose Up
-- +goose StatementBegin
CREATE TABLE push_subscriptions (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    account_id uuid NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    user_id    uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    endpoint   text NOT NULL UNIQUE,
    p256dh     text NOT NULL,
    auth       text NOT NULL,
    user_agent text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX push_subscriptions_account_idx ON push_subscriptions (account_id);
ALTER TABLE notifications ADD COLUMN IF NOT EXISTS pushed_at timestamptz;
CREATE INDEX notifications_unpushed_idx ON notifications (created_at) WHERE pushed_at IS NULL;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS notifications_unpushed_idx;
ALTER TABLE notifications DROP COLUMN IF EXISTS pushed_at;
DROP TABLE IF EXISTS push_subscriptions;
-- +goose StatementEnd
