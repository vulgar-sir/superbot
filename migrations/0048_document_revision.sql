-- +goose Up
ALTER TABLE documents ADD COLUMN revision bigint NOT NULL DEFAULT 1;

CREATE TABLE document_events (
    id          bigserial PRIMARY KEY,
    document_id uuid NOT NULL REFERENCES documents(id) ON DELETE CASCADE,
    payload     jsonb NOT NULL,
    created_at  timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX document_events_doc_idx ON document_events(document_id, id);

-- +goose Down
DROP TABLE IF EXISTS document_events;
ALTER TABLE documents DROP COLUMN IF EXISTS revision;
