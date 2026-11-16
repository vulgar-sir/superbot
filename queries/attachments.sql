-- name: CreateAttachment :one
INSERT INTO attachments (owner_id, filename, mime_type, size_bytes, content)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: GetAttachmentForOwner :one
SELECT * FROM attachments WHERE id = $1 AND owner_id = $2;

-- name: AttachAttachmentToMessage :exec
UPDATE attachments
SET message_id = $3
WHERE id = $1 AND owner_id = $2 AND message_id IS NULL;

-- name: ListAttachmentsForMessages :many
SELECT * FROM attachments
WHERE message_id = ANY($1::uuid[])
ORDER BY created_at;

-- name: ListAttachmentMetaForMessages :many
SELECT id, owner_id, message_id, filename, mime_type, size_bytes, created_at
FROM attachments
WHERE message_id = ANY($1::uuid[])
ORDER BY created_at;
