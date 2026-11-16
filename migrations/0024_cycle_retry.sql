-- +goose Up
ALTER TABLE cycles
    ADD COLUMN attempt integer NOT NULL DEFAULT 0,
    ADD COLUMN max_attempts integer NOT NULL DEFAULT 3,
    ADD COLUMN next_attempt_at timestamptz NOT NULL DEFAULT now();

CREATE INDEX cycles_retry_claim_idx ON cycles(status, next_attempt_at, created_at)
    WHERE status IN ('queued', 'running');

-- +goose Down
DROP INDEX IF EXISTS cycles_retry_claim_idx;
ALTER TABLE cycles
    DROP COLUMN IF EXISTS next_attempt_at,
    DROP COLUMN IF EXISTS max_attempts,
    DROP COLUMN IF EXISTS attempt;
