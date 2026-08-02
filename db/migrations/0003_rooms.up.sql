-- 0003_rooms.up.sql
-- Sprint 1 WP-1: rooms table.
--
-- Per docs/rfc/phase-2-minimal-demo.md §4 WP-2 and docs/design/01-data-model.md §2.
-- Per Q1+Q3: id is CHAR(26) ULID (matching users.id from Sprint 1-3 migration);
--            Sprint 1 stub: rooms are never ended (status stays 'active').
-- Per Q2: room name is NOT unique — UI dedups by host prefix.
-- Per Q4: keep_messages_on_end is a per-room toggle (host chooses at create).
-- Per Q7: max_participants defaults to 8 (was D3's 50; Sprint 1 chooses 8 for
--         easier capacity-test edge cases).
-- Per D29: announcement column shipped now (TEXT ≤500) but NOT editable in
--          Sprint 1 (PATCH endpoint deferred to Sprint 2); default empty.
--
-- Idempotency: CREATE TYPE ... AS ENUM is not IF NOT EXISTS-aware in older PG;
-- use a DO block to skip if already created.

DO $$ BEGIN
    CREATE TYPE room_status AS ENUM ('active', 'ended');
EXCEPTION
    WHEN duplicate_object THEN NULL;
END $$;

CREATE TABLE rooms (
    id                   CHAR(26)     PRIMARY KEY,
    host_user_id         CHAR(26)     NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    name                 VARCHAR(128) NOT NULL,
    max_participants     INT          NOT NULL DEFAULT 8 CHECK (max_participants BETWEEN 1 AND 50),
    keep_messages_on_end BOOLEAN      NOT NULL DEFAULT false,
    status               room_status  NOT NULL DEFAULT 'active',
    announcement         TEXT         NOT NULL DEFAULT '' CHECK (length(announcement) <= 500),
    created_at           TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    ended_at             TIMESTAMPTZ
);

-- Host's active rooms — used by host-dashboard lookup.
CREATE INDEX idx_room_host_status    ON rooms(host_user_id, status);
-- Lobby list: all active rooms, newest first.
CREATE INDEX idx_room_status_created ON rooms(status, created_at DESC);

-- Partial uniqueness on host-side name (one host cannot have two active rooms
-- with the exact same name; ended rooms do not count). Sprint 1: kept loose
-- per Q2; this is a Sprint 2 follow-up if owner wants.
-- (Intentionally not added now.)