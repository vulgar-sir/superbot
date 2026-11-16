-- +goose Up
ALTER TABLE projects
    ADD COLUMN visibility text NOT NULL DEFAULT 'account'
    CHECK (visibility IN ('owner', 'account'));
ALTER TABLE projects
    ADD COLUMN created_by_user_id uuid REFERENCES users(id) ON DELETE SET NULL;
UPDATE projects
SET created_by_user_id = owner_id
WHERE created_by_user_id IS NULL;

-- +goose Down
ALTER TABLE projects DROP COLUMN IF EXISTS created_by_user_id;
ALTER TABLE projects DROP COLUMN IF EXISTS visibility;
