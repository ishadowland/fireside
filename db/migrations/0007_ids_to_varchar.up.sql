-- 0007_ids_to_varchar.up.sql
-- Sprint 1 WP-2 code-review fix: change all CHAR(26) ID columns to
-- VARCHAR(26). Eliminates the CHAR-trailing-space workaround that
-- required strings.TrimSpace at every DB-read site (see §6 of issue #13).
--
-- Why this is safe in Sprint 1:
--   - ADR-0014 declares the BIGINT-era data throwaway; only Sprint 0/1-1/1-2
--     stub-registered users (a handful, no prod) exist in this column.
--   - VARCHAR(26) accepts the same ULID string content, length 26.
--     Postgres only refuses data > 26 chars; existing rows fit.
--   - USING trim(id) strips the CHAR-padded spaces before casting so the
--     stored bytes match the trimmed Go-side string exactly.
--   - Indexes (PK + FK + idx_* on id columns) follow the column type
--     automatically (no separate rebuild needed).
--
-- Sprint 2 code cleanup follows this migration:
--   - Remove all strings.TrimSpace calls in
--     internal/rooms/{types.go,service.go}.
--   - Remove the workaround documentation comments.
--   - Add a unit test that exercises a round-trip ULID without trimming.

-- users.id (PRIMARY KEY, also referenced as FK by auth_tokens / rooms /
-- participants / messages).
ALTER TABLE users
    ALTER COLUMN id TYPE VARCHAR(26) USING trim(id);

-- rooms.id + rooms.host_user_id (FK to users.id)
ALTER TABLE rooms
    ALTER COLUMN id          TYPE VARCHAR(26) USING trim(id),
    ALTER COLUMN host_user_id TYPE VARCHAR(26) USING trim(host_user_id);

-- participants.{id, room_id, user_id}
ALTER TABLE participants
    ALTER COLUMN id      TYPE VARCHAR(26) USING trim(id),
    ALTER COLUMN room_id TYPE VARCHAR(26) USING trim(room_id),
    ALTER COLUMN user_id TYPE VARCHAR(26) USING trim(user_id);

-- messages.{id, room_id, sender_id, reply_to_id}
ALTER TABLE messages
    ALTER COLUMN id          TYPE VARCHAR(26) USING trim(id),
    ALTER COLUMN room_id     TYPE VARCHAR(26) USING trim(room_id),
    ALTER COLUMN sender_id   TYPE VARCHAR(26) USING trim(sender_id),
    ALTER COLUMN reply_to_id TYPE VARCHAR(26) USING trim(reply_to_id);

-- auth_tokens.user_id (FK to users.id)
ALTER TABLE auth_tokens
    ALTER COLUMN user_id TYPE VARCHAR(26) USING trim(user_id);