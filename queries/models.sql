-- name: CreateModel :one
INSERT INTO models (
    provider_id, name, context_window, max_output,
    supports_tools, supports_vision, supports_reasoning,
    price_in_micros_per_mtok, price_cache_micros_per_mtok, price_out_micros_per_mtok
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
RETURNING *;

-- name: ListModelsByProvider :many
SELECT * FROM models
WHERE provider_id = $1
ORDER BY name;

-- name: GetModelByName :one
SELECT * FROM models
WHERE provider_id = $1 AND name = $2;

-- name: UpdateModel :one
UPDATE models
SET name = $3,
    context_window = $4,
    max_output = $5,
    supports_tools = $6,
    supports_vision = $7,
    supports_reasoning = $8,
    price_in_micros_per_mtok = $9,
    price_cache_micros_per_mtok = $10,
    price_out_micros_per_mtok = $11
WHERE id = $1 AND provider_id = $2
RETURNING *;

-- name: DeleteModel :execrows
DELETE FROM models
WHERE id = $1 AND provider_id = $2;
