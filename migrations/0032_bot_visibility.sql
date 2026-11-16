-- +goose Up
ALTER TABLE bots
    ADD COLUMN visibility text NOT NULL DEFAULT 'account'
    CHECK (visibility IN ('owner', 'account'));
ALTER TABLE bots
    ADD COLUMN created_by_user_id uuid REFERENCES users(id) ON DELETE SET NULL;

-- Personal account ids equal user ids, so the legacy owner is the creator.
UPDATE bots SET created_by_user_id = owner_id WHERE created_by_user_id IS NULL;

CREATE INDEX bots_visibility_idx ON bots(owner_id, visibility);

-- +goose Down
DROP INDEX IF EXISTS bots_visibility_idx;
ALTER TABLE bots DROP COLUMN IF EXISTS created_by_user_id;
ALTER TABLE bots DROP COLUMN IF EXISTS visibility;
