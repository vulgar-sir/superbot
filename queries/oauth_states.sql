-- name: CreateOAuthState :exec
INSERT INTO oauth_states (state_hash, owner_id, provider, expires_at)
VALUES ($1, $2, $3, $4)
ON CONFLICT (state_hash) DO UPDATE
SET owner_id = EXCLUDED.owner_id,
    provider = EXCLUDED.provider,
    expires_at = EXCLUDED.expires_at;

-- name: TakeOAuthState :one
DELETE FROM oauth_states
WHERE state_hash = $1
  AND expires_at > now()
RETURNING owner_id, provider, expires_at;

-- name: DeleteExpiredOAuthStates :exec
DELETE FROM oauth_states WHERE expires_at <= now();
