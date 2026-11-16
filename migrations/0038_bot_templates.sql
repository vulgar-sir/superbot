-- +goose Up
CREATE TABLE bot_templates (
    id                uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    owner_id          uuid NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    name              text NOT NULL,
    description       text NOT NULL DEFAULT '',
    version           integer NOT NULL DEFAULT 1,
    bot_id            uuid REFERENCES bots(id) ON DELETE SET NULL,
    template          jsonb NOT NULL,
    visibility        text NOT NULL DEFAULT 'account' CHECK (visibility IN ('owner', 'account')),
    status            text NOT NULL DEFAULT 'published' CHECK (status IN ('published', 'disabled')),
    created_by_user_id uuid REFERENCES users(id) ON DELETE SET NULL,
    created_at        timestamptz NOT NULL DEFAULT now(),
    updated_at        timestamptz NOT NULL DEFAULT now(),
    UNIQUE (owner_id, name, version)
);
CREATE INDEX bot_templates_owner_idx ON bot_templates(owner_id, visibility, created_at DESC);

-- +goose Down
DROP TABLE IF EXISTS bot_templates;
