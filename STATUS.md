# Status

> **Phase 1 — Sprint 0 complete. Server-side + Android emulator smoke verified end-to-end.**

Last updated: 2026-07-29

## Where we are

Sprint 0 hello-world is fully verified. A running backend + Android emulator + token-into-app produced:

```
✅ connected — user_id=5427855269522273120
jti=d309b7f4-a9c2-4b27-81f4-6a75e45eb411
```

Backend log confirmed the matching `ws authenticated` call from `OnAuthenticated(uid, jti, conn)`. This is the complete ADR-0007 first-frame handshake working end-to-end: POST /v1/auth/login → JWT → Android paste → WS auth.hello → SUB-003 server validation → auth.welcome → app UI render.

## What's done (since last status)

### DeepTutor 借鉴(ADR-0015 → 0018,2026-07-31)
- ✅ **ADR-0015** — Agent 结构化澄清:`agent.question`/`agent.answer` 帧,agent 一轮内挂起等异步答案,`question_timeout` 超时 + 级联取消。补齐「缺信息时怎么办」闭环。
- ✅ **ADR-0016** — 三层可审计记忆(L1 trace / L2 facts / L3 profile,增补 ADR-0002):每条 agent 结论可溯源 L3→L2→L1,契合 D28。
- ✅ **ADR-0017** — 进度叙述 + 工具线索(增补 ADR-0009):`agent.progress` 折叠步骤,`SendProgress`/`SendToolHints` 房间开关。
- ✅ **ADR-0018** — Workspace 合并输出逐处(hunk)结构化 diff(增补 D23/D24):`MergeDiffSummary.Hunks`,摘要 agent 按 hunk 注解。
- 设计文档同步:`03-protocol.md`(新帧 + 路由表 + 错误码)、`01-data-model.md`(ContentType/记忆结构/MergeHunk)、`02-modules.md`(Driver 接口)、`04-state-machines.md`(awaiting_clarification 态)。

### Sprint 0 doc batch
- ✅ **ADR-0014** — `user_id` is `int64` in Sprint 0, migrates to ULID at Sprint 1 (`1078a1e`)
- ✅ **ADR-0007 (amendment)** — WS close code on auth failure is RFC 6455 `1008`, not `4001` (`1078a1e`)
- ✅ **Sprint 0 scope note** — handoff README documents the 3 intentional divergences from `docs/design/03-protocol.md`
- ✅ **SUB-003 close-code clarification** — explicit citation of `1008` in test annotations

### Owner prep (D1–D9)
- ✅ **D1+D2** — Gin server + `/healthz` + `go.mod` (`d164e74`)
- ✅ **D3+D4** — `db/migrations/0001_init.sql` + `db/queries/auth.sql` + sqlc-equivalent `internal/store/` (`8fca4fd`)
- ✅ **D5+D6** — folded into the doc commit (`1078a1e`)
- ✅ **D7** — `.golangci.yml` Sprint 0 baseline (`2297614`); CI step uncommented locally, push blocked by OAuth (see action item 1)
- ✅ **D8** — README Phase 1 badge (`59b28e6`)
- ✅ **D9** — STATUS.md updates (`59b28e6`)

### Subcontracts
- ✅ **SUB-001** — `internal/auth/{jwt.go, handler.go, router.go, jwt_test.go, handler_test.go}` + `auth.Mount` wired into `main.go`. `cfg.JWTSecret`/`cfg.AccessTokenTTL` from `internal/config.Load()` (`78f5263`).
- ✅ **SUB-ANDROID** — full Android project at `android/` with `ConnectActivity` + `WsClient.kt` + `WsEvent.kt` + Material 3 theme + adaptive launcher icon. `WsClientTest.kt` covers 4 pure-JVM parse-frame cases (`d3ffad8`).
- ✅ **SUB-ANDROID fix (real smoke)** — `ConnectActivity` now actually uses the WS URL text field instead of a hardcoded `ws://10.0.2.2:8080` (`2b302eb`).
- ✅ **SUB-ANDROID build fix** — `Theme.Material3.DayNight.NoActionBar` → `Theme.DeviceDefault.Light.NoActionBar` (Material3 parent isn't a framework resource). Added `values-night/themes.xml` dark variant (`2b302eb`).
- ✅ **SUB-003** — `internal/ws/{protocol.go, upgrader.go, first_frame.go, router.go, upgrader_test.go}` + `ws.Mount` wired into `main.go`. Close code 1008 enforced via `WriteControl(FormatCloseMessage(1008))` then `Close()` (`821d090`, `203829f`).

### Verification (all green)
- ✅ `go build ./...` clean
- ✅ `go test -race ./...` clean — auth (7 tests) + ws (4 tests)
- ✅ `golangci-lint run ./...` 0 issues
- ✅ Integration gate from `docs/handoff/sprint0/README.md` §"Acceptance gate":
  - `curl /healthz` → 200
  - POST `/v1/auth/login` happy → 200 `{token, expires_in:900}`
  - POST `/v1/auth/login` wrong-code → 401 `invalid_credentials`
  - POST `/v1/auth/login` bad-phone → 400 `invalid_request`
  - WS `auth.hello` → `auth.welcome{user_id, jti, server_time}`
  - WS hello with garbage → `auth.error{code:invalid_token}` + close 1008
  - WS no hello within 5s → `auth.error{code:hello_timeout}` + close 1008
  - **Android emulator: app shows "✅ connected" matching the backend's `ws authenticated` log line**

### Concurrent contributions reconciled
- ✅ `internal/config` env loader (`c729a33`, fireside-bot) — main.go routes through `config.Load()`
- ✅ ADR-0007 amendment, ADR-0014, handoff scope notes (`1078a1e`)
- ✅ Real `gradlew` scripts replacing stub + `os.Exit` in TestMain + `SetReadDeadline` errcheck (`22f5289`)

## What's deferred

- ~~**CI lint enforcement**~~ — ✅ done: `golangci-lint-action@v6` uncommented in `.github/workflows/ci.yml`; `go mod tidy` now verified via `git diff --exit-code`; migrate steps invoke the binary directly (CI has no `.env`). Push still blocked by GitHub OAuth scope — owner must push via PAT (workflow scope) or via GitHub web UI.

## What's next (Sprint 1 kickoff)

The Sprint 0 wiring exercise is done. Phase 2 starts when the owner gives the "go" signal. Sprint 1 backlog:

- Replace `deriveStubUserID` with real `GetUserByPhone` lookup against `internal/store/`
- Add `InsertToken` to persist jti for replay defense (ADR-0007 §Risks)
- First ULID migration per ADR-0014 (regenerate users table with CHAR(26))
- Wire `internal/store` into `main.go` (open *sql.DB, construct queries, DI into auth.LoginHandler)

## Open invitations

- 👀 **Read the docs**, challenge the ADRs
- 🧪 **Tell us about your chat-with-agents setup**
- 🌍 **Translate** — docs are English-first; Chinese, Japanese, etc. welcome