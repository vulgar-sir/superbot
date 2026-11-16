-- name: CreateMessage :one
INSERT INTO messages (
    conversation_id, cycle_id, sender_type, sender_bot_id,
    content, metadata, client_msg_id, run_id, parent_message_id
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
RETURNING *;

-- name: SetMessageCycle :exec
UPDATE messages
SET cycle_id = $2
WHERE id = $1;

-- name: GetMessage :one
SELECT * FROM messages WHERE id = $1;

-- name: GetMessageByClientID :one
SELECT * FROM messages
WHERE conversation_id = $1 AND client_msg_id = $2;

-- name: ListMessages :many
SELECT * FROM messages
WHERE conversation_id = $1 AND deleted_at IS NULL
ORDER BY ordinal;

-- name: ListMessagesAfterOrdinal :many
SELECT * FROM messages
WHERE conversation_id = $1 AND ordinal > $2 AND deleted_at IS NULL
ORDER BY ordinal
LIMIT $3;

-- name: ListRecentMessages :many
SELECT * FROM messages
WHERE conversation_id = $1 AND deleted_at IS NULL
ORDER BY ordinal DESC
LIMIT $2;

-- name: GetMessageForOwner :one
SELECT messages.*
FROM messages
JOIN conversations ON conversations.id = messages.conversation_id
WHERE messages.id = $1 AND conversations.created_by = $2;

-- name: AddMessageReaction :exec
INSERT INTO message_reactions (message_id, user_id, emoji)
VALUES ($1, $2, $3)
ON CONFLICT (message_id, user_id, emoji) DO NOTHING;

-- name: RemoveMessageReaction :exec
DELETE FROM message_reactions
WHERE message_id = $1 AND user_id = $2 AND emoji = $3;

-- name: ListMessageReactions :many
SELECT
    message_id,
    emoji,
    count(*)::int AS reaction_count,
    bool_or(user_id = $2) AS reacted
FROM message_reactions
WHERE message_id = ANY($1::uuid[])
GROUP BY message_id, emoji
ORDER BY message_id, emoji;

-- name: GetLatestBotMessage :one
SELECT * FROM messages
WHERE conversation_id = $1 AND sender_type = 'bot' AND deleted_at IS NULL
ORDER BY ordinal DESC
LIMIT 1;

-- name: GetLatestMessageOrdinal :one
SELECT COALESCE(MAX(ordinal), 0)::bigint FROM messages WHERE conversation_id = $1;

-- name: ListUserMessagesAfterOrdinal :many
SELECT * FROM messages
WHERE conversation_id = $1 AND ordinal > $2 AND sender_type = 'user' AND deleted_at IS NULL
ORDER BY ordinal;

-- name: ListUserMessagesAfterTime :many
SELECT * FROM messages
WHERE conversation_id = $1 AND sender_type = 'user' AND created_at > $2 AND deleted_at IS NULL
ORDER BY ordinal;

-- name: UpdateMessageContent :one
UPDATE messages
SET content = $2, edited_at = now()
WHERE id = $1 AND deleted_at IS NULL
RETURNING *;

-- name: SoftDeleteMessage :one
UPDATE messages
SET deleted_at = now()
WHERE id = $1 AND deleted_at IS NULL
RETURNING *;

-- name: SetMessagePinned :one
UPDATE messages
SET pinned_at = CASE WHEN sqlc.arg(pinned)::boolean THEN now() ELSE NULL END
WHERE id = sqlc.arg(id) AND deleted_at IS NULL
RETURNING *;

-- name: AddPollVote :exec
INSERT INTO message_poll_votes (message_id, user_id, option_id)
VALUES ($1, $2, $3)
ON CONFLICT (message_id, user_id, option_id) DO NOTHING;

-- name: RemovePollVote :exec
DELETE FROM message_poll_votes
WHERE message_id = $1 AND user_id = $2 AND option_id = $3;

-- name: RemovePollVotesForUser :exec
DELETE FROM message_poll_votes
WHERE message_id = $1 AND user_id = $2;

-- name: ListPollVotes :many
SELECT
    message_id,
    option_id,
    count(*)::int AS vote_count,
    bool_or(user_id = $2) AS voted
FROM message_poll_votes
WHERE message_id = ANY($1::uuid[])
GROUP BY message_id, option_id
ORDER BY message_id, option_id;

-- Messages that should reach a bot that is already working: user interjections
-- plus results delivered by other bots and subagents.
-- name: ListInjectableMessagesAfterOrdinal :many
SELECT * FROM messages
WHERE conversation_id = $1 AND ordinal > $2 AND deleted_at IS NULL
  AND (sender_type = 'user'
       OR (sender_type = 'system' AND metadata->>'inject' = 'true'))
ORDER BY ordinal;

-- name: SearchConversationMessages :many
SELECT * FROM messages
WHERE conversation_id = $1 AND deleted_at IS NULL
  AND content ILIKE '%' || sqlc.arg(query)::text || '%'
ORDER BY ordinal DESC
LIMIT sqlc.arg(max_rows);

-- name: ListMessagesInOrdinalRange :many
SELECT * FROM messages
WHERE conversation_id = $1 AND deleted_at IS NULL
  AND ordinal >= sqlc.arg(from_ordinal) AND ordinal <= sqlc.arg(to_ordinal)
ORDER BY ordinal
LIMIT sqlc.arg(max_rows);

-- Counts results delivered to a conversation since the user last spoke; used
-- to stop bots from waking each other indefinitely.
-- name: CountInjectedSinceLastUser :one
SELECT count(*)::int FROM messages m
WHERE m.conversation_id = $1 AND m.deleted_at IS NULL
  AND m.sender_type = 'system' AND m.metadata->>'inject' = 'true'
  AND m.ordinal > COALESCE((
      SELECT max(u.ordinal) FROM messages u
      WHERE u.conversation_id = $1 AND u.sender_type = 'user' AND u.deleted_at IS NULL
  ), 0);
