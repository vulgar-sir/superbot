-- +goose Up
CREATE TABLE notifications (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    owner_id      uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    kind          text NOT NULL,
    title         text NOT NULL,
    body          text NOT NULL DEFAULT '',
    resource_type text NOT NULL DEFAULT '',
    resource_id   text NOT NULL DEFAULT '',
    metadata      jsonb NOT NULL DEFAULT '{}'::jsonb,
    read_at       timestamptz,
    created_at    timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX notifications_owner_created_idx
    ON notifications(owner_id, created_at DESC);
CREATE INDEX notifications_owner_unread_idx
    ON notifications(owner_id, created_at DESC)
    WHERE read_at IS NULL;

-- +goose Down
DROP TABLE IF EXISTS notifications;
