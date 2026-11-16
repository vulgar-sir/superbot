-- name: CreateConversation :one
INSERT INTO conversations (team_id, direct_bot_id, kind, title, created_by, budget_micros)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: GetConversation :one
SELECT * FROM conversations WHERE id = $1;

-- name: GetDirectConversationByBot :one
SELECT * FROM conversations
WHERE created_by = $1 AND direct_bot_id = $2 AND kind = 'direct'
  AND (direct_user_id = $3 OR direct_user_id IS NULL)
ORDER BY created_at
LIMIT 1;

-- name: ListConversations :many
SELECT * FROM conversations
WHERE created_by = $1
  AND kind NOT IN ('agent', 'subagent')
  AND (kind <> 'direct' OR direct_user_id = $2 OR direct_user_id IS NULL)
ORDER BY updated_at DESC
LIMIT $3 OFFSET $4;

-- name: DeleteAgentConversationsForBot :exec
DELETE FROM conversations
WHERE kind = 'agent'
  AND id IN (
      SELECT conversation_id FROM agent_threads
      WHERE bot_a_id = $1 OR bot_b_id = $1
  );

-- name: ListTeamConversations :many
SELECT * FROM conversations
WHERE team_id = $1
ORDER BY updated_at DESC
LIMIT $2 OFFSET $3;

-- name: TouchConversation :exec
UPDATE conversations
SET updated_at = now()
WHERE id = $1;

-- name: ReserveConversationBudget :execrows
UPDATE conversations
SET reserved_micros = reserved_micros + $2
WHERE id = $1
  AND (budget_micros = 0 OR spent_micros + reserved_micros + $2 <= budget_micros);

-- name: ReleaseConversationReservation :exec
UPDATE conversations
SET reserved_micros = GREATEST(reserved_micros - $2, 0)
WHERE id = $1;

-- name: SettleConversationUsage :exec
UPDATE conversations
SET reserved_micros = GREATEST(reserved_micros - $2, 0),
    spent_micros = spent_micros + $3,
    updated_at = now()
WHERE id = $1;

-- name: SetConversationDirectUser :exec
UPDATE conversations SET direct_user_id = $2 WHERE id = $1;

-- name: DeleteConversationForOwner :execrows
DELETE FROM conversations
WHERE id = $1 AND created_by = $2;

-- name: CreateDirectUserConversation :one
INSERT INTO conversations (direct_bot_id, direct_user_id, kind, title, created_by)
VALUES ($1, $2, 'direct', $3, $4)
RETURNING *;
