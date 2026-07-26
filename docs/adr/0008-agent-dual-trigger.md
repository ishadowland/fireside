# ADR-0008: Active agents use dual-trigger (new message + 1min timer) with 20s debounce

- **Status**: Accepted
- **Date**: 2026-07-26
- **Source**: docs/design/03-protocol.md (Q2)

## Context

Active agents (those configured to participate proactively) need to know when to consider responding. A pure push trigger misses cases where no human @mentions the agent but the conversation drifts into topics it should weigh in on. A pure poll wastes server resources.

## Decision

**Dual trigger:**

1. **Push trigger**: every new `msg.created` event wakes `agent.ShouldRespond()` for every active agent in the room.
2. **Timer trigger**: every 60 seconds, each room's active-agent timer ticks and re-evaluates `ShouldRespond()`.

Both paths funnel into a single `ShouldRespond()` decision function. A successful agent response locks out further `ShouldRespond()` calls for the same agent for **20 seconds** (debounce / anti-spam window).

## Alternatives Considered

- **Pure push (no timer)**: rejected — agents that should "speak up when relevant" never trigger if no human talks to them.
- **Pure poll (every minute, no push)**: rejected — adds up to 60s latency for direct @mentions, frustrating.
- **Shorter timer (30s)**: rejected — doesn't materially improve responsiveness, doubles wakeups.
- **Longer debounce (60s)**: rejected — too easy to "spam" an agent by typing @agent three times rapidly.

## Consequences

### Positive
- Direct @mentions trigger immediate response (push path).
- Agents stay relevant in slow conversations (timer path).
- 20s debounce prevents a single user from provoking N responses from one agent.

### Negative
- Every push requires evaluating all active agents (cheap — O(active_agents_in_room)).
- Timer requires one goroutine per active room.

### Risks
- **Many active rooms**: timer fan-out. Mitigation: cap active rooms per user to 5; cap active agents per room to 10.

## Related

- ADR-0009 (placeholder for long responses)
- docs/design/03-protocol.md §agent trigger
- docs/design/04-state-machines.md §agent lifecycle