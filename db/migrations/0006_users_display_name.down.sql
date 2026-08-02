-- 0006_users_display_name.down.sql
-- Reverse 0006: drop the display_name column.

ALTER TABLE users
    DROP COLUMN IF EXISTS display_name;