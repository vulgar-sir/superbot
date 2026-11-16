-- +goose Up
ALTER TABLE conversations DROP CONSTRAINT IF EXISTS conversations_kind_check;
ALTER TABLE conversations DROP CONSTRAINT IF EXISTS conversations_check;
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

CREATE TABLE agent_threads (
    id                     uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    owner_id               uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    conversation_id        uuid NOT NULL UNIQUE REFERENCES conversations(id) ON DELETE CASCADE,
    bot_a_id               uuid NOT NULL REFERENCES bots(id) ON DELETE CASCADE,
    bot_b_id               uuid NOT NULL REFERENCES bots(id) ON DELETE CASCADE,
    origin_conversation_id uuid REFERENCES conversations(id) ON DELETE SET NULL,
    created_at             timestamptz NOT NULL DEFAULT now(),
    updated_at             timestamptz NOT NULL DEFAULT now(),
    CHECK (bot_a_id <> bot_b_id)
);
CREATE UNIQUE INDEX agent_threads_pair_idx
    ON agent_threads (owner_id, LEAST(bot_a_id, bot_b_id), GREATEST(bot_a_id, bot_b_id));
CREATE INDEX agent_threads_owner_idx ON agent_threads(owner_id, updated_at DESC);

CREATE TRIGGER agent_threads_set_updated_at
BEFORE UPDATE ON agent_threads
FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TABLE activities (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    owner_id        uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    bot_id          uuid NOT NULL REFERENCES bots(id) ON DELETE CASCADE,
    kind            text NOT NULL DEFAULT 'update'
                    CHECK (kind IN ('update', 'message', 'agent_message', 'agent_reply', 'routine', 'system')),
    summary         text NOT NULL,
    conversation_id uuid REFERENCES conversations(id) ON DELETE CASCADE,
    message_id      uuid REFERENCES messages(id) ON DELETE SET NULL,
    agent_thread_id uuid REFERENCES agent_threads(id) ON DELETE SET NULL,
    read_at         timestamptz,
    created_at      timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX activities_bot_idx ON activities(owner_id, bot_id, created_at DESC);
CREATE INDEX activities_unread_idx ON activities(owner_id, created_at DESC) WHERE read_at IS NULL;

-- +goose Down
DROP TABLE IF EXISTS activities;
DROP TRIGGER IF EXISTS agent_threads_set_updated_at ON agent_threads;
DROP TABLE IF EXISTS agent_threads;
ALTER TABLE conversations DROP CONSTRAINT IF EXISTS conversations_scope_check;
ALTER TABLE conversations DROP CONSTRAINT IF EXISTS conversations_kind_check;
ALTER TABLE conversations
    ADD CONSTRAINT conversations_kind_check
    CHECK (kind IN ('team', 'direct'));
ALTER TABLE conversations
    ADD CONSTRAINT conversations_check
    CHECK (
        (kind = 'team' AND team_id IS NOT NULL AND direct_bot_id IS NULL)
        OR (kind = 'direct' AND direct_bot_id IS NOT NULL AND team_id IS NULL)
    );
