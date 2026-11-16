-- name: CreateBotServiceToken :one
INSERT INTO bot_service_tokens (account_id, bot_id, name, token_hash, token_prefix, created_by)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: ListBotServiceTokens :many
SELECT * FROM bot_service_tokens
WHERE account_id = $1 AND bot_id = $2
ORDER BY created_at DESC;

-- name: RevokeBotServiceToken :execrows
UPDATE bot_service_tokens
SET revoked_at = now()
WHERE id = $1 AND account_id = $2 AND bot_id = $3 AND revoked_at IS NULL;

-- name: GetBotServiceTokenByHash :one
SELECT * FROM bot_service_tokens
WHERE token_hash = $1 AND revoked_at IS NULL;

-- name: TouchBotServiceToken :exec
UPDATE bot_service_tokens
SET last_used_at = now()
WHERE id = $1;
