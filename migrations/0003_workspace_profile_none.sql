-- +goose Up
ALTER TABLE workspaces DROP CONSTRAINT IF EXISTS workspaces_profile_check;
ALTER TABLE workspaces ADD CONSTRAINT workspaces_profile_check
    CHECK (profile IN ('none', 'base', 'browser', 'desktop'));

-- +goose Down
ALTER TABLE workspaces DROP CONSTRAINT IF EXISTS workspaces_profile_check;
ALTER TABLE workspaces ADD CONSTRAINT workspaces_profile_check
    CHECK (profile IN ('base', 'browser', 'desktop'));
