-- +goose Up
ALTER TABLE users ADD COLUMN auto_review boolean NOT NULL DEFAULT true;

CREATE TABLE permission_rules (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    owner_id   uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    kind       text NOT NULL CHECK (kind IN ('always_ask', 'allow')),
    pattern    text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX permission_rules_owner_idx ON permission_rules(owner_id, created_at);

CREATE TABLE tool_approvals (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    owner_id        uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    conversation_id uuid NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
    run_id          uuid NOT NULL REFERENCES runs(id) ON DELETE CASCADE,
    call_id         text NOT NULL,
    tool_name       text NOT NULL,
    args            jsonb NOT NULL DEFAULT '{}'::jsonb,
    status          text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'allowed', 'denied', 'expired')),
    created_at      timestamptz NOT NULL DEFAULT now(),
    resolved_at     timestamptz
);
CREATE INDEX tool_approvals_pending_idx ON tool_approvals(owner_id, status, created_at DESC);
CREATE INDEX tool_approvals_conversation_idx ON tool_approvals(conversation_id, status);

-- +goose Down
DROP TABLE IF EXISTS tool_approvals;
DROP TABLE IF EXISTS permission_rules;
ALTER TABLE users DROP COLUMN IF EXISTS auto_review;
