-- name: GetGroupGoal :one
SELECT * FROM group_goals WHERE conversation_id = $1;

-- name: GetGroupGoalForUpdate :one
SELECT * FROM group_goals WHERE conversation_id = $1 FOR UPDATE;

-- name: UpsertGroupGoal :one
INSERT INTO group_goals (conversation_id, owner_id, status, state)
VALUES ($1, $2, $3, $4)
ON CONFLICT (conversation_id)
DO UPDATE SET status = EXCLUDED.status, state = EXCLUDED.state, updated_at = now()
RETURNING *;

-- name: SetGroupGoalNudge :exec
UPDATE group_goals
SET state = $2, last_nudge_ordinal = $3, updated_at = now()
WHERE conversation_id = $1;

-- name: CreateSideChat :one
INSERT INTO side_chats (conversation_id, owner_id, bot_id, question)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: FinishSideChat :one
UPDATE side_chats
SET answer = $2, status = $3, error = $4, answered_at = now()
WHERE id = $1
RETURNING *;

-- name: ListSideChats :many
SELECT * FROM side_chats
WHERE conversation_id = $1 AND owner_id = $2
ORDER BY created_at DESC
LIMIT $3;

-- name: DeleteSideChat :execrows
DELETE FROM side_chats WHERE id = $1 AND owner_id = $2;

-- A crash can leave a side question marked as still being answered.
-- name: FailStaleSideChats :execrows
UPDATE side_chats
SET status = 'failed', error = '服务重启，未完成回答', answered_at = now()
WHERE status = 'answering' AND created_at < now() - interval '5 minutes';
