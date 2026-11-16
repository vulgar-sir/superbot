-- +goose Up
ALTER TABLE device_jobs ADD COLUMN nonce text NOT NULL DEFAULT '';
CREATE INDEX device_jobs_nonce_idx ON device_jobs(nonce);

-- +goose Down
DROP INDEX IF EXISTS device_jobs_nonce_idx;
ALTER TABLE device_jobs DROP COLUMN IF EXISTS nonce;
