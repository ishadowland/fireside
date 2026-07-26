# ADR-0002: Custom agent memory stored on filesystem

- **Status**: Accepted
- **Date**: 2026-07-26
- **Source**: docs/design/01-data-model.md (Q2)

## Context

Custom agents are LLM-driven agents with persistent memory across sessions. Memory needs to be read/written by the Fireside server (to seed LLM context) and also directly by the agent runtime (which may not be co-located with Fireside). We needed to choose a storage backend.

## Decision

Custom agent memory is stored on the filesystem under `/var/fireside/agents/<agent_id>/memory/` (configurable via `FIRESIDE_AGENT_MEMORY_ROOT`). Each conversation entry is a Markdown file with YAML front-matter (timestamp, room_id, role, content). The agent runtime reads/writes via this shared mount.

## Alternatives Considered

- **Postgres JSONB column per agent**: rejected — forces every agent runtime to be SQL-aware; couples memory schema to DB migrations.
- **S3 / object store**: rejected — adds cloud dependency and cost; overkill for MVP.
- **In-process map**: rejected — lost on restart; doesn't survive server migration.

## Consequences

### Positive
- Agent runtime (Hermes/OpenClaw) reads/writes plain files — matches their natural `MEMORY.md` style.
- Trivial backup (`tar` of `/var/fireside/agents`).
- Inspectable with any text editor.
- No DB schema churn when we evolve memory format.

### Negative
- No transactional consistency between memory writes and message persistence.
- Migration to a different agent runtime requires file-format compatibility.

### Risks
- **Disk full**: server must alert when `df` < 10%; oldest memory files are auto-archived to `/var/fireside/agents/_archive/`.
- **Corrupt file**: agent runtime must tolerate partial writes (write to `.tmp` then rename).

## Related

- ADR-0001 (tool agent)
- docs/design/01-data-model.md §AgentMemory