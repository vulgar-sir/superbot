-- +goose Up
CREATE TABLE bot_share_links (
    token      text PRIMARY KEY,
    owner_id   uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    bot_id     uuid REFERENCES bots(id) ON DELETE CASCADE,
    template   jsonb NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX bot_share_links_owner_idx ON bot_share_links(owner_id, created_at DESC);

-- +goose Down
DROP TABLE IF EXISTS bot_share_links;
