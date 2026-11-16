-- +goose Up
ALTER TABLE bots
    ADD COLUMN kind text NOT NULL DEFAULT 'personal'
        CHECK (kind IN ('personal', 'team')),
    ADD COLUMN created_by_bot_id uuid REFERENCES bots(id) ON DELETE SET NULL;

ALTER TABLE users
    ADD COLUMN primary_bot_id uuid REFERENCES bots(id) ON DELETE SET NULL;

-- Existing accounts keep their oldest personal Bot as the primary Bot.
UPDATE users u
SET primary_bot_id = (
    SELECT b.id
    FROM bots b
    WHERE b.owner_id = u.id AND b.kind = 'personal'
    ORDER BY b.created_at
    LIMIT 1
)
WHERE u.primary_bot_id IS NULL;

CREATE INDEX users_primary_bot_idx ON users(primary_bot_id);
CREATE INDEX bots_created_by_idx ON bots(created_by_bot_id);

-- +goose Down
DROP INDEX IF EXISTS bots_created_by_idx;
DROP INDEX IF EXISTS users_primary_bot_idx;
ALTER TABLE users DROP COLUMN IF EXISTS primary_bot_id;
ALTER TABLE bots
    DROP COLUMN IF EXISTS created_by_bot_id,
    DROP COLUMN IF EXISTS kind;
