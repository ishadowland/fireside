# ADR-0006: Tool agents respond asynchronously with a placeholder message

- **Status**: Accepted
- **Date**: 2026-07-26
- **Source**: docs/design/02-modules.md (Q3)

## Context

Tool agents may take 5-60 seconds to respond (e.g. LLM-backed tools with long contexts). The user-facing client must not block on the response. We needed to decide how the client perceives agent latency.

## Decision

When a user message triggers an agent response, the server **immediately writes a placeholder message** (`is_placeholder=true`) and broadcasts it to all room participants. The agent responds in a background goroutine. When the agent returns:

1. Server writes the real message.
2. Server marks the placeholder as deleted (`deleted_at` timestamp).
3. Server broadcasts `msg.deleted` (for the placeholder) and `msg.created` (for the real message) over WS.

## Alternatives Considered

- **Synchronous response (block until agent returns)**: rejected — WS message would block 30-60s, exceeding typical client timeout. Bad UX.
- **Spinner only (no placeholder msg)**: rejected — message timeline would have a gap; users wouldn't know if agent was even triggered.
- **Streaming tokens (Type C from proposal)**: rejected for MVP — adds server-side SSE proxy logic; defer to Phase 2.

## Consequences

### Positive
- User sees immediate feedback ("⏳ scribe 正在思考...").
- WS frame is small and fast.
- Works for all three agent types (tool / custom / lobster) uniformly.

### Negative
- Two broadcast events instead of one (msg.deleted + msg.created).
- Client must handle placeholder rendering and soft-deletion cleanly.

### Risks
- **Server crash mid-response**: placeholder stays orphaned. Mitigation: server boot reaps placeholders older than 10 minutes.

## Related

- ADR-0007 (placeholder behavior for long agent responses — same pattern)