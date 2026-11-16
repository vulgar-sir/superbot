-- +goose Up
CREATE TABLE accounts (
    id                 uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name               text NOT NULL,
    primary_bot_id     uuid REFERENCES bots(id) ON DELETE SET NULL,
    created_by_user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at         timestamptz NOT NULL DEFAULT now(),
    updated_at         timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE account_members (
    account_id uuid NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    user_id    uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role       text NOT NULL DEFAULT 'member'
               CHECK (role IN ('owner', 'admin', 'member')),
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (account_id, user_id)
);
CREATE INDEX account_members_user_idx ON account_members(user_id);

CREATE TABLE account_invitations (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    account_id uuid NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    email      text NOT NULL,
    role       text NOT NULL DEFAULT 'member'
               CHECK (role IN ('admin', 'member')),
    token_hash bytea NOT NULL UNIQUE,
    invited_by uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    expires_at timestamptz NOT NULL,
    accepted_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE resource_acl (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    account_id  uuid NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    resource_type text NOT NULL,
    resource_id uuid NOT NULL,
    user_id     uuid REFERENCES users(id) ON DELETE CASCADE,
    role        text NOT NULL DEFAULT 'viewer'
                CHECK (role IN ('viewer', 'editor', 'runner')),
    created_at  timestamptz NOT NULL DEFAULT now(),
    UNIQUE (resource_type, resource_id, user_id)
);

CREATE TABLE api_tokens (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id    uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    account_id uuid REFERENCES accounts(id) ON DELETE CASCADE,
    name       text NOT NULL,
    token_hash bytea NOT NULL UNIQUE,
    scopes     jsonb NOT NULL DEFAULT '[]'::jsonb,
    last_used_at timestamptz,
    expires_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now()
);

INSERT INTO accounts (id, name, created_by_user_id)
SELECT id, COALESCE(NULLIF(display_name, ''), email), id FROM users;

UPDATE accounts a
SET primary_bot_id = u.primary_bot_id
FROM users u
WHERE u.id = a.id AND u.primary_bot_id IS NOT NULL;

INSERT INTO account_members (account_id, user_id, role)
SELECT id, id, 'owner' FROM users;

ALTER TABLE sessions ADD COLUMN active_account_id uuid REFERENCES accounts(id) ON DELETE CASCADE;
UPDATE sessions SET active_account_id = user_id;
ALTER TABLE sessions ALTER COLUMN active_account_id SET NOT NULL;
CREATE INDEX sessions_active_account_idx ON sessions(active_account_id);

-- Root resources get account_id for the account-scoped read path. Columns stay
-- nullable until the application has switched every insert; legacy inserts still
-- work while the migration is rolled out.
ALTER TABLE providers ADD COLUMN account_id uuid REFERENCES accounts(id) ON DELETE CASCADE;
ALTER TABLE bots ADD COLUMN account_id uuid REFERENCES accounts(id) ON DELETE CASCADE;
ALTER TABLE teams ADD COLUMN account_id uuid REFERENCES accounts(id) ON DELETE CASCADE;
ALTER TABLE sidebar_sections ADD COLUMN account_id uuid REFERENCES accounts(id) ON DELETE CASCADE;
ALTER TABLE skills ADD COLUMN account_id uuid REFERENCES accounts(id) ON DELETE CASCADE;
ALTER TABLE mcp_servers ADD COLUMN account_id uuid REFERENCES accounts(id) ON DELETE CASCADE;
ALTER TABLE bot_routines ADD COLUMN account_id uuid REFERENCES accounts(id) ON DELETE CASCADE;
ALTER TABLE library_items ADD COLUMN account_id uuid REFERENCES accounts(id) ON DELETE CASCADE;
ALTER TABLE vault_items ADD COLUMN account_id uuid REFERENCES accounts(id) ON DELETE CASCADE;
ALTER TABLE devices ADD COLUMN account_id uuid REFERENCES accounts(id) ON DELETE CASCADE;
ALTER TABLE connectors ADD COLUMN account_id uuid REFERENCES accounts(id) ON DELETE CASCADE;
ALTER TABLE permission_rules ADD COLUMN account_id uuid REFERENCES accounts(id) ON DELETE CASCADE;
ALTER TABLE tool_approvals ADD COLUMN account_id uuid REFERENCES accounts(id) ON DELETE CASCADE;
ALTER TABLE attachments ADD COLUMN account_id uuid REFERENCES accounts(id) ON DELETE CASCADE;
ALTER TABLE bot_share_links ADD COLUMN account_id uuid REFERENCES accounts(id) ON DELETE CASCADE;
ALTER TABLE workspaces ADD COLUMN account_id uuid REFERENCES accounts(id) ON DELETE CASCADE;
ALTER TABLE workspace_actions ADD COLUMN account_id uuid REFERENCES accounts(id) ON DELETE CASCADE;
ALTER TABLE usage_records ADD COLUMN account_id uuid REFERENCES accounts(id) ON DELETE CASCADE;
ALTER TABLE memories ADD COLUMN account_id uuid REFERENCES accounts(id) ON DELETE CASCADE;
ALTER TABLE activities ADD COLUMN account_id uuid REFERENCES accounts(id) ON DELETE CASCADE;
ALTER TABLE notifications ADD COLUMN account_id uuid REFERENCES accounts(id) ON DELETE CASCADE;
ALTER TABLE agent_threads ADD COLUMN account_id uuid REFERENCES accounts(id) ON DELETE CASCADE;
ALTER TABLE subagent_tasks ADD COLUMN account_id uuid REFERENCES accounts(id) ON DELETE CASCADE;
ALTER TABLE conversations ADD COLUMN account_id uuid REFERENCES accounts(id) ON DELETE CASCADE;

UPDATE providers SET account_id = owner_id WHERE account_id IS NULL;
UPDATE bots SET account_id = owner_id WHERE account_id IS NULL;
UPDATE teams SET account_id = owner_id WHERE account_id IS NULL;
UPDATE sidebar_sections SET account_id = owner_id WHERE account_id IS NULL;
UPDATE skills SET account_id = owner_id WHERE account_id IS NULL;
UPDATE mcp_servers SET account_id = owner_id WHERE account_id IS NULL;
UPDATE bot_routines SET account_id = owner_id WHERE account_id IS NULL;
UPDATE library_items SET account_id = owner_id WHERE account_id IS NULL;
UPDATE vault_items SET account_id = owner_id WHERE account_id IS NULL;
UPDATE devices SET account_id = owner_id WHERE account_id IS NULL;
UPDATE connectors SET account_id = owner_id WHERE account_id IS NULL;
UPDATE permission_rules SET account_id = owner_id WHERE account_id IS NULL;
UPDATE tool_approvals SET account_id = owner_id WHERE account_id IS NULL;
UPDATE attachments SET account_id = owner_id WHERE account_id IS NULL;
UPDATE bot_share_links SET account_id = owner_id WHERE account_id IS NULL;
UPDATE workspaces SET account_id = owner_id WHERE account_id IS NULL;
UPDATE workspace_actions SET account_id = owner_id WHERE account_id IS NULL;
UPDATE usage_records SET account_id = user_id WHERE account_id IS NULL;
UPDATE memories SET account_id = owner_id WHERE account_id IS NULL;
UPDATE activities SET account_id = owner_id WHERE account_id IS NULL;
UPDATE notifications SET account_id = owner_id WHERE account_id IS NULL;
UPDATE agent_threads SET account_id = owner_id WHERE account_id IS NULL;
UPDATE subagent_tasks SET account_id = owner_id WHERE account_id IS NULL;
UPDATE conversations SET account_id = created_by WHERE account_id IS NULL;

CREATE INDEX providers_account_idx ON providers(account_id);
CREATE INDEX bots_account_idx ON bots(account_id);
CREATE INDEX teams_account_idx ON teams(account_id);
CREATE INDEX sidebar_sections_account_idx ON sidebar_sections(account_id);
CREATE INDEX conversations_account_idx ON conversations(account_id, updated_at DESC);
CREATE INDEX activities_account_idx ON activities(account_id, created_at DESC);
CREATE INDEX notifications_account_idx ON notifications(account_id, created_at DESC);

-- +goose Down
DROP INDEX IF EXISTS notifications_account_idx;
DROP INDEX IF EXISTS activities_account_idx;
DROP INDEX IF EXISTS conversations_account_idx;
DROP INDEX IF EXISTS sidebar_sections_account_idx;
DROP INDEX IF EXISTS teams_account_idx;
DROP INDEX IF EXISTS bots_account_idx;
DROP INDEX IF EXISTS providers_account_idx;
ALTER TABLE conversations DROP COLUMN IF EXISTS account_id;
ALTER TABLE subagent_tasks DROP COLUMN IF EXISTS account_id;
ALTER TABLE agent_threads DROP COLUMN IF EXISTS account_id;
ALTER TABLE notifications DROP COLUMN IF EXISTS account_id;
ALTER TABLE activities DROP COLUMN IF EXISTS account_id;
ALTER TABLE memories DROP COLUMN IF EXISTS account_id;
ALTER TABLE usage_records DROP COLUMN IF EXISTS account_id;
ALTER TABLE workspace_actions DROP COLUMN IF EXISTS account_id;
ALTER TABLE workspaces DROP COLUMN IF EXISTS account_id;
ALTER TABLE bot_share_links DROP COLUMN IF EXISTS account_id;
ALTER TABLE attachments DROP COLUMN IF EXISTS account_id;
ALTER TABLE tool_approvals DROP COLUMN IF EXISTS account_id;
ALTER TABLE permission_rules DROP COLUMN IF EXISTS account_id;
ALTER TABLE connectors DROP COLUMN IF EXISTS account_id;
ALTER TABLE devices DROP COLUMN IF EXISTS account_id;
ALTER TABLE vault_items DROP COLUMN IF EXISTS account_id;
ALTER TABLE library_items DROP COLUMN IF EXISTS account_id;
ALTER TABLE bot_routines DROP COLUMN IF EXISTS account_id;
ALTER TABLE mcp_servers DROP COLUMN IF EXISTS account_id;
ALTER TABLE skills DROP COLUMN IF EXISTS account_id;
ALTER TABLE sidebar_sections DROP COLUMN IF EXISTS account_id;
ALTER TABLE teams DROP COLUMN IF EXISTS account_id;
ALTER TABLE bots DROP COLUMN IF EXISTS account_id;
ALTER TABLE providers DROP COLUMN IF EXISTS account_id;
ALTER TABLE sessions DROP COLUMN IF EXISTS active_account_id;
DROP TABLE IF EXISTS api_tokens;
DROP TABLE IF EXISTS resource_acl;
DROP TABLE IF EXISTS account_invitations;
DROP TABLE IF EXISTS account_members;
DROP TABLE IF EXISTS accounts;
