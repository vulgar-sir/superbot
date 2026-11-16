-- name: CreatePushSubscription :one
INSERT INTO push_subscriptions (account_id, user_id, endpoint, p256dh, auth, user_agent)
VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (endpoint) DO UPDATE
SET account_id = EXCLUDED.account_id, user_id = EXCLUDED.user_id,
    p256dh = EXCLUDED.p256dh, auth = EXCLUDED.auth, user_agent = EXCLUDED.user_agent
RETURNING *;

-- name: DeletePushSubscription :execrows
DELETE FROM push_subscriptions WHERE endpoint = $1 AND user_id = $2;

-- name: ListPushSubscriptionsByAccount :many
SELECT * FROM push_subscriptions WHERE account_id = $1;

-- name: ListPushSubscriptionsByUser :many
SELECT * FROM push_subscriptions WHERE user_id = $1;

-- name: ListUnpushedNotifications :many
SELECT * FROM notifications
WHERE pushed_at IS NULL AND created_at > now() - interval '1 hour'
ORDER BY created_at
LIMIT $1;

-- name: MarkNotificationPushed :exec
UPDATE notifications SET pushed_at = now() WHERE id = $1;
