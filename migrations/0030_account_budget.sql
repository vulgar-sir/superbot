-- +goose Up
ALTER TABLE accounts
    ADD COLUMN weekly_budget_micros bigint NOT NULL DEFAULT 100000000;

-- +goose Down
ALTER TABLE accounts DROP COLUMN IF EXISTS weekly_budget_micros;
