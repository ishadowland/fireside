-- db/queries/participants.sql
-- Sprint 1 WP-4: participants queries.
--
-- Matches db/migrations/0004_participants.up.sql schema exactly.

-- name: JoinRoom :one
-- Inserts a new on_stage participant row.
-- Caller is responsible for capacity check + UNIQUE collision handling.
-- Returns the new row. Errors:
--   sql.ErrNoRows / unique violation if user is already on_stage in this room.
INSERT INTO participants (id, room_id, user_id, stage_state, joined_at)
VALUES ($1, $2, $3, 'on_stage', NOW())
RETURNING id, room_id, user_id, stage_state, joined_at, left_at;

-- name: LeaveRoom :execresult
-- Mark participant off_stage by setting left_at = NOW().
-- Returns the number of rows updated (1 = success, 0 = not on_stage).
UPDATE participants
SET stage_state = 'off_stage', left_at = NOW()
WHERE room_id = $1 AND user_id = $2 AND stage_state = 'on_stage';

-- name: ListOnStageByRoom :many
-- All participants currently on_stage in a room. Used by room detail endpoint.
SELECT id, room_id, user_id, stage_state, joined_at, left_at
FROM participants
WHERE room_id = $1 AND stage_state = 'on_stage'
ORDER BY joined_at ASC;

-- name: ListByRoom :many
-- All participants (on_stage + off_stage, history) in a room, newest first.
SELECT id, room_id, user_id, stage_state, joined_at, left_at
FROM participants
WHERE room_id = $1
ORDER BY joined_at DESC;

-- name: ListOnStageByUser :many
-- All rooms a user is currently on_stage in. Used by /v1/rooms per-row
-- `me_on_stage` flag in lobby list.
SELECT id, room_id, user_id, stage_state, joined_at, left_at
FROM participants
WHERE user_id = $1 AND stage_state = 'on_stage';

-- name: GetOnStageParticipant :one
-- Look up an active (on_stage) participant row by (room_id, user_id).
-- Returns sql.ErrNoRows if user is not currently on_stage in the room.
SELECT id, room_id, user_id, stage_state, joined_at, left_at
FROM participants
WHERE room_id = $1 AND user_id = $2 AND stage_state = 'on_stage';