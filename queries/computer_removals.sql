-- name: ListDueComputerRemovals :many
SELECT * FROM computer_removals
WHERE next_attempt_at <= now()
ORDER BY requested_at
LIMIT $1;

-- name: DeleteComputerRemoval :exec
DELETE FROM computer_removals WHERE bot_id = $1;

-- name: DeferComputerRemoval :exec
UPDATE computer_removals
SET attempts = attempts + 1,
    last_error = $2,
    next_attempt_at = now() + make_interval(mins => LEAST(60, 1 << LEAST(attempts, 6)))
WHERE bot_id = $1;

-- name: ExistingBotIDs :many
SELECT id FROM bots WHERE id = ANY(sqlc.arg(ids)::uuid[]);
