# ADR-0010: No parent-room relationship in MVP

- **Status**: Accepted
- **Date**: 2026-07-26
- **Source**: docs/design/04-state-machines.md (Q1)

## Context

Users sometimes want to "fork" a room into a related discussion (e.g. a follow-up topic on an earlier conversation). We considered adding a `Room.parent_room_id` foreign key to model this explicitly.

## Decision

**No parent-room relationship in MVP.** Rooms are flat — there is no tree/hierarchy. Users who want continuity can:

1. Search for the prior room in their archive (post-room-end summaries are persisted).
2. Open the prior room (if still live) and reference it.
3. Start a new room and explicitly paste a link to the prior one in their first message.

## Alternatives Considered

- **B. Soft reference (`Room.parent_archive_id`)**: rejected for MVP — adds schema complexity for a feature that archive-search already covers.
- **C. Hard FK relationship (Room.parent_room_id)**: rejected — requires recursive queries, breadcrumb UI, and "what happens if parent is deleted" semantics.

## Consequences

### Positive
- Zero schema overhead.
- Archive-based continuity already exists and works.

### Negative
- No native "fork tree" UI.
- Cross-room navigation is manual.

### Risks
- **Users complain about no tree view**: defer to Phase 2 if actual demand emerges.

## Related

- (none — covered by archive search in design docs)