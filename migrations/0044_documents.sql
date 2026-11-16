-- +goose Up
CREATE TABLE documents (
    id                 uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    owner_id           uuid NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    title              text NOT NULL,
    content            text NOT NULL DEFAULT '',
    kind               text NOT NULL DEFAULT 'document' CHECK (kind IN ('document', 'canvas')),
    created_by_user_id uuid REFERENCES users(id) ON DELETE SET NULL,
    created_at         timestamptz NOT NULL DEFAULT now(),
    updated_at         timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX documents_owner_idx ON documents(owner_id, updated_at DESC);

-- +goose Down
DROP TABLE IF EXISTS documents;
