-- +goose Up
CREATE TABLE diagnostic_events (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    account_id uuid REFERENCES accounts(id) ON DELETE SET NULL,
    user_id    uuid REFERENCES users(id) ON DELETE SET NULL,
    kind       text NOT NULL,
    message    text NOT NULL,
    stack      text NOT NULL DEFAULT '',
    url        text NOT NULL DEFAULT '',
    user_agent text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX diagnostic_events_created_idx ON diagnostic_events(created_at DESC);

-- +goose Down
DROP INDEX IF EXISTS diagnostic_events_created_idx;
DROP TABLE IF EXISTS diagnostic_events;
