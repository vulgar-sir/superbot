-- +goose Up
ALTER TABLE audit_logs ADD COLUMN account_id uuid REFERENCES accounts(id) ON DELETE SET NULL;
CREATE INDEX audit_logs_account_created_idx ON audit_logs(account_id, created_at DESC);

-- +goose Down
DROP INDEX IF EXISTS audit_logs_account_created_idx;
ALTER TABLE audit_logs DROP COLUMN IF EXISTS account_id;
