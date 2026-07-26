# ADR-0011: Driver shutdown is immediate when room ends

- **Status**: Accepted
- **Date**: 2026-07-26
- **Source**: docs/design/04-state-machines.md (Q3)

## Context

When a host ends a room, all participants leave. Agent drivers (Hermes / OpenClaw / custom runtime) are connected via long-lived HTTP connections or local sockets. We needed to decide what happens to those drivers.

## Decision

**Immediate shutdown.** When `room.state` transitions to `ended`:

1. Server sends a `driver.shutdown` signal to each driver.
2. Drivers close their connections within 1 second.
3. Server frees the room's resources (workspace, agent state).

In-flight agent responses are not awaited. If an agent was mid-response when shutdown happened, the partial response is **dropped** (not written to messages). The placeholder (per ADR-0006 / ADR-0009) is **also deleted** (server-side, not broadcast — there are no clients left to receive).

## Alternatives Considered

- **B. Graceful shutdown (60s grace)**: considered — would let in-flight responses complete. Rejected by user preference for simplicity.
- **C. Indefinite wait**: rejected — risk of hanging forever on a wedged driver.

## Consequences

### Positive
- Trivial to implement.
- No "is the room really ended" ambiguity.
- Server resources freed immediately.

### Negative
- A user who @mentions an agent and the host ends the room 1 second later sees nothing — no response, no error message.
- In-flight partial responses vanish silently.

### Risks
- **User confusion**: "I asked the agent something, then the room ended, did it answer?". Mitigation: post-mortem via agent runtime logs (drivers should log their last-in-flight request even on shutdown).

## Related

- ADR-0006 / ADR-0009 (placeholder handling — both are unaffected since there are no live clients at shutdown)