-- name: CreateUsageRecord :one
INSERT INTO usage_records (
    user_id, bot_id, provider_id, model, cycle_id, run_id,
    prompt_tokens, cached_tokens, completion_tokens, cost_micros, estimated
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
RETURNING *;

-- name: SumCycleUsage :one
SELECT COALESCE(SUM(cost_micros), 0)::bigint AS total
FROM usage_records
WHERE cycle_id = $1;

-- name: UsageTotals :one
SELECT
    COALESCE(SUM(prompt_tokens), 0)::bigint AS prompt_tokens,
    COALESCE(SUM(cached_tokens), 0)::bigint AS cached_tokens,
    COALESCE(SUM(completion_tokens), 0)::bigint AS completion_tokens,
    COALESCE(SUM(cost_micros), 0)::bigint AS cost_micros
FROM usage_records
WHERE user_id = $1 AND created_at >= $2 AND created_at < $3;

-- name: UsageByBot :many
SELECT
    usage_records.bot_id,
    COALESCE(bots.name, '(已删除 Bot)') AS bot_name,
    SUM(usage_records.prompt_tokens)::bigint AS prompt_tokens,
    SUM(usage_records.completion_tokens)::bigint AS completion_tokens,
    SUM(usage_records.cost_micros)::bigint AS cost_micros
FROM usage_records
LEFT JOIN bots ON bots.id = usage_records.bot_id
WHERE usage_records.user_id = $1
  AND usage_records.created_at >= $2
  AND usage_records.created_at < $3
GROUP BY usage_records.bot_id, bots.name
ORDER BY cost_micros DESC;

-- name: UsageByModel :many
SELECT
    usage_records.model,
    SUM(usage_records.prompt_tokens)::bigint AS prompt_tokens,
    SUM(usage_records.completion_tokens)::bigint AS completion_tokens,
    SUM(usage_records.cost_micros)::bigint AS cost_micros
FROM usage_records
WHERE usage_records.user_id = $1
  AND usage_records.created_at >= $2
  AND usage_records.created_at < $3
GROUP BY usage_records.model
ORDER BY cost_micros DESC;

-- name: UsageDaily :many
SELECT
    date_trunc('day', usage_records.created_at)::date AS day,
    SUM(usage_records.prompt_tokens + usage_records.completion_tokens)::bigint AS tokens,
    SUM(usage_records.cost_micros)::bigint AS cost_micros
FROM usage_records
WHERE usage_records.user_id = $1
  AND usage_records.created_at >= $2
  AND usage_records.created_at < $3
GROUP BY day
ORDER BY day;

-- name: GetWeeklyUsageCost :one
SELECT COALESCE(SUM(cost_micros), 0)::bigint AS cost_micros
FROM usage_records
WHERE user_id = $1
  AND created_at >= date_trunc('week', now());
