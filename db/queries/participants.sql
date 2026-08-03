-- db/queries/participants.sql
-- Sprint 1 WP-4: participants queries.
--
-- Matches db/migrations/0004_participants.up.sql schema exactly.
--
-- Sprint 1-3 fix (issue #15 L-1/L-2): JoinRoom is now atomic — a
-- single statement that checks capacity AND the partial UNIQUE index,
-- so 9 concurrent joins to an 8-cap room can no longer all pass the
-- check. The unique_violation race (L-2) is also closed by the same
-- atomic statement: ON CONFLICT DO NOTHING + RETURNING returns
-- sql.ErrNoRows on either a capacity miss OR a duplicate, and the
-- caller disambiguates via a follow-up GetOnStageParticipant.
--
-- Sprint 1-3 fix (issue #15 L-3): LeaveRoom now uses RETURNING to
-- return the updated row directly, eliminating the prior N+1-ish
-- UPDATE-then-ListByRoom-then-filter pattern.

-- name: JoinRoom :one
-- Atomic join: checks capacity ($4 = max_participants) AND the partial
-- UNIQUE index in a single statement. Returns the new row, or
-- sql.ErrNoRows if either constraint failed.
--
-- The capacity check uses a correlated subquery against the same
-- table. Single-instance Postgres + a single INSERT serializes the
-- count + insert (the row lock the INSERT acquires also locks the
-- index gap, blocking concurrent inserts until COMMIT). Multi-instance
-- (Sprint 2+) needs SERIALIZABLE or advisory locks.
--
-- Caller disambiguates ErrNoRows via GetOnStageParticipant: if found
-- -> ErrAlreadyOnStage, else -> ErrRoomFull.
--
-- Type casts on every literal/parameter to avoid PG SQLSTATE 42P08
-- ("inconsistent types deduced for parameter $N") — the $4 int
-- parameter would otherwise be ambiguous (int vs bigint) when only
-- used inside a count comparison. The $2 room_id, stage_state
-- 'on_stage' literals get explicit casts for symmetry with the rest
-- of the codebase (see commit `ffdbea4` which added ::enum_type casts
-- across the rest of the store).
INSERT INTO participants (id, room_id, user_id, stage_state, joined_at)
SELECT $1::VARCHAR(26),
       $2::VARCHAR(26),
       $3::VARCHAR(26),
       'on_stage'::stage_state,
       NOW()
WHERE (SELECT COUNT(*) FROM participants
       WHERE room_id = $2::VARCHAR(26)
         AND stage_state = 'on_stage'::stage_state) < $4::INT
ON CONFLICT (room_id, user_id) WHERE stage_state = 'on_stage' DO NOTHING
RETURNING id, room_id, user_id, stage_state, joined_at, left_at;

-- name: LeaveRoom :one
-- Mark participant off_stage by setting left_at = NOW(). Returns the
-- updated row, or sql.ErrNoRows if the user wasn't on_stage.
-- Type casts ($1/$2 -> VARCHAR(26), 'on_stage' -> stage_state) for
-- symmetry with JoinRoom and the rest of the codebase.
UPDATE participants
SET stage_state = 'off_stage'::stage_state, left_at = NOW()
WHERE room_id = $1::VARCHAR(26)
  AND user_id = $2::VARCHAR(26)
  AND stage_state = 'on_stage'::stage_state
RETURNING id, room_id, user_id, stage_state, joined_at, left_at;

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
