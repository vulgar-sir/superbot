-- +goose Up
CREATE TABLE oauth_states (
    state_hash text PRIMARY KEY,
    owner_id   uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    provider   text NOT NULL,
    expires_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX oauth_states_expiry_idx ON oauth_states(expires_at);

CREATE TABLE webauthn_sessions (
    session_key text PRIMARY KEY,
    session     jsonb NOT NULL,
    expires_at  timestamptz NOT NULL,
    created_at  timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX webauthn_sessions_expiry_idx ON webauthn_sessions(expires_at);

-- +goose Down
DROP TABLE IF EXISTS webauthn_sessions;
DROP TABLE IF EXISTS oauth_states;
