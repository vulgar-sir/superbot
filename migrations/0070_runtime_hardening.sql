-- +goose Up
-- Progress checkpoints let a reclaimed cycle resume a bot turn after the last
-- finished tool round instead of replaying tools that already ran.
ALTER TABLE run_checkpoints ADD COLUMN kind text NOT NULL DEFAULT 'approval'
    CHECK (kind IN ('approval', 'progress'));
ALTER TABLE run_checkpoints ALTER COLUMN pending_call SET DEFAULT '[]'::jsonb;
CREATE UNIQUE INDEX run_checkpoints_progress_run_idx
    ON run_checkpoints(run_id) WHERE kind = 'progress';
CREATE INDEX run_checkpoints_cycle_idx ON run_checkpoints(cycle_id, kind, status);

-- Automatic context checkpoints share the table with manual summaries.
ALTER TABLE conversation_summaries ADD COLUMN source text NOT NULL DEFAULT 'manual'
    CHECK (source IN ('manual', 'auto', 'fallback'));

CREATE INDEX IF NOT EXISTS events_prune_idx ON events(type, created_at);

-- +goose Down
DROP INDEX IF EXISTS events_prune_idx;
ALTER TABLE conversation_summaries DROP COLUMN IF EXISTS source;
DROP INDEX IF EXISTS run_checkpoints_cycle_idx;
DROP INDEX IF EXISTS run_checkpoints_progress_run_idx;
DELETE FROM run_checkpoints WHERE kind = 'progress';
ALTER TABLE run_checkpoints DROP COLUMN IF EXISTS kind;
