-- +goose Up
ALTER TABLE messages
    ADD COLUMN edited_at timestamptz,
    ADD COLUMN deleted_at timestamptz,
    ADD COLUMN pinned_at timestamptz;

CREATE INDEX messages_pinned_idx
    ON messages(conversation_id, pinned_at DESC)
    WHERE pinned_at IS NOT NULL AND deleted_at IS NULL;

-- +goose Down
DROP INDEX IF EXISTS messages_pinned_idx;
ALTER TABLE messages
    DROP COLUMN IF EXISTS pinned_at,
    DROP COLUMN IF EXISTS deleted_at,
    DROP COLUMN IF EXISTS edited_at;
