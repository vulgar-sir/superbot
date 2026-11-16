-- +goose Up
-- One goal per team conversation. The goal itself is a small state machine
-- kept as JSON (see internal/goals); only what the scheduler needs to filter
-- on is lifted into columns.
CREATE TABLE group_goals (
    id                 uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    conversation_id    uuid NOT NULL UNIQUE REFERENCES conversations(id) ON DELETE CASCADE,
    owner_id           uuid NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    status             text NOT NULL DEFAULT 'drafting'
                       CHECK (status IN ('drafting', 'active', 'paused', 'complete', 'stopped')),
    state              jsonb NOT NULL DEFAULT '{}'::jsonb,
    last_nudge_ordinal bigint NOT NULL DEFAULT 0,
    created_at         timestamptz NOT NULL DEFAULT now(),
    updated_at         timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX group_goals_owner_idx ON group_goals(owner_id, status);

-- Side questions are answered from a snapshot of the conversation and never
-- become part of it, so they live in their own table.
CREATE TABLE side_chats (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    conversation_id uuid NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
    owner_id        uuid NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    bot_id          uuid REFERENCES bots(id) ON DELETE SET NULL,
    question        text NOT NULL,
    answer          text NOT NULL DEFAULT '',
    status          text NOT NULL DEFAULT 'answering'
                    CHECK (status IN ('answering', 'answered', 'failed')),
    error           text NOT NULL DEFAULT '',
    created_at      timestamptz NOT NULL DEFAULT now(),
    answered_at     timestamptz
);
CREATE INDEX side_chats_conversation_idx ON side_chats(conversation_id, created_at);

-- +goose Down
DROP TABLE IF EXISTS side_chats;
DROP TABLE IF EXISTS group_goals;
