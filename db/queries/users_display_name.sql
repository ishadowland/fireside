-- db/queries/users_display_name.sql
-- Sprint 1 WP-1 follow-up: display_name queries (Q6).
--
-- Matches db/migrations/0006_users_display_name.up.sql: users.display_name
-- VARCHAR(64) NOT NULL DEFAULT ''. Not unique (intentional — same nickname
-- across different users is allowed).

-- name: UpdateUserDisplayName :execresult
-- PATCH /v1/users/me (WP-7.10) calls this. Trimmed length is enforced
-- in the handler.
-- Returns rows updated (1 = ok, 0 = user not found).
UPDATE users
SET display_name = $2
WHERE id = $1;

-- name: GetDisplayName :one
-- Convenience lookup for room detail / lobby list (host.display_name).
-- Returns the user's display_name (may be '' if user hasn't set one).
SELECT display_name
FROM users
WHERE id = $1;