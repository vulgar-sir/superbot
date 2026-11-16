-- +goose Up
ALTER TABLE devices ADD COLUMN public_key text NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE devices DROP COLUMN IF EXISTS public_key;
