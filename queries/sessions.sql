-- name: CreateSession :one
INSERT INTO sessions (user_id, active_account_id, token_hash, expires_at)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: GetSessionUser :one
SELECT sqlc.embed(users), sessions.active_account_id, sessions.expires_at
FROM sessions
JOIN users ON users.id = sessions.user_id
WHERE sessions.token_hash = $1
  AND sessions.expires_at > now();

-- name: DeleteSession :exec
DELETE FROM sessions WHERE token_hash = $1;

-- name: DeleteUserSessions :exec
DELETE FROM sessions WHERE user_id = $1;

-- name: DeleteExpiredSessions :exec
DELETE FROM sessions WHERE expires_at <= now();

-- name: UpdateSessionAccount :execrows
UPDATE sessions
SET active_account_id = $2
WHERE token_hash = $1 AND expires_at > now();

-- name: ListAccountsForUser :many
SELECT accounts.*, account_members.role
FROM accounts
JOIN account_members ON account_members.account_id = accounts.id
WHERE account_members.user_id = $1
ORDER BY accounts.created_at, accounts.id;

-- name: GetAccountMembership :one
SELECT role FROM account_members
WHERE account_id = $1 AND user_id = $2;

-- name: CreateAccount :one
INSERT INTO accounts (name, created_by_user_id)
VALUES ($1, $2)
RETURNING *;

-- name: AddAccountMember :exec
INSERT INTO account_members (account_id, user_id, role)
VALUES ($1, $2, $3)
ON CONFLICT (account_id, user_id) DO NOTHING;

-- name: GetAccountByID :one
SELECT * FROM accounts WHERE id = $1;

-- name: SetAccountPrimaryBot :exec
UPDATE accounts SET primary_bot_id = $2, updated_at = now() WHERE id = $1;

-- name: ListAccountMembers :many
SELECT users.id, users.email, users.display_name, account_members.role, account_members.created_at
FROM account_members
JOIN users ON users.id = account_members.user_id
WHERE account_members.account_id = $1
ORDER BY account_members.created_at, users.email;

-- name: CreateAccountInvitation :one
INSERT INTO account_invitations (account_id, email, role, token_hash, invited_by, expires_at)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: ListAccountInvitations :many
SELECT * FROM account_invitations
WHERE account_id = $1 AND accepted_at IS NULL AND expires_at > now()
ORDER BY created_at DESC;

-- name: GetAccountInvitationByHash :one
SELECT * FROM account_invitations
WHERE token_hash = $1 AND accepted_at IS NULL AND expires_at > now();

-- name: AcceptAccountInvitation :exec
UPDATE account_invitations SET accepted_at = now() WHERE id = $1;

-- name: SetAccountWeeklyBudget :one
UPDATE accounts SET weekly_budget_micros = $2, updated_at = now()
WHERE id = $1 RETURNING weekly_budget_micros;

-- name: CreatePersonalAccount :one
INSERT INTO accounts (id, name, created_by_user_id)
VALUES ($1, $2, $3)
RETURNING *;
