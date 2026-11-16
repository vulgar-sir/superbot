-- +goose Up
ALTER TABLE runs DROP CONSTRAINT IF EXISTS runs_bot_id_fkey;
ALTER TABLE runs
    ADD CONSTRAINT runs_bot_id_fkey
    FOREIGN KEY (bot_id) REFERENCES bots(id) ON DELETE CASCADE;

-- +goose Down
ALTER TABLE runs DROP CONSTRAINT IF EXISTS runs_bot_id_fkey;
ALTER TABLE runs
    ADD CONSTRAINT runs_bot_id_fkey
    FOREIGN KEY (bot_id) REFERENCES bots(id) ON DELETE RESTRICT;
