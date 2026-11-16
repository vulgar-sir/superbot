-- name: CreateConnector :one
INSERT INTO connectors (
    owner_id, provider, name, auth_type, access_token_enc, refresh_token_enc,
    config, status, expires_at
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
RETURNING *;

-- name: ListConnectors :many
SELECT * FROM connectors
WHERE owner_id = $1
ORDER BY created_at DESC;

-- name: GetConnector :one
SELECT * FROM connectors
WHERE id = $1 AND owner_id = $2;

-- name: GetConnectorByProvider :one
SELECT * FROM connectors
WHERE owner_id = $1 AND provider = $2 AND status = 'active'
ORDER BY created_at DESC
LIMIT 1;

-- name: SetConnectorStatus :one
UPDATE connectors
SET status = $3
WHERE id = $1 AND owner_id = $2
RETURNING *;

-- name: DeleteConnector :execrows
DELETE FROM connectors
WHERE id = $1 AND owner_id = $2;

-- name: GetConnectorByID :one
SELECT * FROM connectors WHERE id = $1;

-- name: UpdateConnectorTokens :one
UPDATE connectors
SET access_token_enc = $3,
    refresh_token_enc = $4,
    expires_at = $5,
    status = 'active'
WHERE id = $1 AND owner_id = $2
RETURNING *;
