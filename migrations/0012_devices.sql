-- +goose Up
CREATE TABLE devices (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    owner_id     uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name         text NOT NULL,
    token_hash   bytea NOT NULL UNIQUE,
    platform     text NOT NULL DEFAULT '',
    status       text NOT NULL DEFAULT 'offline'
                 CHECK (status IN ('online', 'offline')),
    allow_bot    boolean NOT NULL DEFAULT false,
    last_seen_at timestamptz,
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX devices_owner_idx ON devices(owner_id, created_at DESC);

CREATE TABLE device_jobs (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    owner_id    uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    device_id   uuid NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    kind        text NOT NULL CHECK (kind IN ('shell', 'screenshot')),
    command     text NOT NULL DEFAULT '',
    status      text NOT NULL DEFAULT 'pending'
                CHECK (status IN ('pending', 'running', 'done', 'failed')),
    output      text NOT NULL DEFAULT '',
    error       text NOT NULL DEFAULT '',
    created_at  timestamptz NOT NULL DEFAULT now(),
    started_at  timestamptz,
    finished_at timestamptz
);
CREATE INDEX device_jobs_pending_idx ON device_jobs(device_id, created_at) WHERE status = 'pending';
CREATE INDEX device_jobs_owner_idx ON device_jobs(owner_id, created_at DESC);

CREATE TRIGGER devices_set_updated_at
BEFORE UPDATE ON devices
FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- +goose Down
DROP TRIGGER IF EXISTS devices_set_updated_at ON devices;
DROP TABLE IF EXISTS device_jobs;
DROP TABLE IF EXISTS devices;
