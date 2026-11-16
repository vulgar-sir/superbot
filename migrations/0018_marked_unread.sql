-- +goose Up
ALTER TABLE bots ADD COLUMN marked_unread boolean NOT NULL DEFAULT false;
ALTER TABLE teams ADD COLUMN marked_unread boolean NOT NULL DEFAULT false;

-- +goose Down
ALTER TABLE teams DROP COLUMN IF EXISTS marked_unread;
ALTER TABLE bots DROP COLUMN IF EXISTS marked_unread;
