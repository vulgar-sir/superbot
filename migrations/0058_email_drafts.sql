-- +goose Up
CREATE TABLE email_drafts (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    owner_id   uuid NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    to_addr    text NOT NULL DEFAULT '',
    cc_addr    text NOT NULL DEFAULT '',
    subject    text NOT NULL DEFAULT '',
    body       text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX email_drafts_owner_idx ON email_drafts(owner_id, updated_at DESC);

-- +goose Down
DROP INDEX IF EXISTS email_drafts_owner_idx;
DROP TABLE IF EXISTS email_drafts;
