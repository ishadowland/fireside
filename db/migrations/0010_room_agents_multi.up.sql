-- room_agents: multiple invited AI assistants per room + per-slot cooldown.
--
-- Before: one row per room (PK = room_id), one well-known agent, system
-- prompt only.
-- After: PK = (room_id, agent_id) so a host can invite more than one AI
-- assistant (slot 1 / slot 2). cooldown_seconds is the minimum interval
-- that agent must wait between two of its own replies (the Service adds a
-- 0-5s random jitter on top, per owner decision 2026-08-13).

ALTER TABLE room_agents DROP CONSTRAINT room_agents_pkey;
ALTER TABLE room_agents ADD CONSTRAINT room_agents_pkey PRIMARY KEY (room_id, agent_id);

ALTER TABLE room_agents ADD COLUMN cooldown_seconds INTEGER NOT NULL DEFAULT 0;