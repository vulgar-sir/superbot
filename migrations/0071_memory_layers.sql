-- +goose Up
ALTER TABLE memories
    ADD COLUMN source text NOT NULL DEFAULT 'manual'
        CHECK (source IN ('manual', 'tool', 'review', 'import', 'tidy')),
    ADD COLUMN pinned boolean NOT NULL DEFAULT false,
    ADD COLUMN archived_at timestamptz,
    ADD COLUMN source_conversation_id uuid,
    ADD COLUMN source_ordinal bigint,
    ADD COLUMN updated_at timestamptz NOT NULL DEFAULT now();
UPDATE memories SET updated_at = created_at;
UPDATE memories SET source = 'import' WHERE metadata->>'imported' = 'true';
CREATE INDEX memories_active_scope_idx
    ON memories(owner_id, scope_type, scope_id, created_at DESC)
    WHERE archived_at IS NULL;

-- One row per conversation: how far automatic memory review has read.
-- last_reviewed_at is the time of the last attempt (successful or not) and
-- failures counts consecutive failed attempts, which drives retry backoff.
CREATE TABLE memory_review_cursors (
    conversation_id  uuid PRIMARY KEY REFERENCES conversations(id) ON DELETE CASCADE,
    bot_id           uuid,
    last_ordinal     bigint NOT NULL DEFAULT 0,
    last_reviewed_at timestamptz,
    failures         integer NOT NULL DEFAULT 0,
    updated_at       timestamptz NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE IF EXISTS memory_review_cursors;
DROP INDEX IF EXISTS memories_active_scope_idx;
ALTER TABLE memories
    DROP COLUMN IF EXISTS updated_at,
    DROP COLUMN IF EXISTS source_ordinal,
    DROP COLUMN IF EXISTS source_conversation_id,
    DROP COLUMN IF EXISTS archived_at,
    DROP COLUMN IF EXISTS pinned,
    DROP COLUMN IF EXISTS source;
