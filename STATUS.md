# Status

> **Phase 1 — Sprint 1 complete (rooms + messages + participants + hub; no Agent, no WS business frames yet).**
> RFC: [`docs/rfc/phase-2-minimal-demo.md`](rfc/phase-2-minimal-demo.md)
> Milestone: [`Sprint 1: Minimal Demo`](https://github.com/ishadowland/fireside/milestone/1) (issues #2–#12)
> Sprint 1.5 (deferred Android): tracked separately under WP-9 (issue #11).
> Sprint 2 (deferred Agent + business frames): tracked under WP-6..WP-8.

Last updated: 2026-08-03

## Where we are

**Sprint 1 backend stack is complete and end-to-end verified.** Sprint 0
hello-world is augmented with the rooms/messages/participants REST
surface, a real PostgreSQL schema (CHAR(26) IDs converted to VARCHAR(26)
in migration 0007), and an in-process broadcast hub (WP-5) ready to
be driven by WP-6 WS business frames.

```text
$ curl -X POST .../v1/auth/login {phone,code:1234}
  → 200 {token, expires_in:900}

$ curl -X POST .../v1/rooms -H "Bearer $TOKEN" {name,max}
  → 200 {room: {id, status: "active", ...}}

$ curl -X POST .../v1/rooms/:id/join -H "Bearer $TOKEN"
  → 200 {participant: {stage_state: "on_stage", ...}}
  (also writes a system message: participant.joined)

$ curl -X POST .../v1/rooms/:id/messages -H "Bearer $TOKEN" {content}
  → 200 {message: {id, sender_kind: "human", mentions: [], ...}}
```

The hub (`internal/hub`) is wired in `main.go` and unit-tested
(10/10 tests pass, including cross-room isolation, dead-conn
cleanup, and 5-conn × 50-broadcast concurrent). It is not yet
driven by any WS handler — that lands in WP-6.

## What's done (since the last status)

### Sprint 1 REST surface (WP-1 through WP-4, 2026-08-02)

- ✅ **WP-1: data layer** — 4 migrations (rooms / participants /
  messages / users_display_name) + sqlc query files
  (hand-generated; sqlc v1.31 requires Go 1.26, not 1.22). Store
  models include `Room` / `Participant` / `Message`.
  (commit `12550b5`, plus reviewer enum-cast fix `ffdbea4`)
- ✅ **WP-2: internal/rooms** — `Service` with
  CreateRoom / GetRoom / ListActive / EndRoom + 4 REST endpoints
  (POST / GET / GET :id / POST :id/end) + JWT middleware
  (commit `9b4fb47`; reviewer fix `061690e`:
  migration 0007 CHAR(26) → VARCHAR(26) + removed 6 Trim workaround
  sites)
- ✅ **WP-3: internal/messages** — `Service` with
  CreateMessage / CreateSystemMessage / ListMessagesByRoom /
  GetMessage + 2 REST endpoints (POST / GET :id/messages) +
  cursor pagination. (commit `d38cb0e`; reviewer fix `1d80955`:
  `::stage_state` casts + `ListMessagesByRoom` 404 on unknown
  room; follow-up `448d68c`: aliased `ErrRoomNotFound` to
  `rooms.ErrRoomNotFound` to fix cross-package `errors.Is` bug)
- ✅ **WP-4: internal/participants** — `Service` with
  JoinRoom / LeaveRoom / ListOnStageByRoom / ListOnStageByUser /
  GetOnStageParticipant + 2 REST endpoints (POST join / leave).
  Capacity 8 enforced; system message written on every transition.
  (commit `ccaaff5`; reviewer fix `4c89d73`: atomic JoinRoom +
  RETURNING LeaveRoom; follow-up `acf9415`: SQLSTATE 42P08 cast fix)

### Sprint 1 hub (WP-5, 2026-08-03)

- ✅ **WP-5: internal/hub** — in-process WS broadcast hub with
  11 methods (Register / Unregister / UnregisterFromRoom /
  BroadcastToRoom / IsSubscribed / Count / RoomCount / RoomMembers /
  ConnID / StartHeartbeat / MarshalFrame). Concurrency model:
  single sync.RWMutex, atomic two-phase broadcast (snapshot under
  RLock, writes outside). Dead-conn auto-unregister on write failure.
  Wired in `main.go`; not yet driven by any handler (WP-6).
  (commit `761094f`)

### Sprint 1 code-review fixes

| Issue | Root cause | Fix |
|---|---|---|
| #13 WP-2 review | CHAR(26) padding → Trim workaround scattered | `061690e` migration 0007 → VARCHAR(26) + drop 6 Trim calls |
| #14 WP-3 review | `errors.Is` cross-package mismatch (500 instead of 404) | `448d68c` alias `ErrRoomNotFound = rooms.ErrRoomNotFound` |
| #15 WP-4 review | SQLSTATE 42P08 (parameter type inference) | `acf9415` explicit `::CHAR(26)` / `::INT` / `::stage_state` casts |

### Sprint 1 CI + tech-debt (2026-08-03)

- ✅ **CI integration test gate** (commit `05c30b6`): adds
  `Integration test DB setup` step that creates `fireside_test` +
  runs all migrations. `go test` step now sets
  `FIRESIDE_TEST_DSN` + `-p 1 -count=1`. All three integration
  suites (`rooms` / `messages` / `participants`) actually run in
  CI for the first time.
- ✅ **sqlc version pin** (commit `5ab6de5`): `go install
  github.com/sqlc-dev/sqlc/cmd/sqlc@v1.27.0`. Fixes pre-existing
  Go-version-mismatch bug (v1.31+ requires Go 1.26; CI uses 1.22).

### Issue closeouts (2026-08-02 → 2026-08-03)

- #1 TestValidateTampered (base64 padding) — fixed in `d738757`
- #3 WP-1 data layer — closed in `12550b5`
- #4 WP-2 rooms — closed in `9b4fb47`
- #5 WP-3 messages — closed in `d38cb0e`
- #6 WP-4 participants — closed in `ccaaff5`
- #7 WP-5 hub — closed in `761094f`
- #13 WP-2 review — closed in `061690e`
- #14 WP-3 review — closed in `1d80955` + `448d68c`
- #15 WP-4 review — closed in `4c89d73` + `acf9415`
- #16 Sprint 1 Tech Debt — L-1 closed in `5ab6de5`; L-2 + L-3 open

### Sprint 1 verification (latest CI run `30781525415`)

- ✅ sqlc v1.27.0 install + verify
- ✅ migrations up + down (round-trip on `fireside`)
- ✅ migrations up on `fireside_test`
- ✅ `go build ./...` clean
- ✅ `go test -race -count=1 -p 1 ./...` — 10/10 hub + 10/10
  messages + 10/10 participants + 7/7 rooms + auth + dashboard + ws
  **all green** (CI run time 1m9s)
- ✅ golangci-lint v2 — 0 issues

## What's deferred

### Sprint 1.5 (WP-9, issue #11)

- Android UI: `RoomListActivity` + `RoomActivity` + WsClient
  extension for room.subscribe / msg.send / msg.created / room.subscribe.
  Defer to Sprint 1.5 because the WS business frames
  (WP-6) are not yet implemented.

### Sprint 2 (WP-6, WP-7, WP-8)

- **WP-6 WS business frames** — the `internal/ws/HandleConnect`
  dispatch loop after `auth.welcome`. Frames: `room.subscribe`,
  `room.unsubscribe`, `msg.send`, `msg.created`, `room.ended`,
  `error`. The hub is already wired and ready to be driven.
- **WP-7 REST additions** — `POST /v1/auth/refresh` (refresh
  token), `PATCH /v1/users/me` (display_name), and the full
  `join` / `leave` / `end` REST surface for participants (already
  done in WP-4).
- **WP-8 Dashboard UI** — `rooms.html` + `room.html` for in-browser
  chat. The REST API + WS first-frame already work; WP-8 is the
  client-side HTML / JS layer.

### Sprint 1 RFC §2.3 deviations (revisit Sprint 2)

- **D3 max 50 → max 8** in Sprint 1 (Q7) — still open, evaluate
  in Sprint 2.
- **D6 ephemeral (room end clears messages)** — still open, no
  end-of-room cleanup yet (per RFC Q3).

### Sprint 1 Tech Debt (issue #16, still open)

- **L-2** per-package schema for integration test parallelization
  (replace `-p 1` with 3 separate test DBs).
- **L-3** Node 20 deprecation warning in CI (cosmetic).

## What's next (Sprint 2 kickoff)

Sprint 2 priority order:

1. **WP-6 WS business frames** — finish the dispatch loop and
   drive the existing `internal/hub` package. Unblocks the
   Sprint 1 demo (2 browsers in dashboard, real-time chat).
2. **WP-8 Dashboard UI** — once WS frames land, build the
   client-side HTML / JS to exercise them. This is the
   user-visible Sprint 1 demo surface.
3. **WP-7 REST additions** — `auth/refresh` (for 1h JWT
   ergonomics) and `PATCH /users/me` (display name update).
4. **Sprint 2 ADR review** — re-evaluate D3 (max 8 vs 50) and
   D6 (ephemeral vs persistent). The current values are Sprint 1
   demo choices, not design locks.

## Open invitations

- 👀 **Read the docs**, challenge the ADRs
- 🧪 **Tell us about your chat-with-agents setup**
- 🌍 **Translate** — docs are English-first; Chinese, Japanese, etc. welcome