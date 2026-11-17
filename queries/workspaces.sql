-- name: UpsertWorkspaceRunning :one
INSERT INTO workspaces (
    owner_id, bot_id, profile, desired_state, status,
    container_name, workspace_path, vnc_addr, cdp_addr, last_used_at, last_error
)
VALUES ($1, $2, $3, 'running', 'running', $4, $5, $6, $7, now(), '')
ON CONFLICT (bot_id)
DO UPDATE SET
    profile = EXCLUDED.profile,
    desired_state = 'running',
    status = 'running',
    container_name = EXCLUDED.container_name,
    workspace_path = EXCLUDED.workspace_path,
    vnc_addr = EXCLUDED.vnc_addr,
    cdp_addr = EXCLUDED.cdp_addr,
    last_used_at = now(),
    last_error = ''
RETURNING *;

-- name: GetWorkspaceByBot :one
SELECT * FROM workspaces WHERE bot_id = $1;

-- name: SetWorkspaceDesiredState :one
INSERT INTO workspaces (owner_id, bot_id, profile, desired_state, status, workspace_path, last_error)
VALUES ($1, $2, $3, $4, 'stopped', '', '')
ON CONFLICT (bot_id)
DO UPDATE SET desired_state = EXCLUDED.desired_state
RETURNING *;

-- name: ListWorkspacesForReconcile :many
SELECT * FROM workspaces
WHERE (desired_state = 'running' AND status IN ('stopped', 'error'))
   OR (desired_state = 'stopped' AND status = 'running');

-- name: MarkWorkspaceStopped :one
UPDATE workspaces
SET desired_state = 'stopped',
    status = 'stopped',
    vnc_addr = '',
    cdp_addr = '',
    updated_at = now()
WHERE bot_id = $1
RETURNING *;

-- name: MarkWorkspaceError :exec
UPDATE workspaces
SET status = 'error', last_error = $2, updated_at = now()
WHERE bot_id = $1;

-- name: StopIdleWorkspaces :execrows
UPDATE workspaces w
SET desired_state = 'stopped', updated_at = now()
FROM bots b
WHERE w.bot_id = b.id
  AND w.desired_state = 'running'
  AND w.status = 'running'
  AND COALESCE(w.last_used_at, b.last_activity_at, b.updated_at) < now() - sqlc.arg(idle)::text::interval;

-- name: ListIdleRunningWorkspaces :many
-- The running computers StopIdleWorkspaces would put to sleep now.
SELECT w.bot_id, w.owner_id
FROM workspaces w
JOIN bots b ON b.id = w.bot_id
WHERE w.desired_state = 'running'
  AND w.status = 'running'
  AND COALESCE(w.last_used_at, b.last_activity_at, b.updated_at) < now() - sqlc.arg(idle)::text::interval;

-- name: TouchWorkspaceUsed :exec
UPDATE workspaces SET last_used_at = now() WHERE bot_id = $1;

-- name: CreateComputerSnapshot :one
INSERT INTO computer_snapshots (account_id, bot_id, label, archive_path, size_bytes)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: ListComputerSnapshots :many
SELECT * FROM computer_snapshots
WHERE account_id = $1 AND bot_id = $2
ORDER BY created_at DESC;

-- name: GetComputerSnapshot :one
SELECT * FROM computer_snapshots
WHERE id = $1 AND account_id = $2;

-- name: DeleteComputerSnapshot :execrows
DELETE FROM computer_snapshots
WHERE id = $1 AND account_id = $2;

-- name: CreateWorkspaceRestoreAction :one
INSERT INTO workspace_actions (owner_id, bot_id, kind, snapshot_id)
VALUES ($1, $2, 'restore', $3)
RETURNING *;

-- name: RecordWorkspaceProvisioned :exec
INSERT INTO workspaces (owner_id, bot_id, profile, desired_state, status, container_name, workspace_path, last_error)
VALUES ($1, $2, $3, 'stopped', 'stopped', $4, $5, '')
ON CONFLICT (bot_id) DO NOTHING;

-- name: ListRunningWorkspaces :many
SELECT bot_id, owner_id, COALESCE(last_used_at, updated_at)::timestamptz AS last_used
FROM workspaces
WHERE status = 'running'
ORDER BY 3 ASC;

-- name: ListWorkspacesToMeasure :many
SELECT bot_id FROM workspaces
WHERE disk_measured_at IS NULL
   OR disk_measured_at < COALESCE(last_used_at, updated_at)
   OR disk_measured_at < now() - interval '6 hours'
ORDER BY disk_measured_at NULLS FIRST
LIMIT $1;

-- name: SetWorkspaceDiskUsage :exec
UPDATE workspaces SET disk_bytes = $2, disk_measured_at = now() WHERE bot_id = $1;
