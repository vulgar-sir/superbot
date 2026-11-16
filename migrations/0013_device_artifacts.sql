-- +goose Up
ALTER TABLE device_jobs ADD COLUMN artifact_path text NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE device_jobs DROP COLUMN IF EXISTS artifact_path;
