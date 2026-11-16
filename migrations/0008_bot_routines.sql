-- +goose Up
CREATE TABLE bot_routines (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    owner_id     uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    bot_id       uuid NOT NULL REFERENCES bots(id) ON DELETE CASCADE,
    name         text NOT NULL,
    schedule     text NOT NULL,
    timezone     text NOT NULL DEFAULT 'Asia/Shanghai',
    timing       text NOT NULL DEFAULT 'exact'
                 CHECK (timing IN ('exact', 'flexible')),
    enabled      boolean NOT NULL DEFAULT true,
    paused       boolean NOT NULL DEFAULT false,
    prompt       text NOT NULL DEFAULT '',
    last_run_at  timestamptz,
    next_run_at  timestamptz,
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX bot_routines_bot_idx ON bot_routines(bot_id, created_at DESC);
CREATE INDEX bot_routines_due_idx ON bot_routines(next_run_at) WHERE enabled = true AND paused = false;

CREATE TRIGGER bot_routines_set_updated_at
BEFORE UPDATE ON bot_routines
FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- +goose Down
DROP TRIGGER IF EXISTS bot_routines_set_updated_at ON bot_routines;
DROP TABLE IF EXISTS bot_routines;
