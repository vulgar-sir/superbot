-- name: CreateCycle :one
INSERT INTO cycles (conversation_id, trigger_message_id, max_rounds)
VALUES ($1, $2, $3)
RETURNING *;

-- name: GetCycle :one
SELECT * FROM cycles WHERE id = $1;

-- name: GetActiveCycle :one
SELECT * FROM cycles
WHERE conversation_id = $1
  AND status IN ('queued', 'running')
ORDER BY created_at DESC
LIMIT 1;

-- name: LockCycleLease :one
SELECT id FROM cycles
WHERE id = $1
  AND lease_id = $2
  AND lease_until > now()
FOR UPDATE;

-- name: ListCyclesByConversation :many
SELECT * FROM cycles
WHERE conversation_id = $1
ORDER BY created_at DESC
LIMIT $2;

-- ClaimCycle 领取一个排队中的 cycle，或接管一个租约已过期的 running cycle。
-- 新 lease_id 用于 fencing：后续所有写入都必须带同一个 lease_id。
-- name: ClaimCycle :one
WITH candidate AS (
    SELECT id
    FROM cycles
    WHERE (status = 'queued' AND next_attempt_at <= now())
       OR (status = 'running' AND lease_until < now() AND attempt < max_attempts)
    ORDER BY next_attempt_at, created_at
    FOR UPDATE SKIP LOCKED
    LIMIT 1
)
UPDATE cycles c
SET status = 'running',
    attempt = c.attempt + 1,
    lease_id = gen_random_uuid(),
    lease_until = now() + interval '60 seconds',
    updated_at = now()
FROM candidate
WHERE c.id = candidate.id
RETURNING c.*;

-- name: HeartbeatCycle :one
UPDATE cycles
SET lease_until = now() + interval '60 seconds',
    updated_at = now()
WHERE id = $1 AND lease_id = $2
RETURNING lease_until;

-- name: UpdateCycleProgress :execrows
UPDATE cycles
SET round = $3,
    coordinator_state = $4,
    last_seen_message_id = $5,
    updated_at = now()
WHERE id = $1 AND lease_id = $2;

-- name: SetCycleWaitingUser :execrows
UPDATE cycles
SET status = 'waiting_user',
    lease_id = NULL,
    lease_until = NULL,
    updated_at = now()
WHERE id = $1 AND lease_id = $2 AND status = 'running';

-- name: RetryOrFailCycle :one
UPDATE cycles
SET status = CASE WHEN attempt >= max_attempts THEN 'failed' ELSE 'queued' END,
    error = $3,
    lease_id = NULL,
    lease_until = NULL,
    next_attempt_at = CASE
        WHEN attempt >= max_attempts THEN next_attempt_at
        ELSE now() + make_interval(secs => power(2, attempt)::int)
    END,
    updated_at = now()
WHERE id = $1 AND lease_id = $2 AND status = 'running'
RETURNING status, attempt, max_attempts, error;

-- name: SetCycleDone :execrows
UPDATE cycles
SET status = 'done',
    lease_id = NULL,
    lease_until = NULL,
    updated_at = now()
WHERE id = $1 AND lease_id = $2 AND status = 'running';

-- name: SetCycleCanceled :execrows
UPDATE cycles
SET status = 'canceled',
    lease_id = NULL,
    lease_until = NULL,
    updated_at = now()
WHERE id = $1 AND lease_id = $2 AND status = 'running';

-- name: SetCycleFailed :execrows
UPDATE cycles
SET status = 'failed',
    error = $3,
    lease_id = NULL,
    lease_until = NULL,
    updated_at = now()
WHERE id = $1 AND lease_id = $2 AND status = 'running';

-- name: RequestCycleCancel :execrows
UPDATE cycles
SET cancel_requested = true,
    updated_at = now()
WHERE conversation_id = $1
  AND status IN ('queued', 'running');

-- name: ReserveCycleBudget :execrows
UPDATE cycles c
SET reserved_micros = c.reserved_micros + $2
WHERE c.id = $1
  AND (
      (SELECT budget_micros FROM conversations WHERE conversations.id = c.conversation_id) = 0
      OR c.spent_micros + c.reserved_micros + $2
         <= (SELECT budget_micros FROM conversations WHERE conversations.id = c.conversation_id)
  );

-- name: ReleaseCycleReservation :exec
UPDATE cycles
SET reserved_micros = GREATEST(reserved_micros - $2, 0)
WHERE id = $1;

-- name: SettleCycleUsage :exec
UPDATE cycles
SET reserved_micros = GREATEST(reserved_micros - $2, 0),
    spent_micros = spent_micros + $3,
    updated_at = now()
WHERE id = $1;

-- name: SetCycleWaitingApproval :execrows
UPDATE cycles
SET status = 'waiting_approval',
    lease_id = NULL,
    lease_until = NULL,
    updated_at = now()
WHERE id = $1 AND lease_id = $2 AND status = 'running';

-- name: SetCycleResumeCheckpoint :exec
UPDATE cycles SET resume_checkpoint_id = $2, updated_at = now() WHERE id = $1;

-- A worker that keeps dying mid-cycle must not be reclaimed forever.
-- name: FailExhaustedCycles :many
UPDATE cycles
SET status = 'failed',
    error = 'worker 多次中断，已停止自动重试',
    lease_id = NULL,
    lease_until = NULL,
    updated_at = now()
WHERE status = 'running'
  AND lease_until < now()
  AND attempt >= max_attempts
RETURNING id, conversation_id;

-- Graceful shutdown hands the cycle back immediately and does not count the
-- interrupted attempt against max_attempts.
-- name: ReleaseCycleLease :execrows
UPDATE cycles
SET lease_until = now() - interval '1 second',
    attempt = GREATEST(attempt - 1, 0),
    updated_at = now()
WHERE id = $1 AND lease_id = $2 AND status = 'running';
