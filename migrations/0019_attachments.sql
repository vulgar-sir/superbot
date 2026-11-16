-- +goose Up
CREATE TABLE attachments (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    owner_id   uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    message_id uuid REFERENCES messages(id) ON DELETE CASCADE,
    filename   text NOT NULL,
    mime_type  text NOT NULL,
    size_bytes bigint NOT NULL,
    content    bytea NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX attachments_owner_idx ON attachments(owner_id, created_at DESC);
CREATE INDEX attachments_message_idx ON attachments(message_id);

-- +goose Down
DROP TABLE IF EXISTS attachments;
