-- name: CreateSkill :one
INSERT INTO skills (
    owner_id, name, description, version, source, status, created_by, files_path
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
RETURNING *;

-- name: GetSkillByName :one
SELECT * FROM skills
WHERE owner_id = $1 AND name = $2;

-- name: ListBuiltinSkills :many
SELECT * FROM skills
WHERE owner_id = $1 AND source = 'builtin' AND status = 'active'
ORDER BY name;

-- name: ListSkills :many
SELECT * FROM skills
WHERE owner_id = $1
ORDER BY created_at DESC;

-- name: GetSkill :one
SELECT * FROM skills
WHERE id = $1 AND owner_id = $2;

-- name: SetSkillStatus :one
UPDATE skills
SET status = $3, updated_at = now()
WHERE id = $1 AND owner_id = $2
RETURNING *;

-- name: SetSkillFilesPath :one
UPDATE skills
SET files_path = $3, updated_at = now()
WHERE id = $1 AND owner_id = $2
RETURNING *;

-- name: DeleteSkill :execrows
DELETE FROM skills
WHERE id = $1 AND owner_id = $2;

-- name: UpsertBotSkill :exec
INSERT INTO bot_skills (bot_id, skill_id, enabled)
VALUES ($1, $2, true)
ON CONFLICT (bot_id, skill_id)
DO UPDATE SET enabled = true;

-- name: ClearBotSkills :exec
DELETE FROM bot_skills WHERE bot_id = $1;

-- name: ListBotSkills :many
SELECT sqlc.embed(skills), bot_skills.enabled
FROM bot_skills
JOIN skills ON skills.id = bot_skills.skill_id
WHERE bot_skills.bot_id = $1
ORDER BY skills.name;

-- name: ListEnabledSkills :many
SELECT skills.*
FROM bot_skills
JOIN skills ON skills.id = bot_skills.skill_id
WHERE bot_skills.bot_id = $1
  AND bot_skills.enabled = true
  AND skills.status = 'active'
ORDER BY skills.name;

-- name: SetSkillAccess :exec
UPDATE skills
SET visibility = $3, created_by_user_id = COALESCE(created_by_user_id, $4), updated_at = now()
WHERE id = $1 AND owner_id = $2;

-- name: SetSkillDescriptionEn :exec
UPDATE skills SET description_en = $3 WHERE id = $1 AND owner_id = $2;
