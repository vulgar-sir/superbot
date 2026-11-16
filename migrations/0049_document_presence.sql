-- +goose Up
CREATE TABLE document_presence (
    document_id uuid NOT NULL REFERENCES documents(id) ON DELETE CASCADE,
    client_id   text NOT NULL,
    actor       text NOT NULL DEFAULT '',
    x           double precision NOT NULL DEFAULT 0,
    y           double precision NOT NULL DEFAULT 0,
    updated_at  timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (document_id, client_id)
);
CREATE INDEX document_presence_doc_idx ON document_presence(document_id, updated_at);

-- +goose Down
DROP TABLE IF EXISTS document_presence;
