# ADR-0003: No raise-hand auto-timeout in MVP

- **Status**: Accepted
- **Date**: 2026-07-26
- **Source**: docs/design/01-data-model.md (Q3), docs/design/04-state-machines.md (Q2)

## Context

In the lobby, users raise hands to request being pulled into a room. A stale raise (user went offline but the hand is still "up") could clutter the host's view. We needed to decide whether the server auto-clears stale raises.

## Decision

**No auto-timeout in MVP.** Raise hands persist until the host approves, rejects, or the user explicitly cancels. The host's lobby view already shows online presence, making stale hands visually obvious.

## Alternatives Considered

- **24h auto-clear**: rejected — too long for an active lobby, too short for slow conversations.
- **7d auto-clear**: rejected — same problem; arbitrary number invites future debugging.

## Consequences

### Positive
- Zero edge cases for MVP.
- Host has full history of who wanted in.
- No background timer needed.

### Negative
- Long-lived rooms could accumulate many stale hands.
- Host must manually clear if they care.

### Risks
- **Lobby spam**: a malicious user could raise-then-go-offline repeatedly. Mitigation is at user-account level (rate-limit raises per user per hour, see Phase 2).

## Related

- ADR-0011 (driver shutdown immediate — no equivalent timer here)