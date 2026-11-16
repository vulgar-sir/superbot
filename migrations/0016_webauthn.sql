-- +goose Up
CREATE TABLE webauthn_credentials (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id       uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    credential_id bytea NOT NULL UNIQUE,
    credential    jsonb NOT NULL,
    created_at    timestamptz NOT NULL DEFAULT now(),
    updated_at    timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX webauthn_credentials_user_idx ON webauthn_credentials(user_id, created_at DESC);

CREATE TRIGGER webauthn_credentials_set_updated_at
BEFORE UPDATE ON webauthn_credentials
FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- +goose Down
DROP TRIGGER IF EXISTS webauthn_credentials_set_updated_at ON webauthn_credentials;
DROP TABLE IF EXISTS webauthn_credentials;
