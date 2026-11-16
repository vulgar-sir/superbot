-- +goose Up
CREATE TABLE calendar_events (
    id                 uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    owner_id           uuid NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    title              text NOT NULL,
    description        text NOT NULL DEFAULT '',
    location           text NOT NULL DEFAULT '',
    starts_at          timestamptz NOT NULL,
    ends_at            timestamptz NOT NULL,
    created_by_user_id uuid REFERENCES users(id) ON DELETE SET NULL,
    created_at         timestamptz NOT NULL DEFAULT now(),
    updated_at         timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX calendar_events_owner_start_idx ON calendar_events(owner_id, starts_at);

-- +goose Down
DROP TABLE IF EXISTS calendar_events;
