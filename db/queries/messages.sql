-- db/queries/messages.sql
-- Sprint 1 WP-3: messages queries.
--
-- Matches db/migrations/0005_messages.up.sql schema exactly.
-- sender_kind CHECK constraint enforced: 'system' must have sender_id NULL;
-- 'human'/'agent' must have sender_id NOT NULL.

-- name: CreateMessage :one
-- Inserts a message. id (ULID) supplied by caller. mentions default '[]'.
-- sender_id is nullable; pass NULL for system messages.
INSERT INTO messages (
    id, room_id, sender_kind, sender_id, content_type, content,
    mentions, reply_to_id, created_at
) VALUES (
    $1, $2, $3, $4, $5, $6,
    COALESCE($7, '[]'::jsonb), $8, COALESCE($9, NOW())
)
RETURNING id, room_id, sender_kind, sender_id, content_type, content,
          mentions, reply_to_id, created_at;

-- name: ListMessagesByRoom :many
-- Cursor pagination: fetch up to $3 messages with id < $4 (cursor).
-- Sprint 1 simple cursor — pass NULL for cursor to get the newest batch.
-- Order: DESC by created_at then id, for "latest first" UX.
SELECT id, room_id, sender_kind, sender_id, content_type, content,
       mentions, reply_to_id, created_at
FROM messages
WHERE room_id = $1
  AND ($2::CHAR(26) IS NULL OR id < $2::CHAR(26))
ORDER BY created_at DESC, id DESC
LIMIT $3;

-- name: CountMessagesByRoom :one
-- Used by room detail to show message count.
SELECT COUNT(*)
FROM messages
WHERE room_id = $1;

-- name: ListMessagesBySender :many
-- Admin / debug: all messages from a specific sender.
SELECT id, room_id, sender_kind, sender_id, content_type, content,
       mentions, reply_to_id, created_at
FROM messages
WHERE sender_kind = $1 AND sender_id = $2
ORDER BY created_at DESC
LIMIT $3;

-- name: GetMessage :one
-- Single message lookup by id (used by reply chains).
SELECT id, room_id, sender_kind, sender_id, content_type, content,
       mentions, reply_to_id, created_at
FROM messages
WHERE id = $1;