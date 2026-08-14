-- 0011_agent_presets.up.sql
-- room_agents: reference a persisted agent preset (Agent 管理器, issue #38).
--
-- Before: every invitation carried its system_prompt in-room and the
-- connection config (endpoint/key/model) was one global in-memory value.
-- After: an invitation may reference an agent_preset_id; the preset holds
-- the connection kind (openai/simple/openclaw), endpoint, api token, model
-- and system prompt. Empty agent_preset_id = legacy in-room prompt + global
-- config fallback (unchanged behavior for old rows).

ALTER TABLE room_agents ADD COLUMN agent_preset_id CHAR(26) NOT NULL DEFAULT '';
