-- +goose Up
ALTER TABLE accounts ADD COLUMN plan text NOT NULL DEFAULT 'self_hosted';
ALTER TABLE accounts ADD COLUMN credits_micros bigint NOT NULL DEFAULT 0;

CREATE TABLE credit_ledger (
    id                    uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    account_id            uuid NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    kind                  text NOT NULL,
    amount_micros         bigint NOT NULL,
    balance_after_micros  bigint NOT NULL,
    note                  text NOT NULL DEFAULT '',
    created_by_user_id    uuid REFERENCES users(id) ON DELETE SET NULL,
    created_at            timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX credit_ledger_account_created_idx ON credit_ledger(account_id, created_at DESC);

-- +goose Down
DROP INDEX IF EXISTS credit_ledger_account_created_idx;
DROP TABLE IF EXISTS credit_ledger;
ALTER TABLE accounts DROP COLUMN IF EXISTS credits_micros;
ALTER TABLE accounts DROP COLUMN IF EXISTS plan;
