-- name: CreateWorkspaceAction :one
INSERT INTO workspace_actions (owner_id, bot_id, kind)
VALUES ($1, $2, $3)
RETURNING *;

-- name: ClaimWorkspaceActions :many
UPDATE workspace_actions
SET status = 'running'
WHERE id IN (
    SELECT id FROM workspace_actions
    WHERE status = 'pending'
    ORDER BY created_at
    LIMIT $1
    FOR UPDATE SKIP LOCKED
)
RETURNING *;

-- name: MarkWorkspaceActionDone :exec
UPDATE workspace_actions
SET status = 'done', processed_at = now()
WHERE id = $1;

-- name: MarkWorkspaceActionFailed :exec
UPDATE workspace_actions
SET status = 'failed', error = $2, processed_at = now()
WHERE id = $1;

-- name: ListWorkspaceActionsForBot :many
SELECT * FROM workspace_actions
WHERE owner_id = $1 AND bot_id = $2
ORDER BY created_at DESC
LIMIT 20;
