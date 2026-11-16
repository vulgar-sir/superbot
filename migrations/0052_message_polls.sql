-- +goose Up
CREATE TABLE message_poll_votes (
    message_id uuid NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
    user_id    uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    option_id  text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (message_id, user_id, option_id)
);
CREATE INDEX message_poll_votes_message_idx ON message_poll_votes(message_id);

-- +goose Down
DROP INDEX IF EXISTS message_poll_votes_message_idx;
DROP TABLE IF EXISTS message_poll_votes;
