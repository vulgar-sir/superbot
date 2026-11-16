-- +goose Up
ALTER TABLE users
    ADD COLUMN role text NOT NULL DEFAULT 'member'
        CHECK (role IN ('admin', 'member'));

UPDATE users
SET role = 'admin'
WHERE id = (
    SELECT id FROM users ORDER BY created_at LIMIT 1
);

-- +goose Down
ALTER TABLE users DROP COLUMN IF EXISTS role;
