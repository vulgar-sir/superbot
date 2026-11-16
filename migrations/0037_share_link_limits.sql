-- +goose Up
ALTER TABLE bot_share_links
    ADD COLUMN revoked_at timestamptz,
    ADD COLUMN expires_at timestamptz,
    ADD COLUMN uses integer NOT NULL DEFAULT 0,
    ADD COLUMN max_uses integer;

-- +goose Down
ALTER TABLE bot_share_links DROP COLUMN IF EXISTS max_uses;
ALTER TABLE bot_share_links DROP COLUMN IF EXISTS uses;
ALTER TABLE bot_share_links DROP COLUMN IF EXISTS expires_at;
ALTER TABLE bot_share_links DROP COLUMN IF EXISTS revoked_at;
