# Status

> **Phase 0 — Concept (frozen).** No code yet. Awaiting Go-signal to start Phase 1.

Last updated: 2026-07-26

## Where we are

Fireside is in the **concept → plan** handoff of an IPD-style workflow. All 25 product decisions (D1–D25) and 12 architectural decisions (ADR-0001–0012) are locked. Design documents for data model, modules, protocol, and state machines are drafted. Phase 1 RFC (Sprint 0 backend skeleton) is drafted.

## What's done

- ✅ Product overview, tech rationale, conversation log
- ✅ Decision snapshot (D1–D25)
- ✅ Design docs v0.1 (data model, modules, protocol, state machines)
- ✅ 12 architectural decision records
- ✅ Phase 1 RFC + PDCP self-check

## What's next (waiting for "Go")

When the owner signals "start Sprint 0", the next 1 day delivers:

- Gin + Gorilla WS single-port boot
- Postgres + golang-migrate + sqlc wired up
- JWT auth on REST + WS first-frame
- Android Compose activity that connects and shows "✅ connected"

Hard exit criteria are in `docs/rfc/phase-1-mvp.md`.

## How decisions are tracked

| Kind | Lives in | Format |
|---|---|---|
| Product decisions (D-numbers) | `docs/requirements/03-decision-snapshot.md` | Markdown table |
| Architecture decisions (ADR-numbers) | `docs/adr/0001-...0012-*.md` | One file per decision |
| Design specs | `docs/design/01-04-*.md` | Markdown |
| Phase plans | `docs/rfc/phase-N-*.md` | One file per phase |
| Reviews / gate outputs | `docs/reviews/*.md` | Checklist markdown |

## How to contribute

Until Phase 4 (launch), this project is single-owner. External contributions are **welcomed as PRs against the docs** (not the code, since there's no code yet). See `README.md` for contribution guide.

If you spot a problem with a locked decision, **open an issue**, don't silently work around it. We have 12 ADRs and 25 product decisions — that's a lot of "why we did this" history; help us keep it accurate.

## Cadence

- Owner publishes a `STATUS.md` update daily (every day at 22:00 local, via scheduled cron)
- Each update summarizes: what shipped, what's blocked, what's next
- Phase transitions require the corresponding RFC + checklist to be ticked

## Open invitations

- 👀 **Read the docs**, challenge the ADRs — the most useful feedback at this stage is "this decision has a hidden cost"
- 🧪 **Tell us about your chat-with-agents setup** — does Hermes, OpenClaw, or another runtime matter to you?
- 🌍 **Translate** — docs are English-first; Chinese, Japanese, etc. welcome