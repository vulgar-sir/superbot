-- +goose Up
-- Every bot has its own computer. A new bot gets a browser computer, and the
-- bots that had none are given one.
ALTER TABLE bots ALTER COLUMN computer_profile SET DEFAULT 'browser';
UPDATE bots SET computer_profile = 'browser' WHERE computer_profile = 'none';

-- 'provision' creates a bot's computer without starting it, so a new bot has
-- one right away but costs no CPU or memory until it is used.
ALTER TABLE workspace_actions DROP CONSTRAINT IF EXISTS workspace_actions_kind_check;
ALTER TABLE workspace_actions ADD CONSTRAINT workspace_actions_kind_check
    CHECK (kind IN ('update', 'reset', 'snapshot', 'restore', 'provision'));

-- Disk used by the bot's workspace, measured by the worker.
ALTER TABLE workspaces ADD COLUMN IF NOT EXISTS disk_bytes bigint NOT NULL DEFAULT 0;
ALTER TABLE workspaces ADD COLUMN IF NOT EXISTS disk_measured_at timestamptz;

-- Computers whose bot was deleted. There is no foreign key: the bot row is
-- gone by the time the worker removes its container, workspace and snapshots.
CREATE TABLE computer_removals (
    bot_id          uuid PRIMARY KEY,
    owner_id        uuid NOT NULL,
    requested_at    timestamptz NOT NULL DEFAULT now(),
    attempts        integer NOT NULL DEFAULT 0,
    last_error      text NOT NULL DEFAULT '',
    next_attempt_at timestamptz NOT NULL DEFAULT now()
);

-- Creating a bot creates its computer and deleting a bot deletes it, however
-- the bot was created or deleted (API, template, admin tool, account removal).
-- +goose StatementBegin
CREATE FUNCTION bot_computer_lifecycle() RETURNS trigger AS $$
BEGIN
    IF TG_OP = 'INSERT' THEN
        INSERT INTO workspace_actions (owner_id, bot_id, kind)
        VALUES (NEW.owner_id, NEW.id, 'provision');
        RETURN NEW;
    END IF;
    INSERT INTO computer_removals (bot_id, owner_id)
    VALUES (OLD.id, OLD.owner_id)
    ON CONFLICT (bot_id) DO NOTHING;
    RETURN OLD;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

CREATE TRIGGER bots_provision_computer AFTER INSERT ON bots
    FOR EACH ROW EXECUTE FUNCTION bot_computer_lifecycle();
CREATE TRIGGER bots_remove_computer AFTER DELETE ON bots
    FOR EACH ROW EXECUTE FUNCTION bot_computer_lifecycle();

-- The existing bots get their computers now rather than on first use.
INSERT INTO workspace_actions (owner_id, bot_id, kind)
SELECT owner_id, id, 'provision' FROM bots;

-- +goose Down
DROP TRIGGER IF EXISTS bots_remove_computer ON bots;
DROP TRIGGER IF EXISTS bots_provision_computer ON bots;
DROP FUNCTION IF EXISTS bot_computer_lifecycle();
DROP TABLE IF EXISTS computer_removals;
ALTER TABLE workspaces DROP COLUMN IF EXISTS disk_measured_at;
ALTER TABLE workspaces DROP COLUMN IF EXISTS disk_bytes;
DELETE FROM workspace_actions WHERE kind = 'provision';
ALTER TABLE workspace_actions DROP CONSTRAINT IF EXISTS workspace_actions_kind_check;
ALTER TABLE workspace_actions ADD CONSTRAINT workspace_actions_kind_check
    CHECK (kind IN ('update', 'reset', 'snapshot', 'restore'));
ALTER TABLE bots ALTER COLUMN computer_profile SET DEFAULT 'none';
