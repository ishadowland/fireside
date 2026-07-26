# ADR-0009: Placeholder message for agent responses exceeding 60s

- **Status**: Accepted
- **Date**: 2026-07-26
- **Source**: docs/design/03-protocol.md (Q3)

## Context

Following ADR-0006, tool agents reply asynchronously with a placeholder. Custom and lobster agents may take longer than 60 seconds (e.g. multi-step LLM reasoning). We needed to decide what UI signal the user sees during this extended wait.

## Decision

**Reuse the placeholder pattern from ADR-0006** for all agent types. The placeholder message text is `"⏳ <agent_name> 正在思考..."`. On agent response:

- **Success**: write real message + soft-delete placeholder + broadcast `msg.deleted` + `msg.created`.
- **Failure**: write error message `"❌ <agent_name> 暂时不可用"` + soft-delete placeholder + broadcast same two events.

The placeholder message remains visible until the `msg.deleted` event arrives, even if the agent takes 5 minutes.

## Alternatives Considered

- **A. Do nothing — just show empty space**: rejected — user can't tell if anything is happening.
- **A+. Show system hint after 30s "agent still thinking..."**: rejected — system hint is a different UI element; message timeline has a gap.
- **B. Placeholder message (CHOSEN)**: same pattern as ADR-0006, consistent UX for all agent types.
- **C. Streaming tokens**: rejected for MVP — Phase 2.

## Consequences

### Positive
- Consistent UX: all "agent is thinking" states look the same.
- Trivial to implement: same code path as ADR-0006.
- Clients that already handle placeholder rendering (per ADR-0006) need no new logic.

### Negative
- Long placeholders clutter the timeline.
- No streaming — user can't preview partial answer.

### Risks
- **Agent hangs forever**: server boot must reap placeholders older than 10 minutes (covered in ADR-0006).

## Related

- ADR-0006 (tool agent async placeholder — this is the same pattern for longer waits)