-- name: CreateSubagentTask :one
INSERT INTO subagent_tasks (
    owner_id, parent_conversation_id, parent_cycle_id, parent_bot_id,
    child_conversation_id, child_cycle_id, objective, parent_task_id, depth, status
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, 'queued')
RETURNING *;

-- name: GetSubagentTaskForOwner :one
SELECT * FROM subagent_tasks WHERE id = $1 AND owner_id = $2;

-- name: GetSubagentTaskByChildCycle :one
SELECT * FROM subagent_tasks WHERE child_cycle_id = $1;

-- name: ListSubagentTasksByParent :many
WITH RECURSIVE tree AS (
    SELECT * FROM subagent_tasks
    WHERE subagent_tasks.owner_id = $1 AND subagent_tasks.parent_conversation_id = $2 AND subagent_tasks.parent_task_id IS NULL
    UNION ALL
    SELECT child.* FROM subagent_tasks child
    JOIN tree ON child.parent_task_id = tree.id
    WHERE child.owner_id = $1
)
SELECT * FROM tree
ORDER BY depth, created_at DESC
LIMIT $3;

-- name: CountActiveSubagentsForOwner :one
SELECT count(*) FROM subagent_tasks
WHERE owner_id = $1 AND status IN ('queued', 'running');

-- name: MarkSubagentRunning :execrows
UPDATE subagent_tasks
SET status = 'running', updated_at = now()
WHERE id = $1 AND status = 'queued';

-- name: FinishSubagentTask :execrows
UPDATE subagent_tasks
SET status = $2,
    result_summary = $3,
    error = $4,
    finished_at = now(),
    updated_at = now()
WHERE id = $1 AND status IN ('queued', 'running');

-- name: CancelSubagentTask :execrows
UPDATE subagent_tasks
SET status = 'canceled', finished_at = now(), updated_at = now()
WHERE id = $1 AND owner_id = $2 AND status IN ('queued', 'running');

-- name: GetSubagentTaskByChildConversation :one
SELECT * FROM subagent_tasks
WHERE child_conversation_id = $1
ORDER BY created_at DESC
LIMIT 1;

-- name: ListRunningSubagentTasks :many
SELECT * FROM subagent_tasks
WHERE status = 'running'
ORDER BY created_at
LIMIT 100;
