-- name: CreateNotification :one
INSERT INTO notifications (owner_id, kind, title, body, resource_type, resource_id, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING *;

-- name: ListNotifications :many
SELECT * FROM notifications
WHERE owner_id = $1
  AND (sqlc.narg(unread_only)::boolean IS NOT TRUE OR read_at IS NULL)
ORDER BY created_at DESC
LIMIT $2;

-- name: CountUnreadNotifications :one
SELECT count(*) FROM notifications
WHERE owner_id = $1 AND read_at IS NULL;

-- name: MarkNotificationRead :execrows
UPDATE notifications
SET read_at = now()
WHERE id = $1 AND owner_id = $2 AND read_at IS NULL;

-- name: MarkAllNotificationsRead :exec
UPDATE notifications
SET read_at = now()
WHERE owner_id = $1 AND read_at IS NULL;

-- name: DeleteOldNotifications :exec
DELETE FROM notifications
WHERE read_at IS NOT NULL AND read_at < now() - interval '30 days';
