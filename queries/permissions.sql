-- name: GetUserAutoReview :one
SELECT auto_review FROM users WHERE id = $1;

-- name: SetUserAutoReview :exec
UPDATE users SET auto_review = $2 WHERE id = $1;

-- name: ListPermissionRules :many
SELECT * FROM permission_rules WHERE owner_id = $1 ORDER BY created_at;

-- name: CreatePermissionRule :one
INSERT INTO permission_rules (owner_id, kind, pattern)
VALUES ($1, $2, $3)
RETURNING *;

-- name: DeletePermissionRule :exec
DELETE FROM permission_rules WHERE id = $1 AND owner_id = $2;
