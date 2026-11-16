-- name: CreateProvider :one
INSERT INTO providers (owner_id, type, kind, name, base_url, api_key_enc, config, enabled)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
RETURNING *;

-- name: ListProviders :many
SELECT * FROM providers
WHERE owner_id = $1
ORDER BY created_at;

-- name: ListProvidersByType :many
SELECT * FROM providers
WHERE owner_id = $1 AND type = $2
ORDER BY created_at;

-- name: GetProvider :one
SELECT * FROM providers
WHERE id = $1 AND owner_id = $2;

-- name: UpdateProvider :one
UPDATE providers
SET name = $3,
    base_url = $4,
    api_key_enc = $5,
    config = $6,
    enabled = $7
WHERE id = $1 AND owner_id = $2
RETURNING *;

-- name: DeleteProvider :execrows
DELETE FROM providers
WHERE id = $1 AND owner_id = $2;
