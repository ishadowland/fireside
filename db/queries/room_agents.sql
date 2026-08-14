-- db/queries/room_agents.sql
-- Sprint 1-3: room_agents — which room has which agent invited + its
-- system prompt + per-slot cooldown. Matches
-- db/migrations/0009_room_agents.up.sql + 0010_room_agents_multi.up.sql.

-- name: GetRoomAgent :one
-- Fetch one agent invitation in a room. sql.ErrNoRows when that agent
-- was not invited (hook stays silent for that slot).
SELECT room_id, agent_id, system_prompt, cooldown_seconds, created_at
FROM room_agents
WHERE room_id = $1 AND agent_id = $2;

-- name: ListRoomAgentsByRoom :many
-- All invited agents in a room (used to render them in the 在场 list).
SELECT room_id, agent_id, system_prompt, cooldown_seconds, created_at
FROM room_agents
WHERE room_id = $1;

-- name: UpsertRoomAgent :exec
-- Invite / re-invite an agent into a room. Re-inviting replaces the
-- system prompt and cooldown (host may switch the agent's personality
-- mid-room).
INSERT INTO room_agents (room_id, agent_id, system_prompt, cooldown_seconds)
VALUES ($1, $2, $3, $4)
ON CONFLICT (room_id, agent_id) DO UPDATE
SET system_prompt = EXCLUDED.system_prompt,
    cooldown_seconds = EXCLUDED.cooldown_seconds;

-- name: DeleteRoomAgent :exec
-- Remove one agent from a room (host kicks that slot).
DELETE FROM room_agents
WHERE room_id = $1 AND agent_id = $2;

-- name: DeleteRoomAgentsByRoom :exec
-- Remove every agent from a room (host kicks all; called on room end).
DELETE FROM room_agents
WHERE room_id = $1;