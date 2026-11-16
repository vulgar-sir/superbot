-- name: UpsertWebAuthnSession :exec
INSERT INTO webauthn_sessions (session_key, session, expires_at)
VALUES ($1, $2, $3)
ON CONFLICT (session_key) DO UPDATE
SET session = EXCLUDED.session,
    expires_at = EXCLUDED.expires_at,
    created_at = now();

-- name: TakeWebAuthnSession :one
DELETE FROM webauthn_sessions
WHERE session_key = $1
  AND expires_at > now()
RETURNING session, expires_at;

-- name: DeleteExpiredWebAuthnSessions :exec
DELETE FROM webauthn_sessions WHERE expires_at <= now();
