-- 0009_room_agents.up.sql
-- Sprint 1-3: room_agents — an AI agent is invited INTO a room by its
-- host, and the invitation carries the agent's system prompt.
--
-- Design (owner, 2026-08-13): the AI assistant is NOT auto-present in
-- rooms. It only replies in rooms where a room_agents row exists, and
-- the prompt used for that room is the one set at invite time (empty
-- string falls back to the built-in default in internal/agents).
--
--   * One agent per room for now (PK = room_id).
--   * agent_id is CHAR(26), no FK: there is no agents table yet (the FK
--     is added together with that table, Sprint 2+, per migration 0005's
--     comment on messages.sender_id).
--   * Room end removes the row (EndRoom deletes it via DeleteRoomAgent);
--     hard-deleting a room cascades here.
--   * system_prompt is capped at 4000 chars (matches the REST validation
--     in internal/agents).

CREATE TABLE room_agents (
    room_id       CHAR(26)     PRIMARY KEY REFERENCES rooms(id) ON DELETE CASCADE,
    agent_id      CHAR(26)     NOT NULL,
    system_prompt TEXT         NOT NULL DEFAULT '' CHECK (length(system_prompt) <= 4000),
    created_at    TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);