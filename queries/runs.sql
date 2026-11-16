-- name: CreateRun :one
INSERT INTO runs (cycle_id, conversation_id, bot_id)
VALUES ($1, $2, $3)
RETURNING *;

-- name: GetRun :one
SELECT * FROM runs WHERE id = $1;

-- name: ListRunsByCycle :many
SELECT * FROM runs
WHERE cycle_id = $1
ORDER BY created_at;

-- name: MarkRunRunning :exec
UPDATE runs
SET status = 'running',
    started_at = now(),
    updated_at = now()
WHERE id = $1;

-- name: UpdateRunStep :exec
UPDATE runs
SET step = $2,
    updated_at = now()
WHERE id = $1;

-- name: FinishRun :one
UPDATE runs
SET status = $2,
    warning = $3,
    error = $4,
    usage_in = $5,
    usage_out = $6,
    cost_micros = $7,
    step = $8,
    finished_at = now(),
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: MarkCycleRunsInterrupted :exec
UPDATE runs
SET status = 'interrupted',
    error = 'worker lease expired',
    finished_at = now(),
    updated_at = now()
WHERE cycle_id = $1
  AND status IN ('queued', 'running');
