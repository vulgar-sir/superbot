-- name: CreateToolInvocation :one
INSERT INTO tool_invocations (run_id, call_id, name, args)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: MarkToolInvocationRunning :exec
UPDATE tool_invocations
SET status = 'running',
    started_at = now(),
    updated_at = now()
WHERE id = $1;

-- name: FinishToolInvocation :one
UPDATE tool_invocations
SET status = $2,
    result_artifact = $3,
    error = $4,
    finished_at = now(),
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: GetToolInvocation :one
SELECT * FROM tool_invocations
WHERE run_id = $1 AND call_id = $2;

-- name: ListToolInvocations :many
SELECT * FROM tool_invocations
WHERE run_id = $1
ORDER BY created_at;
