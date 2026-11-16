-- +goose Up
ALTER TABLE webauthn_credentials ADD COLUMN name text NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE webauthn_credentials DROP COLUMN IF EXISTS name;
