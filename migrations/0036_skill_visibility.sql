-- +goose Up
ALTER TABLE skills
    ADD COLUMN visibility text NOT NULL DEFAULT 'account'
    CHECK (visibility IN ('owner', 'account'));
ALTER TABLE skills
    ADD COLUMN created_by_user_id uuid REFERENCES users(id) ON DELETE SET NULL;
UPDATE skills SET created_by_user_id = owner_id WHERE created_by_user_id IS NULL;
CREATE INDEX skills_owner_visibility_idx ON skills(owner_id, visibility);

-- +goose Down
DROP INDEX IF EXISTS skills_owner_visibility_idx;
ALTER TABLE skills DROP COLUMN IF EXISTS created_by_user_id;
ALTER TABLE skills DROP COLUMN IF EXISTS visibility;
