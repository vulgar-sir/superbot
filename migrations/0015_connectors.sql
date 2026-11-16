-- +goose Up
CREATE TABLE connectors (
    id                uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    owner_id          uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    provider          text NOT NULL,
    name              text NOT NULL,
    auth_type         text NOT NULL DEFAULT 'token'
                      CHECK (auth_type IN ('token', 'oauth')),
    access_token_enc  bytea,
    refresh_token_enc bytea,
    config            jsonb NOT NULL DEFAULT '{}'::jsonb,
    status            text NOT NULL DEFAULT 'active'
                      CHECK (status IN ('active', 'disabled')),
    expires_at        timestamptz,
    created_at        timestamptz NOT NULL DEFAULT now(),
    updated_at        timestamptz NOT NULL DEFAULT now(),
    UNIQUE (owner_id, provider, name)
);
CREATE INDEX connectors_owner_idx ON connectors(owner_id, provider, created_at DESC);

CREATE TRIGGER connectors_set_updated_at
BEFORE UPDATE ON connectors
FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- +goose Down
DROP TRIGGER IF EXISTS connectors_set_updated_at ON connectors;
DROP TABLE IF EXISTS connectors;
