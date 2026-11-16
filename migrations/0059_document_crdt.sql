-- +goose Up
CREATE TABLE document_crdt_updates (
    id          bigserial PRIMARY KEY,
    document_id uuid NOT NULL REFERENCES documents(id) ON DELETE CASCADE,
    update_data bytea NOT NULL,
    created_at  timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX document_crdt_updates_document_idx ON document_crdt_updates(document_id, id);

-- +goose Down
DROP INDEX IF EXISTS document_crdt_updates_document_idx;
DROP TABLE IF EXISTS document_crdt_updates;
