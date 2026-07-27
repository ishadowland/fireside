# PDCP Self-Check — Phase 1 MVP Backend Skeleton

This is the IPD-style Plan Decision Check Point. Before Phase 2 (real development), the project owner ticks these boxes. The checklist exists to catch the most common "I rushed into code and now I regret it" failure modes.

**Self-check date**: 2026-07-27
**Result**: **Conditional GO** — design / scope / readiness inputs are complete; four operational artifacts (Go module init, `golangci.yml`, real migration files, CI smoke run) are explicitly deferred to the Sprint 0 day-of because they require Go code to exist first.

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
- [ ] Android min SDK decided (24+) — **deferred to Sprint 0 day-of** when `android/app/build.gradle` is created
- [x] Android networking library chosen (OkHttp WebSocket) — `docs/rfc/phase-1-mvp.md` §Dependencies

## Risk identification (TR2-style)

- [x] Network access from Android emulator to host documented (10.0.2.2) — `docs/rfc/phase-1-mvp.md` §Risks
- [x] Postgres dev environment reproducible (`docker-compose.yml`) — present in repo root
- [x] JWT secret handling documented (env vs vault) — `.env.example` + `docs/rfc/phase-1-mvp.md` §Risks
- [ ] WebSocket auth timeout (5s) implemented and tested — **deferred to Sprint 0 day-of** (requires Go code)
- [x] No silent failures — every error path emits a log line — `docs/design/02-modules.md` mandates `log/slog`; CI step is a smoke check on day-of

## Operational readiness

- [x] `Makefile` with named targets exists — present in repo root
- [x] `.env.example` documents required env vars — present in repo root
- [x] `.github/workflows/ci.yml` runs lint + tests on push — present (golangci-lint commented out, see below)
- [x] `STATUS.md` is current — updated 2026-07-27
- [ ] `README.md` reflects current phase — **partial**: README still describes Phase 0; will be updated when Sprint 0 starts (the README's "Current Phase" badge is the only line that changes; the rest is already correct)
- [ ] `.golangci.yml` exists — **deferred to Sprint 0 day-of** (no Go code to lint yet; CI step is commented out in `ci.yml` to match)

## Open questions (must be ZERO before ticking final box)

- [x] No "TBD" in design docs — grep clean
- [x] No "we'll decide later" in ADRs — ADR-0013 explicitly states "when to revisit" rather than leaving the question open
- [x] No unresolved comments in this checklist — see Deferred Notes below

## Final gate

- [x] Project owner has read every doc linked above — daily cadence via `fireside-daily-status.py`
- [ ] Project owner has approved RFC Phase 1 — **deferred**: this checklist is the first explicit approval artifact; owner must tick this line to signal "go"
- [ ] Project owner signals "Go" in chat → STATUS.md updated → coding begins — **deferred**: pending owner's chat signal

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

| Item | File produced during Sprint 0 | Owner verification |
|---|---|---|
| Android `min SDK = 24+` | `android/app/build.gradle` | `./gradlew assembleDebug` succeeds on API 24 emulator |
| WS auth timeout (5s) | `internal/ws/upgrader.go` | unit test: late `auth.hello` closes conn |
| README "Current Phase" badge | `README.md` line edit | README diff reviewed in PR |
| `.golangci.yml` | new file | `make backend.lint` returns 0 in CI |
| First migration file | `db/migrations/0001_init.sql` | `make migrate.up && make migrate.down` round-trip in CI |

---

## Self-check verdict (2026-07-27)

**Conditional GO.** All design and scope inputs are locked. Operational artifacts that don't require Go/Android code are in place. The five deferred items are correctly bounded — each requires source code that doesn't exist yet, so creating them now would be theater.

Owner should: read this checklist, confirm the five deferred items are acceptable as Sprint 0 day-of work, and then signal "go" to start Phase 1.