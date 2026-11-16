-- +goose Up
ALTER TABLE conversations
    ADD COLUMN direct_user_id uuid REFERENCES users(id) ON DELETE CASCADE;

UPDATE conversations
SET direct_user_id = created_by
WHERE kind = 'direct' AND direct_user_id IS NULL;

CREATE INDEX conversations_direct_user_idx
    ON conversations(direct_user_id, direct_bot_id)
    WHERE kind = 'direct';

-- +goose Down
DROP INDEX IF EXISTS conversations_direct_user_idx;
ALTER TABLE conversations DROP COLUMN IF EXISTS direct_user_id;
