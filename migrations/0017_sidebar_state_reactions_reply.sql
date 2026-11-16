-- +goose Up
ALTER TABLE bots ADD COLUMN hidden boolean NOT NULL DEFAULT false;
ALTER TABLE teams ADD COLUMN hidden boolean NOT NULL DEFAULT false;
ALTER TABLE messages ADD COLUMN parent_message_id uuid REFERENCES messages(id) ON DELETE SET NULL;
CREATE INDEX messages_parent_idx ON messages(parent_message_id);

CREATE TABLE message_reactions (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    message_id uuid NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
    user_id    uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    emoji      text NOT NULL CHECK (char_length(emoji) BETWEEN 1 AND 32),
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (message_id, user_id, emoji)
);
CREATE INDEX message_reactions_message_idx ON message_reactions(message_id);

-- +goose Down
DROP TABLE IF EXISTS message_reactions;
DROP INDEX IF EXISTS messages_parent_idx;
ALTER TABLE messages DROP COLUMN IF EXISTS parent_message_id;
ALTER TABLE teams DROP COLUMN IF EXISTS hidden;
ALTER TABLE bots DROP COLUMN IF EXISTS hidden;
