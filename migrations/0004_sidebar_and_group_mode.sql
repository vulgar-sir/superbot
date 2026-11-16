-- +goose Up
CREATE TABLE sidebar_sections (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    owner_id   uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name       text NOT NULL,
    position   integer NOT NULL DEFAULT 0,
    collapsed  boolean NOT NULL DEFAULT false,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX sidebar_sections_owner_idx ON sidebar_sections(owner_id, position, created_at);

CREATE TRIGGER sidebar_sections_set_updated_at
BEFORE UPDATE ON sidebar_sections
FOR EACH ROW EXECUTE FUNCTION set_updated_at();

ALTER TABLE bots
    ADD COLUMN section_id uuid REFERENCES sidebar_sections(id) ON DELETE SET NULL,
    ADD COLUMN position integer NOT NULL DEFAULT 0,
    ADD COLUMN title text NOT NULL DEFAULT '',
    ADD COLUMN avatar_shape text NOT NULL DEFAULT '',
    ADD COLUMN avatar_color text NOT NULL DEFAULT '',
    ADD COLUMN pinned boolean NOT NULL DEFAULT false,
    ADD COLUMN last_activity_at timestamptz,
    ADD COLUMN proactivity jsonb NOT NULL DEFAULT '{}'::jsonb;
CREATE INDEX bots_sidebar_idx ON bots(owner_id, section_id, pinned DESC, position, created_at);

ALTER TABLE teams DROP CONSTRAINT IF EXISTS teams_mode_check;
ALTER TABLE teams
    ADD CONSTRAINT teams_mode_check
    CHECK (mode IN ('roundtable', 'pipeline', 'router', 'group'));
ALTER TABLE teams
    ADD COLUMN section_id uuid REFERENCES sidebar_sections(id) ON DELETE SET NULL,
    ADD COLUMN position integer NOT NULL DEFAULT 0,
    ADD COLUMN pinned boolean NOT NULL DEFAULT false,
    ADD COLUMN room_conversation_id uuid REFERENCES conversations(id) ON DELETE SET NULL;
CREATE INDEX teams_sidebar_idx ON teams(owner_id, section_id, pinned DESC, position, created_at);

-- +goose Down
DROP INDEX IF EXISTS teams_sidebar_idx;
ALTER TABLE teams
    DROP COLUMN IF EXISTS room_conversation_id,
    DROP COLUMN IF EXISTS pinned,
    DROP COLUMN IF EXISTS position,
    DROP COLUMN IF EXISTS section_id;
ALTER TABLE teams DROP CONSTRAINT IF EXISTS teams_mode_check;
ALTER TABLE teams
    ADD CONSTRAINT teams_mode_check
    CHECK (mode IN ('roundtable', 'pipeline', 'router'));

DROP INDEX IF EXISTS bots_sidebar_idx;
ALTER TABLE bots
    DROP COLUMN IF EXISTS proactivity,
    DROP COLUMN IF EXISTS last_activity_at,
    DROP COLUMN IF EXISTS pinned,
    DROP COLUMN IF EXISTS avatar_color,
    DROP COLUMN IF EXISTS avatar_shape,
    DROP COLUMN IF EXISTS title,
    DROP COLUMN IF EXISTS position,
    DROP COLUMN IF EXISTS section_id;

DROP TRIGGER IF EXISTS sidebar_sections_set_updated_at ON sidebar_sections;
DROP TABLE IF EXISTS sidebar_sections;
