-- name: CreateMcpServer :one
INSERT INTO mcp_servers (
    owner_id, name, transport, command, args, env_enc, url, headers_enc, status, created_by, config
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
RETURNING *;

-- name: ListMcpServers :many
SELECT * FROM mcp_servers
WHERE owner_id = $1
ORDER BY created_at DESC;

-- name: GetMcpServer :one
SELECT * FROM mcp_servers
WHERE id = $1 AND owner_id = $2;

-- name: SetMcpServerStatus :one
UPDATE mcp_servers
SET status = $3, updated_at = now()
WHERE id = $1 AND owner_id = $2
RETURNING *;

-- name: DeleteMcpServer :execrows
DELETE FROM mcp_servers
WHERE id = $1 AND owner_id = $2;

-- name: UpsertBotMcpServer :exec
INSERT INTO bot_mcp_servers (bot_id, mcp_server_id, enabled)
VALUES ($1, $2, true)
ON CONFLICT (bot_id, mcp_server_id)
DO UPDATE SET enabled = true;

-- name: ClearBotMcpServers :exec
DELETE FROM bot_mcp_servers WHERE bot_id = $1;

-- name: ListEnabledMcpServers :many
SELECT mcp_servers.*
FROM bot_mcp_servers
JOIN mcp_servers ON mcp_servers.id = bot_mcp_servers.mcp_server_id
WHERE bot_mcp_servers.bot_id = $1
  AND bot_mcp_servers.enabled = true
  AND mcp_servers.status = 'active'
ORDER BY mcp_servers.name;

-- name: ListBotMcpServers :many
SELECT sqlc.embed(mcp_servers), bot_mcp_servers.enabled
FROM bot_mcp_servers
JOIN mcp_servers ON mcp_servers.id = bot_mcp_servers.mcp_server_id
WHERE bot_mcp_servers.bot_id = $1
ORDER BY mcp_servers.name;
