-- +goose Up
ALTER TABLE memories DROP CONSTRAINT IF EXISTS memories_scope_type_check;
ALTER TABLE memories ADD CONSTRAINT memories_scope_type_check
    CHECK (scope_type IN ('bot', 'team', 'user'));

-- +goose Down
ALTER TABLE memories DROP CONSTRAINT IF EXISTS memories_scope_type_check;
ALTER TABLE memories ADD CONSTRAINT memories_scope_type_check
    CHECK (scope_type IN ('bot', 'team'));
