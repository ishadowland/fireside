# Status

> **Phase 0 → Phase 1 handoff complete (docs-side). Awaiting owner's "Go" signal to start Sprint 0.**

Last updated: 2026-07-27

## Where we are

Fireside is at the **end of the Phase 0/1 IPD handoff**. All design decisions, ADRs, and the Sprint 0 RFC are locked. The Phase 1 PDCP self-check has been run on 2026-07-27 and produced a **Conditional GO** — all docs-side and workspace-side inputs are ticked; five items are explicitly deferred to the Sprint 0 day-of because they require Go/Android source code that does not exist yet.

## What's done (since last status)

- ✅ **ADR-0013** — Redis is deferred with explicit re-evaluation triggers (`docs/adr/0013-redis-deferred-with-triggers.md`)
- ✅ **PDCP self-check** — `docs/reviews/pdcp-checklist.md` updated 2026-07-27, verdict = Conditional GO
- ✅ **Sprint 0 workspace preflight** — five config files committed so Sprint 0 day-of does not start with infra setup:
  - `.env.example` (PORT / POSTGRES_DSN / JWT_SECRET / JWT_ACCESS_TTL_MIN / LOG_LEVEL / SMS_STUB_CODE)
  - `docker-compose.yml` (Postgres 16-alpine, named volume, healthcheck)
  - `sqlc.yaml` (engine + queries + schema paths, Go overrides)
  - `Makefile` (db.up/down, migrate.up/down, sqlc.generate/verify, backend.run/test/lint, android.install/test)
  - `.github/workflows/ci.yml` (Postgres service, sqlc vet, migrate round-trip, go test -race)

## What's deferred (Sprint 0 day-of, not pre-coding blockers)

| Item | Why deferred |
|---|---|
| Android `min SDK = 24+` in `build.gradle` | No Android source yet |
| WebSocket auth 5s timeout (`internal/ws/upgrader.go`) | No Go source yet |
| `README.md` "Current Phase" badge line edit | Trivial edit, but pairs with first commit |
| `.golangci.yml` | No Go code to lint yet (CI step is commented out) |
| `db/migrations/0001_init.sql` | First migration needs Go side wired |

These are tracked at the bottom of `docs/reviews/pdcp-checklist.md` §"Deferred notes".

## What's next (waiting for "Go")

When the owner signals "start Sprint 0", the next 1 day delivers:

- `go mod init github.com/ishadowland/fireside` + Gin + `/healthz`
- Postgres up via `make db.up`, `db/migrations/0001_init.sql` applies cleanly
- `internal/auth/` HS256 JWT issue/validate + `POST /v1/auth/login` (stub SMS accepts any phone + `1234`)
- `internal/ws/` upgrader + first-frame router (`auth.hello` → `auth.welcome`)
- Android Compose `ConnectActivity` connects and shows "✅ connected"
- `make backend.run` + `make android.install` both succeed
- CI smoke run green on first push

Hard exit criteria are in `docs/rfc/phase-1-mvp.md` §"Hard exit criteria".

## How decisions are tracked

| Kind | Lives in | Format |
|---|---|---|
| Product decisions (D-numbers) | `docs/requirements/03-decision-snapshot.md` | Markdown table (D1–D29) |
| Architecture decisions (ADR-numbers) | `docs/adr/0001-...0013-*.md` | One file per decision |
| Design specs | `docs/design/01-04-*.md` + `07-three-sages-coalition.md` | Markdown |
| Phase plans | `docs/rfc/phase-N-*.md` | One file per phase |
| Reviews / gate outputs | `docs/reviews/*.md` | Checklist markdown |

## How to contribute

Until Phase 4 (launch), this project is single-owner. External contributions are **welcomed as PRs against the docs** (not the code, since there's no code yet). See `README.md` for contribution guide.

If you spot a problem with a locked decision, **open an issue**, don't silently work around it. We have 13 ADRs and 29 product decisions — that's a lot of "why we did this" history; help us keep it accurate.

## Cadence

- Owner publishes a `STATUS.md` update daily (every day at 22:00 local, via scheduled cron)
- Each update summarizes: what shipped, what's blocked, what's next
- Phase transitions require the corresponding RFC + checklist to be ticked

## Open invitations

- 👀 **Read the docs**, challenge the ADRs — the most useful feedback at this stage is "this decision has a hidden cost"
- 🧪 **Tell us about your chat-with-agents setup** — does Hermes, OpenClaw, or another runtime matter to you?
- 🌍 **Translate** — docs are English-first; Chinese, Japanese, etc. welcome