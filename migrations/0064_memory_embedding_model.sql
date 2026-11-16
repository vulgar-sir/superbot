-- +goose Up
-- +goose StatementBegin
ALTER TABLE memories
    ADD COLUMN IF NOT EXISTS embedding_model text NOT NULL DEFAULT 'local-hash-1024';
CREATE INDEX IF NOT EXISTS memories_embedding_model_idx ON memories (owner_id, embedding_model);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS memories_embedding_model_idx;
ALTER TABLE memories DROP COLUMN IF EXISTS embedding_model;
-- +goose StatementEnd
