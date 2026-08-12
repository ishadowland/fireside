-- db/queries/rooms.sql
-- Sprint 1 WP-2: rooms queries.
--
-- Matches db/migrations/0003_rooms.up.sql schema exactly.

-- name: CreateRoom :one
-- Inserts a new room. id (ULID) is supplied by the caller (auth.LoginHandler
-- pattern from Sprint 1-3 — no DB DEFAULT for id).
INSERT INTO rooms (
    id, host_user_id, name, max_participants, keep_messages_on_end,
    status, announcement, created_at, ended_at
) VALUES (
    $1, $2, $3, $4, $5,
    COALESCE($6, 'active'::room_status), COALESCE($7, ''), COALESCE($8, NOW()), $9
)
RETURNING id, host_user_id, name, max_participants, keep_messages_on_end,
          status, announcement, created_at, ended_at;

-- name: GetRoom :one
-- Fetches a single room by id.
SELECT id, host_user_id, name, max_participants, keep_messages_on_end,
       status, announcement, created_at, ended_at
FROM rooms
WHERE id = $1;

-- name: ListActiveRooms :many
-- Lobby list: all rooms with status='active', newest first.
-- Used by GET /v1/rooms. Cursor pagination later; for Sprint 1 just LIMIT.
SELECT id, host_user_id, name, max_participants, keep_messages_on_end,
       status, announcement, created_at, ended_at
FROM rooms
WHERE status = 'active'
ORDER BY created_at DESC
LIMIT $1;

-- name: ListActiveRoomsByHost :many
-- Host's own active rooms.
SELECT id, host_user_id, name, max_participants, keep_messages_on_end,
       status, announcement, created_at, ended_at
FROM rooms
WHERE host_user_id = $1 AND status = 'active'
ORDER BY created_at DESC;

-- name: EndRoom :execresult
-- Mark a room as ended. Sprint 1 stub: no message cleanup (D6 deferred).
-- Returns the number of rows updated (1 = success, 0 = not found or already ended).
UPDATE rooms
SET status = 'ended', ended_at = NOW()
WHERE id = $1 AND status = 'active';

-- name: CountOnStageParticipants :one
-- Used by capacity check on Join (WP-4): returns count of participants
-- currently on_stage in the room.
SELECT COUNT(*)
FROM participants
WHERE room_id = $1 AND stage_state = 'on_stage';
-- name: ListAllRoomsWithStats :many
-- Admin view: every room (active + ended), newest first, with
-- participant + message counts so the admin page can show impact
-- before force-close / delete.
SELECT r.id, r.host_user_id, r.name, r.max_participants,
       r.keep_messages_on_end, r.status, r.announcement, r.created_at,
       r.ended_at,
       (SELECT COUNT(*) FROM participants p WHERE p.room_id = r.id) AS participant_count,
       (SELECT COUNT(*) FROM messages m WHERE m.room_id = r.id) AS message_count
FROM rooms r
ORDER BY r.created_at DESC;

-- name: DeleteRoom :exec
-- Admin: delete a single room. participants + messages cascade
-- (ON DELETE CASCADE in migrations 0004/0005).
DELETE FROM rooms
WHERE id = $1;

-- name: DeleteAllRooms :exec
-- Admin: clear every room (and by cascade all participants/messages).
DELETE FROM rooms;
