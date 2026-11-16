-- name: CreateAgentThread :one
INSERT INTO agent_threads (owner_id, conversation_id, bot_a_id, bot_b_id, origin_conversation_id)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: GetAgentThreadByPair :one
SELECT * FROM agent_threads
WHERE owner_id = sqlc.arg(owner_id)
  AND LEAST(bot_a_id, bot_b_id) = LEAST(sqlc.arg(bot_a_id)::uuid, sqlc.arg(bot_b_id)::uuid)
  AND GREATEST(bot_a_id, bot_b_id) = GREATEST(sqlc.arg(bot_a_id)::uuid, sqlc.arg(bot_b_id)::uuid);

-- name: GetAgentThread :one
SELECT * FROM agent_threads
WHERE id = $1 AND owner_id = $2;

-- name: GetAgentThreadByConversation :one
SELECT * FROM agent_threads
WHERE conversation_id = $1;

-- name: ListAgentThreadsForBot :many
SELECT * FROM agent_threads
WHERE owner_id = $1 AND (bot_a_id = $2 OR bot_b_id = $2)
ORDER BY updated_at DESC
LIMIT $3;

-- name: TouchAgentThread :exec
UPDATE agent_threads SET updated_at = now() WHERE id = $1;
