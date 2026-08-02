-- 0004_participants.up.sql
-- Sprint 1 WP-1: participants table.
--
-- Per docs/design/01-data-model.md §3 and docs/design/04-state-machines.md §2.
-- One participant row per (user, room) join *event*. Same user can have multiple
-- historical rows in the same room (off_stage records) but only one on_stage at
-- any time — enforced by the partial UNIQUE index.
--
-- Per RFC Q5 (basic lobby): no RaiseHand table in Sprint 1; everyone can join
-- any active room via POST /v1/rooms/:id/join (subject to capacity).
--
-- Idempotency: enum wrapped in DO block.

DO $$ BEGIN
    CREATE TYPE stage_state AS ENUM ('on_stage', 'off_stage');
EXCEPTION
    WHEN duplicate_object THEN NULL;
END $$;

CREATE TABLE participants (
    id          CHAR(26)     PRIMARY KEY,
    room_id     CHAR(26)     NOT NULL REFERENCES rooms(id) ON DELETE CASCADE,
    user_id     CHAR(26)     NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    stage_state stage_state  NOT NULL DEFAULT 'on_stage',
    joined_at   TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    left_at     TIMESTAMPTZ
);

-- All participants in a room (used by room detail endpoint).
CREATE INDEX idx_participant_room
    ON participants(room_id);

-- All rooms a user is currently on_stage in (used by lobby/me_on_stage flag).
CREATE INDEX idx_participant_user_rooms
    ON participants(user_id)
    WHERE stage_state = 'on_stage';

-- CRITICAL: same user cannot have two on_stage records in the same room.
-- Partial UNIQUE — historical off_stage rows are fine.
CREATE UNIQUE INDEX uniq_participant_room_user_active
    ON participants(room_id, user_id)
    WHERE stage_state = 'on_stage';

-- left_at consistency: if stage_state='off_stage', left_at should be NOT NULL;
-- if stage_state='on_stage', left_at should be NULL. Enforced at the
-- application layer (no CHECK constraint because PG doesn't support subqueries
-- in CHECK constraints without trigger overhead — kept simple).

-- Capacity enforcement (max_participants on rooms) is application-side via
-- SELECT COUNT(*) inside a transaction before INSERT. Single-instance
-- Postgres + serializable isolation handles the race; for multi-instance,
-- revisit in Sprint 2.