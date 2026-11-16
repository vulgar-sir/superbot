-- name: CreateSidebarSection :one
INSERT INTO sidebar_sections (owner_id, name, position)
VALUES ($1, $2, $3)
RETURNING *;

-- name: GetSidebarSection :one
SELECT * FROM sidebar_sections
WHERE id = $1 AND owner_id = $2;

-- name: ListSidebarSections :many
SELECT * FROM sidebar_sections
WHERE owner_id = $1
ORDER BY position, created_at;

-- name: UpdateSidebarSection :one
UPDATE sidebar_sections
SET name = $3,
    position = $4,
    collapsed = $5
WHERE id = $1 AND owner_id = $2
RETURNING *;

-- name: DeleteSidebarSection :execrows
DELETE FROM sidebar_sections
WHERE id = $1 AND owner_id = $2;
