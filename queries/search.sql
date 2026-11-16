-- name: SearchMessages :many
SELECT
    m.id,
    m.conversation_id,
    m.content,
    m.sender_type,
    m.sender_bot_id,
    m.created_at,
    c.title AS conversation_title,
    COALESCE(b.name, '') AS bot_name
FROM messages m
JOIN conversations c ON c.id = m.conversation_id
LEFT JOIN bots b ON b.id = m.sender_bot_id
WHERE c.created_by = sqlc.arg(owner_id)
  AND m.deleted_at IS NULL
  AND m.content ILIKE '%' || sqlc.arg(query) || '%'
ORDER BY m.created_at DESC
LIMIT sqlc.arg(row_limit);
