-- name: CreateDocument :one
INSERT INTO documents (owner_id, title, content, kind, created_by_user_id)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: ListDocuments :many
SELECT * FROM documents WHERE owner_id = $1 ORDER BY updated_at DESC;

-- name: GetDocument :one
SELECT * FROM documents WHERE id = $1 AND owner_id = $2;

-- name: UpdateDocument :one
UPDATE documents
SET title = $3, content = $4, kind = $5, revision = revision + 1, updated_at = now()
WHERE id = $1 AND owner_id = $2
RETURNING *;

-- name: DeleteDocument :execrows
DELETE FROM documents WHERE id = $1 AND owner_id = $2;

-- name: CreateDocumentVersion :one
INSERT INTO document_versions (document_id, owner_id, title, content, kind, created_by_user_id)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: ListDocumentVersions :many
SELECT * FROM document_versions
WHERE document_id = $1 AND owner_id = $2
ORDER BY created_at DESC
LIMIT 50;

-- name: CreateDocumentEvent :one
INSERT INTO document_events (document_id, payload)
VALUES ($1, $2)
RETURNING id, created_at;

-- name: ListDocumentEventsAfter :many
SELECT id, document_id, payload, created_at
FROM document_events
WHERE document_id = $1 AND id > $2
ORDER BY id
LIMIT 100;

-- name: GetLatestDocumentEventID :one
SELECT COALESCE(MAX(id), 0)::bigint FROM document_events WHERE document_id = $1;

-- name: UpsertDocumentPresence :exec
INSERT INTO document_presence (document_id, client_id, actor, x, y, updated_at)
VALUES ($1, $2, $3, $4, $5, now())
ON CONFLICT (document_id, client_id)
DO UPDATE SET actor = EXCLUDED.actor, x = EXCLUDED.x, y = EXCLUDED.y, updated_at = now();

-- name: DeleteDocumentPresence :exec
DELETE FROM document_presence WHERE document_id = $1 AND client_id = $2;

-- name: ListDocumentPresence :many
SELECT document_id, client_id, actor, x, y, updated_at
FROM document_presence
WHERE document_id = $1 AND updated_at > now() - interval '15 seconds'
ORDER BY updated_at DESC;

-- name: DeleteStaleDocumentPresence :exec
DELETE FROM document_presence WHERE updated_at < now() - interval '1 minute';

-- name: SetDocumentAccess :exec
UPDATE documents
SET visibility = $3, created_by_user_id = COALESCE(created_by_user_id, $4), updated_at = now()
WHERE id = $1 AND owner_id = $2;
