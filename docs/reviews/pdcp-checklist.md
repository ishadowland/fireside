# PDCP Self-Check — Phase 1 MVP Backend Skeleton

This is the IPD-style Plan Decision Check Point. Before Phase 2 (real development), the project owner ticks these boxes. The checklist exists to catch the most common "I rushed into code and now I regret it" failure modes.

**Sprint 0 Self-check date**: 2026-07-27
**Sprint 1 Self-check date**: 2026-08-03
**Sprint 0 Result**: **Conditional GO** — design / scope / readiness inputs are complete; four operational artifacts (Go module init, `golangci.yml`, real migration files, CI smoke run) are explicitly deferred to the Sprint 0 day-of because they require Go code to exist first.
**Sprint 1 Result**: **Sprint 1 complete** — WP-1..WP-5 delivered, integration test gate live, all review feedback addressed. See the "Sprint 1 self-check (2026-08-03)" section at the bottom.

## Inputs (must exist before ticking)

- [x] `docs/requirements/00-overview.md` — product overview
- [x] `docs/requirements/01-tech-decisions.md` — Go vs Python rationale
- [x] `docs/requirements/02-conversation-summary.md` — clarification log
- [x] `docs/requirements/03-decision-snapshot.md` — D1–D29 decisions (D26-D29 added 2026-07-25 via Three-Sages MAGI)
- [x] `docs/design/01-data-model.md` — entities, fields, indexes
- [x] `docs/design/02-modules.md` — Go package layout, Android module layout
- [x] `docs/design/03-protocol.md` — WS frames, REST routes, error codes
- [x] `docs/design/04-state-machines.md` — room/participant/agent/workspace lifecycles
- [x] `docs/adr/0001-0013-*.md` — **13** architectural decisions recorded (ADR-0013 added 2026-07-27: Redis deferred with re-evaluation triggers)
- [x] `docs/rfc/phase-1-mvp.md` — Sprint 0 plan with hard 1-day deadline

## Scope clarity

- [x] Sprint 0 scope is explicit (in / out) — `docs/rfc/phase-1-mvp.md` §Scope
- [x] "What we are NOT doing" section is honest about deferrals — `docs/rfc/phase-1-mvp.md` §"What we are NOT doing"
- [x] Hard exit criteria are concrete (verifiable, not vague) — `docs/rfc/phase-1-mvp.md` §"Hard exit criteria"
- [x] Dependencies are listed with versions — `docs/rfc/phase-1-mvp.md` §"Dependencies added"

## Technical readiness

- [x] Go module path decided (`github.com/ishadowland/fireside`) — `docs/requirements/01-tech-decisions.md` + repo origin
- [x] Postgres version pinned (16-alpine) — `docker-compose.yml`
- [x] sqlc config file drafted (`sqlc.yaml`) — present in repo root
- [x] golang-migrate config drafted — targets wired into `Makefile`
- [x] JWT algorithm chosen (HS256) — `docs/adr/0007-ws-auth-first-frame.md` + `.env.example`
- [x] WebSocket library chosen (gorilla/websocket) — `docs/requirements/01-tech-decisions.md`
- [x] Android min SDK decided (24+) — `android/app/build.gradle` (Sprint 0 fix: Material3 → DeviceDefault parent + values-night dark variant)
- [x] Android networking library chosen (OkHttp WebSocket) — `docs/rfc/phase-1-mvp.md` §Dependencies

## Risk identification (TR2-style)

- [x] Network access from Android emulator to host documented (10.0.2.2) — `docs/rfc/phase-1-mvp.md` §Risks
- [x] Postgres dev environment reproducible (`docker-compose.yml`) — present in repo root
- [x] JWT secret handling documented (env vs vault) — `.env.example` + `docs/rfc/phase-1-mvp.md` §Risks
- [x] WebSocket auth timeout (5s) implemented and tested — `internal/ws/upgrader.go` (unit test: late `auth.hello` closes conn; commit `821d090` + `203829f`)
- [x] No silent failures — every error path emits a log line — `docs/design/02-modules.md` mandates `log/slog`; CI step is a smoke check on day-of

## Operational readiness

- [x] `Makefile` with named targets exists — present in repo root
- [x] `.env.example` documents required env vars — present in repo root
- [x] `.github/workflows/ci.yml` runs lint + tests on push — present (golangci-lint v2 active; integration test gate added in commit `05c30b6`)
- [x] `STATUS.md` is current — updated 2026-08-03 (Sprint 1 final)
- [x] `README.md` reflects current phase — Sprint 1 CI-green badge live
- [x] `.golangci.yml` exists — v2 schema; `make backend.lint` returns 0 in CI

## Open questions (must be ZERO before ticking final box)

- [x] No "TBD" in design docs — grep clean
- [x] No "we'll decide later" in ADRs — ADR-0013 explicitly states "when to revisit" rather than leaving the question open
- [x] No unresolved comments in this checklist — see Deferred Notes below

## Final gate

- [x] Project owner has read every doc linked above — daily cadence via `fireside-daily-status.py`
- [x] Project owner has approved RFC Phase 1 — **Conditional GO** (Sprint 0) ticked 2026-07-27
- [x] Project owner signals "Go" in chat → STATUS.md updated → coding begins — Sprint 0 launched 2026-07-28; Sprint 1 launched 2026-08-02; Sprint 1 complete 2026-08-03

---

## Anti-patterns to reject

If any of these are true, **do not start coding**:

- ❌ "I'll figure it out as I go" → design docs are placeholders — **clean**, all design docs have entities + state transitions locked
- ❌ "The deadline is loose" → no hard Sprint 0 deadline (must be 1 day per RFC) — **clean**, RFC explicitly 1-day
- ❌ "I have 3 different auth schemes in mind" → design doc not pinned — **clean**, HS256 + WS first-frame pinned in ADR-0007
- ❌ "Tests can come later" → CI workflow not configured — **clean**, CI present
- ❌ "I'll write docs after the code" → this checklist skipped — **clean**, this is the checklist

## Deferred notes (Sprint 0 day-of, not pre-coding blockers)

These items require Go or Android code to exist. They are tracked here so they cannot be silently skipped on the day:

| Item | File produced during Sprint 0 | Owner verification | Status |
|---|---|---|---|
| Android `min SDK = 24+` | `android/app/build.gradle` | `./gradlew assembleDebug` succeeds on API 24 emulator | ✅ Sprint 0 |
| WS auth timeout (5s) | `internal/ws/upgrader.go` | unit test: late `auth.hello` closes conn | ✅ Sprint 0 |
| README "Current Phase" badge | `README.md` line edit | README diff reviewed in PR | ✅ Sprint 0 |
| `.golangci.yml` | new file | `make backend.lint` returns 0 in CI | ✅ Sprint 0 |
| First migration file | `db/migrations/0001_init.sql` | `make migrate.up && make migrate.down` round-trip in CI | ✅ Sprint 0 |

## Self-check verdict (2026-07-27)

**Conditional GO.** All design and scope inputs are locked. Operational artifacts that don't require Go/Android code are in place. The five deferred items are correctly bounded — each requires source code that doesn't exist yet, so creating them now would be theater.

Owner should: read this checklist, confirm the five deferred items are acceptable as Sprint 0 day-of work, and then signal "go" to start Phase 1.

---

## Sprint 1 self-check (2026-08-03)

A second self-check, covering the Sprint 1 deliverables, was added
when the Sprint 0 checklist above was first finalized. This is a
**delta-only** self-check — items already ✓ from Sprint 0 are not
re-listed. See the "Sprint 1 self-check" commit for the full
context.

### Sprint 1 inputs (must exist before ticking)

- [x] `docs/rfc/phase-2-minimal-demo.md` — Sprint 1 RFC (DCP-2
  plan; 25 clarifications resolved; WBS WP-1..WP-10)
- [x] 6 new migrations: 0003_rooms / 0004_participants /
  0005_messages / 0006_users_display_name / 0007_ids_to_varchar
  (WP-1 + reviewer fix)
- [x] 4 sqlc query files: `db/queries/{rooms,participants,messages,
  users_display_name}.sql` (WP-1)
- [x] ADR-0019 (local test Dashboard) — wired in Sprint 0
- [x] Sprint 1 deviation table in `docs/rfc/phase-2-minimal-demo.md`
  §2.3 — most items obsolete after Sprint 1-2/1-3 commits

### Sprint 1 technical readiness (incremental over Sprint 0)

- [x] `internal/auth/Middleware` (JWT bearer) — `internal/auth/middleware.go`
- [x] `internal/rooms` package — WP-2
- [x] `internal/messages` package — WP-3
- [x] `internal/participants` package — WP-4
- [x] `internal/hub` package — WP-5
- [x] `internal/store` hand-augmented for Sprint 1 entities
  (Rooms / Participants / Messages)
- [x] Store `enums.go` typed constants for all 4 enums
  (room_status / stage_state / sender_kind / content_type)

### Sprint 1 operational readiness (incremental over Sprint 0)

- [x] CI integration test gate (commit `05c30b6`): `fireside_test`
  DB auto-created in CI; integration tests no longer skipped
- [x] sqlc version pinned to v1.27.0 (commit `5ab6de5`): fixes
  pre-existing Go-1.26 mismatch bug
- [x] `docs/api/openapi.yaml` version 0.4.0 — covers all 4 REST
  namespaces (auth / rooms / messages / participants)
- [x] `STATUS.md` reflects Sprint 1 final state
- [x] Cross-package `ErrRoomNotFound` aliased to canonical
  `rooms.ErrRoomNotFound` (commit `448d68c`) — avoids the
  "two equal-but-different errors.Is" footgun

### Sprint 1 deferred items (Sprint 2+)

- WP-6 WS business frames (`room.subscribe` / `msg.send` etc.) —
  not yet implemented; `internal/hub` is wired but not driven
- WP-7 REST additions (`auth/refresh` / `PATCH /users/me`)
- WP-8 Dashboard UI (`rooms.html` / `room.html` for in-browser chat)
- WP-9 Android UI (Sprint 1.5)
- Sprint 1 RFC §2.3 deviations: D3 max 50 vs Sprint 1 max 8; D6
  ephemeral still not implemented (per RFC Q3)
- Issue #16 L-2: per-package schema for parallel integration tests
- Issue #16 L-3: Node 20 deprecation warning in CI

### Sprint 1 self-check verdict (2026-08-03)

**Sprint 1 complete.** All Sprint 1 WP items (WP-1..WP-5) shipped with
end-to-end verification; CI integration test gate is live; all
review feedback issues (#13, #14, #15) closed. Sprint 1 acceptance
gate (RFC §7.2) is met for the REST surface; the WS broadcast
surface lands with WP-6.

Owner should: review `STATUS.md` "What's next" for Sprint 2
priority order (WP-6 first, then WP-8, then WP-7).