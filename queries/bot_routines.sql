-- name: CreateBotRoutine :one
INSERT INTO bot_routines (
    owner_id, bot_id, name, schedule, timezone, timing, enabled, paused, prompt, next_run_at,
    trigger_token
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
RETURNING *;

-- name: ListBotRoutinesByBot :many
SELECT * FROM bot_routines
WHERE owner_id = $1 AND bot_id = $2
ORDER BY created_at DESC;

-- name: ListBotRoutinesByOwner :many
SELECT * FROM bot_routines
WHERE owner_id = $1
ORDER BY created_at DESC;

-- name: GetBotRoutineForOwner :one
SELECT * FROM bot_routines
WHERE id = $1 AND owner_id = $2;

-- name: UpdateBotRoutine :one
UPDATE bot_routines
SET name = $3,
    schedule = $4,
    timezone = $5,
    timing = $6,
    enabled = $7,
    paused = $8,
    prompt = $9,
    next_run_at = $10
WHERE id = $1 AND owner_id = $2
RETURNING *;

-- name: DeleteBotRoutine :execrows
DELETE FROM bot_routines
WHERE id = $1 AND owner_id = $2;

-- name: ListDueBotRoutines :many
SELECT * FROM bot_routines
WHERE enabled = true AND paused = false
  AND schedule NOT LIKE 'email:%'
  AND (next_run_at IS NULL OR next_run_at <= now())
ORDER BY next_run_at
LIMIT 20;

-- name: MarkBotRoutineRun :one
UPDATE bot_routines
SET last_run_at = now(),
    next_run_at = $2
WHERE id = $1
RETURNING *;

-- name: DisableBotRoutine :execrows
UPDATE bot_routines
SET enabled = false, next_run_at = NULL
WHERE id = $1;

-- name: GetBotRoutineByTriggerToken :one
SELECT * FROM bot_routines
WHERE trigger_token = $1;

-- name: ListEmailTriggerRoutines :many
SELECT * FROM bot_routines
WHERE enabled = true AND paused = false
  AND schedule LIKE 'email:%'
ORDER BY created_at;

-- name: InsertEmailTriggerSeen :execrows
INSERT INTO email_trigger_seen (message_id, account_id)
VALUES ($1, $2)
ON CONFLICT (message_id) DO NOTHING;
