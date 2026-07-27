# Status

> **Phase 1 — Sprint 0 in progress (owner prep complete; Wave 1 dispatched).**

Last updated: 2026-07-27

## Where we are

Owner prep (D1–D9) for Sprint 0 is complete on `main`:
- Gin server boots on `:8080` with `/healthz`, slog JSON logging, graceful shutdown (`d164e74`).
- `users` (BIGINT id) + `auth_tokens` (UUID jti) baseline migration + sqlc-equivalent `internal/store/` (`8fca4fd`).
- `.golangci.yml` Sprint 0 baseline (errcheck, govet, ineffassign, staticcheck, unused) — `2297614`.
- ADR-0014 records the Sprint 0 → Sprint 1 `user_id` int64 → ULID migration plan; ADR-0007 amended to close code 1008 (`1078a1e`).

**⚠️ Owner action item**: `.github/workflows/ci.yml` has the `golangci-lint` step uncommented locally but the push was rejected by GitHub (OAuth token lacks `workflow` scope). The owner must push this change via a PAT with workflow scope OR edit it via the GitHub web UI. Until then, CI will skip lint.

**⚠️ Owner action item**: `make sqlc.verify` in CI will fail until SUB-001 lands and `go mod tidy` resolves `go.sum`. The CI run after D7 must either be marked as expected-fail or this prep is acknowledged in the PR description.

Three subcontracts are now dispatched in two waves per [`docs/handoff/sprint0/README.md`](./docs/handoff/sprint0/README.md).

## What's done (since last status)

- ✅ **ADR-0014** — Sprint 0 `user_id` is `int64`; ULID migration planned for Sprint 1
- ✅ **ADR-0007 (amendment)** — WS close code on auth failure is RFC 6455 `1008`, not `4001`
- ✅ **D1 — `cmd/fireside/main.go`** — Gin + `/healthz` + graceful shutdown
- ✅ **D2 — `go.mod`** — all Sprint 0 deps pinned
- ✅ **D3 — `db/migrations/0001_init.sql`** — users + auth_tokens baseline
- ✅ **D4 — `db/queries/auth.sql` + `internal/store/`** — sqlc-equivalent output
- ✅ **D5 — ADR-0014** — `user_id` type pin
- ✅ **D6 — ADR-0007 amendment** — close code correction
- ✅ **D7 (part 1) — `.golangci.yml`** — Sprint 0 baseline lint config; CI step uncommented locally but not pushed (OAuth scope)
- ✅ **D8 — README "Current Phase" badge** — updated to Phase 1 Sprint 0 in progress
- ✅ **D9 — STATUS.md** — this file
- ✅ **D10 — Sprint 0 scope note in handoff README** — the three intentional divergences from `docs/design/03-protocol.md` documented

## What's blocked

- **CI lint enforcement** — `.github/workflows/ci.yml` needs owner push (PAT with `workflow` scope). Until then, CI runs `go test -race ./...` but no lint.

## What's next

**Wave 1 (parallel, dispatched now):**
- **SUB-001** → Go backend agent — `internal/auth/` + `POST /v1/auth/login`
- **SUB-ANDROID** → Android agent — Compose `ConnectActivity` + `WsClient.kt`

**Wave 2 (after SUB-001 merges):**
- **SUB-003** → Go backend agent — `internal/ws/` + `GET /ws/v1/connect`

**Integration gate** (owner runs after Wave 2):
```
make db.up && make backend.run
TOKEN=$(curl -s -X POST localhost:8080/v1/auth/login -d '{"phone":"+8613800138000","code":"1234"}' | jq -r .token)
wscat -c ws://localhost:8080/ws/v1/connect
> {"type":"auth.hello","token":"$TOKEN"}
< {"type":"auth.welcome","user_id":42,"jti":"...","server_time":...}
```

## Open invitations

- 👀 **Read the docs**, challenge the ADRs — the most useful feedback at this stage is "this decision has a hidden cost"
- 🧪 **Tell us about your chat-with-agents setup** — does Hermes, OpenClaw, or another runtime matter to you?
- 🌍 **Translate** — docs are English-first; Chinese, Japanese, etc. welcome