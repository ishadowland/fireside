# ADR-0007: WebSocket auth via first frame (auth.hello)

- **Status**: Accepted
- **Date**: 2026-07-26
- **Source**: docs/design/03-protocol.md (Q1)

## Context

WebSocket connections must be authenticated (users only join rooms they're authorized for). We needed to choose how to authenticate the WS upgrade request.

## Decision

**WS upgrade itself is unauthenticated.** The client must send an `auth.hello` frame as the **first message** within 5 seconds of upgrading, containing a JWT (obtained via the REST `/v1/auth/login` endpoint). Server validates the JWT, binds the connection to the user, and replies with `auth.welcome` or `auth.deny`. WS frames received before `auth.hello` are rejected; connections that don't send `auth.hello` within 5s are closed with code `4001`.

## Alternatives Considered

- **JWT in query string (`?token=...`)**: rejected — tokens leak into Nginx access logs, browser history, and Android process listings. First-frame pattern avoids this.
- **Cookie-based auth**: rejected — Android app needs to manage cookies; JWT is simpler for native apps.

## Consequences

### Positive
- JWT never appears in URL/logs.
- Connection-level binding (`conn.user_id`) is established immediately.
- Server can rate-limit auth.hello attempts per IP.

### Negative
- 5s window requires client to handle reconnect quickly.
- Two round-trips (REST login + WS auth.hello) instead of one.

### Risks
- **Replay**: JWT is short-lived (15min) and includes a `jti` claim; server tracks recently-seen `jti` for the access-token TTL.

## Related

- ADR-0008 (agent dual-trigger)
- docs/design/03-protocol.md §frame format