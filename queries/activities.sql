-- name: CreateActivity :one
INSERT INTO activities (
    owner_id, bot_id, kind, summary, conversation_id, message_id, agent_thread_id
)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING *;

-- name: ListActivitiesForOwner :many
SELECT sqlc.embed(activities), bots.name AS bot_name
FROM activities
JOIN bots ON bots.id = activities.bot_id
WHERE activities.owner_id = $1
ORDER BY activities.created_at DESC
LIMIT $2;

-- name: ListActivitiesForBot :many
SELECT sqlc.embed(activities), bots.name AS bot_name
FROM activities
JOIN bots ON bots.id = activities.bot_id
WHERE activities.owner_id = $1 AND activities.bot_id = $2
ORDER BY activities.created_at DESC
LIMIT $3;

-- name: GetLatestActivity :one
SELECT * FROM activities
WHERE owner_id = $1 AND bot_id = $2
ORDER BY created_at DESC
LIMIT 1;

-- name: CountUnreadActivities :one
SELECT count(*) FROM activities
WHERE owner_id = $1 AND bot_id = $2 AND read_at IS NULL;

-- name: ListBotActivitySummaries :many
SELECT
    b.id AS bot_id,
    COALESCE((
        SELECT a.summary FROM activities a
        WHERE a.owner_id = b.owner_id AND a.bot_id = b.id
        ORDER BY a.created_at DESC
        LIMIT 1
    ), ''::text)::text AS latest_summary,
    COALESCE((
        SELECT a.created_at FROM activities a
        WHERE a.owner_id = b.owner_id AND a.bot_id = b.id
        ORDER BY a.created_at DESC
        LIMIT 1
    ), b.created_at) AS latest_at,
    (
        SELECT count(*) FROM activities a
        WHERE a.owner_id = b.owner_id AND a.bot_id = b.id AND a.read_at IS NULL
    ) AS unread_count
FROM bots b
WHERE b.owner_id = $1;

-- name: MarkActivitiesRead :exec
UPDATE activities
SET read_at = now()
WHERE owner_id = $1 AND bot_id = $2 AND read_at IS NULL;

-- name: MarkAllActivitiesRead :exec
UPDATE activities
SET read_at = now()
WHERE owner_id = $1 AND read_at IS NULL;

-- name: SearchActivities :many
SELECT sqlc.embed(activities), bots.name AS bot_name
FROM activities
JOIN bots ON bots.id = activities.bot_id
WHERE activities.owner_id = sqlc.arg(owner_id)
  AND activities.summary ILIKE '%' || sqlc.arg(query) || '%'
ORDER BY activities.created_at DESC
LIMIT sqlc.arg(row_limit);

-- name: MarkBotActivitiesUnread :exec
UPDATE activities
SET read_at = NULL
WHERE owner_id = $1 AND bot_id = $2;
