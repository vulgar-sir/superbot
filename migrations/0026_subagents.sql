-- +goose Up
ALTER TABLE conversations DROP CONSTRAINT IF EXISTS conversations_kind_check;
ALTER TABLE conversations DROP CONSTRAINT IF EXISTS conversations_scope_check;
ALTER TABLE conversations
    ADD CONSTRAINT conversations_kind_check
    CHECK (kind IN ('team', 'direct', 'agent', 'subagent'));
ALTER TABLE conversations
    ADD CONSTRAINT conversations_scope_check
    CHECK (
        (kind = 'team' AND team_id IS NOT NULL AND direct_bot_id IS NULL)
        OR (kind = 'direct' AND direct_bot_id IS NOT NULL AND team_id IS NULL)
        OR (kind = 'agent' AND team_id IS NULL AND direct_bot_id IS NULL)
        OR (kind = 'subagent' AND team_id IS NULL AND direct_bot_id IS NOT NULL)
    );

CREATE TABLE subagent_tasks (
    id                    uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    owner_id              uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    parent_conversation_id uuid NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
    parent_cycle_id       uuid NOT NULL REFERENCES cycles(id) ON DELETE CASCADE,
    parent_bot_id         uuid NOT NULL REFERENCES bots(id) ON DELETE CASCADE,
    child_conversation_id uuid NOT NULL UNIQUE REFERENCES conversations(id) ON DELETE CASCADE,
    child_cycle_id        uuid REFERENCES cycles(id) ON DELETE SET NULL,
    objective             text NOT NULL,
    status                text NOT NULL DEFAULT 'queued'
                          CHECK (status IN ('queued', 'running', 'succeeded', 'failed', 'canceled', 'timed_out')),
    result_summary        text NOT NULL DEFAULT '',
    error                 text NOT NULL DEFAULT '',
    created_at            timestamptz NOT NULL DEFAULT now(),
    updated_at            timestamptz NOT NULL DEFAULT now(),
    finished_at           timestamptz
);

CREATE INDEX subagent_tasks_parent_idx ON subagent_tasks(parent_conversation_id, created_at DESC);
CREATE INDEX subagent_tasks_child_cycle_idx ON subagent_tasks(child_cycle_id);
CREATE INDEX subagent_tasks_owner_active_idx ON subagent_tasks(owner_id, status)
    WHERE status IN ('queued', 'running');

-- +goose Down
DROP TABLE IF EXISTS subagent_tasks;
ALTER TABLE conversations DROP CONSTRAINT IF EXISTS conversations_scope_check;
ALTER TABLE conversations DROP CONSTRAINT IF EXISTS conversations_kind_check;
ALTER TABLE conversations
    ADD CONSTRAINT conversations_kind_check
    CHECK (kind IN ('team', 'direct', 'agent'));
ALTER TABLE conversations
    ADD CONSTRAINT conversations_scope_check
    CHECK (
        (kind = 'team' AND team_id IS NOT NULL AND direct_bot_id IS NULL)
        OR (kind = 'direct' AND direct_bot_id IS NOT NULL AND team_id IS NULL)
        OR (kind = 'agent' AND team_id IS NULL AND direct_bot_id IS NULL)
    );
