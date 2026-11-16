-- name: CreateToolApproval :one
INSERT INTO tool_approvals (owner_id, conversation_id, run_id, call_id, tool_name, args)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: GetToolApproval :one
SELECT * FROM tool_approvals WHERE id = $1;

-- name: ResolveToolApproval :one
UPDATE tool_approvals
SET status = $3, resolved_at = now()
WHERE id = $1 AND owner_id = $2 AND status = 'pending'
RETURNING *;

-- name: ListPendingToolApprovals :many
SELECT * FROM tool_approvals
WHERE owner_id = $1 AND status = 'pending'
ORDER BY created_at DESC;

-- name: ExpireToolApproval :exec
UPDATE tool_approvals
SET status = 'expired', resolved_at = now()
WHERE id = $1 AND status = 'pending';

-- name: CreateRunCheckpoint :one
INSERT INTO run_checkpoints (owner_id, run_id, cycle_id, conversation_id, bot_id, step, messages, pending_call, approval_id)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
RETURNING *;

-- name: GetRunCheckpointForApproval :one
SELECT * FROM run_checkpoints
WHERE approval_id = $1 AND status = 'waiting' AND kind = 'approval'
ORDER BY created_at DESC LIMIT 1;

-- name: MarkRunCheckpointConsumed :exec
UPDATE run_checkpoints
SET status = 'consumed', consumed_at = now()
WHERE id = $1 AND status = 'waiting';

-- name: SetApprovalCheckpoint :exec
UPDATE tool_approvals SET checkpoint_id = $2 WHERE id = $1;

-- name: GetRunCheckpointByID :one
SELECT * FROM run_checkpoints WHERE id = $1;

-- name: ListExpiredPendingApprovals :many
SELECT * FROM tool_approvals
WHERE status = 'pending' AND created_at < now() - interval '5 minutes'
ORDER BY created_at
LIMIT 100;

-- name: DiscardCheckpoint :exec
UPDATE run_checkpoints
SET status = 'discarded', consumed_at = now()
WHERE id = $1 AND status = 'waiting';

-- name: CancelCycleForCheckpoint :exec
UPDATE cycles
SET status = 'canceled', lease_id = NULL, lease_until = NULL
WHERE id = (SELECT run_checkpoints.cycle_id FROM run_checkpoints WHERE run_checkpoints.id = $1)
  AND status = 'waiting_approval';

-- name: ListPendingApprovalsForConversation :many
SELECT * FROM tool_approvals
WHERE conversation_id = $1 AND status = 'pending'
ORDER BY created_at;

-- name: GetWaitingCheckpointForConversation :one
SELECT * FROM run_checkpoints
WHERE conversation_id = $1 AND status = 'waiting' AND kind = 'approval'
ORDER BY created_at DESC
LIMIT 1;

-- name: UpdateRunCheckpointMessages :exec
UPDATE run_checkpoints
SET messages = $2
WHERE id = $1 AND status = 'waiting';

-- name: UpsertProgressCheckpoint :exec
INSERT INTO run_checkpoints (owner_id, run_id, cycle_id, conversation_id, bot_id, step, messages, pending_call, kind)
VALUES ($1, $2, $3, $4, $5, $6, $7, '[]'::jsonb, 'progress')
ON CONFLICT (run_id) WHERE kind = 'progress'
DO UPDATE SET step = EXCLUDED.step, messages = EXCLUDED.messages, created_at = now();

-- name: GetProgressCheckpointForCycle :one
SELECT * FROM run_checkpoints
WHERE cycle_id = $1 AND kind = 'progress' AND status = 'waiting'
ORDER BY created_at DESC
LIMIT 1;

-- name: CloseProgressCheckpoints :exec
UPDATE run_checkpoints
SET status = 'consumed', consumed_at = now()
WHERE cycle_id = $1 AND kind = 'progress' AND status = 'waiting';
