# ADR-0004: HTTP REST and WebSocket share a single port

- **Status**: Accepted
- **Date**: 2026-07-26
- **Source**: docs/design/02-modules.md (Q1)

## Context

The Fireside server exposes REST endpoints (login, room CRUD, history query) and a WebSocket endpoint for real-time messaging. We needed to choose deployment topology: separate ports vs unified port.

## Decision

**Single port.** Both REST (Gin) and WebSocket (gorilla/websocket) listen on `:8080`. Path routing differentiates them (`/ws/v1/...` is WS, everything else is REST). Nginx upstream sets the `Upgrade` header to bridge browsers to WS.

## Alternatives Considered

- **Two ports** (e.g. `:8080` REST, `:8081` WS): rejected — doubles firewall config, doubles Nginx upstream blocks, doubles cert renewal. No real benefit since both are TLS-protected by Nginx anyway.

## Consequences

### Positive
- One cert, one Nginx `server` block.
- Single systemd unit, single port to monitor.
- Mobile clients (Android) only need one connection endpoint.

### Negative
- Slight request-routing complexity in Gin (trivial — gorilla mux handles `Upgrade` header transparently).
- Nginx config must preserve `Upgrade` / `Connection` headers explicitly.

### Risks
- **WS downgrade attack**: client must send `Connection: Upgrade` header; Gin path handler rejects non-WS clients on `/ws/v1/*`.

## Related

- docs/design/02-modules.md §HTTP routing
- docs/design/03-protocol.md §WS frame format