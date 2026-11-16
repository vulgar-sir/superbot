-- +goose Up
CREATE TABLE vault_items (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    owner_id     uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    kind         text NOT NULL DEFAULT 'login'
                 CHECK (kind IN ('login', 'payment', 'contact')),
    name         text NOT NULL,
    username     text NOT NULL DEFAULT '',
    password_enc bytea,
    url          text NOT NULL DEFAULT '',
    notes_enc    bytea,
    metadata     jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX vault_items_owner_idx ON vault_items(owner_id, created_at DESC);

CREATE TRIGGER vault_items_set_updated_at
BEFORE UPDATE ON vault_items
FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- +goose Down
DROP TRIGGER IF EXISTS vault_items_set_updated_at ON vault_items;
DROP TABLE IF EXISTS vault_items;
