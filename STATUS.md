# Status

> **Phase 1 — Sprint 1 in progress (minimal chat-room demo, no Agent).**
> RFC: [`docs/rfc/phase-2-minimal-demo.md`](rfc/phase-2-minimal-demo.md)
> Milestone: [`Sprint 1: Minimal Demo`](https://github.com/ishadowland/fireside/milestone/1) (issues #2–#12)
> Sprint 1.5 (deferred Android): tracked separately under WP-9 (issue #11).

Last updated: 2026-08-02

## Where we are

Sprint 0 hello-world is fully verified. A running backend + Android emulator + token-into-app produced:

```
✅ connected — user_id=5427855269522273120
jti=d309b7f4-a9c2-4b27-81f4-6a75e45eb411
```

Backend log confirmed the matching `ws authenticated` call from `OnAuthenticated(uid, jti, conn)`. This is the complete ADR-0007 first-frame handshake working end-to-end: POST /v1/auth/login → JWT → Android paste → WS auth.hello → SUB-003 server validation → auth.welcome → app UI render.

## What's done (since last status)

### CI 工作流修复(2026-08-01)
- ✅ **CI 全绿** —— 首次通过所有检查(sqlc verify / migrations up / migrations down / go mod tidy / go build / go test / golangci-lint),GitHub Actions run 30741820638。
- ✅ **Migrations 命名规范** —— `0001_init.sql` → `0001_init.up.sql` + `0001_init.down.sql`(golang-migrate v4 iofs regex `^([0-9]+)_(.*)\.(up|down)\.(.*)$`)。原来的 plain `0001_init.sql` v4 完全不识别 → `first .: file does not exist`。
- ✅ **golangci-lint v2 升级** —— `golangci/golangci-lint-action@v6` 拒绝 v2 linter(`v2 is not supported by v6, you must update to v7`),且 v6 `latest` 解析到 v1.64.8。升 v7 + pin `version: v2.0.0`。
- ✅ **.golangci.yml v2 schema** —— `linters.disable-all`→`linters.default: none`;`linters-settings`→`linters.settings`;`issues.exclude-rules` 在 v2 schema 完全移除(改用 `//nolint` 内联或加 directive);`check-blank: true` → `false` 允许 `defer _ = x.Close()` 模式。
- 引用:`GitHub Actions run 30741820638` —— 之前所有 CI run 自 2026-07-27 起均失败。

### Sprint 1-3: ULID 迁移(ADR-0014,2026-08-01)
- ✅ **`db/migrations/0002_users_ulid.sql`** —— DROP + CREATE `users(id CHAR(26))` 与 `auth_tokens(user_id CHAR(26) FK)`,按 ADR 的「effectively DROP TABLE」路径。
- ✅ **Go 层 string 贯穿** —— `store.User.ID` / `auth_tokens.user_id` / `auth.Claims.UserID` / `ws.AuthWelcome.UserID` / `OnAuthenticated` 全部改 `string`,由 oklog/ulid/v2 生成。
- ✅ **删 `deriveStubUserID`** —— 新增 `auth.newULID()` = `ulid.Make().String()`;re-login 仍确定性(phone UNIQUE 索引 → 同 user → 同 ULID)。
- ✅ **Wire 协议变更** —— `auth.welcome.user_id` 由 JSON 数字改字符串;`docs/api/openapi.yaml` 同步;Android `WsEvent.Welcome.userId: String`、`WsClient.optString("user_id")`、测试断言改字符串。
- ✅ **测试** —— 新增 `isULID` 26-char Crockford 正则;`TestLoginHandlerHappyPath` 改为断言 ULID 格式;ws 端 `wantUID` 常量、store fake 改 string。
- ✅ **ADR-0014 增补** —— 在文件末尾追加「Sprint 1-3 executed」段落,记录 schema/类型选择/wire/移除 fnv 的决策。
- ✅ **构建** —— Go `go build` ✓ test -race ✓ lint 0 issues ✓;Android `./gradlew assembleDebug test` 通过。

### Sprint 1-2: jti replay defense(2026-07-31)
- ✅ **`InsertToken` 在 login 时持久化 jti** —— `auth.LoginHandler` 签发 JWT 后写入 `auth_tokens(jti, user_id, expires_at)`;持久化失败 → 500(不签发无追踪 token)。
- ✅ **`GetTokenByJTI` 在 WS first-frame 校验 jti** —— `ws.HandleConnect` JWT 验签通过后查 `auth_tokens`,`sql.ErrNoRows` → 写 `auth.error(invalid_token)` + close 1008。语义对标 ADR-0007 §Risks「tracks recently-seen jti」:只接受 login 真签发的 token,清理过期后可自然失效。
- ✅ **`DeleteExpiredTokens` 后台清理** —— `cmd/fireside` 启动 goroutine,每 5 分钟扫一次过期 jti 行,日志报告删除条数。
- ✅ **接口** —— 新增 `auth.TokenStore`(`InsertToken`)和 `ws.TokenLookup`(`GetTokenByJTI`);两者都由 `*store.Queries` 直接满足,`main.go` 同一 store 实例传给两端。
- ✅ **测试** —— `fakeStore` 扩展 token 字段;`TestLoginHandlerPersistsJTI` 验证登录入库、`TestLoginHandlerNilTokensKeepsLegacyBehavior` 验证 nil Tokens 兼容旧路径;`ws` 侧 `TestHandleConnectReplayDefenseRejected`(未知 jti 拒绝)+ `TestHandleConnectReplayDefenseAccepted`(已持久化 jti 通过)。
- ✅ **单库 SQL** —— `db/queries/auth.sql` 加 `GetTokenByJTI`;`internal/store/{auth.sql,querier}.go` 手动同步生成代码(sqlc v2 CLI 当前不可用,保持本仓原 sqlc-equivalent 风格)。

### Sprint 1-1: 真实用户查找(2026-07-31)
- ✅ **`auth.UserStore` 接口 + LoginHandler 接入 store** — 登录改为 `GetUserByPhone` 查 `users` 表;未知手机号自动注册(`InsertUser`,stub 合约不变,直到真实短信接入)。
- ✅ **`main.go` wire `internal/store`** — `sql.Open("pgx", POSTGRES_DSN)` + `store.New(db)` DI 进 `auth.Config.Users`;DB 不可达时服务照常启动(healthz/dashboard 可用),login 返回 500 直至 PG 就绪。
- ✅ **依赖** — `github.com/jackc/pgx/v5/stdlib`(database/sql driver)。
- ✅ **测试** — `fakeStore` 内存实现;新增 `TestLoginHandlerAutoRegisters`(首登入库、二登复用);既有 7 测试全部转 store 路径。
- ✅ **`docs/api/openapi.yaml`** — 补齐 RFC 硬性退出条件缺口:REST(/healthz、login、dashboard)+ WS(auth.hello/welcome/error 帧契约)。

### 本地测试 Dashboard(ADR-0019,2026-07-31)
- ✅ **ADR-0019** — 本地测试 Dashboard:loopback-only、自动 stub 登录、零前端构建链(go:embed)。
- ✅ **`internal/dashboard`** — 挂载 `/dashboard/`(回环 IP 限流)+ `/v1/dashboard/config`(下发 stub code)。
- ✅ **免登录流程**:页面加载 → 自动 `GET /v1/dashboard/config` → `POST /v1/auth/login` → 展示 JWT → 点「Connect & Hello」→ `auth.hello` → `auth.welcome`。浏览器打开 `http://localhost:18080/dashboard/` 即可自测,无需 Android 模拟器。
- ✅ 验证:单元测试(回环/远端 200/404、config 内容)+ 全链路 smoke(config → login → WS welcome 成功)。
- 设计文档同步:`02-modules.md`(internal/dashboard 模块)。

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
- ✅ **SUB-ANDROID fix (real smoke)** — `ConnectActivity` now actually uses the WS URL text field instead of a hardcoded `ws://10.0.2.2:18080` (`2b302eb`).
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

## What's next (Sprint 1 plan + Sprint 2 kickoff)

Sprint 1 规划已完成(2026-08-02),see [`docs/rfc/phase-2-minimal-demo.md`](rfc/phase-2-minimal-demo.md) for full WBS, decisions, and acceptance gate. Issue tracker: milestone [`Sprint 1: Minimal Demo`](https://github.com/ishadowland/fireside/milestone/1) (issues #2–#12).

**Sprint 1 已完成项目**(per remote `main`):
- ✅ Sprint 1-1: real user lookup + openapi.yaml (commit `6f5db01`)
- ✅ Sprint 1-2: jti replay defense via `InsertToken` (commit `dbe0a82`) — **supersedes** RFC deviation note
- ✅ Sprint 1-3: ULID migration per ADR-0014 (commit `0739943`) — **supersedes** RFC deviation note
- ✅ Issue #1: `TestValidateTampered` fixed by tampering payload not signature (commit `d738757`)
- ✅ CI 全绿 (commit `2c41877`): golangci-lint v2 schema + golangci-lint-action v7 + migrate v4 iofs naming + TestValidateMissingExp

**Sprint 1 deviations from existing ADRs / design docs (RFC §2.3)** — most are now **obsolete** due to remote commits above. RFC needs reconciliation:
- ~~D3 max 50 → max 8 in Sprint 1~~ — **still valid**, evaluate in Sprint 2
- ~~D6 ephemeral (room end clears messages) → not implemented~~ — **still valid**, evaluate in Sprint 2
- ~~ADR-0014 ULID → deferred to Sprint 2~~ — **DONE** in Sprint 1-3
- ~~ADR-0007 §Risks Replay → not called~~ — **DONE** in Sprint 1-2

**Remaining Sprint 1 work**(per RFC §4, not yet implemented in remote `main`):
- WP-1 rooms / participants / messages migrations
- WP-2/3/4 internal/rooms / internal/messages / internal/packages
- WP-5 hub (CORE — critical path)
- WP-6 WS business frames (msg.* / room.*)
- WP-7 REST endpoints (rooms CRUD-lite + auth/refresh + display_name)
- WP-8 Dashboard UI (room list + chat)

**Sprint 1.5 / Sprint 2 backlog** (post Sprint 1):
- WP-9 Android UI (issue #11) — Sprint 1.5
- Decide: restore D3 (max 50) or keep D3-modified (max 8)
- Decide: implement D6 (room end + message clear)
- Real DB integration smoke test (owner action — needs Postgres locally)
- Sprint 2 路线图:房间/消息/agent 框架(参见 docs/rfc/phase-1-mvp.md 后续阶段 + ADR-0015..0018 待实现)

## Open invitations

- 👀 **Read the docs**, challenge the ADRs
- 🧪 **Tell us about your chat-with-agents setup**
- 🌍 **Translate** — docs are English-first; Chinese, Japanese, etc. welcome