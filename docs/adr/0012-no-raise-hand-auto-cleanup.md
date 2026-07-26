# ADR-0012: No raise-hand auto-cleanup; host manages manually

- **Status**: Accepted
- **Date**: 2026-07-26
- **Source**: docs/design/04-state-machines.md (Q2)

## Context

This is the second half of the "stale raise hand" question. ADR-0003 declined auto-timeout on the **persistence** side; this ADR covers whether the host's lobby view auto-prunes.

## Decision

**No auto-cleanup.** The host's lobby always shows every raise-hand event ever recorded in this room (since room start), with online/offline status of each user. The host can:

- **Approve** (pull user into room)
- **Reject** (drops the raise)
- **Ignore** (leave it there)

There is no auto-prune timer.

## Alternatives Considered

- **24h auto-prune**: rejected — too aggressive for slow conversations.
- **Per-room start-of-day reset**: rejected — would surprise hosts.

## Consequences

### Positive
- Host has full history of who wanted in.
- Zero edge cases.

### Negative
- Long-lived rooms accumulate dead raise entries.

### Risks
- **UI clutter**: host can scroll. Future improvement: collapse "offline raises older than 7d" into a separate section.

## Related

- ADR-0003 (no auto-timeout — same answer for a different question)