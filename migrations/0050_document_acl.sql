-- +goose Up
ALTER TABLE documents
    ADD COLUMN visibility text NOT NULL DEFAULT 'account'
    CHECK (visibility IN ('owner', 'account'));
CREATE INDEX documents_owner_visibility_idx ON documents(owner_id, visibility);
UPDATE documents SET created_by_user_id = owner_id WHERE created_by_user_id IS NULL;

-- +goose Down
DROP INDEX IF EXISTS documents_owner_visibility_idx;
ALTER TABLE documents DROP COLUMN IF EXISTS visibility;
