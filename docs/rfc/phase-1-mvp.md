# RFC: Phase 1 — MVP Backend Skeleton (Sprint 0)

- **Status**: Draft (awaiting PDCP self-check)
- **Author**: project owner + Hermes
- **Created**: 2026-07-26
- **Target start**: when owner signals "go"
- **Hard deadline**: 3 days from "go" → hello-world end-to-end

## Goal

In 3 days, stand up the minimum backend skeleton that proves the design docs are implementable:

1. A single Go process serves REST + WS on one port.
2. Postgres + golang-migrate + sqlc compile and a single migration applies.
3. A REST endpoint `POST /v1/auth/login` returns a JWT.
4. A WS endpoint `/ws/v1/connect` accepts an `auth.hello` frame and replies `auth.welcome`.
5. Android side: an empty Compose activity connects to the WS, sees `auth.welcome`, displays "✅ connected".

No business logic yet. No agents. No rooms. Just the wiring.

## Scope (in)

- `cmd/fireside/main.go` boots Gin + Gorilla WS upgrade on `:8080`.
- `internal/auth/` — JWT issue/validate (HS256, single secret from env).
- `internal/ws/` — gorilla/websocket upgrader + first-frame dispatcher.
- `db/migrations/0001_init.sql` — just `users(id, phone, created_at)` and `auth_tokens(jti, user_id, expires_at)` tables.
- `db/queries/auth.sql` — sqlc-generated `GetUserByPhone`, `InsertToken`.
- `android/app/` — single Compose activity that opens a WebSocket to `ws://10.0.2.2:8080/ws/v1/connect`, sends `auth.hello`, renders result.
- `Makefile` targets: `db.up`, `db.down`, `sqlc.generate`, `backend.run`, `android.install`.

## Scope (out)

- Room creation, message persistence, agent drivers — Sprint 1+.
- Authentication provider (SMS gateway) — use a stub that accepts any phone + `1234` code.
- Real Android UI — only the connection-status screen exists.

## Architecture (file layout)

```
fireside/
├── cmd/fireside/main.go
├── internal/
│   ├── auth/        # JWT, login handler
│   ├── ws/          # upgrader + first-frame router
│   ├── store/       # sqlc-generated (DO NOT EDIT)
│   └── config/      # env loading
├── db/
│   ├── migrations/0001_init.sql
│   └── queries/auth.sql
├── android/app/
│   └── src/main/.../ConnectActivity.kt
├── Makefile
├── go.mod
└── docs/
    ├── adr/
    ├── design/
    └── rfc/
```

## Sprint 0 task breakdown

| Day | Task | Acceptance |
|---|---|---|
| 1 | `go mod init`, Gin boot, `/healthz` returns 200 | `curl localhost:8080/healthz` → 200 |
| 1 | Postgres via docker-compose, golang-migrate applies `0001_init.sql` | `make db.up` succeeds, `psql` shows tables |
| 1 | sqlc generates `internal/store/`, `InsertUser/GetUserByPhone` callable | unit test passes |
| 2 | `internal/auth/` — HS256 JWT issue + validate | unit test passes (sign → parse → verify roundtrip) |
| 2 | `POST /v1/auth/login` accepts `{phone, code}`, returns JWT | `curl` with `1234` code returns token |
| 2 | `internal/ws/` upgrader, first-frame router reads `auth.hello`, validates JWT, replies `auth.welcome` | `wscat` connects, sends frame, gets welcome |
| 3 | Android project scaffold (Gradle, Compose, OkHttp WebSocket) | `./gradlew assembleDebug` succeeds |
| 3 | `ConnectActivity` opens WS, sends `auth.hello`, shows status | emulator → app shows "✅ connected" |

## Hard exit criteria (PDCP self-check)

Phase 1 only graduates to Phase 2 when ALL of these are true:

- [ ] `make backend.run` boots without errors
- [ ] `make db.up && make db.down` is clean (idempotent)
- [ ] `curl localhost:8080/healthz` returns 200
- [ ] `wscat` connects to `/ws/v1/connect` and roundtrips `auth.hello` → `auth.welcome`
- [ ] Android emulator shows "✅ connected" after sending the frame
- [ ] All endpoints appear in `docs/api/openapi.yaml`
- [ ] CI workflow `.github/workflows/ci.yml` runs `go test ./...` on every push
- [ ] `STATUS.md` updated to "Phase 1 — Sprint 0 in progress"

## Risks & mitigations

| Risk | Mitigation |
|---|---|
| Android emulator network (10.0.2.2 host alias) | Document in README; add `android-connect.sh` helper |
| Postgres not installed locally | Provide `docker-compose.yml` for `postgres:16` |
| WebSocket first-frame timing (5s window) | Server logs a warning if `auth.hello` is late; closed connection counted as metric |
| JWT secret in env | Document `.env.example`; production uses systemd `Environment=` |

## Dependencies added

- `github.com/gin-gonic/gin` v1.10+
- `github.com/gorilla/websocket` v1.5+
- `github.com/golang-jwt/jwt/v5` v5.2+
- `github.com/jackc/pgx/v5` (for sqlc runtime)
- `github.com/sqlc-dev/pq` (sqlc database/sql driver)
- `github.com/golang-migrate/migrate/v4` (CLI)
- `github.com/google/uuid` (jti generation)

Android:
- `androidx.compose.material3:material3`
- `com.squareup.okhttp3:okhttp`
- `org.jetbrains.kotlinx:kotlinx-serialization-json`

## What we are NOT doing (yet)

This RFC explicitly defers the following to Sprint 1+:

- Real SMS provider (Twilio etc.) — use stub.
- Refresh tokens — single JWT, 15min TTL, re-login.
- TLS — handled by Nginx upstream in production; backend is plain HTTP in dev.
- WebSocket reconnect / heartbeat — basic ping/pong only.
- Multi-tenant separation — single DB, no row-level tenant filter.

## Review process

This RFC is reviewed by the project owner against `docs/reviews/pdcp-checklist.md`. No external review needed for Phase 1 — it's a wiring exercise.

Once "Go" is signaled, this RFC moves to "Implementing" status and `STATUS.md` is updated.