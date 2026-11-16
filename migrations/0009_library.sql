-- +goose Up
CREATE TABLE library_items (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    owner_id        uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    bot_id          uuid NOT NULL REFERENCES bots(id) ON DELETE CASCADE,
    kind            text NOT NULL DEFAULT 'page'
                    CHECK (kind IN ('page', 'document', 'file', 'media')),
    title           text NOT NULL,
    description     text NOT NULL DEFAULT '',
    content         text NOT NULL DEFAULT '',
    mime_type       text NOT NULL DEFAULT 'text/markdown',
    file_path       text NOT NULL DEFAULT '',
    conversation_id uuid REFERENCES conversations(id) ON DELETE SET NULL,
    message_id      uuid REFERENCES messages(id) ON DELETE SET NULL,
    metadata        jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX library_items_bot_idx ON library_items(bot_id, created_at DESC);
CREATE INDEX library_items_owner_idx ON library_items(owner_id, created_at DESC);

CREATE TRIGGER library_items_set_updated_at
BEFORE UPDATE ON library_items
FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- +goose Down
DROP TRIGGER IF EXISTS library_items_set_updated_at ON library_items;
DROP TABLE IF EXISTS library_items;
