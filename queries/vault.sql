-- name: CreateVaultItem :one
INSERT INTO vault_items (owner_id, kind, name, username, password_enc, url, notes_enc, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
RETURNING *;

-- name: ListVaultItems :many
SELECT * FROM vault_items
WHERE owner_id = $1
ORDER BY created_at DESC;

-- name: GetVaultItem :one
SELECT * FROM vault_items
WHERE id = $1 AND owner_id = $2;

-- name: UpdateVaultItem :one
UPDATE vault_items
SET kind = $3,
    name = $4,
    username = $5,
    password_enc = $6,
    url = $7,
    notes_enc = $8
WHERE id = $1 AND owner_id = $2
RETURNING *;

-- name: DeleteVaultItem :execrows
DELETE FROM vault_items
WHERE id = $1 AND owner_id = $2;
