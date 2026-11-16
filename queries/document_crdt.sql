-- name: CreateDocumentCRDTUpdate :one
INSERT INTO document_crdt_updates (document_id, update_data)
VALUES ($1, $2)
RETURNING id, document_id, created_at;

-- name: ListDocumentCRDTUpdates :many
SELECT update_data FROM document_crdt_updates
WHERE document_id = $1
ORDER BY id
LIMIT 20000;

-- name: SeedDocumentCRDTUpdate :one
INSERT INTO document_crdt_updates (document_id, update_data)
SELECT $1, $2
WHERE NOT EXISTS (
    SELECT 1 FROM document_crdt_updates WHERE document_id = $1
)
RETURNING id, document_id, created_at;
