-- +goose Up
ALTER TABLE workspaces ADD COLUMN IF NOT EXISTS vnc_addr text NOT NULL DEFAULT '';
ALTER TABLE workspaces ADD COLUMN IF NOT EXISTS cdp_addr text NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE workspaces DROP COLUMN IF EXISTS vnc_addr;
ALTER TABLE workspaces DROP COLUMN IF EXISTS cdp_addr;
