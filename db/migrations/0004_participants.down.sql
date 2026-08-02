-- 0004_participants.down.sql
-- Reverse 0004: drop indexes first, then table, then enum.

DROP INDEX IF EXISTS uniq_participant_room_user_active;
DROP INDEX IF EXISTS idx_participant_user_rooms;
DROP INDEX IF EXISTS idx_participant_room;
DROP TABLE IF EXISTS participants;
DROP TYPE IF EXISTS stage_state;