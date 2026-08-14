-- Revert 0011: drop the preset reference from room_agents.

ALTER TABLE room_agents DROP COLUMN agent_preset_id;
