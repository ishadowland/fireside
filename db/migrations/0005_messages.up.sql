-- 0005_messages.up.sql
-- Sprint 1 WP-1: messages table.
--
-- Per docs/design/01-data-model.md §5 and docs/design/03-protocol.md §"消息帧 Schema".
-- Sprint 1 only uses (sender_kind, content_type) = (human, text) and (system, system).
-- All other combinations are reserved for Sprint 2+ (agent-driven messages,
-- question/answer/progress per ADR-0015/0017).
--
-- mentions: JSONB array of participant_id strings (CHAR(26) ULIDs). Validated
--           application-side on insert.
-- reply_to_id: self-reference for threading. NULL = top-level message.
--
-- Idempotency: enums wrapped in DO blocks.

DO $$ BEGIN
    CREATE TYPE sender_kind AS ENUM ('human', 'agent', 'system');
EXCEPTION
    WHEN duplicate_object THEN NULL;
END $$;

DO $$ BEGIN
    CREATE TYPE content_type AS ENUM ('text', 'system', 'image', 'question', 'answer', 'progress');
EXCEPTION
    WHEN duplicate_object THEN NULL;
END $$;

CREATE TABLE messages (
    id           CHAR(26)     PRIMARY KEY,
    room_id      CHAR(26)     NOT NULL REFERENCES rooms(id) ON DELETE CASCADE,
    sender_kind  sender_kind  NOT NULL,
    sender_id    CHAR(26),  -- FK to users(id) (human) or agents.id (agent, Sprint 2+); NULL for system
    content_type content_type NOT NULL,
    content      TEXT         NOT NULL,
    mentions     JSONB        NOT NULL DEFAULT '[]'::jsonb,
    reply_to_id  CHAR(26)     REFERENCES messages(id) ON DELETE SET NULL,
    created_at   TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    -- System messages must NOT carry a sender_id; everything else MUST.
    -- agents.id is a Sprint 2+ table, so the FK on sender_id when
    -- sender_kind='agent' is intentionally omitted here; the column
    -- remains CHAR(26) and the FK is added in that sprint.
    CONSTRAINT chk_sender_id_consistency CHECK (
        (sender_kind = 'system' AND sender_id IS NULL) OR
        (sender_kind <> 'system' AND sender_id IS NOT NULL)
    )
);

-- Lobby / room detail: list messages in a room, newest first (DESC for
-- pagination: `ORDER BY created_at DESC, id DESC LIMIT N`).
CREATE INDEX idx_message_room_created
    ON messages(room_id, created_at DESC, id DESC);

-- Look up messages by sender (e.g. "all messages from user X").
CREATE INDEX idx_message_sender
    ON messages(sender_kind, sender_id);

-- reply_to_id FK already self-references above; no extra index needed —
-- reply lookups are rare and typically pull a single message by id (PK).