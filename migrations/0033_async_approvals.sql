-- +goose Up
ALTER TABLE cycles DROP CONSTRAINT IF EXISTS cycles_status_check;
ALTER TABLE cycles
    ADD CONSTRAINT cycles_status_check
    CHECK (status IN ('queued', 'running', 'waiting_user', 'waiting_approval', 'done', 'canceled', 'failed'));
ALTER TABLE cycles ADD COLUMN resume_checkpoint_id uuid;

CREATE TABLE run_checkpoints (
    id                uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    owner_id          uuid NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    run_id            uuid NOT NULL REFERENCES runs(id) ON DELETE CASCADE,
    cycle_id          uuid NOT NULL REFERENCES cycles(id) ON DELETE CASCADE,
    conversation_id   uuid NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
    bot_id            uuid NOT NULL REFERENCES bots(id) ON DELETE CASCADE,
    step              integer NOT NULL,
    messages          jsonb NOT NULL DEFAULT '[]'::jsonb,
    pending_call      jsonb NOT NULL,
    approval_id       uuid REFERENCES tool_approvals(id) ON DELETE SET NULL,
    status            text NOT NULL DEFAULT 'waiting'
                      CHECK (status IN ('waiting', 'consumed', 'discarded')),
    created_at        timestamptz NOT NULL DEFAULT now(),
    consumed_at       timestamptz
);

CREATE INDEX run_checkpoints_approval_idx ON run_checkpoints(approval_id);
ALTER TABLE tool_approvals ADD COLUMN checkpoint_id uuid REFERENCES run_checkpoints(id) ON DELETE SET NULL;

-- +goose Down
ALTER TABLE tool_approvals DROP COLUMN IF EXISTS checkpoint_id;
DROP TABLE IF EXISTS run_checkpoints;
ALTER TABLE cycles DROP COLUMN IF EXISTS resume_checkpoint_id;
ALTER TABLE cycles DROP CONSTRAINT IF EXISTS cycles_status_check;
ALTER TABLE cycles
    ADD CONSTRAINT cycles_status_check
    CHECK (status IN ('queued', 'running', 'waiting_user', 'done', 'canceled', 'failed'));
