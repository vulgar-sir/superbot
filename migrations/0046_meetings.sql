-- +goose Up
CREATE TABLE meetings (
    id                 uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    owner_id           uuid NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    title              text NOT NULL,
    transcript         text NOT NULL DEFAULT '',
    summary            text NOT NULL DEFAULT '',
    starts_at          timestamptz NOT NULL DEFAULT now(),
    created_by_user_id uuid REFERENCES users(id) ON DELETE SET NULL,
    created_at         timestamptz NOT NULL DEFAULT now(),
    updated_at         timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX meetings_owner_idx ON meetings(owner_id, starts_at DESC);

-- +goose Down
DROP TABLE IF EXISTS meetings;
