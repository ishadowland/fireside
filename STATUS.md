# Status

> **Phase 1 — Sprint 0 complete (server-side hello-world verified). Awaiting Android emulator smoke + Phase 1 exit.**

Last updated: 2026-07-29

## Where we are

Sprint 0 server-side hello-world is end-to-end green:
- `go build ./...` clean
- `go test -race ./...` clean (auth: 7 tests, ws: 4 tests)
- `golangci-lint run ./...` 0 issues
- Manual integration gate from `docs/handoff/sprint0/README.md` §"Acceptance gate" passes:
  - `curl /healthz` → 200 `{"status":"ok"}`
  - `POST /v1/auth/login {phone, code:1234}` → 200 `{token, expires_in:900}`
  - `POST /v1/auth/login {phone, code:0000}` → 401 `invalid_credentials`
  - `POST /v1/auth/login {phone:'bad'}` → 400 `invalid_request`
  - WS `auth.hello` → `auth.welcome{user_id, jti, server_time}` (jti matches the JWT's jti)
  - WS no hello within 5s → `auth.error{code:hello_timeout}` + close **1008** (RFC 6455 policy violation)
  - WS hello with garbage token → `auth.error{code:invalid_token}` + close 1008

**⚠️ Owner action items** (not blocking Sprint 0 exit but should land before Phase 2):
1. `.github/workflows/ci.yml` has the `golangci-lint` step uncommented locally but the push was rejected by GitHub (OAuth token lacks `workflow` scope). Owner must push this change via a PAT with workflow scope OR edit it via the GitHub web UI.
2. Android `ConnectActivity` is implemented but not yet smoke-tested against the running backend on an emulator — this requires a real Android device / emulator (out of scope for this session's automated verification).
3. Sprint 1 backlog: swap `deriveStubUserID` for `internal/store.GetUserByPhone`, migrate `users.id` BIGINT → ULID per ADR-0014, add jti persistence via `InsertToken` for replay defense per ADR-0007.

## What's done (since last status)

### Baseline verification (2026-07-29)
- ✅ Installed Go 1.22.5 toolchain (user-local, `/tmp/go`)
- ✅ Installed `golangci-lint v1.60.1` per `.golangci.yml` `version: "2"` config
- ✅ Fixed 6 latent lint issues uncovered by the verification run:
  - `internal/ws/upgrader.go:126` — `_ = conn.SetReadDeadline(time.Time{})` → log-on-error (errcheck)
  - `internal/ws/upgrader_test.go:122,152,191` — same `_ = SetReadDeadline` → `t.Fatalf` on error
  - `internal/auth/handler_test.go:14` + `internal/ws/upgrader_test.go:21` — `TestMain` now calls `os.Exit(m.Run())` (staticcheck SA3000)
- ✅ Re-ran all three gates: build / test -race / golangci-lint all 0 issues

### Documentation (pre-coding)
- ✅ **ADR-0014** — `user_id` is `int64` in Sprint 0, migrates to ULID at Sprint 1 (`1078a1e`)
- ✅ **ADR-0007 (amendment)** — WS close code on auth failure is RFC 6455 `1008`, not `4001` (`1078a1e`)
- ✅ **Sprint 0 scope note** — `docs/handoff/sprint0/README.md` documents the 3 intentional divergences from `docs/design/03-protocol.md` (`1078a1e`)
- ✅ **SUB-003 close-code clarification** — explicit citation of `1008` in test annotations (`1078a1e`)

### Owner prep (D1–D9)
- ✅ **D1+D2** — Gin server + `/healthz` + `go.mod` (`d164e74`)
- ✅ **D3+D4** — `db/migrations/0001_init.sql` + `db/queries/auth.sql` + sqlc-equivalent `internal/store/` (`8fca4fd`)
- ✅ **D5+D6** — folded into the doc commit (`1078a1e`)
- ✅ **D7** — `.golangci.yml` Sprint 0 baseline (`2297614`); CI step uncommented locally, push blocked by OAuth (see action item 1)
- ✅ **D8** — README Phase 1 badge (`59b28e6`)
- ✅ **D9** — STATUS.md updates (`59b28e6`, `8d166c5`, today)

### Subcontracts
- ✅ **SUB-001** — `internal/auth/{jwt.go, handler.go, router.go, jwt_test.go, handler_test.go}` + `auth.Mount` wired into `main.go`. `auth.Mount` reads `cfg.JWTSecret` + `cfg.AccessTokenTTL` from `internal/config.Load()` (`78f5263`).
- ✅ **SUB-ANDROID** — full Android project at `android/` with `ConnectActivity` + `WsClient.kt` + `WsEvent.kt` + Material 3 theme + adaptive launcher icon. `WsClientTest.kt` covers 4 pure-JVM parse-frame cases. The gradle wrapper jar is gitignored and must be regenerated on first checkout via `gradle wrapper --gradle-version 8.9` (documented in `gradlew`) (`d3ffad8`).
- ✅ **SUB-003** — `internal/ws/{protocol.go, upgrader.go, first_frame.go, router.go, upgrader_test.go}` + `ws.Mount` wired into `main.go`. Close code 1008 enforced via `WriteControl(FormatCloseMessage(1008))` then `Close()` (gorilla v1.5.x's `Close()` takes no args) (`821d090`, `203829f`).

### Concurrent contributions reconciled
- ✅ `internal/config` env loader (`c729a33`, fireside-bot) — main.go routes through `config.Load()`
- ✅ ADR-0007 amendment, ADR-0014, handoff scope notes (`1078a1e`, this session)

## What's next

- Android emulator smoke (owner action item 2)
- Sprint 1 kickoff:
  - Replace `deriveStubUserID` with real `GetUserByPhone` lookup against `internal/store/`
  - Add `InsertToken` call to persist jti for replay defense (ADR-0007 §Risks)
  - First ULID migration per ADR-0014
  - Wire `internal/store` into `main.go` (open *sql.DB, pass to queries, dependency-inject into auth)

## Open invitations

- 👀 **Read the docs**, challenge the ADRs
- 🧪 **Tell us about your chat-with-agents setup**
- 🌍 **Translate** — docs are English-first; Chinese, Japanese, etc. welcome