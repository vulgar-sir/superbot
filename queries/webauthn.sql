-- name: CreateWebAuthnCredential :one
INSERT INTO webauthn_credentials (user_id, credential_id, credential)
VALUES ($1, $2, $3)
ON CONFLICT (credential_id) DO UPDATE SET credential = EXCLUDED.credential
RETURNING *;

-- name: ListWebAuthnCredentials :many
SELECT * FROM webauthn_credentials
WHERE user_id = $1
ORDER BY created_at DESC;

-- name: UpdateWebAuthnCredential :exec
UPDATE webauthn_credentials
SET credential = $3
WHERE user_id = $1 AND credential_id = $2;

-- name: DeleteWebAuthnCredential :execrows
DELETE FROM webauthn_credentials
WHERE id = $1 AND user_id = $2;

-- name: RenameWebAuthnCredential :one
UPDATE webauthn_credentials
SET name = $3
WHERE id = $1 AND user_id = $2
RETURNING *;
