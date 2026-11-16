-- +goose Up
CREATE EXTENSION IF NOT EXISTS vector;
CREATE EXTENSION IF NOT EXISTS pg_trgm;

CREATE TABLE users (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    email         text NOT NULL UNIQUE,
    password_hash text NOT NULL,
    display_name  text NOT NULL DEFAULT '',
    created_at    timestamptz NOT NULL DEFAULT now(),
    updated_at    timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE sessions (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id    uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash bytea NOT NULL UNIQUE,
    expires_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX sessions_user_idx ON sessions(user_id);
CREATE INDEX sessions_expires_idx ON sessions(expires_at);

CREATE TABLE providers (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    owner_id    uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    type        text NOT NULL CHECK (type IN ('llm', 'embedding', 'search')),
    kind        text NOT NULL,
    name        text NOT NULL,
    base_url    text NOT NULL DEFAULT '',
    api_key_enc bytea,
    config      jsonb NOT NULL DEFAULT '{}'::jsonb,
    enabled     boolean NOT NULL DEFAULT true,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now(),
    UNIQUE (owner_id, type, name)
);

CREATE TABLE models (
    id                            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    provider_id                   uuid NOT NULL REFERENCES providers(id) ON DELETE CASCADE,
    name                          text NOT NULL,
    context_window                integer NOT NULL DEFAULT 0,
    max_output                    integer NOT NULL DEFAULT 0,
    supports_tools                boolean NOT NULL DEFAULT true,
    supports_vision               boolean NOT NULL DEFAULT false,
    supports_reasoning            boolean NOT NULL DEFAULT false,
    price_in_micros_per_mtok      bigint NOT NULL DEFAULT 0,
    price_cache_micros_per_mtok   bigint NOT NULL DEFAULT 0,
    price_out_micros_per_mtok     bigint NOT NULL DEFAULT 0,
    created_at                    timestamptz NOT NULL DEFAULT now(),
    updated_at                    timestamptz NOT NULL DEFAULT now(),
    UNIQUE (provider_id, name)
);

CREATE TABLE bots (
    id               uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    owner_id         uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name             text NOT NULL,
    slug             text NOT NULL,
    avatar           text NOT NULL DEFAULT '',
    description      text NOT NULL DEFAULT '',
    system_prompt    text NOT NULL DEFAULT '',
    provider_id      uuid REFERENCES providers(id) ON DELETE SET NULL,
    model            text NOT NULL DEFAULT '',
    params           jsonb NOT NULL DEFAULT '{}'::jsonb,
    capabilities     jsonb NOT NULL DEFAULT '{}'::jsonb,
    tool_policy      jsonb NOT NULL DEFAULT '{}'::jsonb,
    memory_enabled   boolean NOT NULL DEFAULT true,
    computer_profile text NOT NULL DEFAULT 'none'
                     CHECK (computer_profile IN ('none', 'browser', 'desktop')),
    created_at       timestamptz NOT NULL DEFAULT now(),
    updated_at       timestamptz NOT NULL DEFAULT now(),
    UNIQUE (owner_id, slug)
);

CREATE TABLE teams (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    owner_id    uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name        text NOT NULL,
    description text NOT NULL DEFAULT '',
    mode        text NOT NULL DEFAULT 'roundtable'
                CHECK (mode IN ('roundtable', 'pipeline', 'router')),
    config      jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE team_bots (
    team_id  uuid NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
    bot_id   uuid NOT NULL REFERENCES bots(id) ON DELETE CASCADE,
    position integer NOT NULL DEFAULT 0,
    role     text NOT NULL DEFAULT 'member',
    PRIMARY KEY (team_id, bot_id)
);

CREATE TABLE conversations (
    id                  uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    team_id             uuid REFERENCES teams(id) ON DELETE CASCADE,
    direct_bot_id       uuid REFERENCES bots(id) ON DELETE CASCADE,
    kind                text NOT NULL CHECK (kind IN ('team', 'direct')),
    title               text NOT NULL DEFAULT '',
    status              text NOT NULL DEFAULT 'active'
                        CHECK (status IN ('active', 'archived')),
    created_by          uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    budget_micros       bigint NOT NULL DEFAULT 0,
    spent_micros        bigint NOT NULL DEFAULT 0,
    reserved_micros     bigint NOT NULL DEFAULT 0,
    last_event_seq      bigint NOT NULL DEFAULT 0,
    last_seen_message_id uuid,
    created_at          timestamptz NOT NULL DEFAULT now(),
    updated_at          timestamptz NOT NULL DEFAULT now(),
    CHECK (
        (kind = 'team' AND team_id IS NOT NULL AND direct_bot_id IS NULL)
        OR
        (kind = 'direct' AND direct_bot_id IS NOT NULL AND team_id IS NULL)
    )
);
CREATE INDEX conversations_team_idx ON conversations(team_id, created_at DESC);
CREATE INDEX conversations_direct_bot_idx ON conversations(direct_bot_id, created_at DESC);

-- cycles 是多 Bot 协作周期的聚合根。trigger_message_id 不建外键，避免
-- messages <-> cycles 的循环依赖；写入时先落消息、再建 cycle、最后回填消息的 cycle_id。
CREATE TABLE cycles (
    id                   uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    conversation_id      uuid NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
    trigger_message_id   uuid NOT NULL,
    status               text NOT NULL DEFAULT 'queued'
                         CHECK (status IN ('queued', 'running', 'waiting_user', 'done', 'canceled', 'failed')),
    round                integer NOT NULL DEFAULT 0,
    max_rounds           integer NOT NULL DEFAULT 6,
    coordinator_state    jsonb NOT NULL DEFAULT '{}'::jsonb,
    cancel_requested     boolean NOT NULL DEFAULT false,
    spent_micros         bigint NOT NULL DEFAULT 0,
    reserved_micros      bigint NOT NULL DEFAULT 0,
    lease_id             uuid,
    lease_until          timestamptz,
    last_seen_message_id uuid,
    error                text NOT NULL DEFAULT '',
    created_at           timestamptz NOT NULL DEFAULT now(),
    updated_at           timestamptz NOT NULL DEFAULT now()
);
-- 同一会话同一时刻只允许一个 queued/running cycle，互斥由数据库保证。
CREATE UNIQUE INDEX cycles_one_active_per_conversation
    ON cycles(conversation_id)
    WHERE status IN ('queued', 'running');
CREATE INDEX cycles_claim_idx ON cycles(status, lease_until)
    WHERE status IN ('queued', 'running');

CREATE TABLE runs (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    cycle_id        uuid NOT NULL REFERENCES cycles(id) ON DELETE CASCADE,
    conversation_id uuid NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
    bot_id          uuid NOT NULL REFERENCES bots(id) ON DELETE RESTRICT,
    status          text NOT NULL DEFAULT 'queued'
                    CHECK (status IN ('queued', 'running', 'succeeded', 'failed', 'canceled', 'interrupted')),
    attempt         integer NOT NULL DEFAULT 0,
    step            integer NOT NULL DEFAULT 0,
    usage_in        bigint NOT NULL DEFAULT 0,
    usage_out       bigint NOT NULL DEFAULT 0,
    cost_micros     bigint NOT NULL DEFAULT 0,
    warning         text NOT NULL DEFAULT '',
    error           text NOT NULL DEFAULT '',
    started_at      timestamptz,
    finished_at     timestamptz,
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX runs_cycle_idx ON runs(cycle_id);

CREATE TABLE messages (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    ordinal         bigserial NOT NULL,
    conversation_id uuid NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
    cycle_id        uuid REFERENCES cycles(id) ON DELETE SET NULL,
    sender_type     text NOT NULL CHECK (sender_type IN ('user', 'bot', 'system')),
    sender_bot_id   uuid REFERENCES bots(id) ON DELETE SET NULL,
    content         text NOT NULL DEFAULT '',
    metadata        jsonb NOT NULL DEFAULT '{}'::jsonb,
    client_msg_id   text,
    run_id          uuid REFERENCES runs(id) ON DELETE SET NULL,
    created_at      timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX messages_conversation_idx ON messages(conversation_id, ordinal);
CREATE UNIQUE INDEX messages_client_msg_uidx
    ON messages(conversation_id, client_msg_id)
    WHERE client_msg_id IS NOT NULL;

CREATE TABLE tool_invocations (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    run_id          uuid NOT NULL REFERENCES runs(id) ON DELETE CASCADE,
    call_id         text NOT NULL,
    name            text NOT NULL,
    args            jsonb NOT NULL DEFAULT '{}'::jsonb,
    status          text NOT NULL DEFAULT 'intent'
                    CHECK (status IN ('intent', 'running', 'done', 'failed')),
    result_artifact text NOT NULL DEFAULT '',
    error           text NOT NULL DEFAULT '',
    started_at      timestamptz,
    finished_at     timestamptz,
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now(),
    UNIQUE (run_id, call_id)
);

CREATE TABLE events (
    id              bigserial PRIMARY KEY,
    conversation_id uuid NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
    seq             bigint NOT NULL,
    cycle_id        uuid REFERENCES cycles(id) ON DELETE SET NULL,
    run_id          uuid REFERENCES runs(id) ON DELETE SET NULL,
    type            text NOT NULL,
    payload         jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at      timestamptz NOT NULL DEFAULT now(),
    UNIQUE (conversation_id, seq)
);
CREATE INDEX events_run_idx ON events(run_id, seq);

CREATE TABLE conversation_summaries (
    id                uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    conversation_id   uuid NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
    up_to_message_id  uuid,
    up_to_seq         bigint NOT NULL DEFAULT 0,
    content           text NOT NULL,
    tokens            integer NOT NULL DEFAULT 0,
    created_at        timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX conversation_summaries_idx ON conversation_summaries(conversation_id, up_to_seq DESC);

CREATE TABLE skills (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    owner_id    uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name        text NOT NULL,
    description text NOT NULL DEFAULT '',
    version     text NOT NULL DEFAULT '1',
    source      text NOT NULL DEFAULT 'upload',
    status      text NOT NULL DEFAULT 'draft'
                CHECK (status IN ('draft', 'active', 'disabled')),
    created_by  text NOT NULL DEFAULT 'user' CHECK (created_by IN ('user', 'bot')),
    files_path  text NOT NULL DEFAULT '',
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now(),
    UNIQUE (owner_id, name, version)
);

CREATE TABLE bot_skills (
    bot_id   uuid NOT NULL REFERENCES bots(id) ON DELETE CASCADE,
    skill_id uuid NOT NULL REFERENCES skills(id) ON DELETE CASCADE,
    enabled  boolean NOT NULL DEFAULT true,
    PRIMARY KEY (bot_id, skill_id)
);

CREATE TABLE mcp_servers (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    owner_id     uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name         text NOT NULL,
    transport    text NOT NULL CHECK (transport IN ('stdio', 'http')),
    command      text NOT NULL DEFAULT '',
    args         jsonb NOT NULL DEFAULT '[]'::jsonb,
    env_enc      bytea,
    url          text NOT NULL DEFAULT '',
    headers_enc  bytea,
    status       text NOT NULL DEFAULT 'draft'
                 CHECK (status IN ('draft', 'active', 'disabled')),
    created_by   text NOT NULL DEFAULT 'user' CHECK (created_by IN ('user', 'bot')),
    config       jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now(),
    UNIQUE (owner_id, name)
);

CREATE TABLE extension_versions (
    id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    extension_type text NOT NULL CHECK (extension_type IN ('skill', 'mcp')),
    extension_id   uuid NOT NULL,
    version        text NOT NULL,
    snapshot       jsonb NOT NULL DEFAULT '{}'::jsonb,
    files_path     text NOT NULL DEFAULT '',
    created_at     timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX extension_versions_idx ON extension_versions(extension_type, extension_id, created_at DESC);

CREATE TABLE bot_mcp_servers (
    bot_id        uuid NOT NULL REFERENCES bots(id) ON DELETE CASCADE,
    mcp_server_id uuid NOT NULL REFERENCES mcp_servers(id) ON DELETE CASCADE,
    enabled       boolean NOT NULL DEFAULT true,
    PRIMARY KEY (bot_id, mcp_server_id)
);

CREATE TABLE workspaces (
    id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    owner_id       uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    bot_id         uuid NOT NULL REFERENCES bots(id) ON DELETE CASCADE,
    profile        text NOT NULL DEFAULT 'base'
                   CHECK (profile IN ('base', 'browser', 'desktop')),
    desired_state  text NOT NULL DEFAULT 'stopped'
                   CHECK (desired_state IN ('running', 'stopped')),
    status         text NOT NULL DEFAULT 'stopped'
                   CHECK (status IN ('stopped', 'starting', 'running', 'error')),
    container_name text NOT NULL DEFAULT '',
    workspace_path text NOT NULL DEFAULT '',
    last_used_at   timestamptz,
    last_error     text NOT NULL DEFAULT '',
    created_at     timestamptz NOT NULL DEFAULT now(),
    updated_at     timestamptz NOT NULL DEFAULT now(),
    UNIQUE (bot_id)
);

CREATE TABLE memories (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    owner_id   uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    scope_type text NOT NULL CHECK (scope_type IN ('bot', 'team')),
    scope_id   uuid NOT NULL,
    content    text NOT NULL,
    embedding  vector(1024),
    metadata   jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX memories_scope_idx ON memories(scope_type, scope_id, created_at DESC);
CREATE INDEX memories_embedding_idx ON memories USING hnsw (embedding vector_cosine_ops);

CREATE TABLE usage_records (
    id                uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id           uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    bot_id            uuid REFERENCES bots(id) ON DELETE SET NULL,
    provider_id       uuid REFERENCES providers(id) ON DELETE SET NULL,
    model             text NOT NULL DEFAULT '',
    cycle_id          uuid REFERENCES cycles(id) ON DELETE SET NULL,
    run_id            uuid REFERENCES runs(id) ON DELETE SET NULL,
    prompt_tokens     bigint NOT NULL DEFAULT 0,
    cached_tokens     bigint NOT NULL DEFAULT 0,
    completion_tokens bigint NOT NULL DEFAULT 0,
    cost_micros       bigint NOT NULL DEFAULT 0,
    estimated         boolean NOT NULL DEFAULT false,
    created_at        timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX usage_created_idx ON usage_records(created_at);
CREATE INDEX usage_cycle_idx ON usage_records(cycle_id);

CREATE TABLE routines (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    team_id     uuid NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
    name        text NOT NULL,
    cron        text NOT NULL,
    prompt      text NOT NULL,
    enabled     boolean NOT NULL DEFAULT true,
    last_run_at timestamptz,
    next_run_at timestamptz,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE audit_logs (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    actor_type  text NOT NULL,
    actor_id    uuid,
    action      text NOT NULL,
    target_type text NOT NULL,
    target_id   uuid,
    payload     jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at  timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX audit_logs_created_idx ON audit_logs(created_at DESC);

CREATE TABLE settings (
    key        text PRIMARY KEY,
    value      jsonb NOT NULL,
    updated_at timestamptz NOT NULL DEFAULT now()
);

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION set_updated_at() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    NEW.updated_at = now();
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

-- +goose StatementBegin
DO $$
DECLARE
    t text;
BEGIN
    FOR t IN
        SELECT unnest(ARRAY[
            'users', 'providers', 'models', 'bots', 'teams', 'conversations',
            'cycles', 'runs', 'tool_invocations', 'skills', 'mcp_servers',
            'workspaces', 'routines'
        ])
    LOOP
        EXECUTE format(
            'CREATE TRIGGER %I_set_updated_at BEFORE UPDATE ON %I FOR EACH ROW EXECUTE FUNCTION set_updated_at()',
            t, t
        );
    END LOOP;
END;
$$;
-- +goose StatementEnd

-- +goose Down
DROP TABLE IF EXISTS settings;
DROP TABLE IF EXISTS audit_logs;
DROP TABLE IF EXISTS routines;
DROP TABLE IF EXISTS usage_records;
DROP TABLE IF EXISTS memories;
DROP TABLE IF EXISTS workspaces;
DROP TABLE IF EXISTS bot_mcp_servers;
DROP TABLE IF EXISTS extension_versions;
DROP TABLE IF EXISTS mcp_servers;
DROP TABLE IF EXISTS bot_skills;
DROP TABLE IF EXISTS skills;
DROP TABLE IF EXISTS conversation_summaries;
DROP TABLE IF EXISTS events;
DROP TABLE IF EXISTS tool_invocations;
DROP TABLE IF EXISTS messages;
DROP TABLE IF EXISTS runs;
DROP TABLE IF EXISTS cycles;
DROP TABLE IF EXISTS conversations;
DROP TABLE IF EXISTS team_bots;
DROP TABLE IF EXISTS teams;
DROP TABLE IF EXISTS bots;
DROP TABLE IF EXISTS models;
DROP TABLE IF EXISTS providers;
DROP TABLE IF EXISTS sessions;
DROP TABLE IF EXISTS users;
DROP FUNCTION IF EXISTS set_updated_at();
