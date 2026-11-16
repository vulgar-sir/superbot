-- name: GetAccountBilling :one
SELECT plan, credits_micros FROM accounts WHERE id = $1;

-- name: UpdateAccountCredits :exec
UPDATE accounts SET credits_micros = $2 WHERE id = $1;

-- name: CreateCreditLedgerEntry :one
INSERT INTO credit_ledger (account_id, kind, amount_micros, balance_after_micros, note, created_by_user_id)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: ListCreditLedger :many
SELECT * FROM credit_ledger
WHERE account_id = $1
ORDER BY created_at DESC
LIMIT $2;
