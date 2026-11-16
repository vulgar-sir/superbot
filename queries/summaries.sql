-- name: GetLatestConversationSummary :one
SELECT * FROM conversation_summaries
WHERE conversation_id = $1
ORDER BY created_at DESC
LIMIT 1;

-- name: CreateConversationSummary :one
INSERT INTO conversation_summaries (
    conversation_id, up_to_message_id, up_to_seq, content, tokens
)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: CreateAutoConversationSummary :one
INSERT INTO conversation_summaries (
    conversation_id, up_to_message_id, up_to_seq, content, tokens, source
)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: CountConversationSummaries :one
SELECT count(*)::int FROM conversation_summaries WHERE conversation_id = $1;

-- name: ListConversationSummaries :many
SELECT * FROM conversation_summaries
WHERE conversation_id = $1
ORDER BY created_at DESC
LIMIT $2;
