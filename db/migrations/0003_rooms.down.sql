-- 0003_rooms.down.sql
-- Reverse 0003: drop indexes, drop table, drop enum. Order:
-- drop dependents (indexes, FK-referencing tables) first; enum last.

DROP INDEX IF EXISTS idx_room_host_status;
DROP INDEX IF EXISTS idx_room_status_created;
DROP TABLE IF EXISTS rooms;
DROP TYPE IF EXISTS room_status;