-- name: AllocateConversationEventSeq :one
UPDATE conversations
SET last_event_seq = last_event_seq + 1,
    updated_at = now()
WHERE id = $1
RETURNING last_event_seq;

-- name: InsertEvent :one
INSERT INTO events (conversation_id, seq, cycle_id, run_id, type, payload)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: ListEventsAfter :many
SELECT * FROM events
WHERE conversation_id = $1 AND seq > $2
ORDER BY seq
LIMIT $3;

-- name: ListRunEvents :many
SELECT * FROM events
WHERE run_id = $1
ORDER BY seq;

-- name: MinEventSeq :one
SELECT COALESCE(MIN(seq), 0)::bigint AS min_seq
FROM events
WHERE conversation_id = $1;

-- Streaming deltas are only useful for live replay; the final message row is
-- the durable record. Keep the newest rows of every conversation regardless.
-- name: PruneDeltaEvents :execrows
DELETE FROM events e
USING conversations c
WHERE e.conversation_id = c.id
  AND e.type = ANY(sqlc.arg(types)::text[])
  AND e.created_at < sqlc.arg(before)
  AND e.seq <= c.last_event_seq - sqlc.arg(keep_last)::bigint;
