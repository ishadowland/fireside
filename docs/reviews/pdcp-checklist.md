# PDCP Self-Check — Phase 1 MVP Backend Skeleton

This is the IPD-style Plan Decision Check Point. Before Phase 2 (real development), the project owner ticks these boxes. The checklist exists to catch the most common "I rushed into code and now I regret it" failure modes.

## Inputs (must exist before ticking)

- [ ] `docs/requirements/00-overview.md` — product overview
- [ ] `docs/requirements/01-tech-decisions.md` — Go vs Python rationale
- [ ] `docs/requirements/02-conversation-summary.md` — clarification log
- [ ] `docs/requirements/03-decision-snapshot.md` — D1–D25 decisions
- [ ] `docs/design/01-data-model.md` — entities, fields, indexes
- [ ] `docs/design/02-modules.md` — Go package layout, Android module layout
- [ ] `docs/design/03-protocol.md` — WS frames, REST routes, error codes
- [ ] `docs/design/04-state-machines.md` — room/participant/agent/workspace lifecycles
- [ ] `docs/adr/0001-0012-*.md` — 12 architectural decisions recorded
- [ ] `docs/rfc/phase-1-mvp.md` — Sprint 0 plan with hard 1-day deadline

## Scope clarity

- [ ] Sprint 0 scope is explicit (in / out)
- [ ] "What we are NOT doing" section is honest about deferrals
- [ ] Hard exit criteria are concrete (verifiable, not vague)
- [ ] Dependencies are listed with versions

## Technical readiness

- [ ] Go module path decided (`github.com/ishadowland/fireside`)
- [ ] Postgres version pinned (16.x)
- [ ] sqlc config file drafted (`sqlc.yaml`)
- [ ] golang-migrate config drafted
- [ ] JWT algorithm chosen (HS256)
- [ ] WebSocket library chosen (gorilla/websocket)
- [ ] Android min SDK decided (24+)
- [ ] Android networking library chosen (OkHttp WebSocket)

## Risk identification (TR2-style)

- [ ] Network access from Android emulator to host documented (10.0.2.2)
- [ ] Postgres dev environment reproducible (`docker-compose.yml`)
- [ ] JWT secret handling documented (env vs vault)
- [ ] WebSocket auth timeout (5s) implemented and tested
- [ ] No silent failures — every error path emits a log line

## Operational readiness

- [ ] `Makefile` with named targets exists
- [ ] `.env.example` documents required env vars
- [ ] `.github/workflows/ci.yml` runs lint + tests on push
- [ ] `STATUS.md` is current
- [ ] `README.md` reflects current phase

## Open questions (must be ZERO before ticking final box)

- [ ] No "TBD" in design docs
- [ ] No "we'll decide later" in ADRs
- [ ] No unresolved comments in this checklist

## Final gate

- [ ] Project owner has read every doc linked above
- [ ] Project owner has approved RFC Phase 1
- [ ] Project owner signals "Go" in chat → STATUS.md updated → coding begins

---

## Anti-patterns to reject

If any of these are true, **do not start coding**:

- ❌ "I'll figure it out as I go" → design docs are placeholders
- ❌ "The deadline is loose" → no hard Sprint 0 deadline (must be 1 day per RFC)
- ❌ "I have 3 different auth schemes in mind" → design doc not pinned
- ❌ "Tests can come later" → CI workflow not configured
- ❌ "I'll write docs after the code" → this checklist skipped

## If a box is unchecked

Stop. Do not start coding. Either:

1. Tick the box (do the missing work), or
2. Amend the RFC to remove the unmet requirement, or
3. Document explicitly why it's deferred (and to which RFC/phase).