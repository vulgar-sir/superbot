-- +goose Up
ALTER TABLE bots ADD COLUMN team_settings jsonb NOT NULL DEFAULT '{}'::jsonb;

CREATE TABLE team_reviews (
    id                   uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    owner_id             uuid NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    team_bot_id          uuid NOT NULL REFERENCES bots(id) ON DELETE CASCADE,
    kind                 text NOT NULL CHECK (kind IN ('skill', 'memory', 'template')),
    resource_id          uuid,
    title                text NOT NULL,
    description          text NOT NULL DEFAULT '',
    content              text NOT NULL DEFAULT '',
    status               text NOT NULL DEFAULT 'pending'
                         CHECK (status IN ('pending', 'approved', 'rejected')),
    submitted_by_bot_id  uuid REFERENCES bots(id) ON DELETE SET NULL,
    reviewed_by_user_id  uuid REFERENCES users(id) ON DELETE SET NULL,
    created_at           timestamptz NOT NULL DEFAULT now(),
    reviewed_at          timestamptz
);
CREATE INDEX team_reviews_owner_idx ON team_reviews(owner_id, status, created_at DESC);

-- +goose Down
DROP INDEX IF EXISTS team_reviews_owner_idx;
DROP TABLE IF EXISTS team_reviews;
ALTER TABLE bots DROP COLUMN IF EXISTS team_settings;
