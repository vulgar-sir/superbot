-- name: CreateMeeting :one
INSERT INTO meetings (owner_id, title, transcript, starts_at, created_by_user_id)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: ListMeetings :many
SELECT id, owner_id, title, summary, starts_at, created_by_user_id, created_at, updated_at
FROM meetings WHERE owner_id = $1 ORDER BY starts_at DESC;

-- name: GetMeeting :one
SELECT * FROM meetings WHERE id = $1 AND owner_id = $2;

-- name: SetMeetingSummary :one
UPDATE meetings SET summary = $3, updated_at = now() WHERE id = $1 AND owner_id = $2 RETURNING *;

-- name: DeleteMeeting :execrows
DELETE FROM meetings WHERE id = $1 AND owner_id = $2;
