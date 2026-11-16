-- name: CreateDiagnosticEvent :exec
INSERT INTO diagnostic_events (
    account_id, user_id, kind, message, stack, url, user_agent
)
VALUES ($1, $2, $3, $4, $5, $6, $7);
