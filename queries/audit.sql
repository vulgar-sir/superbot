-- name: CreateAuditLog :one
INSERT INTO audit_logs (account_id, actor_type, actor_id, action, target_type, target_id, payload)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING *;

-- name: ListAuditLogs :many
SELECT * FROM audit_logs
WHERE account_id = $1
ORDER BY created_at DESC
LIMIT $2 OFFSET $3;
