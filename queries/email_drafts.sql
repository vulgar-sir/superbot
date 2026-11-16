-- name: CreateEmailDraft :one
INSERT INTO email_drafts (owner_id, to_addr, cc_addr, subject, body)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: ListEmailDrafts :many
SELECT * FROM email_drafts
WHERE owner_id = $1
ORDER BY updated_at DESC
LIMIT $2;

-- name: GetEmailDraft :one
SELECT * FROM email_drafts WHERE id = $1 AND owner_id = $2;

-- name: UpdateEmailDraft :one
UPDATE email_drafts
SET to_addr = $3, cc_addr = $4, subject = $5, body = $6, updated_at = now()
WHERE id = $1 AND owner_id = $2
RETURNING *;

-- name: DeleteEmailDraft :execrows
DELETE FROM email_drafts WHERE id = $1 AND owner_id = $2;
