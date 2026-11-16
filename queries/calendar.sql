-- name: CreateCalendarEvent :one
INSERT INTO calendar_events (owner_id, title, description, location, starts_at, ends_at, created_by_user_id)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING *;

-- name: ListCalendarEvents :many
SELECT * FROM calendar_events
WHERE owner_id = $1 AND ends_at >= $2
ORDER BY starts_at
LIMIT 200;

-- name: DeleteCalendarEvent :execrows
DELETE FROM calendar_events WHERE id = $1 AND owner_id = $2;
