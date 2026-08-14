-- Revert 0010: back to one agent per room (PK = room_id), drop cooldown.
-- Any extra agent rows are deleted (first agent per room wins).

ALTER TABLE room_agents DROP CONSTRAINT room_agents_pkey;

DELETE FROM room_agents ra
USING room_agents ra2
WHERE ra.room_id = ra2.room_id AND ra.agent_id <> ra2.agent_id;

ALTER TABLE room_agents ADD CONSTRAINT room_agents_pkey PRIMARY KEY (room_id);

ALTER TABLE room_agents DROP COLUMN cooldown_seconds;