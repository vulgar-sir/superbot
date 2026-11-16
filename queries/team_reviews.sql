-- name: CreateTeamReview :one
INSERT INTO team_reviews (
    owner_id, team_bot_id, kind, resource_id, title, description, content,
    submitted_by_bot_id
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
RETURNING *;

-- name: ListTeamReviews :many
SELECT * FROM team_reviews
WHERE owner_id = $1 AND ($2::text = '' OR status = $2)
ORDER BY created_at DESC
LIMIT $3;

-- name: GetTeamReviewForOwner :one
SELECT * FROM team_reviews WHERE id = $1 AND owner_id = $2;

-- name: ResolveTeamReview :one
UPDATE team_reviews
SET status = $3,
    reviewed_by_user_id = $4,
    reviewed_at = now()
WHERE id = $1 AND owner_id = $2
RETURNING *;
