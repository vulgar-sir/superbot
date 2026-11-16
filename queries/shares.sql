-- name: CreateBotShareLink :one
INSERT INTO bot_share_links (token, owner_id, bot_id, template, expires_at, max_uses)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: GetBotShareLink :one
SELECT * FROM bot_share_links WHERE token = $1;

-- name: ListBotShareLinks :many
SELECT * FROM bot_share_links
WHERE owner_id = $1 AND bot_id = $2
ORDER BY created_at DESC;

-- name: RevokeBotShareLink :execrows
UPDATE bot_share_links
SET revoked_at = now()
WHERE token = $1 AND owner_id = $2 AND bot_id = $3 AND revoked_at IS NULL;

-- name: IncrementBotShareLinkUses :exec
UPDATE bot_share_links SET uses = uses + 1 WHERE token = $1;
