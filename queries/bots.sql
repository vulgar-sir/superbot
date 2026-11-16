-- name: CreateBot :one
INSERT INTO bots (
    owner_id, name, slug, avatar, description, system_prompt,
    provider_id, model, params, capabilities, tool_policy,
    memory_enabled, computer_profile, title, avatar_shape, avatar_color,
    section_id, position, kind
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19)
RETURNING *;

-- name: ListBots :many
SELECT * FROM bots
WHERE owner_id = $1
ORDER BY pinned DESC, position, created_at;

-- name: GetBot :one
SELECT * FROM bots
WHERE id = $1 AND owner_id = $2;

-- name: UpdateBot :one
UPDATE bots
SET name = $3,
    slug = $4,
    avatar = $5,
    description = $6,
    system_prompt = $7,
    provider_id = $8,
    model = $9,
    params = $10,
    capabilities = $11,
    tool_policy = $12,
    memory_enabled = $13,
    computer_profile = $14
WHERE id = $1 AND owner_id = $2
RETURNING *;

-- name: UpdateBotProfile :one
UPDATE bots
SET name = $3,
    title = $4,
    description = $5,
    avatar = $6,
    avatar_shape = $7,
    avatar_color = $8,
    section_id = $9,
    position = $10,
    pinned = $11
WHERE id = $1 AND owner_id = $2
RETURNING *;

-- name: TouchBotActivity :exec
UPDATE bots
SET last_activity_at = now()
WHERE id = $1 AND owner_id = $2;

-- name: DetachBotsCreatedBy :exec
UPDATE bots
SET section_id = NULL
WHERE owner_id = $1 AND created_by_bot_id = $2;

-- name: SetBotCreator :exec
UPDATE bots
SET created_by_bot_id = $3
WHERE id = $1 AND owner_id = $2;

-- name: DeleteBot :execrows
DELETE FROM bots
WHERE id = $1 AND owner_id = $2;

-- name: SetBotHidden :one
UPDATE bots
SET hidden = $3
WHERE id = $1 AND owner_id = $2
RETURNING *;

-- name: SetBotMarkedUnread :exec
UPDATE bots SET marked_unread = $3 WHERE id = $1 AND owner_id = $2;

-- name: SetBotAccess :exec
UPDATE bots
SET visibility = $3,
    created_by_user_id = COALESCE(created_by_user_id, $4),
    updated_at = now()
WHERE id = $1 AND owner_id = $2;

-- name: GetResourceACLRole :one
SELECT role FROM resource_acl
WHERE resource_type = $1 AND resource_id = $2 AND user_id = $3
LIMIT 1;

-- name: UpsertResourceACL :exec
INSERT INTO resource_acl (account_id, resource_type, resource_id, user_id, role)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (resource_type, resource_id, user_id)
DO UPDATE SET role = EXCLUDED.role;

-- name: DeleteResourceACL :exec
DELETE FROM resource_acl
WHERE resource_type = $1 AND resource_id = $2 AND user_id = $3;

-- name: ListResourceACL :many
SELECT resource_acl.user_id, resource_acl.role, users.email, users.display_name
FROM resource_acl
JOIN users ON users.id = resource_acl.user_id
WHERE resource_acl.resource_type = $1 AND resource_acl.resource_id = $2
ORDER BY resource_acl.created_at;

-- name: SetBotProactivity :one
UPDATE bots
SET proactivity = $3
WHERE id = $1 AND owner_id = $2
RETURNING *;

-- name: SetBotTeamSettings :one
UPDATE bots
SET team_settings = $3
WHERE id = $1 AND owner_id = $2
RETURNING *;

-- name: SetBotModel :one
UPDATE bots
SET provider_id = $3, model = $4
WHERE id = $1 AND owner_id = $2
RETURNING *;

-- name: SetBotAvatar :one
UPDATE bots
SET avatar = $3,
    updated_at = now()
WHERE id = $1 AND owner_id = $2
RETURNING *;

-- name: SetBotComputerProfile :one
UPDATE bots
SET computer_profile = $3, updated_at = now()
WHERE id = $1 AND owner_id = $2
RETURNING *;

-- name: BotExists :one
SELECT EXISTS (SELECT 1 FROM bots WHERE id = $1);
