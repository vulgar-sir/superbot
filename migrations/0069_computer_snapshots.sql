-- +goose Up
-- +goose StatementBegin
ALTER TABLE workspace_actions DROP CONSTRAINT IF EXISTS workspace_actions_kind_check;
ALTER TABLE workspace_actions ADD CONSTRAINT workspace_actions_kind_check
    CHECK (kind IN ('update', 'reset', 'snapshot', 'restore'));
ALTER TABLE workspace_actions ADD COLUMN IF NOT EXISTS snapshot_id uuid;

CREATE TABLE computer_snapshots (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    account_id   uuid NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    bot_id       uuid NOT NULL REFERENCES bots(id) ON DELETE CASCADE,
    label        text NOT NULL DEFAULT '',
    archive_path text NOT NULL,
    size_bytes   bigint NOT NULL DEFAULT 0,
    created_at   timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX computer_snapshots_bot_idx ON computer_snapshots (bot_id, created_at DESC);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS computer_snapshots;
ALTER TABLE workspace_actions DROP COLUMN IF EXISTS snapshot_id;
ALTER TABLE workspace_actions DROP CONSTRAINT IF EXISTS workspace_actions_kind_check;
ALTER TABLE workspace_actions ADD CONSTRAINT workspace_actions_kind_check CHECK (kind IN ('update', 'reset'));
-- +goose StatementEnd
