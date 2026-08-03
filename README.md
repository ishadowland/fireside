# Fireside 🔥

> **Async roundtable with AI seats.**
> 围炉鸿笺 — 给 AI 一个座位的圆桌。

> 「圍爐取暖，鴻箋傳心。」

Fireside is an async-first roundtable platform where a human host can pull in other humans and AI agents (custom personas, tools, or full lobster agents) into a persistent room for asynchronous discussion — needs clarification, situation reports, joint review.

## Status

✅ **Phase 1 — Sprint 1 complete (CI green; WS broadcast lands in Sprint 2).**
Sprint 1 ships the REST surface (rooms + messages + participants) end-to-end.
The WS business-frame dispatch (msg.send / room.subscribe / etc.) lands
in WP-6; until then the in-process broadcast hub (`internal/hub`) is wired
but not driven by any handler. End-to-end first-frame loop already works:
stub-code login → JWT (ULID `user_id`, 15-min TTL) → WS `auth.hello` →
`auth.welcome` → Postgres-backed with `InsertToken` jti replay defense.

What's in:

| Area | State |
|---|---|
| Backend | Gin + Gorilla WS on `:8080`, single-port (ADR-0004) |
| Auth | HS256 JWT, ULID `user_id` (ADR-0014, `oklog/ulid/v2`) |
| Persistence | hand-augmented `internal/store/` over pgx v5; `users` / `auth_tokens` / `rooms` / `participants` / `messages` schema |
| Replay defense | jti persisted on login, checked on WS first-frame (ADR-0007 §Risks) |
| REST surface | `/v1/rooms`, `/v1/rooms/:id/messages`, `/v1/rooms/:id/join|leave|end` (WP-2..WP-4) |
| Hub | `internal/hub` with 11 methods + 10/10 unit tests; wired in `main.go` (WP-5) |
| DeepTutor borrows | agent.question/answer, 3-layer memory, agent.progress, hunk diff (ADR-0015..0018) — design only, Sprint 2+ |
| Android | Compose `ConnectActivity` + `WsClient`; reads ULID string `user_id` (Sprint 0) |
| Local testing | `/dashboard/` loopback-only (ADR-0019); auto stub-login, no Android emulator needed |
| CI | GitHub Actions: sqlc v1.27.0 verify, migrations up/down, integration tests with `FIRESIDE_TEST_DSN` (`fireside_test`), go test, golangci-lint v2 — all ✓ |

See:
- [`STATUS.md`](./STATUS.md) — current phase & next steps
- [`docs/api/openapi.yaml`](./docs/api/openapi.yaml) — REST + WS contract
- [`docs/requirements/`](./docs/requirements/) — requirements & decisions
- [`docs/design/`](./docs/design/) — architecture design
- [`docs/adr/`](./docs/adr/) — 20 architectural decision records
- [`docs/rfc/`](./docs/rfc/) — phase plans
- [`docs/reviews/`](./docs/reviews/) — gate checklists (PDCP)

## Quick start (local dev)

Requires Go 1.22+, Docker (for the Postgres 16 sidecar), and Node 20+ (for Android / dashboard dev). `make` reads the Makefile; target names are self-documenting (`make help`).

```sh
cp .env.example .env                  # then edit JWT_SECRET at minimum
make db.up                            # docker compose up -d postgres (waits healthy)
make migrate.up                       # apply db/migrations/*.up.sql
make backend.run                      # boot Gin on :8080

# In a browser (loopback only):
xdg-open http://localhost:8080/dashboard/
# Auto stub-logs in, opens WS, prints auth.welcome — no Android needed.
```

Or hit the API directly:

```sh
# 1. Get a token
TOKEN=$(curl -s -X POST localhost:8080/v1/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"phone":"+8613800138000","code":"1234"}' | jq -r .token)

# 2. Round-trip the WS handshake
#    (wscat, websocat, or any browser WS client to ws://localhost:8080/ws/v1/connect)
#    first frame: {"type":"auth.hello","token":"$TOKEN"}
#    server reply: {"type":"auth.welcome","user_id":"01HXYZ...","jti":"...","server_time":...}
```

For the Android app (emulator or device):

```sh
cd android
./gradlew assembleDebug
./gradlew installDebug   # with an emulator or device attached
```

## Development Process

We follow an IPD-style (Integrated Product Development) workflow, scaled to a single-owner open-source project.

```
┌─────────┐   ┌─────────┐   ┌─────────┐   ┌─────────┐   ┌─────────┐   ┌─────────┐
│ 概念 0   │ → │ 计划 1  │ → │ 开发 2  │ → │ 验证 3  │ → │ 发布 4  │ → │ 生命周期 │
│ Concept │   │  Plan  │   │Develop │   │ Verify │   │ Launch │   │Lifecycle│
└─────────┘   └─────────┘   └─────────┘   └─────────┘   └─────────┘   └─────────┘
   ✅ done       ✅ done       ✅ Sprint 1    ⏸ waiting    ⏸ waiting   ⏸ waiting
```

Each phase has explicit gates:
- **Phase 0 → 1**: PDCP self-check (`docs/reviews/pdcp-checklist.md`)
- **Phase 1 → 2**: Sprint 0 exit criteria + Sprint 1 backlog (`docs/rfc/phase-1-mvp.md`)
- **Phase 2 → 3**: TR4 (integration test), TR5 (beta)
- **Phase 3 → 4**: ADCP (availability decision)
- **Phase 4 → 5**: launch checklist + community onboarding

Every architectural decision is captured as an ADR before code is written, so "why we did this" survives future-you.

## Project layout

```
fireside/
├── cmd/fireside/             # backend entrypoint (Gin + Gorilla WS)
├── internal/
│   ├── auth/                 # JWT issue/validate, /v1/auth/login
│   ├── ws/                   # WS upgrader + first-frame auth dispatcher
│   ├── store/                # sqlc-generated DB layer (DO NOT EDIT)
│   ├── dashboard/            # /dashboard/ + /v1/dashboard/config (ADR-0019)
│   └── config/               # env loader
├── db/
│   ├── migrations/           # golang-migrate *.up.sql / *.down.sql
│   └── queries/              # sqlc input
├── docs/                     # IPD docs (see cross-refs above)
├── android/                  # Compose app
└── ...
```

## Contributing (docs only, for now)

Until Phase 4, this project is single-owner. The most valuable contributions right now are:

1. **Read [`docs/adr/`](./docs/adr/) and challenge a decision** — open an issue if you spot a hidden cost.
2. **Translate docs** — current language is English-first; Chinese, Japanese, etc. welcome.
3. **Improve the design docs** — typo, ambiguity, missing edge case.

When Phase 2 lands, we'll add a `CONTRIBUTING.md` for code PRs.

## License

[MIT](./LICENSE) © 2026 ishadowland