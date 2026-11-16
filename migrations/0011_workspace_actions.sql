-- +goose Up
CREATE TABLE workspace_actions (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    owner_id     uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    bot_id       uuid NOT NULL REFERENCES bots(id) ON DELETE CASCADE,
    kind         text NOT NULL CHECK (kind IN ('update', 'reset')),
    status       text NOT NULL DEFAULT 'pending'
                 CHECK (status IN ('pending', 'running', 'done', 'failed')),
    error        text NOT NULL DEFAULT '',
    created_at   timestamptz NOT NULL DEFAULT now(),
    processed_at timestamptz
);
CREATE INDEX workspace_actions_pending_idx
    ON workspace_actions(created_at)
    WHERE status = 'pending';

-- +goose Down
DROP TABLE IF EXISTS workspace_actions;
