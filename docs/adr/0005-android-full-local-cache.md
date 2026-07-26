# ADR-0005: Android app stores full local message cache

- **Status**: Accepted
- **Date**: 2026-07-26
- **Source**: docs/design/02-modules.md (Q2)

## Context

Rooms in Fireside physically delete messages when ended (server-side enforcement). But mobile users on flaky networks need offline access to messages they have already received, and may want to export their history. We needed to decide the Android caching strategy.

## Decision

**Full local cache** with three controls:

1. **Cache scope**: All messages received by this client are persisted to a Room (AndroidX) database, bucketed per `room_id`.
2. **User export**: User can export any room's cache as Markdown/JSON via the room's overflow menu → "导出对话".
3. **User clear**: User can clear any room's cache via overflow → "清除本地缓存". **Logging out clears all caches by default.**
4. **Server-authoritative re-fetch**: On reconnect to a still-live room, client requests `since=<last_msg_id>` and merges delta. Ended rooms' caches stay on disk until user clears.

## Alternatives Considered

- **A. No cache, fetch on demand**: rejected — flaky networks make this unusable; users complain about missing context.
- **B. Session-only cache (RAM)**: rejected — same problem as A, plus restart loses everything.
- **C. Full cache + user export/clear (CHOSEN)**: aligns with user's "围炉鸿笺" intent — users may want to keep their own paper letters.

## Consequences

### Positive
- Offline read works.
- Users can preserve their own copy even after room ends server-side.
- Aligns with the "纸笺" metaphor: server may burn the room, but the user keeps their letter.

### Negative
- Storage grows; users may need to clear periodically.
- Slight divergence between server view and local view (server-deleted messages persist locally until user clears).

### Risks
- **Privacy leak on shared device**: user must explicitly clear before handing over device. Mitigation: app launches into last room, but "clear on logout" handles most cases.

## Related

- ADR-0010 (room message deletion on end)