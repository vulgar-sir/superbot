-- name: CreateMemoryText :one
INSERT INTO memories (owner_id, scope_type, scope_id, content, metadata)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: CreateMemory :one
INSERT INTO memories (
    owner_id, scope_type, scope_id, content, metadata,
    source, pinned, source_conversation_id, source_ordinal
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
RETURNING *;

-- name: GetMemory :one
SELECT * FROM memories
WHERE id = $1 AND owner_id = $2;

-- name: SearchMemories :many
SELECT * FROM memories
WHERE owner_id = $1
  AND scope_type = $2
  AND scope_id = $3
  AND archived_at IS NULL
  AND (content ILIKE '%' || sqlc.arg(pattern)::text || '%'
       OR similarity(content, sqlc.arg(query)::text) >= sqlc.arg(min_similarity)::real)
ORDER BY similarity(content, sqlc.arg(query)::text) DESC, created_at DESC
LIMIT sqlc.arg(max_results);

-- name: SetMemoryEmbedding :exec
UPDATE memories
SET embedding = sqlc.arg(embedding)::text::vector,
    embedding_model = sqlc.arg(embedding_model)
WHERE id = $1 AND owner_id = $2;

-- name: SearchMemoriesByEmbedding :many
SELECT * FROM memories
WHERE owner_id = $1
  AND scope_type = $2
  AND scope_id = $3
  AND archived_at IS NULL
  AND embedding IS NOT NULL
  AND embedding_model = sqlc.arg(embedding_model)
ORDER BY embedding <=> sqlc.arg(embedding)::text::vector
LIMIT sqlc.arg(max_results);

-- name: SearchMemoriesScored :many
SELECT sqlc.embed(memories),
       (1 - (memories.embedding <=> sqlc.arg(embedding)::text::vector))::float8 AS similarity
FROM memories
WHERE memories.owner_id = $1
  AND memories.scope_type = $2
  AND memories.scope_id = $3
  AND memories.archived_at IS NULL
  AND memories.embedding IS NOT NULL
  AND memories.embedding_model = sqlc.arg(embedding_model)
  AND (1 - (memories.embedding <=> sqlc.arg(embedding)::text::vector)) >= sqlc.arg(min_similarity)::float8
ORDER BY memories.embedding <=> sqlc.arg(embedding)::text::vector
LIMIT sqlc.arg(max_results);

-- name: ListMemories :many
SELECT * FROM memories
WHERE owner_id = $1 AND scope_type = $2 AND scope_id = $3
  AND archived_at IS NULL
ORDER BY created_at DESC
LIMIT $4;

-- name: ListMemoriesIncludingArchived :many
SELECT * FROM memories
WHERE owner_id = $1 AND scope_type = $2 AND scope_id = $3
ORDER BY created_at DESC
LIMIT $4;

-- name: DeleteMemory :execrows
DELETE FROM memories
WHERE id = $1 AND owner_id = $2;

-- name: UpdateBotMemory :one
UPDATE memories
SET content = sqlc.arg(content),
    updated_at = now()
WHERE id = sqlc.arg(id)
  AND owner_id = sqlc.arg(owner_id)
  AND scope_type = 'bot'
  AND scope_id = sqlc.arg(scope_id)
RETURNING *;

-- name: DeleteBotMemory :execrows
DELETE FROM memories
WHERE id = sqlc.arg(id)
  AND owner_id = sqlc.arg(owner_id)
  AND scope_type = 'bot'
  AND scope_id = sqlc.arg(scope_id);

-- Archived rows are included on purpose: a restored memory must already live
-- in the current vector space or recall would never find it.
-- name: ListMemoriesNeedingReembed :many
SELECT * FROM memories
WHERE embedding IS NULL OR embedding_model <> sqlc.arg(embedding_model)
ORDER BY created_at
LIMIT sqlc.arg(max_rows);

-- name: UpdateUserMemory :one
UPDATE memories
SET content = sqlc.arg(content),
    updated_at = now()
WHERE id = sqlc.arg(id)
  AND owner_id = sqlc.arg(owner_id)
  AND scope_type = 'user'
  AND scope_id = sqlc.arg(scope_id)
RETURNING *;

-- name: DeleteUserMemory :execrows
DELETE FROM memories
WHERE id = sqlc.arg(id)
  AND owner_id = sqlc.arg(owner_id)
  AND scope_type = 'user'
  AND scope_id = sqlc.arg(scope_id);

-- name: RefreshMemory :one
UPDATE memories
SET content = sqlc.arg(content),
    pinned = pinned OR sqlc.arg(pinned)::boolean,
    updated_at = now()
WHERE id = sqlc.arg(id) AND owner_id = sqlc.arg(owner_id)
RETURNING *;

-- name: SetMemoryPinned :one
UPDATE memories
SET pinned = sqlc.arg(pinned),
    updated_at = now()
WHERE id = sqlc.arg(id) AND owner_id = sqlc.arg(owner_id)
RETURNING *;

-- name: ArchiveMemory :one
UPDATE memories
SET archived_at = now()
WHERE id = sqlc.arg(id) AND owner_id = sqlc.arg(owner_id) AND archived_at IS NULL
RETURNING *;

-- name: ArchiveUnpinnedMemories :many
UPDATE memories
SET archived_at = now()
WHERE owner_id = sqlc.arg(owner_id)
  AND id = ANY(sqlc.arg(ids)::uuid[])
  AND pinned = false
  AND archived_at IS NULL
RETURNING id;

-- name: RestoreMemory :one
UPDATE memories
SET archived_at = NULL,
    updated_at = now()
WHERE id = sqlc.arg(id) AND owner_id = sqlc.arg(owner_id)
RETURNING *;

-- name: ListMemoryScopesForTidy :many
SELECT owner_id, scope_type, scope_id, count(*)::bigint AS row_count
FROM memories
WHERE archived_at IS NULL
GROUP BY owner_id, scope_type, scope_id
HAVING count(*) > sqlc.arg(min_rows)::bigint
ORDER BY count(*) DESC
LIMIT sqlc.arg(max_scopes);

-- Conversations that went quiet with enough unreviewed messages. The retry
-- window doubles with every consecutive failure (capped at 64 idle windows).
-- name: ListConversationsDueForMemoryReview :many
SELECT c.id AS conversation_id,
       c.kind,
       c.team_id,
       c.direct_user_id,
       c.created_by,
       b.id AS bot_id,
       b.owner_id,
       COALESCE(cur.last_ordinal, 0)::bigint AS last_ordinal,
       stats.max_ordinal,
       stats.new_count
FROM conversations c
JOIN LATERAL (
    SELECT bots.id, bots.owner_id
    FROM bots
    LEFT JOIN team_bots tb ON tb.bot_id = bots.id AND tb.team_id = c.team_id
    WHERE bots.memory_enabled
      AND bots.provider_id IS NOT NULL
      AND ((c.kind = 'direct' AND bots.id = c.direct_bot_id)
           OR (c.kind = 'team' AND tb.team_id IS NOT NULL))
    ORDER BY tb.position NULLS LAST, bots.id
    LIMIT 1
) b ON true
LEFT JOIN memory_review_cursors cur ON cur.conversation_id = c.id
JOIN LATERAL (
    SELECT COALESCE(max(m.ordinal), 0)::bigint AS max_ordinal,
           max(m.created_at) AS newest_at,
           count(*)::bigint AS new_count
    FROM messages m
    WHERE m.conversation_id = c.id
      AND m.ordinal > COALESCE(cur.last_ordinal, 0)
      AND m.deleted_at IS NULL
      AND m.sender_type IN ('user', 'bot')
) stats ON true
WHERE c.kind IN ('direct', 'team')
  AND c.status = 'active'
  AND stats.new_count >= sqlc.arg(min_new_messages)::bigint
  AND stats.newest_at < now() - sqlc.arg(idle_seconds)::float8 * interval '1 second'
  AND stats.newest_at > now() - sqlc.arg(lookback_seconds)::float8 * interval '1 second'
  AND (cur.last_reviewed_at IS NULL
       OR cur.last_reviewed_at < now() - sqlc.arg(idle_seconds)::float8
            * power(2, LEAST(cur.failures, 6)) * interval '1 second')
ORDER BY stats.newest_at
LIMIT sqlc.arg(max_conversations);

-- Stamps the attempt before the model is called, so a second worker (or a
-- crash mid-call) cannot review the same conversation again within the window.
-- name: ClaimMemoryReview :execrows
INSERT INTO memory_review_cursors (conversation_id, bot_id, last_ordinal, last_reviewed_at)
VALUES (sqlc.arg(conversation_id), sqlc.arg(bot_id), 0, now())
ON CONFLICT (conversation_id) DO UPDATE
SET bot_id = EXCLUDED.bot_id,
    last_reviewed_at = now(),
    updated_at = now()
WHERE memory_review_cursors.last_reviewed_at IS NULL
   OR memory_review_cursors.last_reviewed_at < now() - sqlc.arg(idle_seconds)::float8 * interval '1 second';

-- name: ListMessagesForMemoryReview :many
SELECT m.ordinal, m.sender_type, m.content, COALESCE(b.name, '')::text AS bot_name
FROM messages m
LEFT JOIN bots b ON b.id = m.sender_bot_id
WHERE m.conversation_id = sqlc.arg(conversation_id)
  AND m.ordinal > sqlc.arg(after_ordinal)
  AND m.deleted_at IS NULL
  AND m.sender_type IN ('user', 'bot')
  AND btrim(m.content) <> ''
ORDER BY m.ordinal DESC
LIMIT sqlc.arg(max_rows);

-- name: AdvanceMemoryReviewCursor :exec
UPDATE memory_review_cursors
SET last_ordinal = GREATEST(last_ordinal, sqlc.arg(last_ordinal)),
    last_reviewed_at = now(),
    failures = 0,
    updated_at = now()
WHERE conversation_id = sqlc.arg(conversation_id);

-- name: RecordMemoryReviewFailure :exec
UPDATE memory_review_cursors
SET failures = failures + 1,
    last_reviewed_at = now(),
    updated_at = now()
WHERE conversation_id = sqlc.arg(conversation_id);
