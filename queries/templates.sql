-- name: CreateBotTemplate :one
INSERT INTO bot_templates (owner_id, name, description, version, bot_id, template, visibility, created_by_user_id)
VALUES ($1, $2, $3, 1, $4, $5, $6, $7)
RETURNING *;

-- name: ListBotTemplates :many
SELECT * FROM bot_templates
WHERE owner_id = $1
ORDER BY created_at DESC;

-- name: GetBotTemplateForOwner :one
SELECT * FROM bot_templates WHERE id = $1 AND owner_id = $2;

-- name: DeleteBotTemplate :execrows
DELETE FROM bot_templates WHERE id = $1 AND owner_id = $2;

-- name: GetPublicBotTemplate :one
SELECT * FROM bot_templates
WHERE id = $1 AND visibility = 'public' AND status = 'published';

-- name: ListStoreBotTemplates :many
SELECT t.*, COALESCE(a.name, '') AS owner_name
FROM bot_templates t
JOIN accounts a ON a.id = t.owner_id
WHERE t.visibility = 'public' AND t.status = 'published'
ORDER BY t.install_count DESC, t.updated_at DESC
LIMIT $1 OFFSET $2;

-- name: IncrementBotTemplateInstall :exec
UPDATE bot_templates SET install_count = install_count + 1 WHERE id = $1;
