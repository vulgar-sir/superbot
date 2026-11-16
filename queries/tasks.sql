-- name: CreateTask :one
INSERT INTO tasks (owner_id, project_id, title, description, status, priority, assignee_bot_id, created_by_user_id, due_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
RETURNING *;

-- name: ListTasks :many
SELECT * FROM tasks
WHERE owner_id = $1
  AND (sqlc.narg(status)::text IS NULL OR status = sqlc.narg(status))
ORDER BY
  CASE status WHEN 'in_progress' THEN 0 WHEN 'blocked' THEN 1 WHEN 'todo' THEN 2 WHEN 'done' THEN 3 ELSE 4 END,
  priority DESC,
  created_at DESC
LIMIT $2;

-- name: GetTaskForOwner :one
SELECT * FROM tasks WHERE id = $1 AND owner_id = $2;

-- name: UpdateTask :one
UPDATE tasks
SET title = $3,
    description = $4,
    status = $5,
    priority = $6,
    assignee_bot_id = $7,
    due_at = $8,
    updated_at = now()
WHERE id = $1 AND owner_id = $2
RETURNING *;

-- name: DeleteTask :execrows
DELETE FROM tasks WHERE id = $1 AND owner_id = $2;

-- name: CreateProject :one
INSERT INTO projects (owner_id, name, description)
VALUES ($1, $2, $3)
RETURNING *;

-- name: ListProjects :many
SELECT * FROM projects WHERE owner_id = $1 ORDER BY created_at DESC;

-- name: SetProjectAccess :exec
UPDATE projects
SET visibility = $3, created_by_user_id = COALESCE(created_by_user_id, $4), updated_at = now()
WHERE id = $1 AND owner_id = $2;

-- name: GetProjectForOwner :one
SELECT * FROM projects WHERE id = $1 AND owner_id = $2;

-- name: DeleteProject :execrows
DELETE FROM projects WHERE id = $1 AND owner_id = $2;
