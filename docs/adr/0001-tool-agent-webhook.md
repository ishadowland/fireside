# ADR-0001: Tool agent integration via outbound HTTP webhook

- **Status**: Accepted
- **Date**: 2026-07-26
- **Source**: docs/design/01-data-model.md (Q1), docs/design/02-modules.md (Q3)

## Context

Fireside supports three agent types (tool, custom, lobster). The tool agent is the simplest: it takes a user prompt and returns a response without memory or LLM reasoning on the Fireside side. We needed to choose how the server talks to tool agents.

## Decision

Tool agents integrate via an **outbound HTTP webhook** configured per-room in `Room.Config.ToolAgentConfig`. The Fireside server is the HTTP client; the tool agent endpoint is the server.

## Alternatives Considered

- **Inbound webhook (agent pulls)**: rejected — requires the tool agent to be reachable from the public internet, which raises NAT/firewall concerns for typical deployment.
- **Long-polling / Server-Sent Events from agent**: rejected — adds protocol complexity for MVP; tool agents are stateless and don't need streaming.
- **Local exec (shell command)**: rejected — couples deployment; bad security posture.

## Consequences

### Positive
- Simple, stateless, fits MVP.
- No inbound ports on tool agent side.
- Trivial to test with `curl` or `nc`.

### Negative
- Server becomes a webhook caller — needs retry / timeout policy (see Risks).
- Each tool agent endpoint must be reachable from server (cloud egress).

### Risks
- **Network failure**: server must cap retries at N=2 and surface failure as `msg.deleted` placeholder collapse (per ADR-0009).
- **Slow agent**: cap at 60s timeout; long responses beyond timeout get truncated + retry-failure message.

## Related

- ADR-0002 (custom agent memory)
- ADR-0003 (raise hand auto-timeout — none in MVP)
- docs/design/01-data-model.md §ToolAgentConfig
- docs/design/02-modules.md §agent tool driver