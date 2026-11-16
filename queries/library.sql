-- name: CreateLibraryItem :one
INSERT INTO library_items (
    owner_id, bot_id, kind, title, description, content, mime_type,
    file_path, conversation_id, message_id, metadata
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
RETURNING *;

-- name: ListLibraryItemsForBot :many
SELECT * FROM library_items
WHERE owner_id = $1 AND bot_id = $2
ORDER BY created_at DESC
LIMIT $3;

-- name: GetLibraryItem :one
SELECT * FROM library_items
WHERE id = $1 AND owner_id = $2;

-- name: DeleteLibraryItem :execrows
DELETE FROM library_items
WHERE id = $1 AND owner_id = $2;

-- name: GetLibraryItemByFilePath :one
SELECT * FROM library_items
WHERE bot_id = $1 AND file_path = $2;

-- name: GetLibraryItemBySource :one
SELECT * FROM library_items
WHERE bot_id = $1 AND metadata->>'source' = sqlc.arg(source)::text AND metadata->>'source_id' = sqlc.arg(source_id)::text;

-- name: DeleteLibraryItemsBySource :execrows
DELETE FROM library_items
WHERE owner_id = $1 AND metadata->>'source' = sqlc.arg(source)::text AND metadata->>'source_id' = sqlc.arg(source_id)::text;

-- name: UpdateLibraryItemsBySource :execrows
UPDATE library_items
SET title = sqlc.arg(title)::text, content = sqlc.arg(content)::text, updated_at = now()
WHERE owner_id = sqlc.arg(owner_id) AND metadata->>'source' = sqlc.arg(source)::text AND metadata->>'source_id' = sqlc.arg(source_id)::text;
