-- +goose Up
CREATE TABLE document_versions (
    id                 uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    document_id        uuid NOT NULL REFERENCES documents(id) ON DELETE CASCADE,
    owner_id           uuid NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    title              text NOT NULL,
    content            text NOT NULL DEFAULT '',
    kind               text NOT NULL,
    created_by_user_id uuid REFERENCES users(id) ON DELETE SET NULL,
    created_at         timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX document_versions_doc_idx ON document_versions(document_id, created_at DESC);

-- +goose Down
DROP TABLE IF EXISTS document_versions;
