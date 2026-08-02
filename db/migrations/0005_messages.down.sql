-- 0005_messages.down.sql
-- Reverse 0005: drop indexes, table, enums.

DROP INDEX IF EXISTS idx_message_sender;
DROP INDEX IF EXISTS idx_message_room_created;
DROP TABLE IF EXISTS messages;
DROP TYPE IF EXISTS content_type;
DROP TYPE IF EXISTS sender_kind;