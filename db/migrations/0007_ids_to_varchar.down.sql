-- 0007_ids_to_varchar.down.sql
-- Reverse 0007: change VARCHAR(26) ID columns back to CHAR(26).
-- USING id::char(26) left-pads with spaces if needed; CHAR semantics
-- then pad with spaces on write to fit the column width.
--
-- Tests that exercise CHAR behavior after down will see the
-- trailing-space issue re-emerge; this is expected (and the reason
-- 0007_up was created in the first place).

ALTER TABLE users
    ALTER COLUMN id TYPE CHAR(26) USING id::char(26);

ALTER TABLE rooms
    ALTER COLUMN id          TYPE CHAR(26) USING id::char(26),
    ALTER COLUMN host_user_id TYPE CHAR(26) USING host_user_id::char(26);

ALTER TABLE participants
    ALTER COLUMN id      TYPE CHAR(26) USING id::char(26),
    ALTER COLUMN room_id TYPE CHAR(26) USING room_id::char(26),
    ALTER COLUMN user_id TYPE CHAR(26) USING user_id::char(26);

ALTER TABLE messages
    ALTER COLUMN id          TYPE CHAR(26) USING id::char(26),
    ALTER COLUMN room_id     TYPE CHAR(26) USING room_id::char(26),
    ALTER COLUMN sender_id   TYPE CHAR(26) USING sender_id::char(26),
    ALTER COLUMN reply_to_id TYPE CHAR(26) USING reply_to_id::char(26);

ALTER TABLE auth_tokens
    ALTER COLUMN user_id TYPE CHAR(26) USING user_id::char(26);