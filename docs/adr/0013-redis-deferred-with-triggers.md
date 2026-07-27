# ADR-0013: Redis is deferred — explicit triggers for re-evaluation

- **Status**: Accepted
- **Date**: 2026-07-27
- **Source**: docs/requirements/01-tech-decisions.md §Why Not Redis (Yet); docs/adr/0007-ws-auth-first-frame.md; docs/adr/0008-agent-dual-trigger.md; docs/design/02-modules.md §concurrency

## Context

`01-tech-decisions.md` §"Why Not Redis (Yet)" listed three triggers that would force re-evaluation: horizontal scaling, persistent message queue, hot cache. Today (2026-07-27) we re-checked whether any of those triggers have actually fired in MVP scope. We also re-examined three specific subsystems where Redis is the conventional answer: `jti` replay defense (ADR-0007), agent dual-trigger timer coordination (ADR-0008), and the in-process broadcast channel (`internal/hub`, design §02-modules §concurrency model). On user confirmation, none of the three has crossed the cost threshold that would justify adding a Redis container to a 1-day Sprint 0 hard-cut on a 2GB VPS.

## Decision

**Redis is deferred. Single-process Go + Postgres + in-process channels remain the entire runtime stack for MVP.**

Each of the three Redis-classic use cases has a non-Redis fallback that is correct under the MVP constraint of a single process:

- **`jti` replay defense (ADR-0007)**: in-process `sync.Map` keyed by `jti` with a per-entry TTL of 15 min, swept by a single background goroutine. Memory ceiling at 1000 concurrent WS = ~1000 entries × ~48 bytes ≈ 48 KB.
- **Agent dual-trigger timer (ADR-0008)**: one goroutine per active room with a 60s `time.Ticker`. ADR-0008 already caps active rooms/user to 5 and active agents/room to 10, bounding fan-out.
- **Broadcast channel (`internal/hub`)**: per-room Go `chan []byte` fan-out within one process. Single instance means single-process channels are sufficient.

## Alternatives Considered

- **Add Redis to Sprint 0** (the C path from the 2026-07-27 review): rejected — adds 2-3 hours of install + client wiring + retry semantics + deploy-doc updates that the 1-day Sprint 0 cannot absorb, and buys nothing while we stay single-process.
- **Add Postgres LISTEN/NOTIFY now** as a pub/sub hedge: rejected — premature; the cross-instance pub/sub problem only exists once D12's "Docker 多实例扩展" actually fires.
- **Drop D12's "Docker 多实例扩展"** to permanently close the topic: rejected — user wants to keep that promise open; horizontal scale is a real future need even if not Phase 1.

## Consequences

### Positive
- Sprint 0 stays a 1-day wiring exercise; no new infra component.
- Memory ceiling for MVP is bounded and small (in-process `jti` map + per-room ticker goroutines).
- All three fallbacks are observable through standard `slog` — no new failure modes (Redis network drops, eviction, RDB/AOF issues) to diagnose.

### Negative
- We are deliberately carrying a known technical debt: the day we run two instances behind a load balancer, **every one of the three fallbacks silently breaks**:
  - `jti` map: replay works across instances (each instance has its own map).
  - Timer goroutines: every instance wakes every agent → duplicate responses + wasted tokens.
  - Broadcast channels: room state is split across instances, users on different instances never see each other's messages.

### Risks
- **Re-evaluation hygiene**: if we don't write down the re-evaluation triggers *with concrete metrics*, this ADR becomes shelf-ware and Redis gets re-debated from scratch every quarter. Mitigation: the trigger table below is a hard checklist; any one of them firing = a new proposal, not a fresh discussion.
- **D12 promise creep**: "Docker 多实例扩展" in the requirements snapshot reads casually; under load it will look like a deployment choice before it looks like an architecture choice. Mitigation: when that day comes, ADR-0013 is the first file opened.

## Re-evaluation triggers (when to open a new ADR proposing Redis)

| # | Trigger | How we would notice | Subsystem affected |
|---|---|---|---|
| T1 | First deployment runs > 1 server instance (D12 fires) | `systemctl list-units` shows `fireside.service` on 2+ hosts, or k8s deployment `replicas > 1` | ALL three (jti, timer, broadcast) |
| T2 | `jti` in-process map exceeds 50 MB or 100k entries | SLO breach: memory > 1.5GB sustained, or a replayed token succeeds (security incident) | `jti` map only |
| T3 | Active rooms > 200 sustained (ADR-0008's 5×10 cap is no longer enough) | Metrics: active_room_goroutines > 200 for > 1 hour | Timer goroutines only |
| T4 | Hot cache emerges — same message body queried by > 100 users in 1 min | pg_stat_statements shows a SELECT in top-10 by calls | Hot-cache (not yet a subsystem) |
| T5 | Persistent message queue requirement appears (job retries, scheduled send) | Any feature request that needs "guaranteed delivery" semantics | Future pub/sub or job queue |

Any single T1..T5 firing means **open ADR-XXXX "introduce Redis"** — do not patch ADR-0013 in place.

## Related
- docs/requirements/01-tech-decisions.md §Why Not Redis (Yet) — original YAGNI stance
- docs/requirements/03-decision-snapshot.md §六、技术栈 — current stack snapshot
- docs/adr/0007-ws-auth-first-frame.md — `jti` requirement (uses in-process map fallback)
- docs/adr/0008-agent-dual-trigger.md — 60s timer per active room (uses per-room goroutine fallback)
- docs/design/02-modules.md §concurrency — broadcast channel / hub model
- D12 (MVP 单租户 + Docker 多实例扩展) — the trigger most likely to fire first