-- name: CreateTeam :one
INSERT INTO teams (owner_id, name, description, mode, config)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: ListTeams :many
SELECT * FROM teams
WHERE owner_id = $1
ORDER BY pinned DESC, position, created_at;

-- name: GetTeam :one
SELECT * FROM teams
WHERE id = $1 AND owner_id = $2;

-- name: UpdateTeam :one
UPDATE teams
SET name = $3,
    description = $4,
    mode = $5,
    config = $6
WHERE id = $1 AND owner_id = $2
RETURNING *;

-- name: UpdateTeamPlacement :one
UPDATE teams
SET section_id = $3,
    position = $4,
    pinned = $5
WHERE id = $1 AND owner_id = $2
RETURNING *;

-- name: SetTeamRoomConversation :exec
UPDATE teams
SET room_conversation_id = $3
WHERE id = $1 AND owner_id = $2;

-- name: DeleteTeam :execrows
DELETE FROM teams
WHERE id = $1 AND owner_id = $2;

-- name: AddTeamBot :exec
INSERT INTO team_bots (team_id, bot_id, position, role)
VALUES ($1, $2, $3, $4)
ON CONFLICT (team_id, bot_id)
DO UPDATE SET position = EXCLUDED.position, role = EXCLUDED.role;

-- name: RemoveTeamBot :exec
DELETE FROM team_bots
WHERE team_id = $1 AND bot_id = $2;

-- name: ClearTeamBots :exec
DELETE FROM team_bots WHERE team_id = $1;

-- name: ListTeamBots :many
SELECT sqlc.embed(bots), team_bots.position, team_bots.role
FROM team_bots
JOIN bots ON bots.id = team_bots.bot_id
WHERE team_bots.team_id = $1
ORDER BY team_bots.position, bots.name;

-- name: SetTeamHidden :one
UPDATE teams
SET hidden = $3
WHERE id = $1 AND owner_id = $2
RETURNING *;

-- name: SetTeamMarkedUnread :exec
UPDATE teams SET marked_unread = $3 WHERE id = $1 AND owner_id = $2;

-- name: ListTeamIDsForBot :many
SELECT team_id FROM team_bots WHERE bot_id = $1 ORDER BY team_id;
