-- name: CreateRoutine :one
INSERT INTO routines (team_id, name, cron, prompt, enabled, next_run_at)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: ListRoutinesByOwner :many
SELECT routines.*
FROM routines
JOIN teams ON teams.id = routines.team_id
WHERE teams.owner_id = $1
ORDER BY routines.created_at DESC;

-- name: GetRoutineForOwner :one
SELECT routines.*
FROM routines
JOIN teams ON teams.id = routines.team_id
WHERE routines.id = $1 AND teams.owner_id = $2;

-- name: UpdateRoutine :one
UPDATE routines
SET name = $3,
    cron = $4,
    prompt = $5,
    enabled = $6,
    next_run_at = $7,
    updated_at = now()
WHERE id = $1 AND team_id = $2
RETURNING *;

-- name: DeleteRoutine :execrows
DELETE FROM routines
WHERE id = $1 AND team_id = $2;

-- name: ListDueRoutines :many
SELECT * FROM routines
WHERE enabled = true
  AND (next_run_at IS NULL OR next_run_at <= now())
ORDER BY next_run_at
LIMIT 20;

-- name: MarkRoutineRun :one
UPDATE routines
SET last_run_at = now(),
    next_run_at = $2,
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: DisableRoutine :execrows
UPDATE routines
SET enabled = false, next_run_at = NULL, updated_at = now()
WHERE id = $1;

-- name: GetTeamByID :one
SELECT * FROM teams WHERE id = $1;
