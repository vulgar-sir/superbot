-- name: CreateDevice :one
INSERT INTO devices (owner_id, name, token_hash, platform)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: ListDevicesByOwner :many
SELECT * FROM devices
WHERE owner_id = $1
ORDER BY created_at DESC;

-- name: GetDevice :one
SELECT * FROM devices
WHERE id = $1 AND owner_id = $2;

-- name: GetDeviceByTokenHash :one
SELECT * FROM devices
WHERE token_hash = $1;

-- name: DeleteDevice :execrows
DELETE FROM devices
WHERE id = $1 AND owner_id = $2;

-- name: SetDeviceAllowBot :one
UPDATE devices
SET allow_bot = $3
WHERE id = $1 AND owner_id = $2
RETURNING *;

-- name: TouchDevice :exec
UPDATE devices
SET status = 'online', last_seen_at = now()
WHERE id = $1;

-- name: CreateDeviceJob :one
INSERT INTO device_jobs (owner_id, device_id, kind, command, nonce)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: ClaimDeviceJob :one
UPDATE device_jobs
SET status = 'running', started_at = now()
WHERE id = (
    SELECT j.id FROM device_jobs j
    WHERE j.device_id = $1 AND j.status = 'pending'
    ORDER BY j.created_at
    LIMIT 1
    FOR UPDATE SKIP LOCKED
)
RETURNING *;

-- name: CompleteDeviceJob :execrows
UPDATE device_jobs
SET status = 'done', output = $3, error = $4, finished_at = now()
WHERE id = $1 AND device_id = $2 AND nonce = $5 AND status = 'running';

-- name: FailDeviceJob :execrows
UPDATE device_jobs
SET status = 'failed', output = $3, error = $4, finished_at = now()
WHERE id = $1 AND device_id = $2 AND nonce = $5 AND status = 'running';

-- name: CompleteDeviceJobArtifact :execrows
UPDATE device_jobs
SET status = 'done', output = $3, artifact_path = $4, finished_at = now()
WHERE id = $1 AND device_id = $2 AND nonce = $5 AND status = 'running';

-- name: GetDeviceJob :one
SELECT * FROM device_jobs
WHERE id = $1 AND owner_id = $2;

-- name: SetDevicePublicKey :exec
UPDATE devices SET public_key = $2, updated_at = now() WHERE id = $1;
